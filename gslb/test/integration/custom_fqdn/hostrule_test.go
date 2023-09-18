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
	akov1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1beta1"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"
)

var (
	routeCluster    = "oshift"
	ingCluster      = "k8s"
	hmRefs          = []string{"my-hm1"}
	hrNameK8s       = ""
	hrNameOC        = ""
	gfqdn           = ""
	expectedMembers = []nodes.AviGSK8sObj{}
)

func GetTestGDP(t *testing.T, name, ns string) *gdpalphav2.GlobalDeploymentPolicy {
	gdp, err := gslbutils.AMKOControlConfig().GDPClientset().AmkoV1alpha2().GlobalDeploymentPolicies(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get GDP object %s: %v", name, err)
	}
	return gdp
}

func UpdateTestGDP(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy) *gdpalphav2.GlobalDeploymentPolicy {
	newGdp, err := gslbutils.AMKOControlConfig().GDPClientset().AmkoV1alpha2().GlobalDeploymentPolicies(gdp.Namespace).Update(context.TODO(), gdp, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("update on GDP %v failed with %v", gdp, err)
	}
	VerifyGDPStatus(t, gdp.Namespace, gdp.Name, "success")
	return newGdp
}

func AddIngressAndRouteObjects(t *testing.T, testPrefix string) (*networkingv1.Ingress, *routev1.Route) {
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

// Initialize HR names, Gfdn, expectedMembers. Creates GDP and return ingObj and routeObj
func Initialize(t *testing.T, hrPrefix string, hmRefs []string) (*networkingv1.Ingress, *routev1.Route) {
	hrNameK8s = hrPrefix + "hr1"
	hrNameOC = hrPrefix + "hr1"
	gfqdn = "test-gs.avi.com"
	expectedMembers = []nodes.AviGSK8sObj{}
	AddTestGDPWithProperties(t, hmRefs, nil, nil, nil)
	return AddIngressAndRouteObjects(t, hrPrefix)
}

func AddInsecureIngressAndRouteObjects(t *testing.T, testPrefix string) (*networkingv1.Ingress, *routev1.Route) {
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

func AddTestGDPWithProperties(t *testing.T, hmRefs []string, ttl *int, sitePersistence *string, hmTemplate *string) *gdpalphav2.GlobalDeploymentPolicy {
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
	gdpObj.Spec.HealthMonitorTemplate = hmTemplate

	newGDP, err := AddAndVerifyTestGDPSuccess(t, gdpObj)
	if err != nil {
		t.Fatalf("error in creating and verifying GDP object %v: %v", newGDP, err)
	}

	t.Cleanup(func() {
		DeleteTestGDP(t, gdpObj.Namespace, gdpObj.Name)
	})

	return newGDP
}

/*
1. create a GDP object
2. Add ingress and route objects
3. Test and verify with various cases of hostrule
*/

// Create host rules
func TestHRCreate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add ingress and route objects, create a GDP object with Hm template, create host rules and verify
func TestHostRuleCreateWithHmTemplateInGDP(t *testing.T) {
	testPrefix := "hr-hm-"
	hmTemplate := "System-GSLB-HTTPS"
	hrNameK8s := testPrefix + "hr"
	hrNameOC := testPrefix + "hr"
	gfqdn := "test-gs.avi.com"

	AddTestGDPWithProperties(t, nil, nil, nil, &hmTemplate)
	ingObj, routeObj := AddIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), nil, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}), nil, &hmTemplate)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, Oshift, ocHr)
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), nil, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}), nil, &hmTemplate)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create host rules with includeAliases = false
func TestHRCreateUnsetIncludeAliases(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname with includeAliases = false, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, false)
	createHostRule(t, K8s, k8sHr)

	defaultDomainNames := []string{gfqdn}
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname with includeAliases = false, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, false)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create host rules and remove all aliases
func TestHRRemoveAliases(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update HostRule to remove all aliases
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = []string{}
	updateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update HostRule to remove all aliases
	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = []string{}
	updateHostRule(t, Oshift, newOcHr)
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, []string{gfqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create host rules and toggle the includeAliases flag from true -> false
func TestHRCreateToggleIncludeAliases(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update HostRule to toggle includeAliases
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Gslb.IncludeAliases = false
	updateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update HostRule to toggle includeAliases
	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Gslb.IncludeAliases = false
	updateHostRule(t, Oshift, newOcHr)
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, []string{gfqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create, update gqfdn and delete host rules
func TestHRCreateUpdateGfdnDelete(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	newgfqdn := "new-" + gfqdn
	// Update gfqdn for hostrule
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Gslb.Fqdn = newgfqdn
	updateHostRule(t, K8s, newK8sHr)

	expectedMembers = append([]nodes.AviGSK8sObj{}, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, newgfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(newgfqdn, []*akov1alpha1.HostRule{newK8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update gfqdn for hostrule
	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Gslb.Fqdn = newgfqdn
	updateHostRule(t, Oshift, newOcHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, newgfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(newgfqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	deleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	deleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, newgfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create, update gqfdn and delete host rules with includeAliases = false
func TestHRCreateUpdateGfdnDeleteUnsetIncludeAliases(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, false)
	createHostRule(t, K8s, k8sHr)

	defaultDomainNames := []string{gfqdn}
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, false)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update gfqdn for hostrule
	newgfqdn := "new-" + gfqdn
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Gslb.Fqdn = newgfqdn
	updateHostRule(t, K8s, newK8sHr)

	expectedMembers = append([]nodes.AviGSK8sObj{}, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	defaultDomainNames = []string{newgfqdn}
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, newgfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update gfqdn for hostrule
	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Gslb.Fqdn = newgfqdn
	updateHostRule(t, Oshift, newOcHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, newgfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	deleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	deleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, newgfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create, update aliases and delete host rules
// Update cases
// 1. appending new aliases
// 2. replacing old aliases
// 3. removing some of the old aliases
func TestHRCreateUpdateAliasesDelete(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 1 - Appending new aliases
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = append(newK8sHr.Spec.VirtualHost.Aliases, []string{"newK8s_alias1.com", "newK8s_alias2.com"}...)
	updateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{newK8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 2 - replacing old aliases
	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = []string{"newOc_alias1.com", "newOc_alias2.com"}
	updateHostRule(t, Oshift, newOcHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 3 - removing some old aliases
	newK8sHr = getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	// old aliases = {k8s_alias1.avi.com, k8s_alias2.avi.com, k8s_alias3.avi.com, newK8s_alias1.com, newK8s_alias2.com}
	newK8sHr.Spec.VirtualHost.Aliases = []string{"k8s_alias3.avi.com", "newK8s_alias1.com", "newK8s_alias2.com"}
	updateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	deleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	deleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, gfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// // Create, update aliases and delete host rules with includeAliases = false
// Update cases
// 1. appending new aliases
// 2. replacing old aliases
// 3. removing some of the old aliases
func TestHRCreateUpdateAliasesDeleteUnsetIncludeAliases(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, false)
	createHostRule(t, K8s, k8sHr)

	defaultDomainNames := []string{gfqdn}
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, false)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 1 - Appending new aliases
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = append(newK8sHr.Spec.VirtualHost.Aliases, []string{"newK8s_alias1.com", "newK8s_alias2.com"}...)
	updateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 2 - replacing old aliases
	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = []string{"newOc_alias1.com", "newOc_alias2.com"}
	updateHostRule(t, Oshift, newOcHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 3 - removing some old aliases
	newK8sHr = getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	// old aliases = {k8s_alias1.avi.com, k8s_alias2.avi.com, k8s_alias3.avi.com, newK8s_alias1.com, newK8s_alias2.com}
	newK8sHr.Spec.VirtualHost.Aliases = []string{"k8s_alias3.avi.com", "newK8s_alias1.com", "newK8s_alias2.com"}
	updateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, defaultDomainNames)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	deleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	deleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, gfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create, update aliases to have duplicate aliases in the same cluster
func TestHRCreateUpdateDuplicateAliasesInCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update aliases for k8s hr
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = []string{"dupK8s_alias.avi.com", "dupK8s_alias.avi.com"}
	newK8sHr.Status.Status = gslbutils.HostRuleRejected
	updateHostRuleStatus(t, K8s, newK8sHr)

	// ingMember is removed from expectedMembers as the hostrule is rejected
	expectedMembers = []nodes.AviGSK8sObj{getTestGSMemberFromRoute(t, routeObj, routeCluster, 1)}
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil, getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update aliases for openshift hr
	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = []string{"dupOc_alias.avi.com", "dupOc_alias.avi.com"}
	newOcHr.Status.Status = gslbutils.HostRuleRejected
	updateHostRuleStatus(t, Oshift, newOcHr)

	// since both the hostrules are rejected the corresponding GS is deleted
	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, gfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create, update aliases to have duplicate aliases across clusters
func TestHRCreateUpdateDuplicateAliasesAcrossCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, nil, true)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update aliases for k8s hr
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = append(newK8sHr.Spec.VirtualHost.Aliases, []string{"dup_alias.avi.com"}...)
	updateHostRule(t, K8s, newK8sHr)
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{newK8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update aliases for openshift hr
	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = append(newOcHr.Spec.VirtualHost.Aliases, []string{"dup_alias.avi.com"}...)
	updateHostRule(t, Oshift, newOcHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create host rules with duplicate aliases in the same cluster and delete the HR

func TestHRCreateDeleteDuplicateAliasesInCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleRejected, []string{"k8s_dup_alias.avi.com", "k8s_dup_alias.avi.com"}, true)
	createHostRule(t, K8s, k8sHr)

	// since the hostrules is rejected the corresponding GS does not exist
	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, gfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleRejected, []string{"oc_dup_alias.avi.com", "oc_dup_alias.avi.com"}, true)
	createHostRule(t, Oshift, ocHr)

	// since the hostrules is rejected the corresponding GS does not exist
	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, gfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	deleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	deleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)

	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, gfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create host rules with duplicate aliases across clusters and delete the HR

func TestHRCreateDeleteDuplicateAliasesAcrossCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleWithAliasesForCustomFqdn(hrNameK8s, ingCluster, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted, []string{"k8s_alias1.avi.com", "dup_alias.avi.com"}, true)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleWithAliasesForCustomFqdn(hrNameOC, routeCluster, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted, []string{"oshift_alias1.avi.com", "dup_alias.avi.com"}, true)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete hostrule whose duplicate alias was discarded
	deleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	expectedMembers = []nodes.AviGSK8sObj{getTestGSMemberFromIng(t, ingObj, ingCluster, 1)}
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	deleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	g.Eventually(func() bool {
		return verifyGSDoesNotExist(t, gfqdn)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create invalid host rules, update them to valid state
func TestHostRuleInvalidToValidForCustomFqdn(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hriv-", hmRefs)

	// create a invalid host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleForCustomFqdn(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleRejected)
	createHostRule(t, K8s, k8sHr)

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleForCustomFqdn(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, Oshift, ocHr)
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update the hostrule to a valid one for the ingress object
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Status.Status = gslbutils.HostRuleAccepted
	updateHostRuleStatus(t, K8s, newK8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{newK8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add ingress and route objects, create a GDP object, create valid host rules and update them to
// invalid
func TestHostRuleValidToInvalidForCustomFqdn(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hrvi-", hmRefs)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleForCustomFqdn(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := getHostRuleForCustomFqdn(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, Oshift, ocHr)
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// change the ingress's host rule to invalid
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Status.Status = gslbutils.HostRuleRejected
	updateHostRuleStatus(t, K8s, newK8sHr)

	// GS graph should now have only one member
	expectedMembers = []nodes.AviGSK8sObj{getTestGSMemberFromRoute(t, routeObj, routeCluster, 1)}
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add an ingress object, create a GDP object, create multiple host rules, one accepted and other
// rejected
func TestHostRuleMultipleForCustomFqdn(t *testing.T) {
	testPrefix := "hrm-"
	hmRefs := []string{"my-hm1"}
	hrNameK8s1 := testPrefix + "hr1"
	hrNameK8s2 := testPrefix + "hr2"
	gfqdn1 := "test-gs.avi.com"
	gfqdn2 := "test-gs2.avi.com"

	AddTestGDPWithProperties(t, hmRefs, nil, nil, nil)
	ingObj, _ := AddIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := getHostRuleForCustomFqdn(hrNameK8s1, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn1,
		gslbutils.HostRuleAccepted)
	createHostRule(t, K8s, k8sHr)

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn1, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn1, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	newK8sHr := getHostRuleForCustomFqdn(hrNameK8s2, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn2,
		gslbutils.HostRuleRejected)
	createHostRule(t, K8s, newK8sHr)

	// there shouldn't be any change in the GS graph
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, gfqdn1, gslbutils.GetTenant(), hmRefs, nil, nil,
			getDefaultExpectedDomainNames(gfqdn1, []*akov1alpha1.HostRule{k8sHr}))
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
func TestHostRuleInsecureToSecureForCustomFqdn(t *testing.T) {
	testPrefix := "hris-"
	hrNameK8s := testPrefix + "hr"
	hrNameOC := testPrefix + "hr"
	gfqdn := "test-gs.avi.com"

	AddTestGDPWithProperties(t, nil, nil, nil, nil)
	ingObj, routeObj := AddInsecureIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	k8sHr := getHostRuleForCustomFqdn(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, K8s, k8sHr)
	ocHr := getHostRuleForCustomFqdn(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
		gslbutils.HostRuleAccepted)
	createHostRule(t, Oshift, ocHr)

	expectedMembers = []nodes.AviGSK8sObj{getTestGSMemberFromIng(t, ingObj, ingCluster, 1),
		getTestGSMemberFromRoute(t, routeObj, routeCluster, 1)}
	t.Logf("verifying members and GS properties for TLS")
	g.Eventually(func() bool {
		// the last parameter below indicates the type of health monitor (HTTP/HTTPS), in this case,
		// it must be `false` indicating HTTP type.
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), nil, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}), false)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	testCert := akov1alpha1.HostRuleSSLKeyCertificate{Name: "test-cert", Type: "secret"}
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
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), nil, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}), true)
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
func TestHostRuleSecureToInsecureForCustomFqdn(t *testing.T) {
	testPrefix := "hrsi-"
	hrNameK8s := testPrefix + "hr"
	hrNameOC := testPrefix + "hr"
	gfqdn := "test-gs.avi.com"

	AddTestGDPWithProperties(t, nil, nil, nil, nil)
	ingObj, routeObj := AddInsecureIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	g := gomega.NewGomegaWithT(t)

	testCert := akov1alpha1.HostRuleSSLKeyCertificate{Name: "test-cert", Type: "secret"}

	k8sHr := getHostRuleForCustomFqdn(hrNameK8s, ingObj.Namespace, ingObj.Spec.Rules[0].Host, gfqdn,
		gslbutils.HostRuleAccepted)
	k8sHr.Spec.VirtualHost.TLS.SSLKeyCertificate = testCert
	createHostRule(t, K8s, k8sHr)
	ocHr := getHostRuleForCustomFqdn(hrNameOC, routeObj.Namespace, routeObj.Spec.Host, gfqdn,
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
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), nil, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}), true)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// remove the TLS fields from the hostrules
	newK8sHr := getTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.TLS.SSLKeyCertificate = akov1alpha1.HostRuleSSLKeyCertificate{}
	updateHostRule(t, K8s, newK8sHr)

	newOcHr := getTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.TLS.SSLKeyCertificate = akov1alpha1.HostRuleSSLKeyCertificate{}
	updateHostRule(t, Oshift, newOcHr)

	// members will become non-TLS type once the host rules are updated above
	for idx := range expectedMembers {
		expectedMembers[idx].TLS = false
	}
	t.Logf("verifying for members and GS properties for TLS")
	g.Eventually(func() bool {
		// the last parameter below indicates the type of health monitor (HTTP/HTTPS), in this case,
		// it must be `false` indicating HTTP type.
		return verifyGSMembers(t, expectedMembers, gfqdn, gslbutils.GetTenant(), nil, nil, nil,
			getDefaultExpectedDomainNames(gfqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}), false)
	}, 10*time.Second, 1*time.Second).Should(gomega.Equal(true))
}
