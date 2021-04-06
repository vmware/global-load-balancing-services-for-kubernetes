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
	"testing"
	"time"

	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

var (
	routeCluster = "oshift"
	ingCluster   = "k8s"
)

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

func getTestGDP(t *testing.T, name, ns string) *gdpalphav2.GlobalDeploymentPolicy {
	gdp, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get GDP object %s: %v", name, err)
	}
	return gdp
}

func updateTestGDP(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy) *gdpalphav2.GlobalDeploymentPolicy {
	newGdp, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(gdp.Namespace).Update(context.TODO(), gdp, metav1.UpdateOptions{})
	// if k8serrors.
	if err != nil {
		t.Fatalf("update on GDP %v failed with %v", gdp, err)
	}
	VerifyGDPStatus(t, gdp.Namespace, gdp.Name, "success")
	return newGdp
}

func addIngressAndRouteObjects(t *testing.T, testPrefix string) (*networkingv1beta1.Ingress, *routev1.Route) {
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

// Add ingress and route objects, set the health monitor ref and verify
func TestGDPPropertiesForHealthMonitor(t *testing.T) {
	testPrefix := "gdp-hm-"
	hmRefs := []string{"my-hm1"}

	oldGDP := addTestGDPWithProperties(t, hmRefs, nil, nil)

	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g := gomega.NewGomegaWithT(t)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// Update the GDP object with a new health monitor ref
	newGDP := getTestGDP(t, oldGDP.Name, oldGDP.Namespace)
	newGDP.Spec.HealthMonitorRefs = []string{"my-hm2"}
	updateTestGDP(t, newGDP)
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, hmRefs, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add ingress and route objects, set the persistence profile and verify
func TestGDPPropertiesForPersistenceProfile(t *testing.T) {
	testPrefix := "gdp-sp-"
	sitePersistence := "gap-1"

	addTestGDPWithProperties(t, nil, nil, &sitePersistence)

	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g := gomega.NewGomegaWithT(t)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, nil, &sitePersistence,
			nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add ingress and route objects, set the TTL and verify
func TestGDPPropertiesForTTL(t *testing.T) {
	testPrefix := "gdp-ttl-"
	ttl := 10

	oldGDP := addTestGDPWithProperties(t, nil, &ttl, nil)

	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g := gomega.NewGomegaWithT(t)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, nil, nil,
			&ttl)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	t.Logf("Updating TTL value to 20 seconds")
	ttl = 20
	gdpObj := getTestGDP(t, oldGDP.Name, oldGDP.Namespace)
	gdpObj.Spec.TTL = &ttl
	updateTestGDP(t, gdpObj)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, nil, nil,
			&ttl)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create a GSLBHostRule object and check if the GS properties are overriden with the
// the properties specified in the GSLB HostRule object.
func TestGSLBHostRuleCreate(t *testing.T) {
	testPrefix := "gdp-gslbhr-"
	gslbHRName := "test-gslb-hr"
	hmRefs := []string{"my-hm1"}
	sp := "gap-1"
	ttl := 10

	addTestGDPWithProperties(t, hmRefs, &ttl, &sp)
	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)
	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g := gomega.NewGomegaWithT(t)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, hmRefs, &sp,
			&ttl)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	hostName := routeObj.Spec.Host
	gslbHRHmRefs := []string{"my-hm2"}
	gslbHRTTL := 20
	addGSLBHostRule(t, gslbHRName, gslbutils.AVISystem, hostName, gslbHRHmRefs, nil, &gslbHRTTL)
	g.Eventually(func() bool {
		// Site persistence remains unchanged, as it inherits from the GDP object
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, gslbHRHmRefs,
			&sp, &gslbHRTTL)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Update various properties of the GSLBHostRule object and check if the actions are taken accordingly.
// TODO: verify addition of 3rd party members via the GSLB Host Rule.
func TestGSLBHostRuleUpdate(t *testing.T) {
	testPrefix := "gdp-gslbhru-"
	gslbHRName := "test-gslb-hr"
	hmRefs := []string{"my-hm1"}
	sp := "gap-1"
	ttl := 10

	addTestGDPWithProperties(t, hmRefs, &ttl, &sp)
	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)
	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g := gomega.NewGomegaWithT(t)

	hostName := routeObj.Spec.Host
	gslbHRHmRefs := []string{"my-hm2"}
	gslbHRTTL := 20
	oldGSHR := addGSLBHostRule(t, gslbHRName, gslbutils.AVISystem, hostName, gslbHRHmRefs, nil, &gslbHRTTL)
	g.Eventually(func() bool {
		// Site persistence remains unchanged, as it inherits from the GDP object
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, gslbHRHmRefs,
			&sp, &gslbHRTTL)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// add a new site persistence
	newObj := getGSLBHostRule(t, oldGSHR.Name, oldGSHR.Namespace)
	newObj.Spec.SitePersistence = &gslbalphav1.SitePersistence{
		Enabled: false,
	}
	updateGSLBHostRule(t, newObj)

	// verify whether site persistence got updated
	g.Eventually(func() bool {
		// Site persistence changed and set to nil now
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, gslbHRHmRefs,
			nil, &gslbHRTTL)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// delete the health monitor ref from GSLB Host Rule and check if it is inherited from the
	newObj = getGSLBHostRule(t, oldGSHR.Name, oldGSHR.Namespace)
	newObj.Spec.HealthMonitorRefs = nil
	updateGSLBHostRule(t, newObj)

	// verify whether site persistence got updated
	g.Eventually(func() bool {
		// Health monitor refs are deleted, should be inherited from the GDP object
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, hmRefs,
			nil, &gslbHRTTL)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Create a GSLBHostRule object, verify the overriden properties, delete the GSLBHostRule object
// and see if the GS falls back to the GDP properties.
func TestGSLBHostRuleDelete(t *testing.T) {
	testPrefix := "gdp-gslbhrd-"
	gslbHRName := "test-gslb-hr"
	hmRefs := []string{"my-hm1"}
	sp := "gap-1"
	ttl := 10

	addTestGDPWithProperties(t, hmRefs, &ttl, &sp)
	ingObj, routeObj := addIngressAndRouteObjects(t, testPrefix)
	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))
	g := gomega.NewGomegaWithT(t)

	hostName := routeObj.Spec.Host
	gslbHRHmRefs := []string{"my-hm2"}
	gslbHRTTL := 20
	gsHRObj := addGSLBHostRule(t, gslbHRName, gslbutils.AVISystem, hostName, gslbHRHmRefs, nil, &gslbHRTTL)
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, gslbHRHmRefs,
			&sp, &gslbHRTTL)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	t.Logf("will delete the gslb host rule object")
	deleteGSLBHostRule(t, gsHRObj.Name, gsHRObj.Namespace)
	g.Eventually(func() bool {
		// TTL and HM refs will fall back to the GDP object
		return verifyGSMembers(t, expectedMembers, routeObj.Spec.Host, utils.ADMIN_NS, hmRefs,
			&sp, &ttl)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}
