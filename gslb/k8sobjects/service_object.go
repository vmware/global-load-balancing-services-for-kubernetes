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

	corev1 "k8s.io/api/core/v1"
)

var shMapInit sync.Once
var shMap ObjHostMap

func getSvcHostMap() *ObjHostMap {
	rhMapInit.Do(func() {
		rhMap.HostMap = make(map[string]IPHostname)
	})
	return &rhMap
}

type SvcMeta struct {
	Cluster   string
	Name      string
	Namespace string
	Hostname  string
	IPAddr    string
	Labels    map[string]string
}

// GetSvcMeta returns a trimmed down version of a svc
func GetSvcMeta(svc *corev1.Service, cname string) (SvcMeta, bool) {
	ip, hostname := GetSvcStatusIPHostname(svc)
	metaObj := SvcMeta{
		Name:      svc.Name,
		Namespace: svc.ObjectMeta.Namespace,
		Hostname:  hostname,
		IPAddr:    ip,
		Cluster:   cname,
	}
	metaObj.Labels = make(map[string]string)
	for key, value := range svc.GetLabels() {
		metaObj.Labels[key] = value
	}

	if ip == "" || hostname == "" {
		gslbutils.Logf("cluster: %s, msg: service object %s, ns: %s, empty status IP %s or hostname %s",
			cname, svc.Name, svc.Namespace, ip, hostname)
		return metaObj, false
	}
	return metaObj, true
}

func GetSvcStatusIPHostname(svc *corev1.Service) (string, string) {
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return "", ""
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	hostname := svc.Status.LoadBalancer.Ingress[0].Hostname

	return ip, hostname
}

func (svc SvcMeta) GetType() string {
	return gdpv1alpha1.LBSvcObj
}

func (svc SvcMeta) GetName() string {
	return svc.Name
}

func (svc SvcMeta) GetNamespace() string {
	return svc.Namespace
}

func (svc SvcMeta) GetCluster() string {
	return svc.Cluster
}

func (svc SvcMeta) SanityCheck(mr gdpv1alpha1.MatchRule) bool {
	return true
}

func (svc SvcMeta) GlobOperate(mr gdpv1alpha1.MatchRule) bool {
	// not implemented
	return false
}

func (svc SvcMeta) GetHostname() string {
	return svc.Hostname
}

func (svc SvcMeta) GetIPAddr() string {
	return svc.IPAddr
}

func (svc SvcMeta) EqualOperate(mr gdpv1alpha1.MatchRule) bool {
	svcLabels := svc.Labels
	if value, ok := svcLabels[mr.Label.Key]; ok {
		if value == mr.Label.Value {
			return true
		}
	}
	gslbutils.Logf("no match found for svc: %s %v", svc.Name, svc.Labels)
	return false
}

func (svc SvcMeta) NotEqualOperate(mr gdpv1alpha1.MatchRule) bool {
	svcLabels := svc.Labels
	if value, ok := svcLabels[mr.Label.Key]; ok {
		if value == mr.Label.Value {
			return false
		}
	}
	return true
}

func (svc SvcMeta) UpdateHostMap(key string) {
	rhm := getSvcHostMap()
	rhm.Lock.Lock()
	defer rhm.Lock.Unlock()
	rhm.HostMap[key] = IPHostname{
		IP:       svc.IPAddr,
		Hostname: svc.Hostname,
	}
}

func (svc SvcMeta) GetHostnameFromHostMap(key string) string {
	shm := getSvcHostMap()
	shm.Lock.Lock()
	defer shm.Lock.Unlock()
	ipHostname, ok := shm.HostMap[key]
	if !ok {
		return ""
	}
	return ipHostname.Hostname
}

func (svc SvcMeta) DeleteMapByKey(key string) {
	shm := getSvcHostMap()
	shm.Lock.Lock()
	defer shm.Lock.Unlock()
	delete(shm.HostMap, key)
}
