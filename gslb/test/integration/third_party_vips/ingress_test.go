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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avinetworks/sdk/go/models"
	"github.com/onsi/gomega"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/mockaviserver"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

const (
	GsUUID = "gs-uuid"
	HmUUID = "hm-uuid"
	GsType = "gslbservice"
	HmType = "healthmonitor"
)

func BuildHmRefs(hmRefs []string) []string {
	result := []string{}
	for _, h := range hmRefs {
		rhmRefSplit := strings.Split(h, "name=")
		rhmName := rhmRefSplit[1]
		result = append(result, "https://localhost/api/healthmonitor/healthmonitor-"+rhmName+HmUUID+"#"+rhmName)
	}
	return result
}

func GetTestUuid(obj, name string) string {
	switch obj {
	case GsType:
		return fmt.Sprintf("%s-%s-%s", obj, name, name+GsUUID)
	case HmType:
		return fmt.Sprintf("%s-%s-%s", obj, name, name+HmUUID)
	}
	return ""
}

func GetTestRef(obj, name string) string {
	switch obj {
	case GsType:
		return fmt.Sprintf("https://localhost/api/%s/%s-%s#%s", obj, obj,
			name+GsUUID, name)
	case HmType:
		return fmt.Sprintf("https://localhost/api/%s/%s-%s#%s", obj, obj,
			name+HmUUID, name)
	}
	return ""
}

func PostGSHandlerSendOK(data []byte, w http.ResponseWriter) bool {
	gslbutils.Logf("[custom post gs handler]: data: %v", string(data))
	var resp models.GslbService
	err := json.Unmarshal(data, &resp)
	if err != nil {
		gslbutils.Errf("[custom post gs handler]: got an error while unmarshalling request body: %v", err)
		return true
	}
	url := fmt.Sprintf("https://localhost/api/gslbservice/gslbservice-%s#%s",
		*resp.Name+GsUUID, *resp.Name)
	resp.URL = &url
	uuid := GetTestUuid(GsType, *resp.Name)
	resp.UUID = &uuid
	hmRefs := BuildHmRefs(resp.HealthMonitorRefs)
	resp.HealthMonitorRefs = hmRefs
	gslbutils.Logf("[custom post handler]: sending gs object: %v", resp)
	w.WriteHeader(http.StatusOK)
	finalResponse, _ := json.Marshal(resp)
	w.Write(finalResponse)
	return true
}

func PostHMHandlerSendOK(data []byte, w http.ResponseWriter) bool {
	gslbutils.Logf("[custom post hm handler]: got data: %v", string(data))
	var resp models.HealthMonitor
	err := json.Unmarshal(data, &resp)
	if err != nil {
		gslbutils.Errf("[custom post handler]")
	}
	uuid := GetTestUuid(HmType, *resp.Name)
	resp.UUID = &uuid
	finalResponse, _ := json.Marshal(resp)
	w.WriteHeader(http.StatusOK)
	w.Write(finalResponse)
	return true
}

func PutGSHandlerStatusOK(data []byte, w http.ResponseWriter) bool {
	gslbutils.Logf("[custom put handler]: got data: %v", string(data))
	var resp models.GslbService
	err := json.Unmarshal(data, &resp)
	if err != nil {
		gslbutils.Errf("[custom put handler]: got an error while unmarshalling request body: %v", err)
		return true
	}
	uuid := GetTestUuid(GsType, *resp.Name)
	resp.UUID = &uuid
	hmRefs := BuildHmRefs(resp.HealthMonitorRefs)
	resp.HealthMonitorRefs = hmRefs
	finalResponse, _ := json.Marshal(resp)
	w.WriteHeader(http.StatusOK)
	w.Write(finalResponse)
	return true
}

func initMiddlewares(t *testing.T) {
	mockaviserver.PostGSMiddleware = PostGSHandlerSendOK
	mockaviserver.PostHMMiddleware = PostHMHandlerSendOK
	mockaviserver.PutMiddleware = PutGSHandlerStatusOK

	t.Cleanup(func() {
		mockaviserver.PostGSMiddleware = nil
		mockaviserver.PostHMMiddleware = nil
		mockaviserver.PutMiddleware = nil
	})
}

func BuildTestHmNames(hostname string, paths []string, tls bool) []string {
	httpType := "http"
	if tls {
		httpType = "https"
	}
	hmNames := []string{}
	for _, p := range paths {
		hmName := "amko--" + httpType + "--" + hostname + "--" + p
		hmNames = append(hmNames, hmName)
	}
	return hmNames
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
		oshiftDeleteRoute(t, clusterClients[Oshift], routeName, ns)
		DeleteTestGDP(t, newGDP.Namespace, newGDP.Name)
	})
	initMiddlewares(t)

	g := gomega.NewGomegaWithT(t)

	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, false)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, nil, nil, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	hmNames := BuildTestHmNames(host, []string{"/"}, false)
	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, expectedMembers, host, utils.ADMIN_NS, hmNames, nil, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}

// Add an ingress and a route, verify the GS members, remove the status IP from the ingress object,
// verify the GS member again.
func TestEmptyStatusDefaultIngressAndRoutes(t *testing.T) {
	newGDP, err := BuildAddAndVerifyAppSelectorTestGDP(t)
	if err != nil {
		t.Fatalf("error in building, adding and verifying app selector GDP: %v", err)
	}

	testPrefix := "tdrs-"
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

	initMiddlewares(t)
	g := gomega.NewGomegaWithT(t)

	ingObj := k8sAddIngress(t, clusterClients[K8s], ingName, ns, ingestion_test.TestSvc, ingCluster,
		ingHostIPMap, false)
	routeObj := oshiftAddRoute(t, clusterClients[Oshift], routeName, ns, ingestion_test.TestSvc,
		routeCluster, host, routeIPAddr, false)

	var expectedMembers []nodes.AviGSK8sObj
	expectedMembers = append(expectedMembers, getTestGSMemberFromIng(t, ingObj, ingCluster, 1, 10))
	expectedMembers = append(expectedMembers, getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10))

	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, nil, nil, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	// update the ingress object with an empty status field
	newIng := k8sGetIngress(t, clusterClients[K8s], ingObj.Name, ingObj.Namespace, ingCluster)
	k8sCleanupIngressStatus(t, clusterClients[K8s], ingCluster, newIng)

	expectedMembers = []nodes.AviGSK8sObj{getTestGSMemberFromRoute(t, routeObj, routeCluster, 1, 10)}
	t.Logf("verifying the GS to have only 1 member as route")
	g.Eventually(func() bool {
		return verifyGSMembers(t, expectedMembers, host, utils.ADMIN_NS, nil, nil, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))

	hmNames := BuildTestHmNames(host, []string{"/"}, false)
	g.Eventually(func() bool {
		return verifyGSMembersInRestLayer(t, expectedMembers, host, utils.ADMIN_NS, hmNames, nil, nil, nil)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true))
}
