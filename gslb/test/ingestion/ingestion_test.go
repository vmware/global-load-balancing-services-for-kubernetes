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
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"time"

	containerutils "github.com/avinetworks/container-lib/utils"
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"k8s.io/client-go/util/workqueue"

	gslbingestion "amko/gslb/ingestion"
	gslbalphav1 "amko/pkg/apis/avilb/v1alpha1"
	gslbfake "amko/pkg/client/clientset/versioned/fake"
	gslbinformers "amko/pkg/client/informers/externalversions"

	oshiftfake "github.com/openshift/client-go/route/clientset/versioned/fake"
)

var (
	kubeClient      *k8sfake.Clientset
	keyChan         chan string
	oshiftClient    *oshiftfake.Clientset
	fooOshiftClient *oshiftfake.Clientset
	barOshiftClient *oshiftfake.Clientset
	testStopCh      <-chan struct{}
	gslbClient      *gslbfake.Clientset
)

const kubeConfigPath = "/tmp/gslb-kubeconfig"

func syncFuncForTest(key string) error {
	keyChan <- key
	return nil
}

func setupQueue(testStopCh <-chan struct{}) {
	ingestionQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	ingestionQueue.SyncFunc = syncFuncForTest
	ingestionQueue.Run(testStopCh)
}

func TestMain(m *testing.M) {
	setUp()
	ret := m.Run()
	os.Exit(ret)
}

type GSLBTestConfigAddfn func(obj interface{})

func addGSLBTestConfigObject(obj interface{}) {
	// Initialize a foo kube client
	fooKubeClient := k8sfake.NewSimpleClientset()
	fooOshiftClient = oshiftfake.NewSimpleClientset()
	fooInformersArg := make(map[string]interface{})
	fooInformersArg[containerutils.INFORMERS_OPENSHIFT_CLIENT] = fooOshiftClient
	fooInformersArg[containerutils.INFORMERS_INSTANTIATE_ONCE] = false

	fooRegisteredInformers := []string{containerutils.RouteInformer}
	fooInformerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{fooKubeClient}, fooRegisteredInformers, fooInformersArg)
	fooCtrl := gslbingestion.GetGSLBMemberController("cluster1", fooInformerInstance)
	fooCtrl.Start(testStopCh)
	fooCtrl.SetupEventHandlers(gslbingestion.K8SInformers{fooKubeClient})

	// Initialize a bar kube client
	barKubeClient := k8sfake.NewSimpleClientset()
	barOshiftClient = oshiftfake.NewSimpleClientset()
	barInformersArg := make(map[string]interface{})
	barRegisteredInformers := []string{containerutils.RouteInformer}
	barInformersArg[containerutils.INFORMERS_OPENSHIFT_CLIENT] = barOshiftClient
	barInformersArg[containerutils.INFORMERS_INSTANTIATE_ONCE] = false
	barInformerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{barKubeClient}, barRegisteredInformers, barInformersArg)
	barCtrl := gslbingestion.GetGSLBMemberController("cluster2", barInformerInstance)
	barCtrl.Start(testStopCh)
	barCtrl.SetupEventHandlers(gslbingestion.K8SInformers{barKubeClient})
}

func setUp() {
	testStopCh = containerutils.SetupSignalHandler()
	keyChan = make(chan string)

	setupQueue(testStopCh)
}

// Unit test to create a new GSLB client, a kube client and see if a GSLB controller can be created
// using these.
func TestGSLBNewController(t *testing.T) {
	gslbKubeClient := k8sfake.NewSimpleClientset()
	gslbClient := gslbfake.NewSimpleClientset()
	gslbInformerFactory := gslbinformers.NewSharedInformerFactory(gslbClient, time.Second*30)
	gslbCtrl := gslbingestion.GetNewController(gslbKubeClient, gslbClient, gslbInformerFactory, addGSLBTestConfigObject)
	if gslbCtrl == nil {
		t.Fatalf("GSLB Controller not set")
	}
	go gslbCtrl.Run(gslbingestion.GetStopChannel())
}

// Unit test to see if a kube config can be generated from a encoded secret.
func TestGSLBKubeConfig(t *testing.T) {
	kubeconfigData, err := ioutil.ReadFile("./testdata/test-kube-config")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GSLB_CONFIG", string(kubeconfigData))
	err = gslbingestion.GenerateKubeConfig()
	if err != nil {
		t.Fatalf("Failure in generating GSLB Kube config: %s", err.Error())
	}
}

