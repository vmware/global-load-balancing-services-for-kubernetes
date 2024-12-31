/*
 * Copyright 2019-2020 VMware, Inc.
 * All Rights Reserved.
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*   http://www.apache.org/licenses/LICENSE-2.0
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*/

package k8sobjects

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	gdpv1alpha2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

var ihMapInit sync.Once
var ihMap ObjHostMap

func getIngHostMap() *ObjHostMap {
	ihMapInit.Do(func() {
		ihMap.HostMap = make(map[string]IPHostname)
	})
	return &ihMap
}

func getPathsForHost(host string, ingress *networkingv1.Ingress) []string {
	pathList := []string{}
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != host {
			continue
		}
		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				var pathKey string
				if path.Path != "" {
					pathKey = path.Path
				} else {
					pathKey = "/"
				}
				if gslbutils.PresentInList(pathKey, pathList) {
					continue
				}
				pathList = append(pathList, pathKey)
			}
		}
		if rule.Host == host {
			break
		}
	}

	// if nothing in the pathList, always add "/"
	if len(pathList) == 0 {
		pathList = append(pathList, "/")
	}
	return pathList
}

func getTLSHosts(ingress *networkingv1.Ingress) []string {
	tlsHosts := []string{}

	if gslbutils.IsDefaultSecretEnabled(ingress.GetAnnotations()) {
		for _, rule := range ingress.Spec.Rules {
			if gslbutils.PresentInList(rule.Host, tlsHosts) {
				continue
			}
			tlsHosts = append(tlsHosts, rule.Host)
		}
		return tlsHosts
	}

	for _, hosts := range ingress.Spec.TLS {
		for _, host := range hosts.Hosts {
			if gslbutils.PresentInList(host, tlsHosts) {
				continue
			}
			tlsHosts = append(tlsHosts, host)
		}
	}
	return tlsHosts
}

func parseVSAndControllerAnnotations(annotations map[string]string) (map[string]string, string, string, error) {
	vsUUIDs, controllerUUID := make(map[string]string), ""
	tenant, exists := annotations[gslbutils.TenantAnnotation]
	if !exists {
		gslbutils.Debugf("No tenant annotation exist for object, annotations: %v", annotations)
		tenant = gslbutils.GetTenant()
	}
	if len(annotations) == 0 {
		return vsUUIDs, controllerUUID, tenant, fmt.Errorf("empty annotations")
	}
	vsAnnotations, exists := annotations[gslbutils.VSAnnotation]
	if !exists {
		gslbutils.Debugf("No VS Annotations exist for object, annotations: %v", annotations)
		return vsUUIDs, controllerUUID, tenant, fmt.Errorf("no VS UUID annotations for this object: %v", annotations)
	}
	controllerUUID, exists = annotations[gslbutils.ControllerAnnotation]
	if !exists {
		gslbutils.Debugf("No Controller Annotation exist for object, annotations: %v", annotations)
		return vsUUIDs, controllerUUID, tenant, fmt.Errorf("no Controller UUID annotation for this object: %v", annotations)
	}
	if err := json.Unmarshal([]byte(vsAnnotations), &vsUUIDs); err != nil {
		return vsUUIDs, controllerUUID, tenant, fmt.Errorf("error in unmarshalling VS annotations: %v", err)
	}
	return vsUUIDs, controllerUUID, tenant, nil
}

