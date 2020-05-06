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
	"amko/gslb/gslbutils"
	gslbingestion "amko/gslb/ingestion"
	gslbalphav1 "amko/pkg/apis/avilb/v1alpha1"
	gslbfake "amko/pkg/client/clientset/versioned/fake"
	gslbinformers "amko/pkg/client/informers/externalversions"
	"testing"
	"time"

	"github.com/avinetworks/container-lib/utils"
	"github.com/onsi/gomega"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
)

// Test the GDP controller initialization.
func TestGDPNewController(t *testing.T) {
	gdpKubeClient := k8sfake.NewSimpleClientset()
	gdpClient := gslbfake.NewSimpleClientset()
	gdpInformerFactory := gslbinformers.NewSharedInformerFactory(gdpClient, time.Second*30)
	gdpCtrl := gslbingestion.InitializeGDPController(gdpKubeClient, gdpClient, gdpInformerFactory, addSomething, updateSomething,
		addSomething)
	if gdpCtrl == nil {
		t.Fatalf("GDP controller not set")
	}
}

// addSomething is a dummy function used to initialize the GDP controller
func addSomething(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {

}

// updateSomething is a dummy function used to initialize the GDP controller
func updateSomething(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {

}

func updateTestGDPObject(gdp *gslbalphav1.GlobalDeploymentPolicy, clusterList []string, version string) {
	var matchClusters []gslbalphav1.MemberCluster
	for _, cname := range clusterList {
		member := gslbalphav1.MemberCluster{
			ClusterContext: cname,
		}
		matchClusters = append(matchClusters, member)
	}

	gdp.Spec.MatchClusters = matchClusters
	gdp.ObjectMeta.ResourceVersion = version
}

func setAndGetHostMap(host, ip string) map[string]string {
	ingHostMap := make(map[string]string)
	ingHostMap[host] = ip
	return ingHostMap
}

func buildAndAddTestGSLBObject(t *testing.T) {
	gslbObj := getTestGSLBObject()
	gc, err := gslbingestion.IsGSLBConfigValid(gslbObj)
	if err != nil {
		t.Fatal("GSLB object invalid")
	}
	addGSLBTestConfigObject(gc)
	// Add the initialized cluster list out of band
	gslbutils.AddClusterContext("cluster1")
	gslbutils.AddClusterContext("cluster2")
}

func TestGDPSelectAllObjsFromOneCluster(t *testing.T) {
	testPrefix := "sao-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2", testPrefix + "def-ing3"}
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2, testPrefix + TestDomain3}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11", "10.10.10.12"}
	cname := "cluster1"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)
	t.Log("Creating ingresses")
	ingList, allKeys := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	AddTestGDPObj(gdp)

	t.Logf("verifying keys")
	VerifyAllKeys(t, allKeys, false)

	t.Logf("Deleting ingresses")
	DeleteMultipleIngresses(t, fooKubeClient, ingList)
	DeleteTestGDPObj(gdp)
	// verify delete keys
	allKeys = []string{}
	for _, ing := range ingList {
		key := getIngressKey("DELETE", cname, ns, ing.ObjectMeta.Name, ing.Status.LoadBalancer.Ingress[0].Hostname)
		allKeys = append(allKeys, key)
	}
	allKeys = GetMultipleIngDeleteKeys(t, ingList, cname, ns)
	VerifyAllKeys(t, allKeys, false)
}

func TestGDPSelectFewObjsFromOneCluster(t *testing.T) {
	testPrefix := "sfo-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname := "cluster1"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)
	ingList, allKeys := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname)

	// add another ingress with a different label
	hostIPMap3 := make(map[string]string)
	hostIPMap3[testPrefix+TestDomain3] = "10.10.10.12"

	CreateIngressObjWithLabel(t, fooKubeClient, testPrefix+"def-ing3",
		ns, svc, cname, hostIPMap3, true, "key", "invalid-value")

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	AddTestGDPObj(gdp)

	t.Logf("verifying keys")
	VerifyAllKeys(t, allKeys, false)
	DeleteMultipleIngresses(t, fooKubeClient, ingList)
	DeleteTestGDPObj(gdp)

	// verify delete keys
	allKeys = GetMultipleIngDeleteKeys(t, ingList, cname, ns)
	VerifyAllKeys(t, allKeys, false)
}

