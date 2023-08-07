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
	"testing"
	"time"

	"github.com/onsi/gomega"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"
)

const (
	defaultPriority = uint32(10)
	defaultWeight   = uint32(1)
)

func CreateTestGDPObjectWithPriority(t *testing.T, trafficSplit []gdpalphav2.TrafficSplitElem) *gdpalphav2.GlobalDeploymentPolicy {
	newGDP, err := BuildAddAndVerifyPoolPriorityTestGDP(t, trafficSplit)
	if err != nil {
		t.Fatalf("error in building, adding and verifying pool priority GDP: %v", err)
	}
	t.Cleanup(func() {
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})
	return newGDP
}

func BuildTestTrafficSplit(k8sWeight, k8sPriority, oshiftWeight, oshiftPriority uint32) []gdpalphav2.TrafficSplitElem {
	return []gdpalphav2.TrafficSplitElem{
		{
			Cluster:  K8sContext,
			Weight:   k8sWeight,
			Priority: k8sPriority,
		},
		{
			Cluster:  OshiftContext,
			Weight:   oshiftWeight,
			Priority: oshiftPriority,
		},
	}
}

func TestPoolPriorityValidity(t *testing.T) {
	// add a GDP object with pool priority set as 10 for both clusters
	// add an ingress object
	// add a route object
	// verify that the GS graph contains both the members with priorities as 10
	var commonPriority uint32 = 8
	var commonWeight uint32 = 5
	trafficSplit := BuildTestTrafficSplit(commonWeight, commonPriority, commonWeight, commonPriority)
	CreateTestGDPObjectWithPriority(t, trafficSplit)

	testPrefix := "tppv-"
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
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
	})

	g := gomega.NewGomegaWithT(t)
	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, defaultPath, TlsFalse, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, defaultPath[0], TlsFalse)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster,
		int32(commonWeight), int32(commonPriority)))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster,
		int32(commonWeight), int32(commonPriority)))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, gslbutils.GetAviConfig().Tenant, nil, nil, nil, nil, nil, defaultPath, TlsFalse, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

func TestMultiplePriorityValidity(t *testing.T) {
	// add a GDP object with both clusters having different priorities
	// add an ingress object and a route object
	// verify that the gs graph contains both members with different priorities
	var k8sPriority uint32 = 12
	var oshiftPriority uint32 = 15
	var k8sWeight uint32 = 5
	var oshiftWeight uint32 = 5

	trafficSplit := BuildTestTrafficSplit(k8sWeight, k8sPriority, oshiftWeight, oshiftPriority)
	CreateTestGDPObjectWithPriority(t, trafficSplit)

	testPrefix := "tmpv-"
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
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
	})

	g := gomega.NewGomegaWithT(t)
	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, defaultPath, TlsFalse, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, defaultPath[0], TlsFalse)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster,
		int32(k8sWeight), int32(k8sPriority)))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster,
		int32(oshiftWeight), int32(oshiftPriority)))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, gslbutils.GetAviConfig().Tenant, nil, nil, nil, nil, nil, defaultPath, TlsFalse, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

func TestPriorityOnOneCluster(t *testing.T) {
	// add a GDP object with only one cluster in the trafficSplit field.
	// traffic properties for k8s cluster will be provided, oshift cluster's properties
	// won't be provided, AMKO should assign the defaults
	// add an ingress object and a route object
	// verify the properties for both the clusters including the default values for oshift
	// cluster
	var k8sPriority uint32 = 12
	// default priority of 10 for oshift cluster, as we won't be specifying any priority for
	// the oshift cluster in the GDP object
	var oshiftPriority uint32 = defaultPriority
	var k8sWeight uint32 = 5
	// default weight of 1 for oshift cluster, as we won't be specifying any weight for the
	// oshift cluster in the GDP object
	var oshiftWeight uint32 = defaultWeight

	trafficSplit := []gdpalphav2.TrafficSplitElem{
		{
			Cluster:  K8sContext,
			Weight:   k8sWeight,
			Priority: k8sPriority,
		},
	}
	CreateTestGDPObjectWithPriority(t, trafficSplit)

	testPrefix := "tmpv-"
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
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
	})

	g := gomega.NewGomegaWithT(t)
	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, defaultPath, TlsFalse, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, defaultPath[0], TlsFalse)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster,
		int32(k8sWeight), int32(k8sPriority)))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster,
		int32(oshiftWeight), int32(oshiftPriority)))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, gslbutils.GetAviConfig().Tenant, nil, nil, nil, nil, nil, defaultPath, TlsFalse, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

