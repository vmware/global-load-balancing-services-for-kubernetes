/*
 * Copyright 2022 VMware, Inc.
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
	"time"

	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	akov1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	hrcs "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"
)

/*
This test file takes care of Hostrule variations in a non custom_fqdn mode
*/

var (
	hmRefs          = []string{"my-hm1"}
	expectedMembers = []nodes.AviGSK8sObj{}
	hrNameK8s       = ""
	hrNameOC        = ""
	hrIRPath        = []string{"/foo"}
	hrTls           = false
)

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
		ingHostIPMap, hrIRPath, true, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, hrIRPath[0], true)
	return ingObj, routeObj
}

// Initialize HR names, Gfdn, expectedMembers. Creates GDP and return ingObj and routeObj
func Initialize(t *testing.T, hrPrefix string, hmRefs []string) (*networkingv1.Ingress, *routev1.Route) {
	hrNameK8s = hrPrefix + "hr"
	hrNameOC = hrPrefix + "hr"
	expectedMembers = []nodes.AviGSK8sObj{}
	AddTestGDPWithProperties(t, hmRefs, nil, nil, nil)
	return AddIngressAndRouteObjects(t, hrPrefix)
}

func GetDefaultAliases(objType string) []string {
	return []string{
		objType + "_alias1" + ".avi.com",
		objType + "_alias2" + ".avi.com",
		objType + "_alias3" + ".avi.com",
	}
}

func GetDefaultHostRule(name, ns, lfqdn, status string) *akov1alpha1.HostRule {
	return &akov1alpha1.HostRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: akov1alpha1.HostRuleSpec{
			VirtualHost: akov1alpha1.HostRuleVirtualHost{
				Fqdn: lfqdn,
			},
		},
		Status: akov1alpha1.HostRuleStatus{
			Status: status,
		},
	}
}

func GetHostRuleWithAliases(name, objType, ns, lfqdn, status string, aliases []string) *akov1alpha1.HostRule {
	if aliases == nil {
		aliases = GetDefaultAliases(objType)
	}
	hr := GetDefaultHostRule(name, ns, lfqdn, status)
	hr.Spec.VirtualHost.Aliases = aliases
	return hr
}

func DeleteHostRule(t *testing.T, cluster int, name, ns string) {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}

	err = hrClient.AkoV1alpha1().HostRules(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Fatalf("error in deleting hostrule for cluster %d: %v", cluster, err)
	}
}

func CreateHostRule(t *testing.T, cluster int, hr *akov1alpha1.HostRule) *akov1alpha1.HostRule {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}

	newHr, err := hrClient.AkoV1alpha1().HostRules(hr.Namespace).Create(context.TODO(), hr, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating hostrule for cluster %d: %v", cluster, err)
	}
	updateHostRuleStatus(t, cluster, hr)
	t.Cleanup(func() {
		DeleteHostRule(t, cluster, newHr.Name, newHr.Namespace)
	})
	return newHr
}

func UpdateHostRule(t *testing.T, cluster int, hr *akov1alpha1.HostRule) *akov1alpha1.HostRule {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}

	newHr, err := hrClient.AkoV1alpha1().HostRules(hr.Namespace).Update(context.TODO(), hr, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("error in updating hostrule for cluster %d: %v", cluster, err)
	}
	updateHostRuleStatus(t, cluster, hr)
	return newHr
}

func updateHostRuleStatus(t *testing.T, cluster int, hr *akov1alpha1.HostRule) *akov1alpha1.HostRule {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": hr.Status,
	})
	newHr, err := hrClient.AkoV1alpha1().HostRules(hr.Namespace).Patch(context.TODO(), hr.Name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in updating the status of hostrule for cluster %d: %v", cluster, err)
	}
	return newHr
}

func GetTestHostRule(t *testing.T, cluster int, name, ns string) *akov1alpha1.HostRule {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}

	hr, err := hrClient.AkoV1alpha1().HostRules(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error in getting hostrule %s/%s: %v", ns, name, err)
	}
	return hr
}

func GetDefaultExpectedDomainNames(gsName string, hrObjList []*akov1alpha1.HostRule) []string {
	aliasList := []string{}
	for _, hr := range hrObjList {
		aliasList = append(aliasList, hr.Spec.VirtualHost.Aliases...)
	}
	aliasSet := sets.NewString(aliasList...)
	return aliasSet.Insert(gsName).List()
}

