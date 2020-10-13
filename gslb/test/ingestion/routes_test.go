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

package ingestion

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/avinetworks/amko/gslb/gslbutils"
	"github.com/avinetworks/amko/gslb/k8sobjects"

	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	oshiftfake "github.com/openshift/client-go/route/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	acceptedRouteStore = true
	rejectedRouteStore = false
)

func buildRouteObj(name, ns, svc, cname, host, ip string, withStatus bool) *routev1.Route {
	routeObj := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "101",
		},
		Spec: routev1.RouteSpec{
			Host: host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: svc,
			},
		},
	}

	if withStatus {
		routeObj.Status = routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Conditions: []routev1.RouteIngressCondition{
						{
							Message: ip,
						},
					},
					RouterName: "ako-test",
					Host:       host,
				},
			},
		}
	}

	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	routeObj.Labels = labelMap

	return routeObj
}

func ocAddRoute(t *testing.T, oc *oshiftfake.Clientset, name, ns, svc, cname, host, ip string) *routev1.Route {
	routeObj := buildRouteObj(name, ns, svc, cname, host, ip, true)
	_, err := oc.RouteV1().Routes(ns).Create(routeObj)
	if err != nil {
		t.Fatalf("error in creating route: %v", err)
	}
	return routeObj
}

func ocAddRouteWithoutStatus(t *testing.T, oc *oshiftfake.Clientset, name, ns, svc, cname, host string) *routev1.Route {
	routeObj := buildRouteObj(name, ns, svc, cname, host, "", false)
	_, err := oc.RouteV1().Routes(ns).Create(routeObj)
	if err != nil {
		t.Fatalf("error in creating route: %v", err)
	}
	return routeObj
}

func ocDeleteRoute(t *testing.T, oc *oshiftfake.Clientset, name, ns string) {
	t.Logf("deleting route %s in ns %s", name, ns)
	err := oc.RouteV1().Routes(ns).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in deleting route: %v", err)
	}
}

func ocUpdateRoute(t *testing.T, oc *oshiftfake.Clientset, ns, cname string, routeObj *routev1.Route) {
	var newResVer string
	// increment the resource version of this route
	resVer := routeObj.ResourceVersion
	if resVer == "" {
		newResVer = "100"
	}
	resVerInt, err := strconv.Atoi(resVer)
	if err != nil {
		t.Fatalf("error in parsing resource version: %s", err.Error())
	}
	newResVer = strconv.Itoa(resVerInt + 1)
	routeObj.ResourceVersion = newResVer

	_, err = oc.RouteV1().Routes(ns).Update(routeObj)
	if err != nil {
		t.Fatalf("failed to update route: %s", err.Error())
	}
}

func verifyInRouteStore(g *gomega.WithT, accepted bool, present bool, routeName, ns, cname, host, ip string) {
	var cs *gslbutils.ClusterStore
	if accepted {
		cs = gslbutils.GetAcceptedRouteStore()
	} else {
		cs = gslbutils.GetRejectedRouteStore()
	}

	obj, found := cs.GetClusterNSObjectByName(cname, ns, routeName)
	g.Expect(found).To(gomega.Equal(present))

	if present {
		routeMeta := obj.(k8sobjects.RouteMeta)
		// if we are expecting that the object is present in the store, then check the required fields
		fmt.Println(routeMeta)
		g.Expect(routeMeta.Hostname).To(gomega.Equal(host))
		g.Expect(routeMeta.IPAddr).To(gomega.Equal(ip))
	}
}

func TestBasicRouteCD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "rcd-"
	routeName := testPrefix + "def-route"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.20.20"
	cname := "cluster1"

	gdp := addGDPAndGSLBForIngress(t)

	t.Log("adding and testing route")
	ocAddRoute(t, fooOshiftClient, routeName, ns, TestSvc, cname, host, ipAddr)
	buildRouteKeyAndVerify(t, false, "ADD", cname, ns, routeName)
	// verify the presence of the route in the accepted store
	verifyInRouteStore(g, acceptedRouteStore, true, routeName, ns, cname, host, ipAddr)

	// delete and verify
	ocDeleteRoute(t, fooOshiftClient, routeName, ns)
	buildRouteKeyAndVerify(t, false, "DELETE", cname, ns, routeName)

	DeleteTestGDPObj(gdp)
}

