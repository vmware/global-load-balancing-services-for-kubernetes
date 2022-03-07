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

package custom_fqdn

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"
	akov1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	routeCluster = "oshift"
	ingCluster   = "k8s"
)

func getTestGDP(t *testing.T, name, ns string) *gdpalphav2.GlobalDeploymentPolicy {
	gdp, err := gslbutils.AMKOControlConfig().GDPClientset().AmkoV1alpha2().GlobalDeploymentPolicies(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get GDP object %s: %v", name, err)
	}
	return gdp
}

func updateTestGDP(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy) *gdpalphav2.GlobalDeploymentPolicy {
	newGdp, err := gslbutils.AMKOControlConfig().GDPClientset().AmkoV1alpha2().GlobalDeploymentPolicies(gdp.Namespace).Update(context.TODO(), gdp, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("update on GDP %v failed with %v", gdp, err)
	}
	VerifyGDPStatus(t, gdp.Namespace, gdp.Name, "success")
	return newGdp
}

func addIngressAndRouteObjects(t *testing.T, testPrefix string) (*networkingv1.Ingress, *routev1.Route) {
	ingName := testPrefix + "def-ing"
	routeName := testPrefix + "def-route"
	ns := "default"
	host := testPrefix + ingestion_test.TestDomain1
	ingIPAddr := "10.10.100.1"
	routeIPAddr := "10.10.200.1"
	ingHostIPMap := map[string]string{host: ingIPAddr}

	t.Cleanup(func() {
		k8sDeleteIngress(t, clusterClients[K8s], ingName, ns)
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
	})

	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, true)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, true)
	return ingObj, routeObj
}

func addInsecureIngressAndRouteObjects(t *testing.T, testPrefix string) (*networkingv1.Ingress, *routev1.Route) {
	ingName := testPrefix + "def-ing"
	routeName := testPrefix + "def-route"
	ns := "default"
	host := testPrefix + ingestion_test.TestDomain1
	ingIPAddr := "10.10.100.1"
	routeIPAddr := "10.10.200.1"
	ingHostIPMap := map[string]string{host: ingIPAddr}

	t.Cleanup(func() {
		k8sDeleteIngress(t, clusterClients[K8s], ingName, ns)
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
	})

	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, false)
	return ingObj, routeObj
}

func addTestGDPWithProperties(t *testing.T, hmRefs []string, ttl *int, sitePersistence *string) *gdpalphav2.GlobalDeploymentPolicy {
	gdpObj := GetTestDefaultGDPObject()
	gdpObj.Spec.MatchRules.AppSelector = gdpalphav2.AppSelector{
		Label: appLabel,
	}
	gdpObj.Spec.MatchClusters = []gdpalphav2.ClusterProperty{
		{Cluster: K8sContext}, {Cluster: OshiftContext},
	}
	gdpObj.Spec.HealthMonitorRefs = hmRefs
	gdpObj.Spec.TTL = ttl
	gdpObj.Spec.SitePersistenceRef = sitePersistence

	newGDP, err := AddAndVerifyTestGDPSuccess(t, gdpObj)
	if err != nil {
		t.Fatalf("error in creating and verifying GDP object %v: %v", newGDP, err)
	}

	t.Cleanup(func() {
		DeleteTestGDP(t, gdpObj.Namespace, gdpObj.Name)
	})

	return newGDP
}

