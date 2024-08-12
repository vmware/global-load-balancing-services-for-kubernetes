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
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	gdpv1alpha2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"

	routev1 "github.com/openshift/api/route/v1"
)

var rhMapInit sync.Once
var rhMap ObjHostMap

func getRouteHostMap() *ObjHostMap {
	rhMapInit.Do(func() {
		rhMap.HostMap = make(map[string]IPHostname)
	})
	return &rhMap
}

// GetRouteMeta returns a trimmed down version of a route
func GetRouteMeta(route *routev1.Route, cname string) RouteMeta {
	gf := gslbutils.GetGlobalFilter()
	var syncVIPsOnly bool
	var err error
	syncVIPsOnly, err = gf.IsClusterSyncVIPOnly(cname)
	if err != nil {
		gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: skipping route because of error: %v",
			cname, route.Namespace, route.Name, err)
	}
	var vsUUIDs map[string]string
	var controllerUUID, tenant string

	// Parse status field
	vsUUIDs, controllerUUID, tenant, err = parseVSAndControllerUUIDStatus(route.Status, route.Spec.Host)
	if err != nil {
		// parse annotations
		gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: parsing Controller annotations", cname, route.Namespace, route.Name)
		vsUUIDs, controllerUUID, tenant, err = parseVSAndControllerAnnotations(route.Annotations)
	}
	if err != nil && !syncVIPsOnly {
		gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: skipping route because of error: %v",
			cname, route.Namespace, route.Name, err)
	}
	if (len(vsUUIDs) == 0 || controllerUUID == "") && !syncVIPsOnly {
		gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: skipping route because either VS UUID or controller UUID missing from annotations: %v",
			cname, route.Namespace, route.Name, route.Annotations)
	}
	vsUUID, ok := vsUUIDs[route.Spec.Host]
	if !ok && !syncVIPsOnly {
		gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: hostname %s is missing from VS UUID annotations",
			cname, route.Namespace, route.Name, route.Spec.Host)
	}
	ipAddr, _ := gslbutils.RouteGetIPAddr(route)
	metaObj := RouteMeta{
		Name:               route.Name,
		Namespace:          route.ObjectMeta.Namespace,
		Hostname:           route.Spec.Host,
		IPAddr:             ipAddr,
		Cluster:            cname,
		TLS:                false,
		VirtualServiceUUID: vsUUID,
		ControllerUUID:     controllerUUID,
		Tenant:             tenant,
	}
	metaObj.Labels = make(map[string]string)
	routeLabels := route.GetLabels()
	for key, value := range routeLabels {
		metaObj.Labels[key] = value
	}

	if route.Spec.TLS != nil {
		// for passthrough routes, only set the port and protocol
		if route.Spec.TLS.Termination == gslbutils.PassthroughRoute {
			metaObj.Port = gslbutils.DefaultHTTPSHealthMonitorPort
			metaObj.Protocol = gslbutils.ProtocolTCP
			metaObj.Passthrough = true
			return metaObj
		}
		// route is a TLS type
		metaObj.TLS = true
	}

	pathList := []string{}
	if route.Spec.Path != "" {
		pathList = append(pathList, route.Spec.Path)
	} else {
		pathList = append(pathList, "/")
	}
	// only for passthrough routes, we won't add any paths
	metaObj.Paths = pathList

	return metaObj
}