// GetIngressHostMeta returns a ingress split into its backends
func GetIngressHostMeta(ingress *networkingv1.Ingress, cname string) []IngressHostMeta {
	ingHostMetaList := []IngressHostMeta{}
	hostIPList := gslbutils.IngressGetIPAddrs(ingress)
	tlsHosts := getTLSHosts(ingress)

	gf := gslbutils.GetGlobalFilter()

	// we don't return because of errors here, as we need these objects in the our internal cache,
	// so that, when the GDP object gets changed, we can re-apply these objects back again.
	// The errors for syncVIPsOnly are taken care of in the graph layer.
	syncVIPsOnly, err := gf.IsClusterSyncVIPOnly(cname)
	if err != nil {
		gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: skipping ingress because of error: %v",
			cname, ingress.Namespace, ingress.Name, err)
	}
	var vsUUIDs map[string]string
	var controllerUUID, tenant string

	vsUUIDs, controllerUUID, tenant, err = parseVSAndControllerAnnotations(ingress.Annotations)
	if err != nil && !syncVIPsOnly {
		// Note that the ingress key will still be published to graph layer, but the key
		// won't be processed, this is just to maintain the ingress information as part
		// of in-memory map.
		gslbutils.Logf("ns: %s, ingress: %s, msg: skipping ingress because of error: %v",
			ingress.Namespace, ingress.Name, err)
	}
	if (controllerUUID == "" || len(vsUUIDs) == 0) && !syncVIPsOnly {
		gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: skipping ingress because controller UUID absent in annotations",
			cname, ingress.Namespace, ingress.Name)
	}
	passThroughEnabled := false
	if gslbutils.GetCustomFqdnMode() {
		if val, found := ingress.Annotations[gslbutils.PassthroughAnnotation]; found {
			passThroughEnabled = strings.EqualFold(val, "true")
		}
	}
	for _, hip := range hostIPList {
		vsUUID, ok := vsUUIDs[hip.Hostname]
		if !ok && !syncVIPsOnly {
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: hostname %s missing from VS UUID annotations",
				cname, ingress.Namespace, ingress.Name, hip.Hostname)
		}
		metaObj := IngressHostMeta{
			IngName:            ingress.Name,
			Namespace:          ingress.ObjectMeta.Namespace,
			Hostname:           hip.Hostname,
			IPAddr:             hip.IPAddr,
			Cluster:            cname,
			ObjName:            ingress.Name + "/" + hip.Hostname,
			TLS:                false,
			VirtualServiceUUID: vsUUID,
			ControllerUUID:     controllerUUID,
			Tenant:             tenant,
			Passthrough:        passThroughEnabled,
		}
		metaObj.Paths = make([]string, 0)
		metaObj.Labels = make(map[string]string)
		for key, value := range ingress.GetLabels() {
			metaObj.Labels[key] = value
		}
		metaObj.Paths = getPathsForHost(hip.Hostname, ingress)

		if gslbutils.PresentInList(hip.Hostname, tlsHosts) {
			metaObj.TLS = true
		}
		ingHostMetaList = append(ingHostMetaList, metaObj)
	}

	return ingHostMetaList
}

// IngressHostMeta is the metadata for an ingress. It is the minimal information
// that we maintain for each ingress, accepted or rejected.
type IngressHostMeta struct {
	Cluster            string
	IngName            string
	ObjName            string
	Namespace          string
	Hostname           string
	IPAddr             string
	VirtualServiceUUID string
	ControllerUUID     string
	Labels             map[string]string
	Paths              []string
	TLS                bool
	Tenant             string
	Passthrough        bool
}

func (ing IngressHostMeta) GetType() string {
	return gdpv1alpha2.IngressObj
}

func (ing IngressHostMeta) GetName() string {
	return ing.ObjName
}

func (ing IngressHostMeta) GetNamespace() string {
	return ing.Namespace
}

func (ing IngressHostMeta) GetIngressHostMetaKey() string {
	return ing.IngName + "/" + ing.Hostname
}

func (ing IngressHostMeta) GetClusterKey() string {
	return ing.Cluster + "/" + ing.Namespace + "/" + ing.GetIngressHostMetaKey()
}

func (ing IngressHostMeta) GetCluster() string {
	return ing.Cluster
}

func (ing IngressHostMeta) GetHostname() string {
	return ing.Hostname
}

func (ing IngressHostMeta) GetIPAddr() string {
	return ing.IPAddr
}

func (ing IngressHostMeta) GetPort() (int32, error) {
	return 0, errors.New("ingress object doesn't support GetPort function")
}

func (ing IngressHostMeta) GetProtocol() (string, error) {
	return "", errors.New("ingress object doesn't support GetProtocol function")
}

func (ing IngressHostMeta) GetPaths() ([]string, error) {
	pathList := []string{}
	if len(ing.Paths) == 0 {
		return pathList, errors.New("no paths for this ingress " + ing.ObjName)
	}
	copy(pathList, ing.Paths)
	return ing.Paths, nil
}

func (ing IngressHostMeta) GetTLS() (bool, error) {
	return ing.TLS, nil
}

func (ing IngressHostMeta) IsPassthrough() bool {
	return ing.Passthrough
}

func (ing IngressHostMeta) GetVirtualServiceUUID() string {
	return ing.VirtualServiceUUID
}

func (ing IngressHostMeta) GetControllerUUID() string {
	return ing.ControllerUUID
}

func (ing IngressHostMeta) GetTenant() string {
	return ing.Tenant
}

func (ing IngressHostMeta) IngressHostInList(ihmList []IngressHostMeta) (IngressHostMeta, bool) {
	var ihm IngressHostMeta
	for _, ihm = range ihmList {
		if ing.Hostname == ihm.Hostname {
			return ihm, true
		}
	}
	return ihm, false
}

func (ing IngressHostMeta) GetIngressHostCksum() uint32 {
	var cksum uint32
	for lblKey, lblValue := range ing.Labels {
		cksum += utils.Hash(lblKey) + utils.Hash(lblValue)
	}
	paths := ing.Paths
	sort.Strings(paths)
	// TODO: annotations will be checked in later
	cksum += utils.Hash(ing.Cluster) + utils.Hash(ing.Namespace) +
		utils.Hash(ing.IngName) + utils.Hash(ing.Hostname) +
		utils.Hash(ing.IPAddr) + utils.Hash(utils.Stringify(paths)) +
		utils.Hash(ing.VirtualServiceUUID) + utils.Hash(ing.ControllerUUID) + utils.Hash(ing.Tenant) + utils.Hash(utils.Stringify(ing.Passthrough))
	return cksum
}