// Test the initialization of the member clusters.
func TestMemberClusters(t *testing.T) {
	clusterContexts := []string{"dev-default", "exp-scratch"}
	memberClusters1 := make([]gslbalphav1.MemberCluster, 2)
	for idx, clusterContext := range clusterContexts {
		memberClusters1[idx].ClusterContext = clusterContext
	}
	gslbingestion.SetInformerListTimeout(1)
	aviCtrlList := gslbingestion.InitializeGSLBClusters(kubeConfigPath, memberClusters1)
	ctrlCount := 0
	for _, ctrl := range aviCtrlList {
		for _, ctx := range clusterContexts {
			if ctrl.GetName() == ctx {
				ctrlCount++
			}
		}
	}
	if ctrlCount != 2 {
		t.Fatalf("Unexpected cluster controller set")
	}

	memberClusters2 := make([]gslbalphav1.MemberCluster, 2)
	clusterContexts = []string{"fooCluster", "barCluster"}
	for idx, clusterContext := range clusterContexts {
		memberClusters2[idx].ClusterContext = clusterContext
	}
	aviCtrlList = gslbingestion.InitializeGSLBClusters(kubeConfigPath, memberClusters2)
	if len(aviCtrlList) != 0 {
		t.Fatalf("Unexpected cluster controller set")
	}
}

func waitAndVerify(t *testing.T, key string, timeoutExpected bool) (bool, string) {
	waitChan := make(chan interface{})
	go func() {
		time.Sleep(10 * time.Second)
		waitChan <- 1
	}()

	select {
	case data := <-keyChan:
		fmt.Printf("Expected key: %s, got data: %s\n", key, data)
		if timeoutExpected {
			// If the timeout is expected, then there shouldn't be anything on this channel
			if data != "" {
				errMsg := "Unexpected data: %s" + data
				return false, errMsg
			}
		}
		if data != key {
			errMsg := "key match error, expected: " + key + ", got: " + data
			return false, errMsg
		}
	case _ = <-waitChan:
		fmt.Println("waiting for timeout")
		if timeoutExpected {
			return true, "Success"
		}
		return false, "timed out waiting for " + key
	}
	return true, ""
}

func addAndTestRoute(t *testing.T, oc *oshiftfake.Clientset, name string, ns string, host string, svc string, ip string, timeoutExpected bool, cname string) (bool, string) {
	actualKey := "ADD/Route/" + cname + "/" + ns + "/" + name
	routeStatus := make([]routev1.RouteIngress, 2)
	conditions := make([]routev1.RouteIngressCondition, 2)
	conditions[0].Message = ip
	routeStatus[0].Conditions = conditions
	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	routeExample := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "100",
			Labels:          labelMap,
		},
		Spec: routev1.RouteSpec{
			Host: host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: svc,
			},
		},
		Status: routev1.RouteStatus{
			Ingress: routeStatus,
		},
	}
	_, err := oc.RouteV1().Routes(ns).Create(routeExample)
	if err != nil {
		t.Fatalf("error in adding route: %v", err)
	}
	return waitAndVerify(t, actualKey, timeoutExpected)
}

func updateAndTestRoute(t *testing.T, oc *oshiftfake.Clientset, name, ns, host, svc, ip, cname string, timeoutExpected bool) (bool, string) {
	actualKey := "UPDATE/Route/" + cname + "/" + ns + "/" + name
	routeStatus := make([]routev1.RouteIngress, 2)
	conditions := make([]routev1.RouteIngressCondition, 2)
	conditions[0].Message = ip
	routeStatus[0].Conditions = conditions
	routeExample := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "101",
		},
		Spec: routev1.RouteSpec{
			Host: host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: svc,
			},
		},
		Status: routev1.RouteStatus{
			Ingress: routeStatus,
		},
	}

	_, err := oc.RouteV1().Routes(ns).Update(routeExample)
	if err != nil {
		t.Fatalf("error in updating route: %v", err)
	}
	return waitAndVerify(t, actualKey, timeoutExpected)
}

