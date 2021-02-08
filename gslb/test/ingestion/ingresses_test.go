/*
 * Copyright 2019-2020 VMware, Inc.
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
	"strconv"
	"testing"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

const (
	acceptedIngStore = true
	rejectedIngStore = false
)

// TestBasicIngress: Create/Delete
func TestBasicIngressCD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "cd-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestBasicIngressCUD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "cud-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr
	gdp := addGDPAndGSLBForIngress(t)

	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	t.Log("Verifying in the accepted store")
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	newHost := testPrefix + TestDomain2
	ingObj.Spec.Rules[0].Host = newHost
	ingObj.Status.LoadBalancer.Ingress[0].Hostname = newHost
	ingObj.ResourceVersion = "101"

	allKeys := []string{}
	t.Log("updating ingress")
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	allKeys = append(allKeys, GetIngressKey("DELETE", cname, ns, ingName, host))
	allKeys = append(allKeys, GetIngressKey("ADD", cname, ns, ingName, newHost))
	VerifyAllKeys(t, allKeys, false)
	t.Log("Verifying that the ingress hostname doesn't exist in the accepted store")
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	t.Log("Verifying that the ingress hostname exists in the accepted store")
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, newHost, ipAddr)

	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, newHost)
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, newHost, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestMultihostIngressCD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mhcd-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	cname := "cluster1"

	hostIPMap := make(map[string]string)
	hostIPMap[testPrefix+TestDomain1] = "10.10.10.10"
	hostIPMap[testPrefix+TestDomain2] = "10.10.10.20"

	gdp := addGDPAndGSLBForIngress(t)
	k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, hostIPMap)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, h, ip)
	}

	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
	}
	DeleteTestGDPObj(gdp)
}

func TestMultihostIngressCUD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mhcud-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	cname := "cluster1"
	hostIPMap := make(map[string]string)

	host1 := testPrefix + TestDomain1
	host2 := testPrefix + TestDomain2
	host3 := testPrefix + TestDomain3

	hostIPMap[host1] = "10.10.10.10"
	hostIPMap[host2] = "10.10.10.20"

	gdp := addGDPAndGSLBForIngress(t)
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, hostIPMap)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, h, ip)
	}

	replaceHostInIngress(ingObj, host1, host3, "10.10.10.30")
	delete(hostIPMap, host1)
	hostIPMap[host3] = "10.10.10.30"
	ingObj.ResourceVersion = "101"

	t.Log("updating ingress")
	// update the ingress and verify
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// first key should be the DELETE key for host1 and then an ADD key for host3
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host1)
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host1, "10.10.10.10")
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host3)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host3, hostIPMap[host3])

	// delete the ingress and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
	}
	DeleteTestGDPObj(gdp)
}

func TestBasicIngressLabelChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "lu-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	ingObj.Labels["key"] = "value1"
	ingObj.ResourceVersion = "101"
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// the key should be for DELETE, as we have ammended the label on the ingress, which is not
	// allowed by the GDP object selection criteria
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	// the ihm object should be moved from the accepted to the rejected store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	verifyInIngStore(g, rejectedIngStore, true, ingName, ns, cname, host, ipAddr)

	// Update it again, and allow it to pass
	ingObj.Labels["key"] = "value"
	ingObj.ResourceVersion = "102"
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	// ihm should be moved from rejected to accepted store now
	verifyInIngStore(g, rejectedIngStore, false, ingName, ns, cname, host, ipAddr)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	// delete the ingress and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestMultihostIngressLabelChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mhlu-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host1 := testPrefix + TestDomain1
	host2 := testPrefix + TestDomain2
	ipAddr1 := "10.10.10.20"
	ipAddr2 := "10.10.10.30"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host1] = ipAddr1
	ingHostIPMap[host2] = ipAddr2

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, ingHostIPMap)
	for h, ip := range ingHostIPMap {
		verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, h, ip)
	}

	ingObj.Labels["key"] = "value1"
	ingObj.ResourceVersion = "101"
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// the key should be for DELETE, as we have ammended the label on the ingress, which is not
	// allowed by the GDP object selection criteria
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, ingHostIPMap)
	// both the ihms should be now moved to rejected list
	for h, ip := range ingHostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
		verifyInIngStore(g, rejectedIngStore, true, ingName, ns, cname, h, ip)
	}

	// Update it again, and allow it to pass
	ingObj.Labels["key"] = "value"
	ingObj.ResourceVersion = "102"
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, ingHostIPMap)
	// both the ihms should be now moved to accepted store
	for h, ip := range ingHostIPMap {
		verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, h, ip)
		verifyInIngStore(g, rejectedIngStore, false, ingName, ns, cname, h, ip)
	}

	// delete the ingress and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, ingHostIPMap)
	for h, ip := range ingHostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
	}
	DeleteTestGDPObj(gdp)
}

func TestMultihostIngressHostAndLabelChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mhlhu-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host1 := testPrefix + TestDomain1
	host2 := testPrefix + TestDomain2
	host3 := testPrefix + TestDomain3
	ipAddr1 := "10.10.10.20"
	ipAddr2 := "10.10.10.30"
	ipAddr3 := "10.10.10.40"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host1] = ipAddr1
	ingHostIPMap[host2] = ipAddr2

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, ingHostIPMap)
	for h, ip := range ingHostIPMap {
		verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, h, ip)
	}

	ingObj.Labels["key"] = "value1"
	ingObj.ResourceVersion = "101"
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// the key should be for DELETE, as we have ammended the label on the ingress, which is not
	// allowed by the GDP object selection criteria
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, ingHostIPMap)
	// both the ihms should be moved from accepted store to the rejected store
	for h, ip := range ingHostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
		verifyInIngStore(g, rejectedIngStore, true, ingName, ns, cname, h, ip)
	}

	// Update the label again, but this time, also edit the host field
	ingObj.Labels["key"] = "value"
	replaceHostInIngress(ingObj, host1, host3, ipAddr3)
	delete(ingHostIPMap, host1)

	ingHostIPMap[host3] = ipAddr3
	ingObj.ResourceVersion = "102"
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, ingHostIPMap)

	// host1 should be deleted from the rejected and the accepted store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host1, ipAddr1)
	verifyInIngStore(g, rejectedIngStore, false, ingName, ns, cname, host1, ipAddr1)

	// host2 and host3 should be present in accepted store and not in the rejected store
	for h, ip := range ingHostIPMap {
		verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, h, ip)
		verifyInIngStore(g, rejectedIngStore, false, ingName, ns, cname, h, ip)
	}
	// delete the ingress and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, ingHostIPMap)
	for h, ip := range ingHostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
	}
	DeleteTestGDPObj(gdp)
}

func TestEmptyStatusIngress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "es-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	k8sAddIngressWithoutStatus(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, true, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, true, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeToEmptyIngress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "sce-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	ingObj.Status.LoadBalancer.Ingress[0].Hostname = ""
	ingObj.Status.LoadBalancer.Ingress[0].IP = ""
	ingObj.ResourceVersion = "101"
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, true, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeFromEmptyIngress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "ecs-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	ingObj := k8sAddIngressWithoutStatus(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, true, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)

	ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
		IP:       ipAddr,
		Hostname: host,
	})
	ingObj.ResourceVersion = "101"
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeIPAddrIngress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "scip-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"
	newIPAddr := "10.10.10.30"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr
	gdp := addGDPAndGSLBForIngress(t)

	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	ingObj.Status.LoadBalancer.Ingress[0].IP = newIPAddr
	ingObj.ResourceVersion = "101"

	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	buildIngressKeyAndVerify(t, false, "UPDATE", cname, ns, ingObj.Name, host)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, newIPAddr)

	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, newIPAddr)
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeToEmptyMultihostIngress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mhsce-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	cname := "cluster1"
	hostIPMap := make(map[string]string)

	host1 := testPrefix + TestDomain1
	host2 := testPrefix + TestDomain2

	hostIPMap[host1] = "10.10.10.10"
	hostIPMap[host2] = "10.10.10.20"

	gdp := addGDPAndGSLBForIngress(t)
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, hostIPMap)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, h, ip)
	}

	deleteStatusInIngress(host1, ingObj)
	delete(hostIPMap, host1)
	ingObj.ResourceVersion = "101"

	t.Log("updating ingress, removing status for host1")
	// update the ingress and verify
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// first key should be the DELETE key for host1
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host1)
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host1, "10.10.10.10")

	deleteStatusInIngress(host2, ingObj)
	delete(hostIPMap, host2)
	ingObj.ResourceVersion = "102"

	t.Log("updating ingress, removing status for host2")
	// update the ingress and verify
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// key should be the DELETE key for host2
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host2)
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host2, "10.10.10.20")

	// delete the ingress and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, true, "DELETE", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
	}
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeFromEmptyMultihostIngress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mhecs-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	cname := "cluster1"
	hostIPMap := make(map[string]string)

	host1 := testPrefix + TestDomain1
	host2 := testPrefix + TestDomain2

	hostIPMap[host1] = "10.10.10.10"
	hostIPMap[host2] = "10.10.10.20"

	gdp := addGDPAndGSLBForIngress(t)
	ingObj := k8sAddIngressWithoutStatus(t, fooKubeClient, ingName, ns, TestSvc, cname, hostIPMap)
	buildIngMultiHostKeyAndVerify(t, true, "ADD", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
	}

	// Add status for host1
	ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
		IP:       hostIPMap[host1],
		Hostname: host1,
	})
	ingObj.ResourceVersion = "101"

	t.Log("updating ingress, adding status for host1")
	// update the ingress and verify
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// key should be the ADD key for host1
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host1)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host1, "10.10.10.10")

	// Add status for host1
	ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
		IP:       hostIPMap[host2],
		Hostname: host2,
	})
	ingObj.ResourceVersion = "102"

	t.Log("updating ingress, adding status for host2")
	// update the ingress and verify
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// key should be the ADD key for host2
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host2)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host2, "10.10.10.20")

	// delete the ingress and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
	}
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeIPAddrMultihostIngress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mhscip-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	cname := "cluster1"
	hostIPMap := make(map[string]string)

	host1 := testPrefix + TestDomain1
	host2 := testPrefix + TestDomain2

	hostIPMap[host1] = "10.10.10.10"
	hostIPMap[host2] = "10.10.10.20"

	gdp := addGDPAndGSLBForIngress(t)
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, hostIPMap)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, h, ip)
	}

	// Update IP address for host1 status
	for idx, ing := range ingObj.Status.LoadBalancer.Ingress {
		if ing.Hostname == host1 {
			ingObj.Status.LoadBalancer.Ingress[idx].IP = "10.10.10.30"
		}
	}
	ingObj.ResourceVersion = "101"

	t.Log("updating ingress, updating status for host1")
	// update the ingress and verify
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// key should be UPDATE key for host1
	buildIngressKeyAndVerify(t, false, "UPDATE", cname, ns, ingName, host1)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host1, "10.10.10.30")

	// update IP address for host2 status
	for idx, ing := range ingObj.Status.LoadBalancer.Ingress {
		if ing.Hostname == host2 {
			ingObj.Status.LoadBalancer.Ingress[idx].IP = "10.10.10.40"
		}
	}
	ingObj.ResourceVersion = "102"

	t.Log("updating ingress, updating status for host2")
	// update the ingress and verify
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// key should be the ADD key for host2
	buildIngressKeyAndVerify(t, false, "UPDATE", cname, ns, ingName, host2)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host2, "10.10.10.40")

	// delete the ingress and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, hostIPMap)
	for h, ip := range hostIPMap {
		verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, h, ip)
	}
	DeleteTestGDPObj(gdp)
}

func TestBasicTLSIngressCD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "tlscd-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr

	gdp := addGDPAndGSLBForIngress(t)
	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	k8sAddTLSIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteTLSIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestBasicTLSIngressCUD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "cud-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr
	gdp := addGDPAndGSLBForIngress(t)

	// Add and test ingresses
	t.Log("Adding and testing ingresses")
	ingObj := k8sAddTLSIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	newHost := testPrefix + TestDomain2
	ingObj.Spec.Rules[0].Host = newHost
	ingObj.Status.LoadBalancer.Ingress[0].Hostname = newHost
	ingObj.ResourceVersion = "101"

	allKeys := []string{}
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	allKeys = append(allKeys, GetIngressKey("DELETE", cname, ns, ingObj.Name, host))
	allKeys = append(allKeys, GetIngressKey("ADD", cname, ns, ingObj.Name, newHost))
	VerifyAllKeys(t, allKeys, false)

	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	verifyInIngStore(g, acceptedIngStore, true, ingName, ns, cname, newHost, ipAddr)

	k8sDeleteTLSIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, newHost)
	verifyInIngStore(g, acceptedIngStore, false, ingName, ns, cname, newHost, ipAddr)
	DeleteTestGDPObj(gdp)
}

func k8sUpdateIngress(t *testing.T, kc *k8sfake.Clientset, ns, cname string,
	ingObj *extensionv1beta1.Ingress) {

	var newResVer string
	// increment the resource version of this ingress
	resVer := ingObj.ResourceVersion
	if resVer == "" {
		newResVer = "100"
	}
	resVerInt, err := strconv.Atoi(resVer)
	if err != nil {
		t.Fatalf("error in parsing resource version: %s", err)
	}
	newResVer = strconv.Itoa(resVerInt + 1)
	ingObj.ResourceVersion = newResVer

	_, err = kc.ExtensionsV1beta1().Ingresses(ns).Update(ingObj)
	if err != nil {
		t.Fatalf("failed to update ingress: %v\n", err)
	}
}

func k8sDeleteIngress(t *testing.T, kc *k8sfake.Clientset, name, ns string) {
	t.Logf("Deleting ingress %s in ns: %s", name, ns)
	err := kc.ExtensionsV1beta1().Ingresses(ns).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in deleting ingress: %v", err)
	}
}

func k8sAddIngress(t *testing.T, kc *k8sfake.Clientset, name string, ns string, svc string,
	cname string, hostIPs map[string]string) *extensionv1beta1.Ingress {

	ingObj := buildIngressObj(name, ns, svc, cname, hostIPs, true)
	_, err := kc.ExtensionsV1beta1().Ingresses(ns).Create(ingObj)
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}

	return ingObj
}

func buildk8sSecret(ns string) *corev1.Secret {
	secretObj := corev1.Secret{}
	secretObj.Name = "test-secret"
	secretObj.Namespace = ns
	secretObj.Data = make(map[string][]byte)
	secretObj.Data["tls.crt"] = []byte("")
	secretObj.Data["tls.key"] = []byte("")
	return &secretObj
}

func k8sAddTLSIngress(t *testing.T, kc *k8sfake.Clientset, name string, ns string, svc string,
	cname string, hostIPs map[string]string) *extensionv1beta1.Ingress {

	var hosts []string
	secretObj := buildk8sSecret(ns)
	_, err := kc.CoreV1().Secrets(ns).Create(secretObj)
	if err != nil {
		t.Fatalf("error in creating secret: %v", err)
	}
	ingObj := buildIngressObj(name, ns, svc, cname, hostIPs, true)
	for h := range hostIPs {
		hosts = append(hosts, h)
	}
	ingTLSObj := v1beta1.IngressTLS{
		Hosts:      hosts,
		SecretName: secretObj.Name,
	}
	ingObj.Spec.TLS = []v1beta1.IngressTLS{ingTLSObj}
	_, err = kc.ExtensionsV1beta1().Ingresses(ns).Create(ingObj)
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}

	return ingObj
}

func k8sDeleteTLSIngress(t *testing.T, kc *k8sfake.Clientset, ingName, ns string) {
	err := kc.ExtensionsV1beta1().Ingresses(ns).Delete(ingName, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Error in deleting ingress: %v", err)
	}
	err = kc.CoreV1().Secrets(ns).Delete("test-secret", &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Error in deleting secret: %v", err)
	}
}

func k8sAddIngressWithoutStatus(t *testing.T, kc *k8sfake.Clientset, name string, ns string, svc string,
	cname string, hostIPs map[string]string) *extensionv1beta1.Ingress {

	ingObj := buildIngressObj(name, ns, svc, cname, hostIPs, false)
	_, err := kc.ExtensionsV1beta1().Ingresses(ns).Create(ingObj)
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}

	return ingObj
}

func buildIngressObj(name, ns, svc, cname string, hostIPs map[string]string, withStatus bool) *extensionv1beta1.Ingress {
	ingObj := &extensionv1beta1.Ingress{}
	ingObj.Namespace = ns
	ingObj.Name = name
	ingObj.ResourceVersion = "100"

	for ingHost, ingIP := range hostIPs {
		ingObj.Spec.Rules = append(ingObj.Spec.Rules, extensionv1beta1.IngressRule{
			Host: ingHost,
		})
		if !withStatus {
			continue
		}
		ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
			IP:       ingIP,
			Hostname: ingHost,
		})
	}
	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	ingObj.Labels = labelMap
	return ingObj
}

// verify in the accepted or rejected Ingress store
func verifyInIngStore(g *gomega.WithT, accepted bool, present bool, ingName, ns, cname, host, ip string) {
	var cs *gslbutils.ClusterStore
	if accepted {
		cs = gslbutils.GetAcceptedIngressStore()
	} else {
		cs = gslbutils.GetRejectedIngressStore()
	}
	ihmObjName := ingName + "/" + host
	obj, found := cs.GetClusterNSObjectByName(cname, ns, ihmObjName)
	g.Expect(found).To(gomega.Equal(present))
	if present {
		ihm := obj.(k8sobjects.IngressHostMeta)
		// If we are expecting that the object is present in the store, then check the required fields
		g.Expect(ihm.Hostname).To(gomega.Equal(host))
		g.Expect(ihm.IPAddr).To(gomega.Equal(ip))
	}
}

func replaceHostInIngress(ingObj *extensionv1beta1.Ingress, oldHost, newHost, ipAddr string) {
	idx := 0
	// replace the hostname host1
	for i, ingRule := range ingObj.Spec.Rules {
		if ingRule.Host == oldHost {
			idx = i
			break
		}
	}
	ingObj.Spec.Rules[idx].Host = newHost

	// also, replace the status
	for i, ing := range ingObj.Status.LoadBalancer.Ingress {
		if ing.Hostname == oldHost {
			idx = i
			break
		}
	}
	ingObj.Status.LoadBalancer.Ingress[idx].Hostname = newHost
	ingObj.Status.LoadBalancer.Ingress[idx].IP = ipAddr
}

func deleteStatusInIngress(host string, ingObj *extensionv1beta1.Ingress) {
	oldIngObj := ingObj.DeepCopy()
	for i, ing := range oldIngObj.Status.LoadBalancer.Ingress {
		if ing.Hostname == host {
			ingObj.Status.LoadBalancer.Ingress =
				append(oldIngObj.Status.LoadBalancer.Ingress[:i],
					oldIngObj.Status.LoadBalancer.Ingress[i+1:]...)
		}
	}
}