func parseVSAndControllerUUIDStatus(status routev1.RouteStatus, hostName string) (map[string]string, string, string, error) {
	vsUUIDs, controllerUUID := make(map[string]string), ""
	tenant := gslbutils.GetTenant()
	if len(status.Ingress) == 0 {
		return vsUUIDs, controllerUUID, tenant, fmt.Errorf("empty ingress status field")
	}
	for i := 0; i < len(status.Ingress); i++ {
		if status.Ingress[i].Host == hostName {
			if len(status.Ingress[i].Conditions) > 0 && status.Ingress[i].Conditions[0].Reason != "" {
				akoStatusReason := status.Ingress[i].Conditions[0].Reason
				akoVSControllerUUID := make(map[string]string)
				err := json.Unmarshal([]byte(akoStatusReason), &akoVSControllerUUID)
				if err != nil {
					gslbutils.Debugf("Error in unmarshalling Status reason: %v", akoStatusReason)
					continue
				}
				vsUUIDStr, exists := akoVSControllerUUID[gslbutils.VSAnnotation]
				if !exists {
					gslbutils.Debugf("No VS uuid exist for object, Status reason: %v", akoVSControllerUUID)
					continue
				}
				controllerUUID, exists = akoVSControllerUUID[gslbutils.ControllerAnnotation]
				if !exists {
					gslbutils.Debugf("No Controller uuid exist for object,  Status reason: %v", akoVSControllerUUID)
					continue
				}
				tenant, exists = akoVSControllerUUID[gslbutils.TenantAnnotation]
				if !exists {
					gslbutils.Debugf("No tenant annotation exist for object,  Status reason: %v", akoVSControllerUUID)
					tenant = gslbutils.GetTenant()
				}
				if err := json.Unmarshal([]byte(vsUUIDStr), &vsUUIDs); err != nil {
					gslbutils.Debugf("Error in unmarshalling VS UUID : %v", err)
					continue
				}

				//If everything is ok, return from here
				return vsUUIDs, controllerUUID, tenant, nil
			}
		}
	}
	return vsUUIDs, controllerUUID, tenant, fmt.Errorf("no VSUUID Controller-UUID annotations")
}

// RouteMeta is the metadata for a route. It is the minimal information
// that we maintain for each route, accepted or rejected.
type RouteMeta struct {
	Cluster            string
	Name               string
	Namespace          string
	Hostname           string
	IPAddr             string
	Labels             map[string]string
	Paths              []string
	TLS                bool
	Port               int32
	Protocol           string
	Passthrough        bool
	VirtualServiceUUID string
	ControllerUUID     string
	Tenant             string
}

func (route RouteMeta) GetType() string {
	return gdpv1alpha2.RouteObj
}

func (route RouteMeta) GetName() string {
	return route.Name
}

func (route RouteMeta) GetNamespace() string {
	return route.Namespace
}

func (route RouteMeta) GetHostname() string {
	return route.Hostname
}

func (route RouteMeta) GetIPAddr() string {
	return route.IPAddr
}

func (route RouteMeta) GetCluster() string {
	return route.Cluster
}

func (route RouteMeta) GetPort() (int32, error) {
	// we send the port (to be used only for passthrough routes)
	if route.Passthrough {
		return route.Port, nil
	}
	return 0, errors.New("route object doesn't support GetPort function")
}

func (route RouteMeta) GetProtocol() (string, error) {
	// for passthrough routes, we send the protocol
	if route.Passthrough {
		return route.Protocol, nil
	}
	return "", errors.New("route object doesn't support GetProtocol support")
}

func (route RouteMeta) GetPaths() ([]string, error) {
	if len(route.Paths) == 0 {
		return route.Paths, errors.New("path list is empty for route " + route.Name)
	}
	return route.Paths, nil
}

func (route RouteMeta) GetTLS() (bool, error) {
	return route.TLS, nil
}

func (route RouteMeta) IsPassthrough() bool {
	return route.Passthrough
}

func (route RouteMeta) GetVirtualServiceUUID() string {
	return route.VirtualServiceUUID
}

func (route RouteMeta) GetControllerUUID() string {
	return route.ControllerUUID
}

func (route RouteMeta) GetTenant() string {
	return route.Tenant
}

func (route RouteMeta) UpdateHostMap(key string) {
	rhm := getRouteHostMap()
	rhm.Lock.Lock()
	defer rhm.Lock.Unlock()
	rhm.HostMap[key] = IPHostname{
		IP:       route.IPAddr,
		Hostname: route.Hostname,
	}
}