func TestGDPSelectAllObjsFromAllClusters(t *testing.T) {
	testPrefix := "saoac-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2", testPrefix + "def-ing3"}
	// We can keep a single list of hosts and ipAddrs for both the clusters, as the ingestion layer
	// won't have a problem with this.
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2, testPrefix + TestDomain3}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11", "10.10.10.12"}
	cname1 := "cluster1"
	cname2 := "cluster2"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)
	t.Log("Creating ingresses")
	ingList1, allKeys1 := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname1)
	ingList2, allKeys2 := CreateMultipleIngresses(t, barKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname2)

	allKeys := append(allKeys1, allKeys2...)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	// add a matchRule for Ingress object with correct label
	UpdateGDPMatchRuleDefaultLabel(gdp, gslbalphav1.IngressObj, "key", "value")
	// Select both the clusters
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{{ClusterContext: "cluster1"}, {ClusterContext: "cluster2"}}

	AddTestGDPObj(gdp)

	t.Logf("verifying keys")
	VerifyAllKeys(t, allKeys, false)

	t.Logf("Deleting ingresses for cluster1")
	DeleteMultipleIngresses(t, fooKubeClient, ingList1)
	t.Logf("Deleting ingresses for cluster2")
	DeleteMultipleIngresses(t, barKubeClient, ingList2)
	DeleteTestGDPObj(gdp)

	// verify delete keys
	keys1 := GetMultipleIngDeleteKeys(t, ingList1, cname1, ns)
	keys2 := GetMultipleIngDeleteKeys(t, ingList2, cname2, ns)
	allKeys = append(keys1, keys2...)
	VerifyAllKeys(t, allKeys, false)
}

func TestGDPMisnameObjects(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mnc-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	// We can keep a single list of hosts and ipAddrs for both the clusters, as the ingestion layer
	// won't have a problem with this.
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname1 := "cluster1"
	cname2 := "cluster2"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)

	ingList1, allKeys1 := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname1)
	ingList2, allKeys2 := CreateMultipleIngresses(t, barKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname2)

	allKeys := append(allKeys1, allKeys2...)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	gdp.ObjectMeta.SetNamespace(ns)
	// add a matchRule for an invalid object with correct label
	UpdateGDPMatchRuleDefaultLabel(gdp, "InvalidObj", "key", "value")
	// Select both the clusters
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{{ClusterContext: cname1}, {ClusterContext: cname2}}

	AddTestGDPObj(gdp)

	t.Logf("verifying keys")
	VerifyAllKeys(t, allKeys, true)

	t.Logf("verifying GDP status")
	g.Expect(gdp.Status.ErrorStatus).To(gomega.Equal("unsupported object type InvalidObj"))

	t.Logf("Deleting ingresses for cluster1")
	DeleteMultipleIngresses(t, fooKubeClient, ingList1)
	t.Logf("Deleting ingresses for cluster2")
	DeleteMultipleIngresses(t, barKubeClient, ingList2)
	DeleteTestGDPObj(gdp)

	// no objects were added, so no need to verify for delete calls
	// verify delete keys
	keys1 := GetMultipleIngDeleteKeys(t, ingList1, cname1, ns)
	keys2 := GetMultipleIngDeleteKeys(t, ingList2, cname2, ns)
	allKeys = append(keys1, keys2...)
	VerifyAllKeys(t, allKeys, false)
}

