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
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

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
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})

	g := gomega.NewGomegaWithT(t)

	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, false)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, nil, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}
