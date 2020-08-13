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
	"testing"

	"github.com/avinetworks/amko/gslb/gslbutils"
	gslbingestion "github.com/avinetworks/amko/gslb/ingestion"
	"github.com/avinetworks/amko/gslb/k8sobjects"
	gslbalphav1 "github.com/avinetworks/amko/internal/apis/amko/v1alpha1"

	"github.com/avinetworks/ako/pkg/utils"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

const (
	acceptedSvcStore = true
	rejectedSvcStore = false
)

func addGDPAndGSLBForSvc(t *testing.T) *gslbalphav1.GlobalDeploymentPolicy {
	ingestionQ := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	gdp := getTestGDPObject(true, false)
	gslbingestion.AddGDPObj(gdp, ingestionQ.Workqueue, 2)

	gslbObj := getTestGSLBObject()
	gc, err := gslbingestion.IsGSLBConfigValid(gslbObj)
	if err != nil {
		t.Fatal("GSLB object invalid")
	}
	addGSLBTestConfigObject(gc)
	return gdp
}

func TestBasicSvcCD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "cd-"
	svcName := testPrefix + "def-svc"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.10"
	cname := "cluster1"

	gdp := addGDPAndGSLBForSvc(t)

	// Add and test service
	t.Log("Adding and testing service")
	K8sAddSvc(t, fooKubeClient, svcName, ns, cname, host, ipAddr, corev1.ServiceTypeLoadBalancer)
	buildSvcKeyAndVerify(t, false, "ADD", cname, ns, svcName)

	// Verify the presence of the object in the accepted store
	verifyInSvcStore(g, acceptedSvcStore, true, svcName, ns, cname, host, ipAddr)

	// delete and verify
	K8sDeleteSvc(t, fooKubeClient, svcName, ns)
	buildSvcKeyAndVerify(t, false, "DELETE", cname, ns, svcName)

	// should be deleted from the accepted store
	verifyInSvcStore(g, acceptedSvcStore, false, svcName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestSvcWithoutHostInStatus(t *testing.T) {
	testPrefix := "whis-"
	svcName := testPrefix + "def-svc"
	ns := "default"
	cname := "cluster1"

	gdp := addGDPAndGSLBForSvc(t)
	// Add and test service
	t.Log("Adding and testing service")
	K8sAddSvc(t, fooKubeClient, svcName, ns, cname, "", "", corev1.ServiceTypeLoadBalancer)
	buildSvcKeyAndVerify(t, true, "ADD", cname, ns, svcName)
	DeleteTestGDPObj(gdp)
}

func TestSvcWithLabelNotSelected(t *testing.T) {
	testPrefix := "lns-"
	svcName := testPrefix + "def-svc"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.10"
	cname := "cluster1"

	gdp := addGDPAndGSLBForSvc(t)

	// Add and test service
	t.Log("Adding and testing service")
	svcObj := BuildSvcObj(svcName, ns, cname, host, ipAddr, true, corev1.ServiceTypeLoadBalancer)
	svcObj.ObjectMeta.Labels["key"] = "value1"
	_, err := fooKubeClient.CoreV1().Services(ns).Create(svcObj)
	if err != nil {
		t.Fatalf("error in creating service: %v", err)
	}
	buildSvcKeyAndVerify(t, true, "ADD", cname, ns, svcName)
	DeleteTestGDPObj(gdp)
}

func TestBasicSvcCUD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "cud-"
	svcName := testPrefix + "def-svc"
	ns := "default"
	host1 := testPrefix + TestDomain1
	ipAddr1 := "10.10.10.10"
	host2 := testPrefix + TestDomain2
	ipAddr2 := "10.10.10.11"

	cname := "cluster1"

	gdp := addGDPAndGSLBForSvc(t)

	// Add and test service
	t.Log("Adding and testing service")
	svcObj := K8sAddSvc(t, fooKubeClient, svcName, ns, cname, host1, ipAddr1, corev1.ServiceTypeLoadBalancer)
	buildSvcKeyAndVerify(t, false, "ADD", cname, ns, svcName)

	// Verify the presence of the object in the accepted store
	verifyInSvcStore(g, acceptedSvcStore, true, svcName, ns, cname, host1, ipAddr1)

	svcObj.Status.LoadBalancer.Ingress[0].Hostname = host2
	svcObj.Status.LoadBalancer.Ingress[0].IP = ipAddr2
	svcObj.ResourceVersion = "101"
	K8sUpdateSvc(t, fooKubeClient, ns, cname, svcObj)
	buildSvcKeyAndVerify(t, false, "UPDATE", cname, ns, svcObj.Name)
	verifyInSvcStore(g, acceptedSvcStore, true, svcName, ns, cname, host2, ipAddr2)

	// delete and verify
	K8sDeleteSvc(t, fooKubeClient, svcName, ns)
	buildSvcKeyAndVerify(t, false, "DELETE", cname, ns, svcName)

	// should be deleted from the accepted store
	verifyInSvcStore(g, acceptedSvcStore, false, svcName, ns, cname, host2, ipAddr2)
	DeleteTestGDPObj(gdp)
}

