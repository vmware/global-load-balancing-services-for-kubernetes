/*
 * Copyright 2021 VMware, Inc.
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

package third_party_vips

import (
	"context"
	"encoding/json"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

var appLabel map[string]string = map[string]string{"key": "value"}

func k8sAddIngress(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, cname string,
	hostIPs map[string]string) *networkingv1beta1.Ingress {

	ingObj := BuildIngressObj(name, ns, svc, cname, hostIPs, true)
	t.Logf("built an ingress object with name: %s, ns: %s, cname: %s", ns, name, cname)
	t.Logf("ingress: %v", ingObj)
	var hostnames []string
	for _, r := range ingObj.Spec.Rules {
		hostnames = append(hostnames, r.Host)
	}
	ingObj.Annotations = getAnnotations(hostnames)
	_, err := kc.NetworkingV1beta1().Ingresses(ns).Create(context.TODO(), ingObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": ingObj.Status,
	})

	_, err = kc.NetworkingV1beta1().Ingresses(ns).Patch(context.TODO(), name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in patching ingress: %v", err)
	}
	t.Logf("ingress object successfully created with name: %s, ns: %s, cname: %s", ns, name, cname)
	return ingObj
}

func oshiftAddRoute(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, cname, host,
	ip string) *routev1.Route {
	routeObj := BuildRouteObj(name, ns, svc, cname, host, ip, true)
	t.Logf("built a route object with name: %s, ns: %s and cname: %s", name, ns, cname)
	// applying annotations
	hostname := routeObj.Spec.Host
	routeObj.Annotations = getAnnotations([]string{hostname})
	newObj, err := oshiftClient.RouteV1().Routes(ns).Create(context.TODO(), routeObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Couldn't create route obj: %v, err: %v", routeObj, err)
	}
	t.Logf("route object successfully created with name: %s, ns: %s, cname: %s", ns, name, cname)
	return newObj
}

func k8sDeleteIngress(t *testing.T, kc *kubernetes.Clientset, name string, ns string) {
	err := kc.NetworkingV1beta1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}
}

func oshiftDeleteRoute(t *testing.T, kc *kubernetes.Clientset, name string, ns string) {
	err := oshiftClient.RouteV1().Routes(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Couldn't delete route obj: %v, err: %v", name, err)
	}
}

func BuildAddAndVerifyAppSelectorTestGDP(t *testing.T) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	gdpObj := GetTestDefaultGDPObject()
	gdpObj.Spec.MatchRules.AppSelector = gdpalphav2.AppSelector{
		Label: appLabel,
	}
	gdpObj.Spec.MatchClusters = []gdpalphav2.ClusterProperty{
		{Cluster: K8sContext}, {Cluster: OshiftContext},
	}
	return AddAndVerifyTestGDPSuccess(t, gdpObj)
}

func BuildIngressKeyAndVerify(t *testing.T, timeoutExpected bool, op, cname, ns, name, hostname string) {
	expectedKey := ingestion_test.GetIngressKey(op, cname, ns, name, hostname)
	t.Logf("key: %s, msg: will verify key", expectedKey)
	passed, errStr := ingestion_test.WaitAndVerify(t, []string{expectedKey}, timeoutExpected, ingestionKeyChan)
	if !passed {
		t.Fatalf(errStr)
	}
}

func BuildRouteKeyAndVerify(t *testing.T, timeoutExpected bool, op, cname, ns, name string) {
	expectedKey := ingestion_test.GetRouteKey(op, cname, ns, name)
	t.Logf("key: %s, msg: will verify key", expectedKey)
	passed, errStr := ingestion_test.WaitAndVerify(t, []string{expectedKey}, timeoutExpected, ingestionKeyChan)
	if !passed {
		t.Fatalf(errStr)
	}
}

// Add an ingress and a route, verify their keys from ingestion layer
func TestDefaultIngressAndRoutes(t *testing.T) {
	newGDP, err := BuildAddAndVerifyAppSelectorTestGDP(t)
	if err != nil {
		t.Fatalf("error in building, adding and verifying app selector GDP: %v", err)
	}

	testPrefix := "tdr-"
	ingName := testPrefix + "def-ing"
	routeName := testPrefix + "def-route"
	ns := "default"
	host := testPrefix + ingestion_test.TestDomain1
	ingIPAddr := "1.1.1.1"
	routeIPAddr := "2.2.2.2"
	ingCluster := "k8s"
	routeCluster := "oshift"
	ingHostIPMap := map[string]string{host: ingIPAddr}

	t.Cleanup(func() {
		k8sDeleteIngress(t, clusterClients[K8s], ingName, ns)
		BuildIngressKeyAndVerify(t, false, "DELETE", ingCluster, ns, ingName, host)
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
		BuildRouteKeyAndVerify(t, false, "DELETE", routeCluster, ns, routeName)
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})

	k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster, ingHostIPMap)
	BuildIngressKeyAndVerify(t, false, "ADD", ingCluster, ns, ingName, host)
	oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr)
	BuildRouteKeyAndVerify(t, false, "ADD", routeCluster, ns, routeName)
}