// Create host rules
func TestHostRuleCreate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)
	fqdn := ingObj.Spec.Rules[0].Host

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := GetHostRuleWithAliases(hrNameK8s, ingCluster, ingObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, K8s, k8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := GetHostRuleWithAliases(hrNameOC, routeCluster, routeObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, Oshift, ocHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create host rules and remove all aliases
func TestHostRuleRemoveAliases(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)
	fqdn := ingObj.Spec.Rules[0].Host

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := GetHostRuleWithAliases(hrNameK8s, ingCluster, ingObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, K8s, k8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := GetHostRuleWithAliases(hrNameOC, routeCluster, routeObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, Oshift, ocHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update host rule for the ingress object's hostname, verify GS member
	newK8sHr := GetTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = []string{}
	UpdateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update host rule for the route object's hostname, verify GS members
	newOcHr := GetTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = []string{}
	UpdateHostRule(t, Oshift, newOcHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			[]string{fqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create, update aliases and delete host rules
// Update cases
// 1. appending new aliases
// 2. replacing old aliases
// 3. removing some of the old aliases
func TestHostRuleCreateUpdateAliasesDelete(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)
	fqdn := ingObj.Spec.Rules[0].Host

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := GetHostRuleWithAliases(hrNameK8s, ingCluster, ingObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, K8s, k8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := GetHostRuleWithAliases(hrNameOC, routeCluster, routeObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, Oshift, ocHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 1 - Appending new aliases
	newK8sHr := GetTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = append(newK8sHr.Spec.VirtualHost.Aliases, []string{"newK8s_alias1.com", "newK8s_alias2.com"}...)
	UpdateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{newK8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 2 - replacing old aliases
	newOcHr := GetTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = []string{"newOc_alias1.com", "newOc_alias2.com"}
	UpdateHostRule(t, Oshift, newOcHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update case 3 - removing some old aliases
	newK8sHr = GetTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	// old aliases = {k8s_alias1.avi.com, k8s_alias2.avi.com, k8s_alias3.avi.com, newK8s_alias1.com, newK8s_alias2.com}
	newK8sHr.Spec.VirtualHost.Aliases = []string{"k8s_alias3.avi.com", "newK8s_alias1.com", "newK8s_alias2.com"}
	UpdateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	DeleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	DeleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			[]string{fqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create, update aliases to have duplicate aliases in the same cluster
func TestHostRuleCreateUpdateDuplicateAliasesInCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)
	fqdn := ingObj.Spec.Rules[0].Host

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := GetHostRuleWithAliases(hrNameK8s, ingCluster, ingObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, K8s, k8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := GetHostRuleWithAliases(hrNameOC, routeCluster, routeObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, Oshift, ocHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update aliases for k8s hr
	newK8sHr := GetTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = []string{"dupK8s_alias.avi.com", "dupK8s_alias.avi.com"}
	newK8sHr.Status.Status = gslbutils.HostRuleRejected
	UpdateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update aliases for openshift hr
	newOcHr := GetTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = []string{"dupOc_alias.avi.com", "dupOc_alias.avi.com"}
	newOcHr.Status.Status = gslbutils.HostRuleRejected
	UpdateHostRule(t, Oshift, newOcHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			[]string{fqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create, update aliases to have duplicate aliases across clusters
func TestHostRuleCreateUpdateDuplicateAliasesAcrossCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)
	fqdn := ingObj.Spec.Rules[0].Host

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := GetHostRuleWithAliases(hrNameK8s, ingCluster, ingObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, K8s, k8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := GetHostRuleWithAliases(hrNameOC, routeCluster, routeObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, nil)
	CreateHostRule(t, Oshift, ocHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update aliases for k8s hr
	newK8sHr := GetTestHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	newK8sHr.Spec.VirtualHost.Aliases = append(newK8sHr.Spec.VirtualHost.Aliases, []string{"dup_alias.avi.com"}...)
	UpdateHostRule(t, K8s, newK8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{newK8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update aliases for openshift hr
	newOcHr := GetTestHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	newOcHr.Spec.VirtualHost.Aliases = append(newOcHr.Spec.VirtualHost.Aliases, []string{"dup_alias.avi.com"}...)
	UpdateHostRule(t, Oshift, newOcHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{newK8sHr, newOcHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create host rules with duplicate aliases in the same cluster and delete the HR
func TestHostRuleCreateDeleteDuplicateAliasesInCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)
	fqdn := ingObj.Spec.Rules[0].Host

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := GetHostRuleWithAliases(hrNameK8s, ingCluster, ingObj.Namespace, fqdn,
		gslbutils.HostRuleRejected, []string{"k8s_dup_alias.avi.com", "k8s_dup_alias.avi.com"})
	CreateHostRule(t, K8s, k8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			[]string{fqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := GetHostRuleWithAliases(hrNameOC, routeCluster, routeObj.Namespace, fqdn,
		gslbutils.HostRuleRejected, []string{"oc_dup_alias.avi.com", "oc_dup_alias.avi.com"})
	CreateHostRule(t, Oshift, ocHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			[]string{fqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	DeleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	DeleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			[]string{fqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create host rules with duplicate aliases across clusters and delete the HR
func TestHostRuleCreateDeleteDuplicateAliasesAcrossCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ingObj, routeObj := Initialize(t, "hr-", hmRefs)
	fqdn := ingObj.Spec.Rules[0].Host

	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	// create a host rule for the ingress object's hostname, verify GS member
	k8sHr := GetHostRuleWithAliases(hrNameK8s, ingCluster, ingObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, []string{"k8s_alias1.avi.com", "dup_alias.avi.com"})
	CreateHostRule(t, K8s, k8sHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// create a host rule for the route object's hostname, verify GS members
	ocHr := GetHostRuleWithAliases(hrNameOC, routeCluster, routeObj.Namespace, fqdn,
		gslbutils.HostRuleAccepted, []string{"oshift_alias1.avi.com", "dup_alias.avi.com"})
	CreateHostRule(t, Oshift, ocHr)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			GetDefaultExpectedDomainNames(fqdn, []*akov1alpha1.HostRule{k8sHr, ocHr}))
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Delete HostRule
	DeleteHostRule(t, K8s, k8sHr.Name, k8sHr.Namespace)
	DeleteHostRule(t, Oshift, ocHr.Name, ocHr.Namespace)
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, fqdn, gslbutils.GetTenant(), nil, nil, nil, nil, nil, hrIRPath, hrTls, nil,
			[]string{fqdn})
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}
