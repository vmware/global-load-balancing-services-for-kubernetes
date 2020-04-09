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
	"amko/gslb/gslbutils"
	gdpv1alpha1 "amko/pkg/apis/avilb/v1alpha1"
	"sync"

	extensionv1beta1 "k8s.io/api/extensions/v1beta1"

	"github.com/gobwas/glob"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	"k8s.io/api/networking/v1beta1"
)

var ihMapInit sync.Once
var ihMap ObjHostMap

func getIngHostMap() *ObjHostMap {
	ihMapInit.Do(func() {
		ihMap.HostMap = make(map[string]IPHostname)
	})
	return &rhMap
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

func GetExtensionV1IngressHostMeta(ingress *extensionv1beta1.Ingress, cname string) []IngressHostMeta {
	ingHostMetaList := []IngressHostMeta{}
	hostIPList := gslbutils.ExtensionV1IngressGetIPAddrs(ingress)
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

func (ing IngressHostMeta) SanityCheck(mr gdpv1alpha1.MatchRule) bool {
	if len(mr.Hosts) == 0 && mr.Label.Key == "" {
		gslbutils.Errf("object: GDPRule, ingress: %s, msg: %s", ing.ObjName,
			"GDPRule doesn't have either hosts set or label key-value pair")
		return false
	}
	if len(mr.Hosts) > 0 && ing.Hostname == "" {
		return false
	}
	return true
}

func (ing IngressHostMeta) GlobOperate(mr gdpv1alpha1.MatchRule) bool {
	var g glob.Glob
	// ingressHost's hostname has to match
	// If no hostname given, return false
	for _, host := range mr.Hosts {
		g = glob.MustCompile(host.HostName, '.')
		if g.Match(ing.Hostname) {
			return true
		}
	}
	return false
}

func (ing IngressHostMeta) EqualOperate(mr gdpv1alpha1.MatchRule) bool {
	if len(mr.Hosts) != 0 {
		// Host list is of non-zero length, which means has to be a host match expression
		for _, h := range mr.Hosts {
			if h.HostName == ing.Hostname {
				return true
			}
		}
	} else {
		// Its a label key-value match
		ingLabels := ing.Labels
		if value, ok := ingLabels[mr.Label.Key]; ok {
			if value == mr.Label.Value {
				return true
			}
		}
	}
	return false
}

func (ing IngressHostMeta) NotEqualOperate(mr gdpv1alpha1.MatchRule) bool {
	if len(mr.Hosts) != 0 {
		// Host list is of non-zero length, which means it has to be a host match expression
		for _, h := range mr.Hosts {
			if h.HostName == ing.Hostname {
				return false
			}
		}
		// Match not found for host, return true
		return true
	}
	// Its a label key-value match
	ingLabels := ing.Labels
	if value, ok := ingLabels[mr.Label.Key]; ok {
		if value == mr.Label.Value {
			return false
		}
	}
	return true
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
