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
	"errors"
	"sync"

	"github.com/avinetworks/amko/gslb/gslbutils"
	gdpv1alpha1 "github.com/avinetworks/amko/internal/apis/amko/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

var shMapInit sync.Once
var shMap ObjHostMap

func getSvcPortProtocol(svc *corev1.Service) (int32, string, error) {
	if svc == nil {
		gslbutils.Errf("service not found, returning")
		return 0, "", nil
	}

	var minPort int32
	var minProto string

	if len(svc.Spec.Ports) == 0 {
		return 0, "", errors.New("service has no ports, will ignore")
	}
	for idx, port := range svc.Spec.Ports {
		if port.Protocol != "" && (port.Protocol != gslbutils.ProtocolTCP && port.Protocol != gslbutils.ProtocolUDP) {
			gslbutils.Errf("ns: %s, svc: %s, msg: can't enable health monitor for protocol %s, will use the default TCP health monitor",
				svc.ObjectMeta.Namespace, svc.ObjectMeta.Name, port.Protocol)
			return port.Port, gslbutils.ProtocolTCP, nil
		}
		if idx == 0 {
			minPort = port.Port
			minProto = string(port.Protocol)
		}
		if minPort > port.Port {
			minPort = port.Port
			minProto = string(port.Protocol)
		}
	}
	return minPort, minProto, nil
}

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
	Port      int32
	Protocol  string
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

	port, protocol, err := getSvcPortProtocol(svc)
	if err != nil {
		gslbutils.Errf("service rejected because of error: %s", err.Error())
		return metaObj, false
	}
	gslbutils.Debugf("assigning port %d and protocol %s for service %s, ns %s in cluster %s", port, protocol,
		metaObj.Name, metaObj.Namespace, metaObj.Cluster)
	metaObj.Port = port
	metaObj.Protocol = protocol

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

func (svc SvcMeta) GetHostname() string {
	return svc.Hostname
}

func (svc SvcMeta) GetIPAddr() string {
	return svc.IPAddr
}

func (svc SvcMeta) GetPort() (int32, error) {
	return svc.Port, nil
}

func (svc SvcMeta) GetProtocol() (string, error) {
	return svc.Protocol, nil
}

func (svc SvcMeta) GetPaths() ([]string, error) {
	return []string{}, errors.New("service object has no paths configured")
}

func (svc SvcMeta) GetTLS() (bool, error) {
	return false, errors.New("service object doesn't have attribute TLS")
}

func (svc SvcMeta) IsPassthrough() bool {
	return false
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

func (svc SvcMeta) ApplyFilter() bool {
	gf := gslbutils.GetGlobalFilter()
	gf.GlobalLock.RLock()
	gf.GlobalLock.RUnlock()

	if !gslbutils.PresentInList(svc.Cluster, gf.ApplicableClusters) {
		gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: rejected because cluster is not selected",
			svc.Cluster, svc.Namespace, svc.Name)
		return false
	}
	nsFilter := gf.NSFilter
	// will check the namespaces first, whether the namespace for svc is selected
	if nsFilter != nil {
		nsFilter.Lock.RLock()
		defer nsFilter.Lock.RUnlock()
		nsList, ok := gf.NSFilter.SelectedNS[svc.Cluster]
		if !ok {
			gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: rejected because namespace is not selected",
				svc.Cluster, svc.Namespace, svc.Name)
			return false
		}
		if gslbutils.PresentInList(svc.Namespace, nsList) {
			appFilter := gf.AppFilter
			if appFilter == nil {
				gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: accepted because namespace is selected",
					svc.Cluster, svc.Namespace, svc.Name)
				return true
			}
			// Check the appFilter now for this object
			if applyAppFilter(svc.Labels, appFilter) {
				gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: accepted because of namespaceSelector and appSelector",
					svc.Cluster, svc.Namespace, svc.Name)
				return true
			}
			gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: rejected because of appSelector",
				svc.Cluster, svc.Namespace, svc.Name)
			return false
		}
		// this means that the namespace is not selected in the filter
		gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: rejected because namespace is not selected",
			svc.Cluster, svc.Namespace, svc.Name)
		return false
	}

	// Check for app filter
	if gf.AppFilter == nil {
		gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: rejected because no appSelector",
			svc.Cluster, svc.Namespace, svc.Name)
		return false
	}
	if !applyAppFilter(svc.Labels, gf.AppFilter) {
		gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: rejected because of appSelector",
			svc.Cluster, svc.Namespace, svc.Name)
		return false
	}

	gslbutils.Logf("objType: LBSvc, cluster: %s, namespace: %s, name: %s, msg: accepted because of appSelector",
		svc.Cluster, svc.Namespace, svc.Name)
	return true
}