// addSomething is a dummy function used to initialize the GDP controller
func addSomething(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {

}

// updateSomething is a dummy function used to initialize the GDP controller
func updateSomething(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {

}

// Test the GSP controller initialization.
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

func getTestGDPObject() *gslbalphav1.GlobalDeploymentPolicy {
	hosts := []gslbalphav1.Host{
		gslbalphav1.Host{
			HostName: "*.avi.com",
		},
		gslbalphav1.Host{
			HostName: "cluster1",
		},
	}
	label := gslbalphav1.Label{
		Key:   "key",
		Value: "value",
	}
	matchRules := []gslbalphav1.MatchRule{
		gslbalphav1.MatchRule{
			Object: gslbalphav1.RouteObj,
			Hosts:  hosts,
			Op:     gslbalphav1.GlobOp,
			Label:  label,
		},
	}
	matchClusters := []gslbalphav1.MemberCluster{
		gslbalphav1.MemberCluster{
			ClusterContext: "cluster1",
		},
		gslbalphav1.MemberCluster{
			ClusterContext: "cluster2",
		},
	}
	gdpSpec := gslbalphav1.GDPSpec{
		MatchRules:    matchRules,
		MatchClusters: matchClusters,
	}
	gdpMeta := metav1.ObjectMeta{
		Name:            "test-gdp-1",
		Namespace:       "avi-system",
		ResourceVersion: "100",
	}
	gdp := gslbalphav1.GlobalDeploymentPolicy{
		ObjectMeta: gdpMeta,
		Spec:       gdpSpec,
	}
	return &gdp
}

func getTestGSLBObject() *gslbalphav1.GSLBConfig {
	memberClusters := []gslbalphav1.MemberCluster{
		gslbalphav1.MemberCluster{
			ClusterContext: "cluster1",
		},
		gslbalphav1.MemberCluster{
			ClusterContext: "cluster2",
		},
	}
	gslbConfigObj := &gslbalphav1.GSLBConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "avi-system",
			Name:            "gslb-config-1",
			ResourceVersion: "10",
		},
		Spec: gslbalphav1.GSLBConfigSpec{
			GSLBLeader:     gslbalphav1.GSLBLeader{"", "", ""},
			MemberClusters: memberClusters,
			GSLBNameSource: "hostname",
			DomainNames:    []string{},
		},
	}
	return gslbConfigObj
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

// TestGSLBAndGDPWithRoutes adds a GDP, a GSLB config and routes.
func TestGSLBAndGDPWithRoutes(t *testing.T) {
	ingestionQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)

	gdp := getTestGDPObject()
	gslbingestion.AddGDPObj(gdp, ingestionQueue.Workqueue, 2)

	gslbObj := getTestGSLBObject()
	gc, err := gslbingestion.IsGSLBConfigValid(gslbObj)
	if err != nil {
		t.Fatal("GSLB object invalid")
	}
	addGSLBTestConfigObject(gc)
	// Add and test routes
	fmt.Println("adding and testing routes")
	// We expect a success for these routes.
	addAndTestRoute(t, fooOshiftClient, "foo-def-route1", "default", "foo-host1.avi.com", "foo-svc", "10.10.10.10", false, "cluster1")
	addAndTestRoute(t, barOshiftClient, "bar-def-route1", "default", "bar-host1.avi.com", "bar-svc", "10.10.10.10", false, "cluster2")
	// Remove cluster2 from the cluster list of the GDP object.
	clusterList := []string{"cluster1"}
	oldGdp := gdp.DeepCopy()
	updateTestGDPObject(gdp, clusterList, "101")
	gslbingestion.UpdateGDPObj(oldGdp, gdp, ingestionQueue.Workqueue, 2)
	waitAndVerify(t, "DELETE/Route/cluster2/default/bar-def-route1", false)
	// We expect a reject and deletion for the next route, because the host name is not allowed
	updateAndTestRoute(t, fooOshiftClient, "foo-def-route1", "default", "abc.xyz.com", "test-svc", "10.10.10.10", "cluster1", false)

	fooOshiftClient.RouteV1().Routes("default").Delete("foo-def-route1", nil)
	waitAndVerify(t, "DELETE/Route/cluster1/default/foo-def-route1", false)
	gslbingestion.DeleteGDPObj(gdp, ingestionQueue.Workqueue, 2)
	fmt.Println("done...")
}
