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
	"strconv"
	"testing"
	"time"

	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

func TestHMAddIngressAndRoutes(t *testing.T) {
	newGDP, err := BuildAddAndVerifyAppSelectorTestGDP(t)
	if err != nil {
		t.Fatalf("error in building, adding and verifying app selector GDP: %v", err)
	}

	testPrefix := "hm-cir-"
	ingName := testPrefix + "ing"
	routeName := testPrefix + "route"
	ns := "default"
	host := testPrefix + ingestion_test.TestDomain1
	ingIPAddr := "1.1.1.1"
	routeIPAddr := "2.2.2.2"
	ingCluster := K8sContext
	routeCluster := OshiftContext
	ingHostIPMap := map[string]string{host: ingIPAddr}
	path := []string{"/"}

	t.Cleanup(func() {
		k8sDeleteIngress(t, clusterClients[K8s], ingName, ns)
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})

	g := gomega.NewGomegaWithT(t)

	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, path, TlsTrue)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, path[0], TlsTrue)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	hmRefs := BuildTestPathHmNames(host, path, TlsTrue)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, path, TlsTrue, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, path, TlsTrue)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

func TestHMAddIngressAndRoutesMultiplePaths(t *testing.T) {
	newGDP, err := BuildAddAndVerifyAppSelectorTestGDP(t)
	if err != nil {
		t.Fatalf("error in building, adding and verifying app selector GDP: %v", err)
	}

	testPrefix := "hm-cir-"
	ingPaths := []string{"/foo", "/bar"}
	ingName := testPrefix + "ing"
	routePaths := []string{"/foo", "/bar1"}
	routePrefix := testPrefix + "route"
	var routeName []string
	ns := "default"
	host := testPrefix + ingestion_test.TestDomain1
	ingIPAddr := "1.1.1.1"
	routeIPAddr := "2.2.2.2"
	ingCluster := K8sContext
	routeCluster := OshiftContext
	ingHostIPMap := map[string]string{host: ingIPAddr}

	t.Cleanup(func() {
		k8sDeleteIngress(t, clusterClients[K8s], ingName, ns)
		for _, route := range routeName {
			oshiftDeleteRoute(t, clusterClients[Oshift], route, ns)
		}
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})

	g := gomega.NewGomegaWithT(t)

	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, ingPaths, TlsTrue)
	var routeObj []*routev1.Route
	for idx, path := range routePaths {
		name := routePrefix + strconv.Itoa(idx)
		routeObj = append(routeObj, oshiftAddRoute(t, clusterClients[Oshift], name, ns, ingestion_test.TestSvc,
			routeCluster, host, routeIPAddr, path, TlsTrue))
		routeName = append(routeName, name)
	}

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, DefaultPriority))
	expectedMembers = append(expectedMembers, getTestGSMemberFromMultiPathRoute(t, routeObj, routeCluster, 1, DefaultPriority)...)

	paths := GetUniquePaths(append(ingPaths, routePaths...))
	hmRefs := BuildTestPathHmNames(host, paths, TlsTrue)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, paths, TlsTrue, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, GetUniqueMembers(expectedMembers), host, utils.ADMIN_NS, hmRefs, nil, nil, nil, paths, TlsTrue)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