func TestMultipleGDPObjectsForSameNS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mgo-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	// We can keep a single list of hosts and ipAddrs for both the clusters, as the ingestion layer
	// won't have a problem with this.
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname1 := "cluster1"
	cname2 := "cluster2"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)

	ingList1, allKeys1 := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname1)
	ingList2, allKeys2 := CreateMultipleIngresses(t, barKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname2)

	allKeys := append(allKeys1, allKeys2...)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	UpdateGDPMatchRuleDefaultLabel(gdp, gslbalphav1.IngressObj, "key", "value")
	// Select both the clusters
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{{ClusterContext: cname1}, {ClusterContext: cname2}}

	AddTestGDPObj(gdp)

	t.Logf("verifying keys")
	VerifyAllKeys(t, allKeys, false)

	t.Logf("verifying GDP status")
	g.Expect(gdp.Status.ErrorStatus).To(gomega.Equal("success"))

	// Let's add another GDP object
	anotherGdp := getTestGDPObject(false, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	anotherGdp.ObjectMeta.Name = "new-gdp"
	UpdateGDPMatchRuleDefaultLabel(anotherGdp, gslbalphav1.IngressObj, "key", "test")
	t.Logf("adding another gdp object")
	AddTestGDPObj(anotherGdp)

	// check the status of this new object
	g.Expect(anotherGdp.Status.ErrorStatus).To(gomega.Equal("a GDP object already exists for namespace " + ns))

	t.Logf("Deleting ingresses for cluster1")
	DeleteMultipleIngresses(t, fooKubeClient, ingList1)
	t.Logf("Deleting ingresses for cluster2")
	DeleteMultipleIngresses(t, barKubeClient, ingList2)
	DeleteTestGDPObj(gdp)
	DeleteTestGDPObj(anotherGdp)

	// verify delete keys
	keys1 := GetMultipleIngDeleteKeys(t, ingList1, cname1, ns)
	keys2 := GetMultipleIngDeleteKeys(t, ingList2, cname2, ns)
	allKeys = append(keys1, keys2...)
	VerifyAllKeys(t, allKeys, false)
}

func TestUpdateGDPSelectFew(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mgo-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	// We can keep a single list of hosts and ipAddrs for both the clusters, as the ingestion layer
	// won't have a problem with this.
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname1 := "cluster1"
	cname2 := "cluster2"
	ns := "default"
	svc := "test-svc"

	extIngName := testPrefix + "def-ing3"
	extHost := testPrefix + TestDomain3
	extIPAddr := "10.10.10.12"
	extHostMap := make(map[string]string)
	extHostMap[extHost] = extIPAddr

	buildAndAddTestGSLBObject(t)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	// "key":"value1" won't select any objects.
	UpdateGDPMatchRuleDefaultLabel(gdp, gslbalphav1.IngressObj, "key", "value1")
	// Select both the clusters
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{{ClusterContext: cname1}, {ClusterContext: cname2}}
	AddTestGDPObj(gdp)

	// Adding the ingreeses
	ingList1, _ := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname1)
	CreateIngressObjWithLabel(t, fooKubeClient, extIngName, ns, svc, cname1, extHostMap, true, "key", "test")

	ingList2, _ := CreateMultipleIngresses(t, barKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname2)
	CreateIngressObjWithLabel(t, barKubeClient, extIngName, ns, svc, cname2, extHostMap, true, "key", "test")

	allKeys := []string{getIngressKey("ADD", cname1, ns, extIngName, extHost),
		getIngressKey("ADD", cname2, ns, extIngName, extHost)}

	// At this point, there will not be any keys that will be added, as two ingresses in each cluster have
	// label: "key": "value" and one ingress in each cluster has label: "key": "test". The GDP object
	// has a rule only for "key": "value1", hence should not select anything.

	// Let's now update the GDP object
	oldGdp := gdp.DeepCopy()
	UpdateGDPMatchRuleDefaultLabel(gdp, gslbalphav1.IngressObj, "key", "test")
	gdp.ResourceVersion = "101"
	UpdateTestGDPObj(oldGdp, gdp)

	// Now, there should be two keys for one additonal ingresses added for each cluster (ing3)
	t.Logf("verifying keys")
	VerifyAllKeys(t, allKeys, false)

	t.Logf("verifying GDP status")
	g.Expect(gdp.Status.ErrorStatus).To(gomega.Equal("success"))

	t.Logf("Deleting ingresses for cluster1")
	DeleteMultipleIngresses(t, fooKubeClient, ingList1)
	k8sDeleteIngress(t, fooKubeClient, extIngName, ns)
	t.Logf("Deleting ingresses for cluster2")
	DeleteMultipleIngresses(t, barKubeClient, ingList2)
	k8sDeleteIngress(t, barKubeClient, extIngName, ns)
	DeleteTestGDPObj(gdp)

	// verify delete keys
	keys1 := GetMultipleIngDeleteKeys(t, ingList1, cname1, ns)
	keys2 := GetMultipleIngDeleteKeys(t, ingList2, cname2, ns)
	allKeys = append(keys1, keys2...)
	extraKeys := []string{getIngressKey("DELETE", cname1, ns, extIngName, extHost),
		getIngressKey("DELETE", cname2, ns, extIngName, extHost)}
	delKeys := append(allKeys, extraKeys...)
	VerifyAllKeys(t, delKeys, false)
}

func TestUpdateGDPSelectFromOneCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "sfoc-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	// We can keep a single list of hosts and ipAddrs for both the clusters, as the ingestion layer
	// won't have a problem with this.
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname1 := "cluster1"
	cname2 := "cluster2"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	UpdateGDPMatchRuleDefaultLabel(gdp, gslbalphav1.IngressObj, "key", "value")

	// Empty matchClusters, don't select any cluster
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{}

	AddTestGDPObj(gdp)

	ingList1, keys1 := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname1)

	ingList2, _ := CreateMultipleIngresses(t, barKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname2)

	// Let's now update the GDP object
	oldGdp := gdp.DeepCopy()
	gdp.ResourceVersion = "101"
	// Only select cluster 1
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{{ClusterContext: "cluster1"}}
	UpdateTestGDPObj(oldGdp, gdp)

	// Now, there should be two keys, both from cluster1 ingress objects
	t.Logf("verifying keys")
	VerifyAllKeys(t, keys1, false)

	t.Logf("verifying GDP status")
	g.Expect(gdp.Status.ErrorStatus).To(gomega.Equal("success"))

	t.Logf("Deleting ingresses for cluster1")
	DeleteMultipleIngresses(t, fooKubeClient, ingList1)
	t.Logf("Deleting ingresses for cluster2")
	DeleteMultipleIngresses(t, barKubeClient, ingList2)
	DeleteTestGDPObj(gdp)

	// verify delete keys
	keys1 = GetMultipleIngDeleteKeys(t, ingList1, cname1, ns)
	keys2 := GetMultipleIngDeleteKeys(t, ingList2, cname2, ns)
	allKeys := append(keys1, keys2...)
	VerifyAllKeys(t, allKeys, false)
}

func TestUpdateGDPSwitchClusters(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "swc-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	// We can keep a single list of hosts and ipAddrs for both the clusters, as the ingestion layer
	// won't have a problem with this.
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname1 := "cluster1"
	cname2 := "cluster2"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	UpdateGDPMatchRuleDefaultLabel(gdp, gslbalphav1.IngressObj, "key", "value")
	// Select only one cluster
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{{ClusterContext: cname1}}

	AddTestGDPObj(gdp)

	// Create ingresses for both the clusters
	ingList1, keys1 := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname1)

	ingList2, keys2 := CreateMultipleIngresses(t, barKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname2)

	// Only cluster1 has been added in the GDP, so we will verify keys only for that cluster
	t.Logf("verifying cluster 1 keys")
	VerifyAllKeys(t, keys1, false)

	// Let's now update the GDP object
	oldGdp := gdp.DeepCopy()
	gdp.ResourceVersion = "101"
	// Only select cluster 1
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{{ClusterContext: "cluster2"}}
	UpdateTestGDPObj(oldGdp, gdp)

	// Now, cluster 2 keys should be added, and cluster 1 keys should be deleted
	delKeys := []string{}
	for _, ing := range ingList1 {
		delKeys = append(delKeys, getIngressKey("DELETE", cname1, ns, ing.ObjectMeta.Name,
			ing.Status.LoadBalancer.Ingress[0].Hostname))
	}
	allKeys := append(delKeys, keys2...)
	t.Logf("verifying cluster 2 keys")
	VerifyAllKeys(t, allKeys, false)

	t.Logf("verifying GDP status")
	g.Expect(gdp.Status.ErrorStatus).To(gomega.Equal("success"))

	t.Logf("Deleting ingresses for cluster1")
	DeleteMultipleIngresses(t, fooKubeClient, ingList1)
	t.Logf("Deleting ingresses for cluster2")
	DeleteMultipleIngresses(t, barKubeClient, ingList2)
	DeleteTestGDPObj(gdp)

	// verify delete keys
	keys1 = GetMultipleIngDeleteKeys(t, ingList1, cname1, ns)
	keys2 = GetMultipleIngDeleteKeys(t, ingList2, cname2, ns)
	allKeys = append(keys1, keys2...)
	VerifyAllKeys(t, allKeys, false)
}