func TestBasicRouteCUD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "rcud-"
	routeName := testPrefix + "def-route"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.20.20"
	cname := "cluster1"

	gdp := addGDPAndGSLBForIngress(t)

	t.Log("adding and testing route")
	route := ocAddRoute(t, fooOshiftClient, routeName, ns, TestSvc, cname, host, ipAddr)
	buildRouteKeyAndVerify(t, false, "ADD", cname, ns, routeName)
	// verify the presence of the route in the accepted store
	verifyInRouteStore(g, acceptedRouteStore, true, routeName, ns, cname, host, ipAddr)

	newHost := testPrefix + TestDomain2
	route.Spec.Host = newHost
	route.Status.Ingress[0].Host = newHost

	t.Log("updating route")
	ocUpdateRoute(t, fooOshiftClient, ns, cname, route)
	buildRouteKeyAndVerify(t, false, "UPDATE", cname, ns, routeName)

	// delete and verify
	ocDeleteRoute(t, fooOshiftClient, routeName, ns)
	buildRouteKeyAndVerify(t, false, "DELETE", cname, ns, routeName)

	DeleteTestGDPObj(gdp)
}

func TestBasicRouteLabelChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "rlu-"
	routeName := testPrefix + "def-route"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.20.20"
	cname := "cluster1"

	gdp := addGDPAndGSLBForIngress(t)
	// add and test routes
	t.Log("adding and testing routes")
	routeObj := ocAddRoute(t, fooOshiftClient, routeName, ns, TestSvc, cname, host, ipAddr)
	buildRouteKeyAndVerify(t, false, "ADD", cname, ns, routeName)

	routeObj.Labels["key"] = "value1"
	ocUpdateRoute(t, fooOshiftClient, ns, cname, routeObj)

	// the key should be for delete, as we have ammended the label on the route
	buildRouteKeyAndVerify(t, false, "DELETE", cname, ns, routeName)
	verifyInRouteStore(g, acceptedRouteStore, false, routeName, ns, cname, host, ipAddr)
	verifyInRouteStore(g, rejectedRouteStore, true, routeName, ns, cname, host, ipAddr)

	// update it again, and allow it to pass
	routeObj.Labels["key"] = "value"
	ocUpdateRoute(t, fooOshiftClient, ns, cname, routeObj)
	buildRouteKeyAndVerify(t, false, "ADD", cname, ns, routeName)
	verifyInRouteStore(g, rejectedRouteStore, false, routeName, ns, cname, host, ipAddr)
	verifyInRouteStore(g, acceptedRouteStore, true, routeName, ns, cname, host, ipAddr)

	// delete the route and verify
	ocDeleteRoute(t, fooOshiftClient, routeName, ns)
	buildRouteKeyAndVerify(t, false, "DELETE", cname, ns, routeName)
	DeleteTestGDPObj(gdp)
}