func TestHMAddEditHostAndPathIngressAndRoutes(t *testing.T) {
	newGDP, err := BuildAddAndVerifyAppSelectorTestGDP(t)
	if err != nil {
		t.Fatalf("error in building, adding and verifying app selector GDP: %v", err)
	}

	testPrefix := "hm-cir-"
	ingName := testPrefix + "ing"
	routeName := testPrefix + "route"
	ns := "default"
	host := testPrefix + ingestion_test.TestDomain1
	ingIPAddr := "1.1.1.1"
	routeIPAddr := "2.2.2.2"
	ingCluster := K8sContext
	routeCluster := OshiftContext
	ingHostIPMap := map[string]string{host: ingIPAddr}
	path := []string{"/"}

	t.Cleanup(func() {
		k8sDeleteIngress(t, clusterClients[K8s], ingName, ns)
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})

	g := gomega.NewGomegaWithT(t)

	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, nil, TlsTrue)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, path[0], TlsTrue)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, DefaultPriority))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, DefaultPriority))

	hmRefs := BuildTestPathHmNames(host, path, TlsTrue)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, path, TlsTrue, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, path, TlsTrue)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	newPath := []string{"/updatedPath"}
	newHost := "updated-" + host
	updatedIngHostIPMap := map[string]string{newHost: ingIPAddr}

	newIngObj := k8sUpdateIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, updatedIngHostIPMap, newPath)
	newRoute := oshiftUpdateRoute(t, clusterClients[K8s], routeName, ns, ingestion_test.TestSvc, newHost, routeIPAddr, newPath[0])

	expectedMembers = nil
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, newIngObj, ingCluster, 1, DefaultPriority))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, newRoute, routeCluster, 1, DefaultPriority))

	hmRefs = BuildTestPathHmNames(newHost, newPath, TlsTrue)

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, newHost, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, newPath, TlsTrue, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, expectedMembers, newHost, utils.ADMIN_NS, hmRefs, nil, nil, nil, newPath, TlsTrue)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

func TestHMAddLBServices(t *testing.T) {
	newGDP, err := BuildAddAndVerifyAppSelectorTestGDP(t)
	if err != nil {
		t.Fatalf("error in building, adding and verifying app selector GDP: %v", err)
	}

	testPrefix := "hm-cir-"
	svcName := testPrefix + "svc"
	ns := "default"
	host := svcName + "." + ns + "." + ingestion_test.TestDomain1
	svcIPAddr := "3.3.3.3"
	svcHostIPMap := map[string]string{host: svcIPAddr}
	k8sCluster := K8sContext
	var port int32 = 8080

	t.Cleanup(func() {
		k8sDeleteService(t, clusterClients[K8s], svcName, ns)
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})

	g := gomega.NewGomegaWithT(t)

	svcK8sObj := k8sAddLBService(t, clusterClients[K8s], svcName, ns, svcHostIPMap, port)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromSvc(t, svcK8sObj, k8sCluster, 1, DefaultPriority))

	hmRefs := []string{BuildTestNonPathHmNames(host)}

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, nil, false, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, false)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

func TestHMUpdateLBServices(t *testing.T) {
	newGDP, err := BuildAddAndVerifyAppSelectorTestGDP(t)
	if err != nil {
		t.Fatalf("error in building, adding and verifying app selector GDP: %v", err)
	}

	testPrefix := "hm-cir-"
	svcName := testPrefix + "svc"
	ns := "default"
	host := svcName + "." + ns + "." + ingestion_test.TestDomain1
	svcIPAddr := "3.3.3.3"
	svcHostIPMap := map[string]string{host: svcIPAddr}
	k8sCluster := K8sContext
	var port int32 = 8080

	t.Cleanup(func() {
		k8sDeleteService(t, clusterClients[K8s], svcName, ns)
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})

	g := gomega.NewGomegaWithT(t)

	svcK8sObj := k8sAddLBService(t, clusterClients[K8s], svcName, ns, svcHostIPMap, port)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromSvc(t, svcK8sObj, k8sCluster, 1, DefaultPriority))

	hmRefs := []string{BuildTestNonPathHmNames(host)}

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, nil, false, &port)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, false)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	port = 9090
	updatedSvcObj := k8sUpdateLBServicePort(t, clusterClients[K8s], svcName, ns, port)

	expectedMembers = nil
	expectedMembers = append(expectedMembers, getTestGSMemberFromSvc(t, updatedSvcObj, k8sCluster, 1, DefaultPriority))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, nil, false, &port)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, expectedMembers, host, utils.ADMIN_NS, hmRefs, nil, nil, nil, nil, false)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}
