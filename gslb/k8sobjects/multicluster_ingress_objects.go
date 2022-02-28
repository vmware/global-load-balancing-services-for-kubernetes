/*
 * Copyright 2022-2023 VMware, Inc.
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
	"errors"
	"sort"
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	akov1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

var mcihMapInit sync.Once
var mcihMap ObjHostMap

func getMultiClusterIngressHostMap() *ObjHostMap {
	mcihMapInit.Do(func() {
		mcihMap.HostMap = make(map[string]IPHostname)
	})
	return &mcihMap
}

// GetHostMetaForMultiClusterIngress returns a multi-cluster ingress split into its backends
func GetHostMetaForMultiClusterIngress(mci *akov1alpha1.MultiClusterIngress, cname string) []MultiClusterIngressHostMeta {
	metaObjects := []MultiClusterIngressHostMeta{}
	// hostIPList := gslbutils.IngressGetIPAddrs(ingress)
	// TODO: move the below logic to utils
	var hostIPList []gslbutils.IngressHostIP
	for _, ing := range mci.Status.LoadBalancer.Ingress {
		ingHostIP := gslbutils.IngressHostIP{
			Hostname: ing.Hostname,
			IPAddr:   ing.IP,
		}
		hostIPList = append(hostIPList, ingHostIP)
	}

	tlsHosts := getMultiClusterIngressTLSHosts(mci)

	gf := gslbutils.GetGlobalFilter()

	// we don't return because of errors here, as we need these objects in the our internal cache,
	// so that, when the GDP object gets changed, we can re-apply these objects back again.
	// The errors for syncVIPsOnly are taken care of in the graph layer.
	syncVIPsOnly, err := gf.IsClusterSyncVIPOnly(cname)
	if err != nil {
		gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: skipping ingress because of error: %v",
			cname, mci.Namespace, mci.Name, err)
	}
	var vsUUIDs map[string]string
	var controllerUUID string

	vsUUIDs, controllerUUID, err = parseVSAndControllerAnnotations(mci.ObjectMeta.Annotations)
	if err != nil && !syncVIPsOnly {
		// Note that the ingress key will still be published to graph layer, but the key
		// won't be processed, this is just to maintain the ingress information as part
		// of in-memory map.
		gslbutils.Errf("ns: %s, ingress: %s, msg: skipping ingress because of error: %v",
			mci.Namespace, mci.Name, err)
	}
	if (controllerUUID == "" || len(vsUUIDs) == 0) && !syncVIPsOnly {
		gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: skipping ingress because controller UUID absent in annotations",
			cname, mci.Namespace, mci.Name)
	}
	for _, hip := range hostIPList {
		vsUUID, ok := vsUUIDs[hip.Hostname]
		if !ok && !syncVIPsOnly {
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: hostname %s missing from VS UUID annotations",
				cname, mci.Namespace, mci.Name, hip.Hostname)
		}
		metaObj := MultiClusterIngressHostMeta{
			IngName:            mci.Name,
			Namespace:          mci.ObjectMeta.Namespace,
			Hostname:           hip.Hostname,
			IPAddr:             hip.IPAddr,
			Cluster:            cname,
			ObjName:            mci.Name + "/" + hip.Hostname,
			TLS:                false,
			VirtualServiceUUID: vsUUID,
			ControllerUUID:     controllerUUID,
		}
		metaObj.Paths = make([]string, 0)
		metaObj.Labels = make(map[string]string)
		for key, value := range mci.GetLabels() {
			metaObj.Labels[key] = value
		}
		metaObj.Paths = getPathListForMultiClusterIngress(mci)

		if gslbutils.PresentInList(hip.Hostname, tlsHosts) {
			metaObj.TLS = true
		}
		metaObjects = append(metaObjects, metaObj)
	}

	return metaObjects
}

func getMultiClusterIngressTLSHosts(mci *akov1alpha1.MultiClusterIngress) []string {
	if mci.Spec.SecretName != "" {
		return []string{mci.Spec.Hostname}
	}
	return []string{}
}

func getPathListForMultiClusterIngress(mci *akov1alpha1.MultiClusterIngress) []string {
	pathList := []string{}
	for _, rule := range mci.Spec.Config {
		var pathKey string
		if rule.Path != "" {
			pathKey = rule.Path
		} else {
			pathKey = "/"
		}
		if gslbutils.PresentInList(pathKey, pathList) {
			continue
		}
		pathList = append(pathList, pathKey)
	}

	// if nothing in the pathList, always add "/"
	if len(pathList) == 0 {
		pathList = append(pathList, "/")
	}
	return pathList
}

// MultiClusterMultiClusterIngressHostMeta is the metadata for a multi-cluster ingress. It is the minimal information
// that we maintain for each multi-cluster ingress, accepted or rejected.
type MultiClusterIngressHostMeta struct {
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
}

func (mciHostMeta MultiClusterIngressHostMeta) GetType() string {
	return gslbutils.MCIType
}

func (mciHostMeta MultiClusterIngressHostMeta) GetName() string {
	return mciHostMeta.ObjName
}

func (mciHostMeta MultiClusterIngressHostMeta) GetNamespace() string {
	return mciHostMeta.Namespace
}

func (mciHostMeta MultiClusterIngressHostMeta) GetIngressHostMetaKey() string {
	return mciHostMeta.IngName + "/" + mciHostMeta.Hostname
}

func (mciHostMeta MultiClusterIngressHostMeta) GetClusterKey() string {
	return mciHostMeta.Cluster + "/" + mciHostMeta.Namespace + "/" + mciHostMeta.GetIngressHostMetaKey()
}

func (mciHostMeta MultiClusterIngressHostMeta) GetCluster() string {
	return mciHostMeta.Cluster
}

func (mciHostMeta MultiClusterIngressHostMeta) GetHostname() string {
	return mciHostMeta.Hostname
}

func (mciHostMeta MultiClusterIngressHostMeta) GetIPAddr() string {
	return mciHostMeta.IPAddr
}

func (mciHostMeta MultiClusterIngressHostMeta) GetPort() (int32, error) {
	return 0, errors.New("ingress object doesn't support GetPort function")
}

func (mciHostMeta MultiClusterIngressHostMeta) GetProtocol() (string, error) {
	return "", errors.New("ingress object doesn't support GetProtocol function")
}

func (mciHostMeta MultiClusterIngressHostMeta) GetPaths() ([]string, error) {
	pathList := []string{}
	if len(mciHostMeta.Paths) == 0 {
		return pathList, errors.New("no paths for this multi-cluster ingress " + mciHostMeta.ObjName)
	}
	copy(pathList, mciHostMeta.Paths)
	return mciHostMeta.Paths, nil
}

func (mciHostMeta MultiClusterIngressHostMeta) GetTLS() (bool, error) {
	return mciHostMeta.TLS, nil
}

func (mciHostMeta MultiClusterIngressHostMeta) IsPassthrough() bool {
	return false
}

func (mciHostMeta MultiClusterIngressHostMeta) GetVirtualServiceUUID() string {
	return mciHostMeta.VirtualServiceUUID
}

func (mciHostMeta MultiClusterIngressHostMeta) GetControllerUUID() string {
	return mciHostMeta.ControllerUUID
}

func (mciHostMeta MultiClusterIngressHostMeta) GetIngressHostCksum() uint32 {
	var cksum uint32
	for lblKey, lblValue := range mciHostMeta.Labels {
		cksum += utils.Hash(lblKey) + utils.Hash(lblValue)
	}
	paths := mciHostMeta.Paths
	sort.Strings(paths)
	// TODO: annotations will be checked in later
	cksum += utils.Hash(mciHostMeta.Cluster) + utils.Hash(mciHostMeta.Namespace) +
		utils.Hash(mciHostMeta.IngName) + utils.Hash(mciHostMeta.Hostname) +
		utils.Hash(mciHostMeta.IPAddr) + utils.Hash(utils.Stringify(paths)) +
		utils.Hash(mciHostMeta.VirtualServiceUUID) + utils.Hash(mciHostMeta.ControllerUUID)
	return cksum
}

func (mciHostMeta MultiClusterIngressHostMeta) UpdateHostMap(key string) {
	mcihm := getMultiClusterIngressHostMap()
	mcihm.Lock.Lock()
	defer mcihm.Lock.Unlock()
	mcihm.HostMap[key] = IPHostname{
		IP:       mciHostMeta.IPAddr,
		Hostname: mciHostMeta.Hostname,
	}
}

func (mciHostMeta MultiClusterIngressHostMeta) GetHostnameFromHostMap(key string) string {
	mcihm := getMultiClusterIngressHostMap()
	mcihm.Lock.Lock()
	defer mcihm.Lock.Unlock()
	ipHostname, ok := mcihm.HostMap[key]
	if !ok {
		return ""
	}
	return ipHostname.Hostname
}

func (mciHostMeta MultiClusterIngressHostMeta) DeleteMapByKey(key string) {
	mcihm := getMultiClusterIngressHostMap()
	mcihm.Lock.Lock()
	defer mcihm.Lock.Unlock()
	delete(mcihm.HostMap, key)
}

func (mciHostMeta MultiClusterIngressHostMeta) ApplyFilter() bool {
	fqdnMap := gslbutils.GetFqdnMap()

	selectedByGDP := mciHostMeta.ApplyGDPSelector()
	if selectedByGDP {
		if gslbutils.GetCustomFqdnMode() {
			_, err := fqdnMap.GetGlobalFqdnForLocalFqdn(mciHostMeta.Cluster, mciHostMeta.Hostname)
			if err != nil {
				gslbutils.Debugf("cluster: %s, ns: %s, ingress host: %s, msg: error in fetching global fqdn: %v",
					mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.Hostname, err)
				return false
			}
			return true
		}
	}

	return selectedByGDP
}

func (mciHostMeta MultiClusterIngressHostMeta) ApplyGDPSelector() bool {
	gf := gslbutils.GetGlobalFilter()
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	if !gslbutils.ClusterContextPresentInList(mciHostMeta.Cluster, gf.ApplicableClusters) {
		gslbutils.Logf("objType: Multi-cluster Ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because cluster is not selected",
			mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)
		return false
	}
	nsFilter := gf.NSFilter
	// will check the namespaces first, whether the namespace for ihm is selected
	if nsFilter != nil {
		nsFilter.Lock.RLock()
		defer nsFilter.Lock.RUnlock()
		nsList, ok := gf.NSFilter.SelectedNS[mciHostMeta.Cluster]
		if !ok {
			gslbutils.Logf("objType: Multi-cluster Ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because of namespaceSelector",
				mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)
			return false
		}
		if gslbutils.PresentInList(mciHostMeta.Namespace, nsList) {
			appFilter := gf.AppFilter
			if appFilter == nil {
				gslbutils.Logf("objType: Multi-cluster ingress, cluster: %s, namespace: %s, name: %s, msg: accepted because of namespaceSelector",
					mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)
				return true
			}
			// Check the appFilter now for this object
			if applyAppFilter(mciHostMeta.Labels, appFilter) {
				gslbutils.Logf("objType: Multi-cluster ingress, cluster: %s, namespace: %s, name: %s, msg: accepted because of namespaceSelector and appSelector",
					mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)
				return true
			}
			gslbutils.Logf("objType: Multi-cluster ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because of appSelector",
				mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)
			return false
		}
		// this means that the namespace is not selected in the filter
		gslbutils.Logf("objType: Multi-cluster ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because namespace is not selected",
			mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)
		return false
	}
	// check for app filter
	if gf.AppFilter == nil {
		gslbutils.Logf("objType: Multi-cluster ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because no appSelector",
			mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)
		return false
	}
	if !applyAppFilter(mciHostMeta.Labels, gf.AppFilter) {
		gslbutils.Logf("objType: Multi-cluster ingress, cluster: %s, namespace: %s, name: %s, msg: rejected because of appSelector",
			mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)
		return false
	}
	gslbutils.Logf("objType: Multi-cluster ingress, cluster: %s, namespace: %s, name: %s, msg: accepted because of appSelector",
		mciHostMeta.Cluster, mciHostMeta.Namespace, mciHostMeta.ObjName)

	return true
}

func (mciHostMeta MultiClusterIngressHostMeta) IngressHostInList(ihmList []MultiClusterIngressHostMeta) (MultiClusterIngressHostMeta, bool) {
	var mcihm MultiClusterIngressHostMeta
	for _, mcihm = range ihmList {
		if mciHostMeta.Hostname == mcihm.Hostname {
			return mcihm, true
		}
	}
	return mcihm, false
}