func TestSvcToNoHost(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "tnh-"
	svcName := testPrefix + "def-svc"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.10"
	cname := "cluster1"

	gdp := addGDPAndGSLBForSvc(t)

	// Add and test service
	t.Log("Adding and testing service")
	svcObj := K8sAddSvc(t, fooKubeClient, svcName, ns, cname, host, ipAddr, corev1.ServiceTypeLoadBalancer)
	buildSvcKeyAndVerify(t, false, "ADD", cname, ns, svcName)

	// Verify the presence of the object in the accepted store
	verifyInSvcStore(g, acceptedSvcStore, true, svcName, ns, cname, host, ipAddr)

	svcObj.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{}
	svcObj.ResourceVersion = "101"
	K8sUpdateSvc(t, fooKubeClient, ns, cname, svcObj)
	buildSvcKeyAndVerify(t, false, "DELETE", cname, ns, svcObj.Name)
	verifyInSvcStore(g, acceptedSvcStore, false, svcName, ns, cname, host, ipAddr)

	// delete and verify
	K8sDeleteSvc(t, fooKubeClient, svcName, ns)
	buildSvcKeyAndVerify(t, false, "DELETE", cname, ns, svcName)
	DeleteTestGDPObj(gdp)
}

func TestSvcToDiffLabel(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "dl-"
	svcName := testPrefix + "def-svc"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.10"
	cname := "cluster1"

	gdp := addGDPAndGSLBForSvc(t)

	// Add and test service
	t.Log("Adding and testing service")
	svcObj := K8sAddSvc(t, fooKubeClient, svcName, ns, cname, host, ipAddr, corev1.ServiceTypeLoadBalancer)
	buildSvcKeyAndVerify(t, false, "ADD", cname, ns, svcName)

	// Verify the presence of the object in the accepted store
	verifyInSvcStore(g, acceptedSvcStore, true, svcName, ns, cname, host, ipAddr)

	svcObj.ObjectMeta.Labels["key"] = "value1"
	svcObj.ResourceVersion = "101"
	K8sUpdateSvc(t, fooKubeClient, ns, cname, svcObj)
	buildSvcKeyAndVerify(t, false, "DELETE", cname, ns, svcObj.Name)
	verifyInSvcStore(g, acceptedSvcStore, false, svcName, ns, cname, host, ipAddr)
	verifyInSvcStore(g, rejectedSvcStore, true, svcName, ns, cname, host, ipAddr)

	// delete and verify
	K8sDeleteSvc(t, fooKubeClient, svcName, ns)
	buildSvcKeyAndVerify(t, false, "DELETE", cname, ns, svcName)
	verifyInSvcStore(g, rejectedSvcStore, false, svcName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestNonLBSvcCD(t *testing.T) {
	testPrefix := "cip-"
	svcName := testPrefix + "def-svc"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.10"
	cname := "cluster1"

	gdp := addGDPAndGSLBForSvc(t)

	// Add and test service
	t.Log("Adding and testing service")
	K8sAddSvc(t, fooKubeClient, svcName, ns, cname, host, ipAddr, "ClusterIP")
	buildSvcKeyAndVerify(t, true, "ADD", cname, ns, svcName)

	// delete the service
	K8sDeleteSvc(t, fooKubeClient, svcName, ns)
	DeleteTestGDPObj(gdp)
}

func K8sAddSvc(t *testing.T, kc *k8sfake.Clientset, name string, ns string, cname string, host string,
	ip string, svcType corev1.ServiceType) *corev1.Service {

	svcObj := BuildSvcObj(name, ns, cname, host, ip, true, svcType)
	_, err := kc.CoreV1().Services(ns).Create(svcObj)
	if err != nil {
		t.Fatalf("error in creating service: %v", err)
	}
	return svcObj
}

func BuildSvcObj(name, ns, cname, host, ip string, withStatus bool, svcType corev1.ServiceType) *corev1.Service {
	svcObj := &corev1.Service{}
	svcObj.Namespace = ns
	svcObj.Name = name
	svcObj.ResourceVersion = "100"

	svcObj.Spec.Type = svcType
	svcObj.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		corev1.LoadBalancerIngress{
			IP:       ip,
			Hostname: host,
		},
	}
	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	svcObj.Labels = labelMap
	return svcObj
}

func verifyInSvcStore(g *gomega.WithT, accepted bool, present bool, svcName, ns, cname, host, ip string) {
	var cs *gslbutils.ClusterStore
	if accepted {
		cs = gslbutils.GetAcceptedLBSvcStore()
	} else {
		cs = gslbutils.GetRejectedLBSvcStore()
	}
	obj, found := cs.GetClusterNSObjectByName(cname, ns, svcName)
	g.Expect(found).To(gomega.Equal(present))
	if present {
		svcMeta := obj.(k8sobjects.SvcMeta)
		// If we are expecting that the object is present in the store, then check the required fields
		g.Expect(svcMeta.Hostname).To(gomega.Equal(host))
		g.Expect(svcMeta.IPAddr).To(gomega.Equal(ip))
	}
}

func K8sUpdateSvc(t *testing.T, kc *k8sfake.Clientset, ns, cname string, svcObj *corev1.Service) {
	_, err := kc.CoreV1().Services(ns).Update(svcObj)
	if err != nil {
		t.Fatalf("failed to update service: %v\n", err)
	}
}

func K8sDeleteSvc(t *testing.T, kc *k8sfake.Clientset, name, ns string) {
	err := kc.CoreV1().Services(ns).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in deleting service: %v", err)
	}
}
