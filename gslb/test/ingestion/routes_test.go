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
package ingestion

import (
	gslbingestion "amko/gslb/ingestion"
	gslbalphav1 "amko/pkg/apis/avilb/v1alpha1"
	"testing"

	containerutils "github.com/avinetworks/container-lib/utils"
	routev1 "github.com/openshift/api/route/v1"
	oshiftfake "github.com/openshift/client-go/route/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGSLBAndGDPWithRoutes adds a GDP, a GSLB config and routes.
func TestGSLBAndGDPWithRoutes(t *testing.T) {
	ingestionQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)

	gdp := getTestGDPObject(true, gslbalphav1.RouteObj, gslbalphav1.EqualsOp, "default")
	gslbingestion.AddGDPObj(gdp, ingestionQueue.Workqueue, 2)

	gslbObj := getTestGSLBObject()
	gc, err := gslbingestion.IsGSLBConfigValid(gslbObj)
	if err != nil {
		t.Fatal("GSLB object invalid")
	}
	addGSLBTestConfigObject(gc)
	// Add and test routes
	t.Log("adding and testing routes")
	// We expect a success for these routes.
	addAndTestRoute(t, fooOshiftClient, "foo-def-route1", "default", "foo-host1.avi.com", "foo-svc", "10.10.10.10", false, "cluster1")
	addAndTestRoute(t, barOshiftClient, "bar-def-route1", "default", "bar-host1.avi.com", "bar-svc", "10.10.10.10", false, "cluster2")
	// Remove cluster2 from the cluster list of the GDP object.
	clusterList := []string{"cluster1"}
	oldGdp := gdp.DeepCopy()
	updateTestGDPObject(gdp, clusterList, "101")
	gslbingestion.UpdateGDPObj(oldGdp, gdp, ingestionQueue.Workqueue, 2)
	waitAndVerify(t, []string{"DELETE/Route/cluster2/default/bar-def-route1"}, false)
	// We expect a reject and deletion for the next route, because the host name is not allowed
	updateAndTestRoute(t, fooOshiftClient, "foo-def-route1", "default", "abc.xyz.com", "test-svc", "10.10.10.10", "cluster1", false)

	fooOshiftClient.RouteV1().Routes("default").Delete("foo-def-route1", nil)
	waitAndVerify(t, []string{"DELETE/Route/cluster1/default/foo-def-route1"}, false)
	gslbingestion.DeleteGDPObj(gdp, ingestionQueue.Workqueue, 2)
}

func addAndTestRoute(t *testing.T, oc *oshiftfake.Clientset, name string, ns string, host string, svc string, ip string, timeoutExpected bool, cname string) (bool, string) {
	actualKey := "ADD/Route/" + cname + "/" + ns + "/" + name
	routeStatus := make([]routev1.RouteIngress, 2)
	conditions := make([]routev1.RouteIngressCondition, 2)
	conditions[0].Message = ip
	routeStatus[0].Conditions = conditions
	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	routeExample := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "100",
			Labels:          labelMap,
		},
		Spec: routev1.RouteSpec{
			Host: host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: svc,
			},
		},
		Status: routev1.RouteStatus{
			Ingress: routeStatus,
		},
	}
	_, err := oc.RouteV1().Routes(ns).Create(routeExample)
	if err != nil {
		t.Fatalf("error in adding route: %v", err)
	}
	return waitAndVerify(t, []string{actualKey}, timeoutExpected)
}

func updateAndTestRoute(t *testing.T, oc *oshiftfake.Clientset, name, ns, host, svc, ip, cname string, timeoutExpected bool) (bool, string) {
	actualKey := "UPDATE/Route/" + cname + "/" + ns + "/" + name
	routeStatus := make([]routev1.RouteIngress, 2)
	conditions := make([]routev1.RouteIngressCondition, 2)
	conditions[0].Message = ip
	routeStatus[0].Conditions = conditions
	routeExample := &routev1.Route{
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
		Status: routev1.RouteStatus{
			Ingress: routeStatus,
		},
	}

	_, err := oc.RouteV1().Routes(ns).Update(routeExample)
	if err != nil {
		t.Fatalf("error in updating route: %v", err)
	}
	return waitAndVerify(t, []string{actualKey}, timeoutExpected)
}