func TestGDPMisnameClusters(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testPrefix := "mnc-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	// We can keep a single list of hosts and ipAddrs for both the clusters, as the ingestion layer
	// won't have a problem with this.
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname1 := "cluster1"
	cname2 := "cluster2"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)
	t.Log("Creating ingresses")
	ingList1, allKeys1 := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname1)
	ingList2, allKeys2 := CreateMultipleIngresses(t, barKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname2)

	allKeys := append(allKeys1, allKeys2...)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	gdp.ObjectMeta.SetNamespace(ns)
	// add a matchRule for Ingress object with correct label
	UpdateGDPMatchRuleDefaultLabel(gdp, gslbalphav1.IngressObj, "key", "value")
	// Select both the clusters
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{{ClusterContext: "abc"}, {ClusterContext: "xyz"}}

	AddTestGDPObj(gdp)

	t.Logf("verifying keys")
	VerifyAllKeys(t, allKeys, true)

	t.Logf("verifying status message")
	g.Expect(gdp.Status.ErrorStatus).To(gomega.Equal("cluster context abc not present in GSLBConfig"))

	t.Logf("Deleting ingresses for cluster1")
	DeleteMultipleIngresses(t, fooKubeClient, ingList1)
	t.Logf("Deleting ingresses for cluster2")
	DeleteMultipleIngresses(t, barKubeClient, ingList2)
	DeleteTestGDPObj(gdp)

	// verify delete keys
	keys1 := GetMultipleIngDeleteKeys(t, ingList1, cname1, ns)
	keys2 := GetMultipleIngDeleteKeys(t, ingList2, cname2, ns)
	allKeys = append(keys1, keys2...)
	VerifyAllKeys(t, allKeys, false)
}

func TestGDPSelectNoClusters(t *testing.T) {
	testPrefix := "snc-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname := "cluster1"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)

	t.Log("Creating ingresses")
	ingList, allKeys := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)
	gdp.ObjectMeta.SetNamespace(ns)

	// add a matchRule for Ingress object with correct label
	for idx, matchRule := range gdp.Spec.MatchRules {
		if matchRule.Object == "INGRESS" {
			gdp.Spec.MatchRules[idx].Label.Value = "value"
			gdp.Spec.MatchRules[idx].Hosts = []gslbalphav1.Host{}
		}
	}
	// But, remove the match clusters from the spec
	gdp.Spec.MatchClusters = []gslbalphav1.MemberCluster{}

	t.Logf("gdp object: %v", gdp)
	ingestionQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	gslbingestion.AddGDPObj(gdp, ingestionQueue.Workqueue, 2)

	t.Logf("verifying keys")
	for range allKeys {
		// Have to verify for all keys, since no order is guranteed
		passed, errStr := waitAndVerify(t, allKeys, true)
		if !passed {
			t.Fatalf(errStr)
		}
	}

	DeleteMultipleIngresses(t, fooKubeClient, ingList)
	DeleteTestGDPObj(gdp)

	// verify delete keys
	keys1 := GetMultipleIngDeleteKeys(t, ingList, cname, ns)
	VerifyAllKeys(t, keys1, false)
}

func TestGDPSelectNoneObjsFromOneCluster(t *testing.T) {
	testPrefix := "sno-"
	ingNameList := []string{testPrefix + "def-ing1", testPrefix + "def-ing2"}
	hosts := []string{testPrefix + TestDomain1, testPrefix + TestDomain2}
	ipAddrs := []string{"10.10.10.10", "10.10.10.11"}
	cname := "cluster1"
	ns := "default"
	svc := "test-svc"

	buildAndAddTestGSLBObject(t)

	t.Log("Creating ingresses")
	ingList, allKeys := CreateMultipleIngresses(t, fooKubeClient, ingNameList, hosts, ipAddrs, ns, svc, cname)

	t.Logf("Adding GDP object")
	gdp := getTestGDPObject(true, gslbalphav1.IngressObj, gslbalphav1.EqualsOp, ns)

	// add a matchRule for Ingress object with wrong label
	UpdateGDPMatchRuleDefaultLabel(gdp, gslbalphav1.IngressObj, "key", "value1")

	t.Logf("gdp object: %v", gdp)
	ingestionQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	gslbingestion.AddGDPObj(gdp, ingestionQueue.Workqueue, 2)

	t.Logf("verifying keys")
	VerifyAllKeys(t, allKeys, true)

	t.Logf("Deleting ingresses")
	DeleteMultipleIngresses(t, fooKubeClient, ingList)
	DeleteTestGDPObj(gdp)

	// verify delete keys
	keys1 := GetMultipleIngDeleteKeys(t, ingList, cname, ns)
	VerifyAllKeys(t, keys1, false)
}