func TestEmptyStatusRoute(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "res-"
	routeName := testPrefix + "def-route"
	ns := "default"
	cname := "cluster1"
	host := testPrefix + TestDomain1

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("adding and testing route")
	ocAddRouteWithoutStatus(t, fooOshiftClient, routeName, ns, TestSvc, cname, host)
	buildRouteKeyAndVerify(t, true, "ADD", cname, ns, routeName)
	// Verify the presence of the object in the accepted store
	verifyInRouteStore(g, acceptedRouteStore, false, routeName, ns, cname, "", "")

	// delete and verify
	ocDeleteRoute(t, fooOshiftClient, routeName, ns)
	buildRouteKeyAndVerify(t, true, "DELETE", cname, ns, routeName)
	// should be deleted from the accepted store
	verifyInRouteStore(g, acceptedRouteStore, false, routeName, ns, cname, "", "")
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeToEmptyRoute(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "rsce-"
	routeName := testPrefix + "def-route"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.20.20"
	cname := "cluster1"

	gdp := addGDPAndGSLBForIngress(t)

	// add and test routes
	t.Log("adding and testing routes")
	routeObj := ocAddRoute(t, fooOshiftClient, routeName, ns, TestSvc, cname, host, ipAddr)
	buildRouteKeyAndVerify(t, false, "ADD", cname, ns, routeName)
	// verify the object in the accepted store as well
	verifyInRouteStore(g, acceptedRouteStore, true, routeName, ns, cname, host, ipAddr)

	routeObj.Status.Ingress[0].Conditions[0].Message = ""
	routeObj.Status.Ingress[0].Host = ""
	ocUpdateRoute(t, fooOshiftClient, ns, cname, routeObj)
	buildRouteKeyAndVerify(t, false, "DELETE", cname, ns, routeName)
	verifyInRouteStore(g, acceptedRouteStore, false, routeName, ns, cname, host, ipAddr)

	// delete and verify
	ocDeleteRoute(t, fooOshiftClient, routeName, ns)
	buildRouteKeyAndVerify(t, true, "DELETE", cname, ns, routeName)
	// should be deleted from the accepted store
	verifyInRouteStore(g, acceptedRouteStore, false, routeName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeFromEmptyRoute(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "recs-"
	routeName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.20.20"
	cname := "cluster1"

	gdp := addGDPAndGSLBForIngress(t)

	// add and test routes
	t.Log("adding and testing routes")
	routeObj := ocAddRouteWithoutStatus(t, fooOshiftClient, routeName, ns, TestSvc, cname, host)
	buildRouteKeyAndVerify(t, true, "ADD", cname, ns, routeName)

	// verify the presence of the object in the accepted store
	verifyInRouteStore(g, acceptedRouteStore, false, routeName, ns, cname, host, ipAddr)
	condition := routev1.RouteIngressCondition{
		Message: ipAddr,
	}
	routeObj.Status.Ingress = []routev1.RouteIngress{
		{
			Conditions: []routev1.RouteIngressCondition{condition},
			Host:       host,
			RouterName: "ako-test",
		},
	}
	t.Log("updating route to have non-empty status field")
	ocUpdateRoute(t, fooOshiftClient, ns, cname, routeObj)
	buildRouteKeyAndVerify(t, false, "ADD", cname, ns, routeName)
	verifyInRouteStore(g, acceptedRouteStore, true, routeName, ns, cname, host, ipAddr)

	// delete and verify
	ocDeleteRoute(t, fooOshiftClient, routeName, ns)
	buildRouteKeyAndVerify(t, false, "DELETE", cname, ns, routeName)
	// should be deleted from the accepted store
	verifyInRouteStore(g, acceptedRouteStore, false, routeName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func testStatusChangeIPAddrRoute(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "rscip-"
	routeName := testPrefix + "def-route"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.20.20"
	cname := "cluster1"
	newIPAddr := "10.10.20.30"

	gdp := addGDPAndGSLBForIngress(t)

	// add and test routes
	t.Log("adding and testing routes")
	routeObj := ocAddRoute(t, fooOshiftClient, routeName, ns, TestSvc, cname, host, ipAddr)
	buildRouteKeyAndVerify(t, false, "ADD", cname, ns, routeName)
	verifyInRouteStore(g, acceptedRouteStore, true, routeName, ns, cname, host, ipAddr)

	routeObj.Status.Ingress[0].Conditions[0].Message = newIPAddr
	ocUpdateRoute(t, fooOshiftClient, ns, cname, routeObj)
	buildRouteKeyAndVerify(t, false, "UPDATE", cname, ns, routeObj.Name)
	verifyInRouteStore(g, acceptedRouteStore, true, routeName, ns, cname, host, newIPAddr)

	ocDeleteRoute(t, fooOshiftClient, routeName, ns)
	buildRouteKeyAndVerify(t, false, "DELETE", cname, ns, routeName)
	verifyInRouteStore(g, acceptedRouteStore, false, routeName, ns, cname, host, newIPAddr)
	DeleteTestGDPObj(gdp)
}