func (ing IngressHostMeta) UpdateHostMap(key string) {
	rhm := getIngHostMap()
	rhm.Lock.Lock()
	defer rhm.Lock.Unlock()
	rhm.HostMap[key] = IPHostname{
		IP:       ing.IPAddr,
		Hostname: ing.Hostname,
	}
}

func (ing IngressHostMeta) GetHostnameFromHostMap(key string) string {
	ihm := getIngHostMap()
	ihm.Lock.Lock()
	defer ihm.Lock.Unlock()
	ipHostname, ok := ihm.HostMap[key]
	if !ok {
		return ""
	}
	return ipHostname.Hostname
}

func (ing IngressHostMeta) DeleteMapByKey(key string) {
	ihm := getIngHostMap()
	ihm.Lock.Lock()
	defer ihm.Lock.Unlock()
	delete(ihm.HostMap, key)
}

func (ihm IngressHostMeta) ApplyFilter() bool {
	fqdnMap := gslbutils.GetFqdnMap()

	selectedByGDP := ihm.ApplyGDPSelector()
	if selectedByGDP {
		if gslbutils.GetCustomFqdnMode() {
			if ihm.IsPassthrough() {
				gslbutils.Logf("cluster: %s, ns: %s, ingress host: %s, msg: passthrough ingress not supported in customfqdn mode",
					ihm.Cluster, ihm.Namespace, ihm.Hostname)
				return false
			}
			_, err := fqdnMap.GetGlobalFqdnForLocalFqdn(ihm.Cluster, ihm.Hostname)
			if err != nil {
				gslbutils.Debugf("cluster: %s, ns: %s, ingress host: %s, msg: error in fetching global fqdn: %v",
					ihm.Cluster, ihm.Namespace, ihm.Hostname, err)
				return false
			}
			return true
		}
	}

	return selectedByGDP
}

func (ihm IngressHostMeta) ApplyGDPSelector() bool {
	gf := gslbutils.GetGlobalFilter()
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	if !gslbutils.ClusterContextPresentInList(ihm.Cluster, gf.ApplicableClusters) {
		gslbutils.Logf("objType: Ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because cluster is not selected",
			ihm.Cluster, ihm.Namespace, ihm.ObjName)
		return false
	}
	nsFilter := gf.NSFilter
	// will check the namespaces first, whether the namespace for ihm is selected
	if nsFilter != nil {
		nsFilter.Lock.RLock()
		defer nsFilter.Lock.RUnlock()
		nsList, ok := gf.NSFilter.SelectedNS[ihm.Cluster]
		if !ok {
			gslbutils.Logf("objType: Ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because of namespaceSelector",
				ihm.Cluster, ihm.Namespace, ihm.ObjName)
			return false
		}
		if gslbutils.PresentInList(ihm.Namespace, nsList) {
			appFilter := gf.AppFilter
			if appFilter == nil {
				gslbutils.Logf("objType: ingress, cluster: %s, namespace: %s, name: %s, msg: accepted because of namespaceSelector",
					ihm.Cluster, ihm.Namespace, ihm.ObjName)
				return true
			}
			// Check the appFilter now for this object
			if applyAppFilter(ihm.Labels, appFilter) {
				gslbutils.Logf("objType: ingress, cluster: %s, namespace: %s, name: %s, msg: accepted because of namespaceSelector and appSelector",
					ihm.Cluster, ihm.Namespace, ihm.ObjName)
				return true
			}
			gslbutils.Logf("objType: ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because of appSelector",
				ihm.Cluster, ihm.Namespace, ihm.ObjName)
			return false
		}
		// this means that the namespace is not selected in the filter
		gslbutils.Logf("objType: ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because namespace is not selected",
			ihm.Cluster, ihm.Namespace, ihm.ObjName)
		return false
	}
	// check for app filter
	if gf.AppFilter == nil {
		gslbutils.Logf("objType: ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because no appSelector",
			ihm.Cluster, ihm.Namespace, ihm.ObjName)
		return false
	}
	if !applyAppFilter(ihm.Labels, gf.AppFilter) {
		gslbutils.Logf("objType: ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because of appSelector",
			ihm.Cluster, ihm.Namespace, ihm.ObjName)
		return false
	}
	gslbutils.Logf("objType: ingress, cluster: %s, namespace: %s, name: %s, msg: accepted because of appSelector",
		ihm.Cluster, ihm.Namespace, ihm.ObjName)

	return true
}

func applyAppFilter(ihmLabels map[string]string, appFilter *gslbutils.AppFilter) bool {
	for k, v := range ihmLabels {
		if k == appFilter.Key && v == appFilter.Value {
			return true
		}
	}
	return false
}