func CreateMultipleIngresses(t *testing.T, kc *k8sfake.Clientset, ingNameList, hosts, ipAddrs []string, ns, svc, cname string) ([]*extensionv1beta1.Ingress, []string) {
	g := gomega.NewGomegaWithT(t)
	g.Expect(len(ingNameList)).To(gomega.Equal(len(hosts)))
	g.Expect(len(hosts)).To(gomega.Equal(len(ipAddrs)))

	t.Logf("Creating ingresses %v", ingNameList)
	ingList := []*extensionv1beta1.Ingress{}
	allKeys := []string{}

	for idx, ingName := range ingNameList {
		ingHostIPMap := setAndGetHostMap(hosts[idx], ipAddrs[idx])
		ingObj := k8sAddIngress(t, kc, ingName, ns, svc, cname, ingHostIPMap)
		ingList = append(ingList, ingObj)
		allKeys = append(allKeys, getIngressKey("ADD", cname, ns, ingName, hosts[idx]))
	}
	return ingList, allKeys
}

func DeleteMultipleIngresses(t *testing.T, kc *k8sfake.Clientset, ingList []*extensionv1beta1.Ingress) {
	for _, ingObj := range ingList {
		k8sDeleteIngress(t, kc, ingObj.ObjectMeta.Name, ingObj.ObjectMeta.Namespace)
	}
}

func UpdateGDPMatchRuleDefaultLabel(gdp *gslbalphav1.GlobalDeploymentPolicy, obj, key, value string) {
	for idx, matchRule := range gdp.Spec.MatchRules {
		if matchRule.Object == obj {
			gdp.Spec.MatchRules[idx].Label.Key = key
			gdp.Spec.MatchRules[idx].Label.Value = value
			return
		}
	}
	// No match found, add a new rule
	gdp.Spec.MatchRules = append(gdp.Spec.MatchRules, gslbalphav1.MatchRule{
		Op:     gslbalphav1.EqualsOp,
		Object: obj,
		Label: gslbalphav1.Label{
			Key:   key,
			Value: value,
		},
	})
}

func AddTestGDPObj(gdp *gslbalphav1.GlobalDeploymentPolicy) {
	ingestionQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	gslbingestion.AddGDPObj(gdp, ingestionQueue.Workqueue, 2)
}

func VerifyAllKeys(t *testing.T, allKeys []string, timeoutExpected bool) {
	for range allKeys {
		// Have to verify for all keys, since no order is guranteed
		passed, errStr := waitAndVerify(t, allKeys, timeoutExpected)
		if !passed {
			t.Fatalf(errStr)
		}
	}
}

func DeleteTestGDPObj(gdp *gslbalphav1.GlobalDeploymentPolicy) {
	ingestionQ := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	gslbingestion.DeleteGDPObj(gdp, ingestionQ.Workqueue, 2)
}

func UpdateTestGDPObj(oldGdp, gdp *gslbalphav1.GlobalDeploymentPolicy) {
	ingestionQ := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	gslbingestion.UpdateGDPObj(oldGdp, gdp, ingestionQ.Workqueue, 2)
}

func CreateIngressObjWithLabel(t *testing.T, kc *k8sfake.Clientset, name, ns, svc, cname string, hostIPs map[string]string, withStatus bool, labelKey, labelValue string) *extensionv1beta1.Ingress {
	ingObj := buildIngressObj(name, ns, svc, cname, hostIPs, withStatus)
	ingObj.Labels[labelKey] = labelValue
	_, err := kc.ExtensionsV1beta1().Ingresses(ns).Create(ingObj)
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}
	return ingObj
}

func GetMultipleIngDeleteKeys(t *testing.T, ingList []*extensionv1beta1.Ingress, cname, ns string) []string {
	allKeys := []string{}
	for _, ing := range ingList {
		key := getIngressKey("DELETE", cname, ns, ing.ObjectMeta.Name, ing.Status.LoadBalancer.Ingress[0].Hostname)
		allKeys = append(allKeys, key)
	}
	return allKeys
}
