/*
* [2013] - [2020] Avi Networks Incorporated
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
	"sync"

	"github.com/avinetworks/amko/gslb/gslbutils"
	gdpv1alpha1 "github.com/avinetworks/amko/internal/apis/amko/v1alpha1"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	"k8s.io/api/networking/v1beta1"
)

var ihMapInit sync.Once
var ihMap ObjHostMap

func getIngHostMap() *ObjHostMap {
	ihMapInit.Do(func() {
		ihMap.HostMap = make(map[string]IPHostname)
	})
	return &ihMap
}

// GetIngressHostMeta returns a ingress split into its backends
func GetIngressHostMeta(ingress *v1beta1.Ingress, cname string) []IngressHostMeta {
	ingHostMetaList := []IngressHostMeta{}
	hostIPList := gslbutils.IngressGetIPAddrs(ingress)
	for _, hip := range hostIPList {
		metaObj := IngressHostMeta{
			IngName:   ingress.Name,
			Namespace: ingress.ObjectMeta.Namespace,
			Hostname:  hip.Hostname,
			IPAddr:    hip.IPAddr,
			Cluster:   cname,
			ObjName:   ingress.Name + "/" + hip.Hostname,
		}
		metaObj.Labels = make(map[string]string)
		for key, value := range ingress.GetLabels() {
			metaObj.Labels[key] = value
		}

		ingHostMetaList = append(ingHostMetaList, metaObj)
	}
	return ingHostMetaList
}

// IngressHostMeta is the metadata for an ingress. It is the minimal information
// that we maintain for each ingress, accepted or rejected.
type IngressHostMeta struct {
	Cluster   string
	IngName   string
	ObjName   string
	Namespace string
	Hostname  string
	IPAddr    string
	Labels    map[string]string
}

var clusterHostMeta map[string]map[string]IngressHostMeta

func (ing IngressHostMeta) GetType() string {
	return gdpv1alpha1.IngressObj
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
	// TODO: annotations will be checked in later
	cksum += utils.Hash(ing.Cluster) + utils.Hash(ing.Namespace) +
		utils.Hash(ing.IngName) + utils.Hash(ing.Hostname) +
		utils.Hash(ing.IPAddr)
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
	gf := gslbutils.GetGlobalFilter()
	gf.GlobalLock.RLock()
	gf.GlobalLock.RUnlock()

	if !gslbutils.PresentInList(ihm.Cluster, gf.ApplicableClusters) {
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
