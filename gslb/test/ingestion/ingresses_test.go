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
	gslbingestion "amko/gslb/ingestion"
	gslbalphav1 "amko/pkg/apis/avilb/v1alpha1"
	"fmt"
	"testing"

	"github.com/avinetworks/container-lib/utils"
	corev1 "k8s.io/api/core/v1"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func addGDPAndGSLB(t *testing.T) {
	ingestionQ := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	gdp := getTestGDPObject(false, true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp)
	gslbingestion.AddGDPObj(gdp, ingestionQ.Workqueue, 2)

	gslbObj := getTestGSLBObject()
	gc, err := gslbingestion.IsGSLBConfigValid(gslbObj)
	if err != nil {
		t.Fatal("GSLB object invalid")
	}
	addGSLBTestConfigObject(gc)
}

// TestBasicIngress: Create/Delete
func TestBasicIngressCD(t *testing.T) {
	testPrefix := "cd-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr

	addGDPAndGSLB(t)
	// Add and test ingresses
	fmt.Println("Adding and testing ingresses")
	k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)

	// delete and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host)
	return
}

func TestBasicIngressCUD(t *testing.T) {
	testPrefix := "cud-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	host := testPrefix + TestDomain1
	ipAddr := "10.10.10.20"
	cname := "cluster1"

	ingHostIPMap := make(map[string]string)
	ingHostIPMap[host] = ipAddr
	addGDPAndGSLB(t)

	// Add and test ingresses
	fmt.Println("Adding and testing ingresses")
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, ingHostIPMap)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host)

	newHost := testPrefix + TestDomain2
	ingObj.Spec.Rules[0].Host = newHost
	ingObj.Status.LoadBalancer.Ingress[0].Hostname = newHost
	ingObj.ResourceVersion = "101"

	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingObj.Name, host)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingObj.Name, newHost)

	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, newHost)
	return
}

func TestMultihostIngressCD(t *testing.T) {
	testPrefix := "mhcd-"
	ingName := testPrefix + "def-ing"
	ns := "default"
	cname := "cluster1"

	hostIPMap := make(map[string]string)
	hostIPMap[testPrefix+TestDomain1] = "10.10.10.10"
	hostIPMap[testPrefix+TestDomain2] = "10.10.10.20"

	addGDPAndGSLB(t)
	k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, hostIPMap)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, hostIPMap)

	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, hostIPMap)
}

func TestMultihostIngressCUD(t *testing.T) {
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

	addGDPAndGSLB(t)
	ingObj := k8sAddIngress(t, fooKubeClient, ingName, ns, TestSvc, cname, hostIPMap)
	buildIngMultiHostKeyAndVerify(t, false, "ADD", cname, ns, ingName, hostIPMap)

	idx := 0
	// replace the hostname host1
	for i, ingRule := range ingObj.Spec.Rules {
		if ingRule.Host == host1 {
			idx = i
			break
		}
	}
	ingObj.Spec.Rules[idx].Host = host3

	// also, replace the status
	for i, ing := range ingObj.Status.LoadBalancer.Ingress {
		if ing.Hostname == host1 {
			idx = i
			break
		}
	}
	ingObj.Status.LoadBalancer.Ingress[idx].Hostname = host3
	ingObj.Status.LoadBalancer.Ingress[idx].IP = "10.10.10.30"
	delete(hostIPMap, host1)
	hostIPMap[host3] = "10.10.10.30"
	ingObj.ResourceVersion = "101"

	fmt.Println("updating ingress")
	// update the ingress and verify
	k8sUpdateIngress(t, fooKubeClient, ns, cname, ingObj)

	// first key should be the DELETE key for host1 and then an ADD key for host3
	buildIngressKeyAndVerify(t, false, "DELETE", cname, ns, ingName, host1)
	buildIngressKeyAndVerify(t, false, "ADD", cname, ns, ingName, host3)

	// delete the ingress and verify
	k8sDeleteIngress(t, fooKubeClient, ingName, ns)
	buildIngMultiHostKeyAndVerify(t, false, "DELETE", cname, ns, ingName, hostIPMap)
}

func k8sUpdateIngress(t *testing.T, kc *k8sfake.Clientset, ns, cname string,
	ingObj *extensionv1beta1.Ingress) {

	_, err := kc.ExtensionsV1beta1().Ingresses(ns).Update(ingObj)
	if err != nil {
		t.Fatalf("failed to update ingress: %v\n", err)
	}
}

func k8sDeleteIngress(t *testing.T, kc *k8sfake.Clientset, name, ns string) {
	err := kc.ExtensionsV1beta1().Ingresses(ns).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in deleting ingress: %v", err)
	}
}

func k8sAddIngress(t *testing.T, kc *k8sfake.Clientset, name string, ns string, svc string,
	cname string, hostIPs map[string]string) *extensionv1beta1.Ingress {

	ingObj := &extensionv1beta1.Ingress{}
	ingObj.Namespace = ns
	ingObj.Name = name
	ingObj.ResourceVersion = "100"

	for ingHost, ingIP := range hostIPs {
		ingObj.Spec.Rules = append(ingObj.Spec.Rules, extensionv1beta1.IngressRule{
			Host: ingHost,
		})
		ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
			IP:       ingIP,
			Hostname: ingHost,
		})
	}
	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	ingObj.Labels = labelMap

	_, err := kc.ExtensionsV1beta1().Ingresses(ns).Create(ingObj)
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}

	return ingObj
}
