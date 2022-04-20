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
	"context"
	"strconv"
	"testing"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/store"

	akov1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	crdfake "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned/fake"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// TestMultiClusterIngressCD: Create/Delete of MCI objects
func TestMultiClusterIngressCD(t *testing.T) {
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
	// Add and test Multi-cluster ingresses
	t.Log("Adding and testing ingresses")
	k8sAddMultiClusterIngress(t, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteMultiClusterIngress(t, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestMultiClusterIngressCUD(t *testing.T) {
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

	// Add and test Multi-cluster ingresses
	t.Log("Adding and testing Multi-cluster ingresses")
	ingObj := k8sAddMultiClusterIngress(t, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	t.Log("Verifying in the accepted store")
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	newHost := testPrefix + TestDomain2
	ingObj.Spec.Hostname = newHost
	ingObj.Status.LoadBalancer.Ingress[0].Hostname = newHost
	ingObj.ResourceVersion = "101"

	allKeys := []string{}
	t.Log("updating Multi-cluster ingress")
	k8sUpdateMultiClusterIngress(t, fooCRDKubeClient, ns, cname, ingObj)
	allKeys = append(allKeys, GetMultiClusterIngressKey("DELETE", cname, ns, ingName, host))
	allKeys = append(allKeys, GetMultiClusterIngressKey("ADD", cname, ns, ingName, newHost))
	VerifyAllKeys(t, allKeys, false)
	t.Log("Verifying that the ingress hostname doesn't exist in the accepted store")
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	t.Log("Verifying that the ingress hostname exists in the accepted store")
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, newHost, ipAddr)

	k8sDeleteMultiClusterIngress(t, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, newHost)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, newHost, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestMultiClusterIngressLabelChange(t *testing.T) {
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
	t.Log("Adding and testing Multi-cluster ingresses")
	ingObj := k8sAddMultiClusterIngress(t, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	ingObj.Labels["key"] = "value1"
	ingObj.ResourceVersion = "101"
	k8sUpdateMultiClusterIngress(t, fooCRDKubeClient, ns, cname, ingObj)

	// the key should be for DELETE, as we have ammended the label on the ingress, which is not
	// allowed by the GDP object selection criteria
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	// the ihm object should be moved from the accepted to the rejected store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	verifyInMultiClusterIngressStore(g, rejectedIngStore, true, ingName, ns, cname, host, ipAddr)

	// Update it again, and allow it to pass
	ingObj.Labels["key"] = "value"
	ingObj.ResourceVersion = "102"
	k8sUpdateMultiClusterIngress(t, fooCRDKubeClient, ns, cname, ingObj)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	// ihm should be moved from rejected to accepted store now
	verifyInMultiClusterIngressStore(g, rejectedIngStore, false, ingName, ns, cname, host, ipAddr)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	// delete the ingress and verify
	k8sDeleteMultiClusterIngress(t, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestEmptyStatusMultiClusterIngress(t *testing.T) {
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
	// Add and test Multi-cluster ingresses
	t.Log("Adding and testing Multi-cluster ingresses")
	k8sAddMultiClusterIngressWithoutStatus(t, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, true, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteMultiClusterIngress(t, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, true, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeToEmptyMultiClusterIngress(t *testing.T) {
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
	// Add and test Multi-cluster ingresses
	t.Log("Adding and testing Multi-cluster ingresses")
	ingObj := k8sAddMultiClusterIngress(t, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	ingObj.Status.LoadBalancer.Ingress[0].Hostname = ""
	ingObj.Status.LoadBalancer.Ingress[0].IP = ""
	ingObj.ResourceVersion = "101"
	k8sUpdateMultiClusterIngress(t, fooCRDKubeClient, ns, cname, ingObj)
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteMultiClusterIngress(t, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, true, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeFromEmptyMultiClusterIngress(t *testing.T) {
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
	// Add and test Multi-cluster ingresses
	t.Log("Adding and testing Multi-cluster ingresses")
	ingObj := k8sAddMultiClusterIngressWithoutStatus(t, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, true, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)

	ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, akov1alpha1.IngressStatus{
		IP:       ipAddr,
		Hostname: host,
	})
	ingObj.ResourceVersion = "101"
	k8sUpdateMultiClusterIngress(t, fooCRDKubeClient, ns, cname, ingObj)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteMultiClusterIngress(t, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestStatusChangeIPAddrMultiClusterIngress(t *testing.T) {
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

	// Add and test Multi-cluster ingresses
	t.Log("Adding and testing Multi-cluster ingresses")
	ingObj := k8sAddMultiClusterIngress(t, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	ingObj.Status.LoadBalancer.Ingress[0].IP = newIPAddr
	ingObj.ResourceVersion = "101"

	k8sUpdateMultiClusterIngress(t, fooCRDKubeClient, ns, cname, ingObj)
	buildMultiClusterIngressKeyAndVerify(t, false, "UPDATE", cname, ns, ingObj.Name, host)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, newIPAddr)

	k8sDeleteMultiClusterIngress(t, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, newIPAddr)
	DeleteTestGDPObj(gdp)
}

func TestTLSMultiClusterIngressCD(t *testing.T) {
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
	// Add and test Multi-cluster ingresses
	t.Log("Adding and testing Multi-cluster ingresses")
	k8sAddTLSMultiClusterIngress(t, fooKubeClient, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	// Verify the presence of the object in the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	// delete and verify
	k8sDeleteTLSMultiClusterIngress(t, fooKubeClient, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	// should be deleted from the accepted store
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	DeleteTestGDPObj(gdp)
}

func TestTLSMultiClusterIngressCUD(t *testing.T) {
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

	// Add and test Multi-cluster ingresses
	t.Log("Adding and testing Multi-cluster ingresses")
	ingObj := k8sAddTLSMultiClusterIngress(t, fooKubeClient, fooCRDKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildMultiClusterIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, host, ipAddr)

	newHost := testPrefix + TestDomain2
	ingObj.Spec.Hostname = newHost
	ingObj.Status.LoadBalancer.Ingress[0].Hostname = newHost
	ingObj.ResourceVersion = "101"

	allKeys := []string{}
	k8sUpdateMultiClusterIngress(t, fooCRDKubeClient, ns, cname, ingObj)
	allKeys = append(allKeys, GetMultiClusterIngressKey("DELETE", cname, ns, ingObj.Name, host))
	allKeys = append(allKeys, GetMultiClusterIngressKey("ADD", cname, ns, ingObj.Name, newHost))
	VerifyAllKeys(t, allKeys, false)

	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, host, ipAddr)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, true, ingName, ns, cname, newHost, ipAddr)

	k8sDeleteTLSMultiClusterIngress(t, fooKubeClient, fooCRDKubeClient, ingName, ns)
	buildMultiClusterIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, newHost)
	verifyInMultiClusterIngressStore(g, acceptedIngStore, false, ingName, ns, cname, newHost, ipAddr)
	DeleteTestGDPObj(gdp)
}

func k8sUpdateMultiClusterIngress(t *testing.T, kc *crdfake.Clientset, ns, cname string,
	ingObj *akov1alpha1.MultiClusterIngress) {

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

	_, err = kc.AkoV1alpha1().MultiClusterIngresses(ns).Update(context.TODO(), ingObj, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to update Multi-cluster ingress: %v\n", err)
	}
}

func k8sDeleteMultiClusterIngress(t *testing.T, kc *crdfake.Clientset, name, ns string) {
	t.Logf("Deleting Multi-cluster ingress %s in ns: %s", name, ns)
	err := kc.AkoV1alpha1().MultiClusterIngresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in deleting Multi-cluster ingress: %v", err)
	}
}

func k8sAddMultiClusterIngress(t *testing.T, kc *crdfake.Clientset, name string, ns string, svc string,
	cname string, hostIPs map[string]string) *akov1alpha1.MultiClusterIngress {

	ingObj := buildMultiClusterIngressObj(name, ns, svc, cname, hostIPs, true)
	_, err := kc.AkoV1alpha1().MultiClusterIngresses(ns).Create(context.TODO(), ingObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating Multi-cluster ingress: %v", err)
	}

	return ingObj
}

func k8sAddTLSMultiClusterIngress(t *testing.T, k8sCs *k8sfake.Clientset, cs *crdfake.Clientset, name string, ns string, svc string,
	cname string, hostIPs map[string]string) *akov1alpha1.MultiClusterIngress {

	var hosts []string
	secretObj := buildk8sSecret(ns)
	_, err := k8sCs.CoreV1().Secrets(ns).Create(context.TODO(), secretObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating secret: %v", err)
	}
	ingObj := buildMultiClusterIngressObj(name, ns, svc, cname, hostIPs, true)
	for h := range hostIPs {
		hosts = append(hosts, h)
	}
	ingObj.Spec.SecretName = secretObj.Name
	_, err = cs.AkoV1alpha1().MultiClusterIngresses(ns).Create(context.TODO(), ingObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating Multi-cluster ingress: %v", err)
	}

	return ingObj
}

func k8sDeleteTLSMultiClusterIngress(t *testing.T, k8sCs *k8sfake.Clientset, cs *crdfake.Clientset, ingName, ns string) {
	err := cs.AkoV1alpha1().MultiClusterIngresses(ns).Delete(context.TODO(), ingName, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Error in deleting Multi-cluster ingress: %v", err)
	}
	err = k8sCs.CoreV1().Secrets(ns).Delete(context.TODO(), "test-secret", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Error in deleting secret: %v", err)
	}
}

func k8sAddMultiClusterIngressWithoutStatus(t *testing.T, kc *crdfake.Clientset, name string, ns string, svc string,
	cname string, hostIPs map[string]string) *akov1alpha1.MultiClusterIngress {

	ingObj := buildMultiClusterIngressObj(name, ns, svc, cname, hostIPs, false)
	_, err := kc.AkoV1alpha1().MultiClusterIngresses(ns).Create(context.TODO(), ingObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating Multi-cluster ingress: %v", err)
	}

	return ingObj
}

func buildMultiClusterIngressObj(name, ns, svc, cname string, hostIPs map[string]string, withStatus bool) *akov1alpha1.MultiClusterIngress {
	ingObj := &akov1alpha1.MultiClusterIngress{}
	ingObj.Namespace = ns
	ingObj.Name = name
	ingObj.ResourceVersion = "100"

	for ingHost, ingIP := range hostIPs {
		ingObj.Spec.Hostname = ingHost
		if !withStatus {
			continue
		}
		ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, akov1alpha1.IngressStatus{
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
func verifyInMultiClusterIngressStore(g *gomega.WithT, accepted bool, present bool, ingName, ns, cname, host, ip string) {
	var cs *store.ClusterStore
	if accepted {
		cs = store.GetAcceptedMultiClusterIngressStore()
	} else {
		cs = store.GetRejectedMultiClusterIngressStore()
	}
	ihmObjName := ingName + "/" + host
	obj, found := cs.GetClusterNSObjectByName(cname, ns, ihmObjName)
	g.Expect(found).To(gomega.Equal(present))
	if present {
		ihm := obj.(k8sobjects.MultiClusterIngressHostMeta)
		// If we are expecting that the object is present in the store, then check the required fields
		g.Expect(ihm.Hostname).To(gomega.Equal(host))
		g.Expect(ihm.IPAddr).To(gomega.Equal(ip))
	}
}
