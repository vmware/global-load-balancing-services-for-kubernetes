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

	"github.com/gobwas/glob"
	routev1 "github.com/openshift/api/route/v1"
)

// GetRouteMeta returns a trimmed down version of a route
func GetRouteMeta(route *routev1.Route, cname string) RouteMeta {
	ipAddr, _ := gslbutils.RouteGetIPAddr(route)
	metaObj := RouteMeta{
		Name:      route.Name,
		Namespace: route.ObjectMeta.Namespace,
		Hostname:  route.Spec.Host,
		IPAddr:    ipAddr,
		Cluster:   cname,
	}
	metaObj.Labels = make(map[string]string)
	for key, value := range route.GetLabels() {
		metaObj.Labels[key] = value
	}
	return metaObj
}

// RouteMeta is the metadata for a route. It is the minimal information
// that we maintain for each route, accepted or rejected.
type RouteMeta struct {
	Cluster   string
	Name      string
	Namespace string
	Hostname  string
	IPAddr    string
	Labels    map[string]string
}

func (route RouteMeta) GetType() string {
	return gdpv1alpha1.RouteObj
}

func (route RouteMeta) GetName() string {
	return route.Name
}

func (route RouteMeta) GetNamespace() string {
	return route.Namespace
}

func (route RouteMeta) SanityCheck(mr gdpv1alpha1.MatchRule) bool {
	if len(mr.Hosts) == 0 && mr.Label.Key == "" {
		gslbutils.Errf("object: GDPRule, route: %s, msg: %s", route.Name,
			"GDPRule doesn't have either hosts set or label key-value pair")
		return false
	}
	if len(mr.Hosts) > 0 && route.Hostname == "" {
		return false
	}
	return true
}

func (route RouteMeta) GlobOperate(mr gdpv1alpha1.MatchRule) bool {
	var g glob.Glob
	// route's hostname has to match
	// If no hostname given, return false
	for _, host := range mr.Hosts {
		g = glob.MustCompile(host.HostName, '.')
		if g.Match(route.Hostname) {
			return true
		}
	}
	return false
}

func (route RouteMeta) EqualOperate(mr gdpv1alpha1.MatchRule) bool {
	if len(mr.Hosts) != 0 {
		// Host list is of non-zero length, which means has to be a host match expression
		for _, h := range mr.Hosts {
			if h.HostName == route.Hostname {
				return true
			}
		}
	} else {
		// Its a label key-value match
		routeLabels := route.Labels
		if value, ok := routeLabels[mr.Label.Key]; ok {
			if value == mr.Label.Value {
				return true
			}
		}
	}
	return false
}

func (route RouteMeta) NotEqualOperate(mr gdpv1alpha1.MatchRule) bool {
	if len(mr.Hosts) != 0 {
		// Host list is of non-zero length, which means it has to be a host match expression
		for _, h := range mr.Hosts {
			if h.HostName == route.Hostname {
				return false
			}
		}
		// Match not found for host, return true
		return true
	}
	// Its a label key-value match
	routeLabels := route.Labels
	if value, ok := routeLabels[mr.Label.Key]; ok {
		if value == mr.Label.Value {
			return false
		}
	}
	return true
}