func (route RouteMeta) GetHostnameFromHostMap(key string) string {
	rhm := getRouteHostMap()
	rhm.Lock.Lock()
	defer rhm.Lock.Unlock()
	ipHostname, ok := rhm.HostMap[key]
	if !ok {
		return ""
	}
	return ipHostname.Hostname
}

func (route RouteMeta) DeleteMapByKey(key string) {
	rhm := getRouteHostMap()
	rhm.Lock.Lock()
	defer rhm.Lock.Unlock()
	delete(rhm.HostMap, key)
}

func (route RouteMeta) ApplyFilter() bool {
	fqdnMap := gslbutils.GetFqdnMap()

	selectedByGDP := route.ApplyGDPSelector()
	if selectedByGDP {
		if gslbutils.GetCustomFqdnMode() {
			_, err := fqdnMap.GetGlobalFqdnForLocalFqdn(route.Cluster, route.Hostname)
			if err != nil {
				gslbutils.Debugf("cluster: %s, ns: %s, route host: %s, msg: error in fetching global fqdn: %v",
					route.Cluster, route.Namespace, route.Hostname, err)
				return false
			}
			return true
		}
	}

	return selectedByGDP
}

func (route RouteMeta) ApplyGDPSelector() bool {
	gf := gslbutils.GetGlobalFilter()
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	if !gslbutils.ClusterContextPresentInList(route.Cluster, gf.ApplicableClusters) {
		gslbutils.Logf("objType: Route, cluster: %s, namespace: %s, name: %s, msg: rejected because cluster is not selected",
			route.Cluster, route.Namespace, route.Name)
		return false
	}

	nsFilter := gf.NSFilter
	// will check the namespaces first, whether the namespace for ihm is selected
	if nsFilter != nil {
		nsFilter.Lock.RLock()
		defer nsFilter.Lock.RUnlock()
		nsList, ok := gf.NSFilter.SelectedNS[route.Cluster]
		if !ok {
			gslbutils.Logf("objType: Route, cluster: %s, namespace: %s, name: %s, msg: rejected because of namespace selector",
				route.Cluster, route.Namespace, route.Name)
			return false
		}
		if gslbutils.PresentInList(route.Namespace, nsList) {
			appFilter := gf.AppFilter
			if appFilter == nil {
				gslbutils.Logf("objType: Route, cluster: %s, namespace: %s, name: %s, msg: accepted because of namespaceSelector",
					route.Cluster, route.Namespace, route.Name)
				return true
			}
			// Check the appFilter now for this object
			if applyAppFilter(route.Labels, appFilter) {
				gslbutils.Logf("objType: Route, cluster: %s, namespace: %s, name: %s, msg: accepted because of namespaceSelector and appSelector",
					route.Cluster, route.Namespace, route.Name)
				return true
			}
			gslbutils.Logf("objType: Route, cluster: %s, namespace: %s, name: %s, msg: rejected because of appSelector",
				route.Cluster, route.Namespace, route.Name)
			return false
		}
		// this means that the namespace is not selected in the filter
		gslbutils.Logf("objType: route, cluster: %s, namespace: %s, name: %s, msg: rejected because namespace is not selected",
			route.Cluster, route.Namespace, route.Name)
		return false
	}

	// check for app filter
	if gf.AppFilter == nil {
		gslbutils.Logf("objType: route, cluster: %s, namespace: %s, name: %s, msg: rejected because no appSelector",
			route.Cluster, route.Namespace, route.Name)
		return false
	}
	if !applyAppFilter(route.Labels, gf.AppFilter) {
		gslbutils.Logf("objType: route, cluster: %s, namespace: %s, name: %s, msg: rejected because of appSelector",
			route.Cluster, route.Namespace, route.Name)
		return false
	}
	gslbutils.Logf("objType: route, cluster: %s, namespace: %s, name: %s, msg: accepted because of appSelector",
		route.Cluster, route.Namespace, route.Name)

	return true
}