func TestPriorityOneClusterUpdate(t *testing.T) {
	// - add a GDP object with only one cluster in the trafficSplit field.
	// - traffic properties for k8s cluster will be provided, oshift cluster's properties
	//   won't be provided, AMKO should assign the defaults
	// - add an ingress object and a route object
	// - verify the properties for both the clusters including the default values for oshift
	//   cluster
	// - update the GDP object with a different priority for the k8s cluster
	// - verify the members again
	var k8sPriority uint32 = 12
	// default priority of 10 for oshift cluster, as we won't be specifying any priority for
	// the oshift cluster in the GDP object
	var oshiftPriority uint32 = defaultPriority
	var k8sWeight uint32 = 5
	// default weight of 1 for oshift cluster, as we won't be specifying any weight for the
	// oshift cluster in the GDP object
	var oshiftWeight uint32 = defaultWeight

	// updated priority
	var k8sPriorityUpdated uint32 = 20

	trafficSplit := []gdpalphav2.TrafficSplitElem{
		{
			Cluster:  K8sContext,
			Weight:   k8sWeight,
			Priority: k8sPriority,
		},
	}
	gdpObj := CreateTestGDPObjectWithPriority(t, trafficSplit)

	testPrefix := "tmpv-"
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
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
	})

	g := gomega.NewGomegaWithT(t)
	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, defaultPath, TlsFalse, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, defaultPath[0], TlsFalse)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster,
		int32(k8sWeight), int32(k8sPriority)))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster,
		int32(oshiftWeight), int32(oshiftPriority)))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, gslbutils.GetAviConfig().Tenant, nil, nil, nil, nil, nil, defaultPath, TlsFalse, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	updTrafficSplit := []gdpalphav2.TrafficSplitElem{
		{
			Cluster:  K8sContext,
			Weight:   k8sWeight,
			Priority: k8sPriorityUpdated,
		},
	}
	_, err := UpdateAndVerifyTestGDPPrioritySuccess(t, gdpObj.Name, gdpObj.Namespace, updTrafficSplit)
	if err != nil {
		t.Fatalf("error while updating and verifying GDP object: %v", err)
	}
	t.Logf("will update the priority in GDP for k8s cluster")
	var updatedGSMembers []nodes.AviGSK8sObj
	updatedGSMembers = append(updatedGSMembers, getTestGSMemberFromIng(t, ingObj, ingCluster,
		int32(k8sWeight), int32(k8sPriorityUpdated)))
	updatedGSMembers = append(updatedGSMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster,
		int32(oshiftWeight), int32(oshiftPriority)))
	g.Eventually(func() bool {
		return verifyGSMembers(t, updatedGSMembers, host, gslbutils.GetAviConfig().Tenant, nil, nil, nil, nil, nil, defaultPath, TlsFalse, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

func TestMultiplePriorityUpdate(t *testing.T) {
	// Add a GDP object with both clusters having different priorities
	// Add an ingress object and a route object
	// Verify that the gs graph contains both members with different priorities
	// Update the priority of the oshift cluster and verify the updated priorities for
	// the GSs.
	var k8sPriority uint32 = 12
	var oshiftPriority uint32 = 15
	var k8sWeight uint32 = 5
	var oshiftWeight uint32 = 5
	var UpdatedPriorityOshift uint32 = 30

	trafficSplit := BuildTestTrafficSplit(k8sWeight, k8sPriority, oshiftWeight, oshiftPriority)
	gdpObj := CreateTestGDPObjectWithPriority(t, trafficSplit)

	testPrefix := "tmpv-"
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
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
	})

	g := gomega.NewGomegaWithT(t)
	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, defaultPath, TlsFalse, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, defaultPath[0], TlsFalse)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster,
		int32(k8sWeight), int32(k8sPriority)))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster,
		int32(oshiftWeight), int32(oshiftPriority)))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, gslbutils.GetAviConfig().Tenant, nil, nil, nil, nil, nil, defaultPath, TlsFalse, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	updTrafficSplit := BuildTestTrafficSplit(k8sWeight, k8sPriority, oshiftWeight, UpdatedPriorityOshift)
	_, err := UpdateAndVerifyTestGDPPrioritySuccess(t, gdpObj.Name, gdpObj.Namespace, updTrafficSplit)
	if err != nil {
		t.Fatalf("error while updating and verifying GDP object: %v", err)
	}
	t.Logf("will update the priority in GDP for k8s cluster")
	var updatedGSMembers []nodes.AviGSK8sObj
	updatedGSMembers = append(updatedGSMembers, getTestGSMemberFromIng(t, ingObj, ingCluster,
		int32(k8sWeight), int32(k8sPriority)))
	updatedGSMembers = append(updatedGSMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster,
		int32(oshiftWeight), int32(UpdatedPriorityOshift)))
	g.Eventually(func() bool {
		return verifyGSMembers(t, updatedGSMembers, host, gslbutils.GetAviConfig().Tenant, nil, nil, nil, nil, nil, defaultPath, TlsFalse, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}