// Add ingress and route objects, create a GDP object, create host rules and verify
func TestHostRuleCreate(t *testing.T) {
	testPrefix := "hr-"
	hmRefs := []string{"my-hm1"}
	hrNameK8s := testPrefix + "hr"
	hrNameOC := testPrefix + "hr"
	gfqdn := "test-gs.avi.com"

	addTestGDPWithProperties(t, hmRefs, nil, nil)
	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getDefaultHostRule(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getDefaultHostRule(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, Oshift, ocHr)
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add ingress and route objects, create a GDP object, create invalid host rules, update them to
// valid state
func TestHostRuleInvalidToValid(t *testing.T) {
	testPrefix := "hriv-"
	hmRefs := []string{"my-hm1"}
	hrNameK8s := testPrefix + "hr"
	hrNameOC := testPrefix + "hr"
	gfqdn := "test-gs.avi.com"

	addTestGDPWithProperties(t, hmRefs, nil, nil)
	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	// create a invalid host rule for the ingress object's hostname, verify GS member
	k8sHr := getDefaultHostRule(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleRejected)
	createHostRule(t, K8s, k8sHr)

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getDefaultHostRule(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, Oshift, ocHr)
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update the hostrule to a valid one for the ingress object
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Status.Status = gslbutils.HostRuleAccepted
	updateHostRule(t, K8s, newK8sHr)
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add ingress and route objects, create a GDP object, create valid host rules and update them to
// invalid
func TestHostRuleValidToInvalid(t *testing.T) {
	testPrefix := "hrvi-"
	hmRefs := []string{"my-hm1"}
	hrNameK8s := testPrefix + "hr"
	hrNameOC := testPrefix + "hr"
	gfqdn := "test-gs.avi.com"

	addTestGDPWithProperties(t, hmRefs, nil, nil)
	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getDefaultHostRule(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getDefaultHostRule(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, Oshift, ocHr)
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// change the ingress's host rule to invalid
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Status.Status = gslbutils.HostRuleRejected
	updateHostRule(t, K8s, newK8sHr)

	// GS graph should now have only one member
	expectedMembers = []nodes.AviGSK8sObj{getTestGSMemberFromRoute(t, routeObj, routeCluster, 1)}
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add an ingress object, create a GDP object, create multiple host rules, one accepted and other
// rejected
func TestHostRuleMultiple(t *testing.T) {
	testPrefix := "hr-"
	hmRefs := []string{"my-hm1"}
	hrNameK8s1 := testPrefix + "hr1"
	hrNameK8s2 := testPrefix + "hr2"
	gfqdn1 := "test-gs.avi.com"
	gfqdn2 := "test-gs2.avi.com"

	addTestGDPWithProperties(t, hmRefs, nil, nil)
	ingObj, _ := addIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getDefaultHostRule(hrNameK8s1, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn1,
		gslbutils.HostRuleAccepted)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn1, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	newK8sHr := getDefaultHostRule(hrNameK8s2, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn2,
		gslbutils.HostRuleRejected)
	createHostRule(t, K8s, newK8sHr)

	// there shouldn't be any change in the GS graph
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn1, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add insecure ingress and route objects, create a GDP object, create host rules with TLS cert
// and verify whether the GS members are now TLS members.
// Sequence:
// 1. Add insecure ingress/route objects.
// 2. Add hostrules for the above objects without TLS fields.
// 3. Verify the members and HTTP health monitor for GS.
// 4. Update the hostrules with TLS fields.
// 5. Verify the members and HTTPS health monitor for GS.
func TestHostRuleInsecureToSecure(t *testing.T) {
	testPrefix := "hris-"
	hrNameK8s := testPrefix + "hr"
	hrNameOC := testPrefix + "hr"
	gfqdn := "test-gs.avi.com"

	addTestGDPWithProperties(t, nil, nil, nil)
	ingObj, routeObj := addInsecureIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	k8sHr := getDefaultHostRule(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, K8s, k8sHr)
	ocHr := getDefaultHostRule(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = []nodes.AviGSK8sObj{getTestGSMemberFromIng(t, ingObj, ingCluster, 1),
		getTestGSMemberFromRoute(t, routeObj, routeCluster, 1)}
	t.Logf("verifying members and GS properties for TLS")
	g.Eventually(func() bool {
		// the last parameter below indicates the type of health monitor (HTTP/HTTPS), in this case,
		// it must be `false` indicating HTTP type.
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, nil, nil, nil, false)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	testCert := akov1alpha1.HostRuleSecret{Name: "test-cert", Type: "test-type"}
	newK8sHr.Spec.VirtualHost.TLS.SSLKeyCertificate = testCert
	updateHostRule(t, K8s, newK8sHr)

	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.TLS.SSLKeyCertificate = testCert
	updateHostRule(t, Oshift, newOcHr)

	// members will also become TLS type once the host rules are updated above
	for idx := range expectedMembers {
		expectedMembers[idx].TLS = true
	}
	t.Logf("verifying members and GS properties for non-TLS")
	g.Eventually(func() bool {
		// the last parameter below indicates the type of health monitor (HTTP/HTTPS), in this case,
		// it must be `true` indicating HTTPS type.
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, nil, nil, nil, true)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add insecure ingress and route objects, create a GDP object, create host rules with TLS cert
// and verify whether the GS members are now TLS members.
// Sequence:
// 1. Add insecure ingress/route objects.
// 2. Add hostrules for the above objects with TLS fields.
// 3. Verify the members and HTTPS health monitor for GS.
// 4. Update the hostrules with TLS fields removed.
// 5. Verify the members and HTTP health monitor for GS.
func TestHostRuleSecureToInsecure(t *testing.T) {
	testPrefix := "hrsi-"
	hrNameK8s := testPrefix + "hr"
	hrNameOC := testPrefix + "hr"
	gfqdn := "test-gs.avi.com"

	addTestGDPWithProperties(t, nil, nil, nil)
	ingObj, routeObj := addInsecureIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	testCert := akov1alpha1.HostRuleSecret{Name: "test-cert", Type: "test-type"}

	k8sHr := getDefaultHostRule(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted)
	k8sHr.Spec.VirtualHost.TLS.SSLKeyCertificate = testCert
	createHostRule(t, K8s, k8sHr)
	ocHr := getDefaultHostRule(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted)
	ocHr.Spec.VirtualHost.TLS.SSLKeyCertificate = testCert
	createHostRule(t, Oshift, ocHr)

	expectedMembers = []nodes.AviGSK8sObj{getTestGSMemberFromIng(t, ingObj, ingCluster, 1),
		getTestGSMemberFromRoute(t, routeObj, routeCluster, 1)}
	// members will become TLS type once the host rules are created with TLS fields
	for idx := range expectedMembers {
		expectedMembers[idx].TLS = true
	}
	t.Logf("verifying for members and GS properties for non-TLS")
	g.Eventually(func() bool {
		// the last parameter below indicates the type of health monitor (HTTP/HTTPS), in this case,
		// it must be `true` indicating HTTPS type.
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, nil, nil, nil, true)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// remove the TLS fields from the hostrules
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.TLS.SSLKeyCertificate = akov1alpha1.HostRuleSecret{}
	updateHostRule(t, K8s, newK8sHr)

	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.TLS.SSLKeyCertificate = akov1alpha1.HostRuleSecret{}
	updateHostRule(t, Oshift, newOcHr)

	// members will become non-TLS type once the host rules are updated above
	for idx := range expectedMembers {
		expectedMembers[idx].TLS = false
	}
	t.Logf("verifying for members and GS properties for TLS")
	g.Eventually(func() bool {
		// the last parameter below indicates the type of health monitor (HTTP/HTTPS), in this case,
		// it must be `false` indicating HTTP type.
		return verifyGSMembers(t, expectedMembers, gfqdn, utils.ADMIN_NS, nil, nil, nil, false)
	}, 10*time.Second, 1*time.Second).Should(gomega.Equal(true))
}
