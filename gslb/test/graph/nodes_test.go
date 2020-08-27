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

package graph

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/avinetworks/amko/gslb/gslbutils"
	"github.com/avinetworks/amko/gslb/k8sobjects"
	"github.com/avinetworks/amko/gslb/nodes"
	"github.com/avinetworks/amko/gslb/test/ingestion"

	"github.com/onsi/gomega"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

const (
	DefSvc     = "svc"
	FooCluster = "foo"
	BarCluster = "bar"
	DefNS      = "default"
)

var testStopCh <-chan struct{}
var keyChan chan string
var ingestionQueue *utils.WorkerQueue

func TestMain(m *testing.M) {
	setUp()
	ret := m.Run()
	os.Exit(ret)
}

func setupQueue(testCh <-chan struct{}) {
	ingestionQueue = utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	ingestionQueue.SyncFunc = nodes.SyncFromIngestionLayer
	ingestionQueue.Run(testStopCh, &sync.WaitGroup{})

	graphQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	graphQueue.SyncFunc = graphSyncFuncForTest
	graphQueue.Run(testStopCh, &sync.WaitGroup{})
}

func setUp() {
	os.Setenv("INGRESS_API", "extensionv1")

	testStopCh = utils.SetupSignalHandler()
	keyChan = make(chan string)

	setupQueue(testStopCh)
}

func graphSyncFuncForTest(key string, wg *sync.WaitGroup) error {
	keyChan <- key
	return nil
}

func waitAndVerify(t *testing.T, key string, timeoutExpected bool) (bool, string) {
	waitChan := make(chan interface{})
	go func() {
		time.Sleep(10 * time.Second)
		waitChan <- 1
	}()

	select {
	case data := <-keyChan:
		t.Logf("Expected key: %s, got data: %s\n", key, data)
		if timeoutExpected {
			// if the timeout is expected, then there shouldn't be anything on this channel
			if data != "" {
				errMsg := "Unexpected data: %s" + data
				return false, errMsg
			}
		}
		if data == key {
			return true, ""
		} else {
			return false, "key match error, expected key: " + key + ", got: " + data
		}
	case _ = <-waitChan:
		t.Logf("waiting for timeout")
		if timeoutExpected {
			return true, "Success"
		}
		return false, "timed out waiting for " + key
	}
	return false, "key match failed"
}

func addKeyToIngestionQueue(ns, key string) {
	k8sQueue := ingestionQueue.Workqueue
	numWorkers := ingestionQueue.NumWorkers
	bkt := utils.Bkt(ns, numWorkers)
	k8sQueue[bkt].AddRateLimited(key)
}

func AddSvcMeta(t *testing.T, name, ns, host, svc, ip, cname string, create bool) k8sobjects.SvcMeta {
	acceptedSvcStore := gslbutils.GetAcceptedLBSvcStore()
	objName := name
	op := gslbutils.ObjectAdd
	if !create {
		op = gslbutils.ObjectUpdate
	}
	key := ingestion.GetSvcKey(op, cname, ns, name)
	svcMeta := k8sobjects.SvcMeta{
		Name:      name,
		Namespace: ns,
		Hostname:  host,
		IPAddr:    ip,
		Cluster:   cname,
	}
	acceptedSvcStore.AddOrUpdate(svcMeta, cname, ns, objName)
	addKeyToIngestionQueue(ns, key)
	return svcMeta
}

func AddIngressMeta(t *testing.T, name, ns, host, svc, ip, cname string, create bool) k8sobjects.IngressHostMeta {
	acceptedIngStore := gslbutils.GetAcceptedIngressStore()
	objName := name + "/" + host
	op := gslbutils.ObjectAdd
	if !create {
		op = gslbutils.ObjectUpdate
	}
	key := ingestion.GetIngressKey(op, cname, ns, name, host)
	ingExample := k8sobjects.IngressHostMeta{
		IngName:   name,
		Namespace: ns,
		Hostname:  host,
		IPAddr:    ip,
		Cluster:   cname,
		ObjName:   objName,
	}
	acceptedIngStore.AddOrUpdate(ingExample, cname, ns, objName)
	addKeyToIngestionQueue(ns, key)
	return ingExample
}

func GetIhmKey(op string, ihm k8sobjects.IngressHostMeta) string {
	return ingestion.GetIngressKey(op, ihm.Cluster, ihm.Namespace, ihm.IngName, ihm.Hostname)
}

func GetSvcKey(op string, svc k8sobjects.SvcMeta) string {
	return ingestion.GetSvcKey(op, svc.Cluster, svc.Namespace, svc.Name)
}

func verifyGsGraph(t *testing.T, metaObj k8sobjects.MetaObject, present bool, nMembers int, memberCheck bool) {
	g := gomega.NewGomegaWithT(t)

	modelName := utils.ADMIN_NS + "/" + nodes.DeriveGSLBServiceName(metaObj.GetHostname())
	ok, aviModelIntf := nodes.SharedAviGSGraphLister().Get(modelName)
	g.Expect(ok).To(gomega.Equal(present))

	aviGsModel := aviModelIntf.(*nodes.AviGSObjectGraph)
	g.Expect(aviGsModel.Tenant).To(gomega.Equal(utils.ADMIN_NS))
	g.Expect(aviGsModel.Name).To(gomega.Equal(metaObj.GetHostname()))
	g.Expect(aviGsModel.MembersLen()).To(gomega.Equal(nMembers))

	if !memberCheck || nMembers == 0 {
		return
	}
	// check the metaObj with the GS member fields
	memberFound := false
	for _, gsMember := range aviGsModel.MemberObjs {
		if gsMember.Cluster != metaObj.GetCluster() || gsMember.ObjType != metaObj.GetType() ||
			gsMember.Namespace != metaObj.GetNamespace() || gsMember.Name != metaObj.GetName() {
			continue
		}
		memberFound = true
		g.Expect(gsMember.IPAddr).To(gomega.Equal(metaObj.GetIPAddr()))
	}
	g.Expect(memberFound).To(gomega.Equal(true))
}

func TestGSGraphsForSingleIhms(t *testing.T) {
	prefix := "si-"
	acceptedIngStore := gslbutils.GetAcceptedRouteStore()
	hostname1 := prefix + "host1.avi.com"
	hostname2 := prefix + "host2.avi.com"
	fooIng1 := prefix + "foo-ing1"
	barIng1 := prefix + "bar-ing1"
	ihm1 := AddIngressMeta(t, fooIng1, DefNS, hostname1, DefSvc, "10.10.10.10", FooCluster, true)
	ok, msg := waitAndVerify(t, utils.ADMIN_NS+"/"+ihm1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	ihm2 := AddIngressMeta(t, barIng1, DefNS, hostname2, DefSvc, "10.10.10.20", BarCluster, true)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// check the GS graph's fields
	verifyGsGraph(t, ihm1, true, 1, true)
	verifyGsGraph(t, ihm2, true, 1, true)

	// delete the Ihms
	key1 := GetIhmKey(gslbutils.ObjectDelete, ihm1)
	acceptedIngStore.DeleteClusterNSObj(ihm1.Cluster, ihm1.Namespace, ihm1.ObjName)
	addKeyToIngestionQueue(DefNS, key1)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm1.Hostname, false)
	verifyGsGraph(t, ihm1, true, 0, false)

	key2 := GetIhmKey(gslbutils.ObjectDelete, ihm2)
	acceptedIngStore.DeleteClusterNSObj(ihm2.Cluster, ihm2.Namespace, ihm2.ObjName)
	addKeyToIngestionQueue(DefNS, key2)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm2.Hostname, false)
	verifyGsGraph(t, ihm2, true, 0, false)
}

func TestGSGraphsForMultiIhms(t *testing.T) {
	prefix := "mi-"
	fooIng1 := prefix + "foo-ing1"
	barIng1 := prefix + "bar-ing1"
	hostname := prefix + "host1.avi.com"
	acceptedIngStore := gslbutils.GetAcceptedRouteStore()
	ihm1 := AddIngressMeta(t, fooIng1, DefNS, hostname, FooCluster+"-"+DefSvc, "10.10.10.10", FooCluster, true)
	ok, msg := waitAndVerify(t, utils.ADMIN_NS+"/"+ihm1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	ihm2 := AddIngressMeta(t, barIng1, DefNS, hostname, BarCluster+"-"+DefSvc, "10.10.10.20", BarCluster, true)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// check the GS graph's fields for both the members
	verifyGsGraph(t, ihm1, true, 2, true)
	verifyGsGraph(t, ihm2, true, 2, true)

	// delete the Ihms
	key1 := GetIhmKey(gslbutils.ObjectDelete, ihm1)
	acceptedIngStore.DeleteClusterNSObj(ihm1.Cluster, ihm1.Namespace, ihm1.ObjName)
	addKeyToIngestionQueue(DefNS, key1)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm1.Hostname, false)
	verifyGsGraph(t, ihm1, true, 1, false)

	key2 := GetIhmKey(gslbutils.ObjectDelete, ihm2)
	acceptedIngStore.DeleteClusterNSObj(ihm2.Cluster, ihm2.Namespace, ihm2.ObjName)
	addKeyToIngestionQueue(DefNS, key2)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm2.Hostname, false)
	verifyGsGraph(t, ihm2, true, 0, false)
}

func TestGSGraphsForSingleIhmUpdate(t *testing.T) {
	prefix := "siu-"
	acceptedIngStore := gslbutils.GetAcceptedRouteStore()
	hostname1 := prefix + "host1.avi.com"
	hostname2 := prefix + "host2.avi.com"
	fooIng1 := prefix + "foo-ing1"
	barIng1 := prefix + "bar-ing1"
	ihm1 := AddIngressMeta(t, fooIng1, DefNS, hostname1, DefSvc, "10.10.10.10", FooCluster, true)
	ok, msg := waitAndVerify(t, utils.ADMIN_NS+"/"+ihm1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	ihm2 := AddIngressMeta(t, barIng1, DefNS, hostname2, DefSvc, "10.10.10.20", BarCluster, true)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// check the GS graph's fields
	verifyGsGraph(t, ihm1, true, 1, true)
	verifyGsGraph(t, ihm2, true, 1, true)

	// update one of the Ihms, ihm1
	updatedIhm1 := AddIngressMeta(t, fooIng1, DefNS, hostname1, DefSvc, "10.10.10.11", FooCluster, false)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	verifyGsGraph(t, updatedIhm1, true, 1, true)

	// delete the Ihms
	key1 := GetIhmKey(gslbutils.ObjectDelete, updatedIhm1)
	acceptedIngStore.DeleteClusterNSObj(updatedIhm1.Cluster, updatedIhm1.Namespace, updatedIhm1.ObjName)
	addKeyToIngestionQueue(DefNS, key1)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedIhm1.Hostname, false)
	verifyGsGraph(t, updatedIhm1, true, 0, false)

	key2 := GetIhmKey(gslbutils.ObjectDelete, ihm2)
	acceptedIngStore.DeleteClusterNSObj(ihm2.Cluster, ihm2.Namespace, ihm2.ObjName)
	addKeyToIngestionQueue(DefNS, key2)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm2.Hostname, false)
	verifyGsGraph(t, ihm2, true, 0, false)
}

func TestGSGraphsForMultiIhmUpdate(t *testing.T) {
	prefix := "miu-"
	fooIng1 := prefix + "foo-ing1"
	barIng1 := prefix + "bar-ing1"
	hostname := prefix + "host1.avi.com"
	acceptedIngStore := gslbutils.GetAcceptedRouteStore()
	ihm1 := AddIngressMeta(t, fooIng1, DefNS, hostname, FooCluster+"-"+DefSvc, "10.10.10.10", FooCluster, true)
	ok, msg := waitAndVerify(t, utils.ADMIN_NS+"/"+ihm1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	ihm2 := AddIngressMeta(t, barIng1, DefNS, hostname, BarCluster+"-"+DefSvc, "10.10.10.20", BarCluster, true)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// check the GS graph's fields for both the members
	verifyGsGraph(t, ihm1, true, 2, true)
	verifyGsGraph(t, ihm2, true, 2, true)

	// update one of the Ihms, ihm1
	// AddIngressMeta not just adds a new object, it can also update an existing object too
	updatedIhm1 := AddIngressMeta(t, fooIng1, DefNS, hostname, FooCluster+"-"+DefSvc, "10.10.10.11", FooCluster, false)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedIhm1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// verifyGSGraph also checks the IP address to make sure that the member got updated
	// properly
	verifyGsGraph(t, updatedIhm1, true, 2, true)

	// let's update the second member as well
	updatedIhm2 := AddIngressMeta(t, barIng1, DefNS, hostname, BarCluster+"-"+DefSvc, "10.10.10.21", BarCluster, false)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+ihm2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	verifyGsGraph(t, updatedIhm2, true, 2, true)

	// delete the Ihms
	key1 := GetIhmKey(gslbutils.ObjectDelete, updatedIhm1)
	acceptedIngStore.DeleteClusterNSObj(updatedIhm1.Cluster, updatedIhm1.Namespace, updatedIhm1.ObjName)
	addKeyToIngestionQueue(DefNS, key1)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedIhm1.Hostname, false)
	verifyGsGraph(t, updatedIhm1, true, 1, false)

	key2 := GetIhmKey(gslbutils.ObjectDelete, updatedIhm2)
	acceptedIngStore.DeleteClusterNSObj(updatedIhm2.Cluster, updatedIhm2.Namespace, updatedIhm2.ObjName)
	addKeyToIngestionQueue(DefNS, key2)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedIhm2.Hostname, false)
	verifyGsGraph(t, updatedIhm2, true, 0, false)
}

func TestGSGraphsForSingleSvc(t *testing.T) {
	prefix := "ss-"
	acceptedIngStore := gslbutils.GetAcceptedRouteStore()
	hostname1 := prefix + "host1.avi.com"
	hostname2 := prefix + "host2.avi.com"
	fooIng1 := prefix + "foo-ing1"
	barIng1 := prefix + "bar-ing1"
	svc1 := AddSvcMeta(t, fooIng1, DefNS, hostname1, DefSvc, "10.10.10.10", FooCluster, true)
	ok, msg := waitAndVerify(t, utils.ADMIN_NS+"/"+svc1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	svc2 := AddSvcMeta(t, barIng1, DefNS, hostname2, DefSvc, "10.10.10.20", BarCluster, true)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+svc2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// check the GS graph's fields
	verifyGsGraph(t, svc1, true, 1, true)
	verifyGsGraph(t, svc2, true, 1, true)

	// delete the svcs
	key1 := GetSvcKey(gslbutils.ObjectDelete, svc1)
	acceptedIngStore.DeleteClusterNSObj(svc1.Cluster, svc1.Namespace, svc1.Name)
	addKeyToIngestionQueue(DefNS, key1)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+svc1.Hostname, false)
	verifyGsGraph(t, svc1, true, 0, false)

	key2 := GetSvcKey(gslbutils.ObjectDelete, svc2)
	acceptedIngStore.DeleteClusterNSObj(svc2.Cluster, svc2.Namespace, svc2.Name)
	addKeyToIngestionQueue(DefNS, key2)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+svc2.Hostname, false)
	verifyGsGraph(t, svc2, true, 0, false)
}

func TestGSGraphsForMultiSvc(t *testing.T) {
	prefix := "ms-"
	acceptedIngStore := gslbutils.GetAcceptedRouteStore()
	hostname := prefix + "host1.avi.com"
	fooIng1 := prefix + "foo-ing1"
	barIng1 := prefix + "bar-ing1"
	svc1 := AddSvcMeta(t, fooIng1, DefNS, hostname, DefSvc, "10.10.10.10", FooCluster, true)
	ok, msg := waitAndVerify(t, utils.ADMIN_NS+"/"+svc1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	svc2 := AddSvcMeta(t, barIng1, DefNS, hostname, DefSvc, "10.10.10.20", BarCluster, true)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+svc2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// check the GS graph's fields for both the members
	verifyGsGraph(t, svc1, true, 2, true)
	verifyGsGraph(t, svc2, true, 2, true)

	// delete the svcs
	key1 := GetSvcKey(gslbutils.ObjectDelete, svc1)
	acceptedIngStore.DeleteClusterNSObj(svc1.Cluster, svc1.Namespace, svc1.Name)
	addKeyToIngestionQueue(DefNS, key1)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+svc1.Hostname, false)
	verifyGsGraph(t, svc1, true, 1, false)

	key2 := GetSvcKey(gslbutils.ObjectDelete, svc2)
	acceptedIngStore.DeleteClusterNSObj(svc2.Cluster, svc2.Namespace, svc2.Name)
	addKeyToIngestionQueue(DefNS, key2)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+svc2.Hostname, false)
	verifyGsGraph(t, svc2, true, 0, false)
}

func TestGSGraphsForSingleSvcUpdate(t *testing.T) {
	prefix := "ssu-"
	acceptedIngStore := gslbutils.GetAcceptedRouteStore()
	hostname1 := prefix + "host1.avi.com"
	hostname2 := prefix + "host2.avi.com"
	fooIng1 := prefix + "foo-ing1"
	barIng1 := prefix + "bar-ing1"
	svc1 := AddSvcMeta(t, fooIng1, DefNS, hostname1, DefSvc, "10.10.10.10", FooCluster, true)
	ok, msg := waitAndVerify(t, utils.ADMIN_NS+"/"+svc1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	svc2 := AddSvcMeta(t, barIng1, DefNS, hostname2, DefSvc, "10.10.10.20", BarCluster, true)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+svc2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// check the GS graph's fields, each of the GSs will have one member each
	verifyGsGraph(t, svc1, true, 1, true)
	verifyGsGraph(t, svc2, true, 1, true)

	updatedSvc1 := AddSvcMeta(t, fooIng1, DefNS, hostname1, DefSvc, "10.10.10.11", FooCluster, false)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedSvc1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	updatedSvc2 := AddSvcMeta(t, barIng1, DefNS, hostname2, DefSvc, "10.10.10.21", BarCluster, false)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedSvc2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// verify if both the graph's members got updated
	verifyGsGraph(t, updatedSvc1, true, 1, true)
	verifyGsGraph(t, updatedSvc2, true, 1, true)

	// delete the svcs
	key1 := GetSvcKey(gslbutils.ObjectDelete, updatedSvc1)
	acceptedIngStore.DeleteClusterNSObj(updatedSvc1.Cluster, updatedSvc1.Namespace, updatedSvc1.Name)
	addKeyToIngestionQueue(DefNS, key1)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedSvc1.Hostname, false)
	verifyGsGraph(t, updatedSvc1, true, 0, false)

	key2 := GetSvcKey(gslbutils.ObjectDelete, svc2)
	acceptedIngStore.DeleteClusterNSObj(updatedSvc2.Cluster, updatedSvc2.Namespace, updatedSvc2.Name)
	addKeyToIngestionQueue(DefNS, key2)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedSvc2.Hostname, false)
	verifyGsGraph(t, updatedSvc2, true, 0, false)
}

func TestGSGraphsForMultiSvcUpdate(t *testing.T) {
	prefix := "msu-"
	acceptedIngStore := gslbutils.GetAcceptedRouteStore()
	hostname := prefix + "host1.avi.com"
	fooIng1 := prefix + "foo-ing1"
	barIng1 := prefix + "bar-ing1"
	svc1 := AddSvcMeta(t, fooIng1, DefNS, hostname, DefSvc, "10.10.10.10", FooCluster, true)
	ok, msg := waitAndVerify(t, utils.ADMIN_NS+"/"+svc1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	svc2 := AddSvcMeta(t, barIng1, DefNS, hostname, DefSvc, "10.10.10.20", BarCluster, true)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+svc2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// check the GS graph's fields for both the members
	verifyGsGraph(t, svc1, true, 2, true)
	verifyGsGraph(t, svc2, true, 2, true)

	// update both the services
	updatedSvc1 := AddSvcMeta(t, fooIng1, DefNS, hostname, DefSvc, "10.10.10.11", FooCluster, false)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedSvc1.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	updatedSvc2 := AddSvcMeta(t, barIng1, DefNS, hostname, DefSvc, "10.10.10.21", BarCluster, false)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedSvc2.Hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// the following will check for only one GS with 2 members
	verifyGsGraph(t, updatedSvc1, true, 2, true)
	verifyGsGraph(t, updatedSvc2, true, 2, true)

	// delete the svcs
	key1 := GetSvcKey(gslbutils.ObjectDelete, updatedSvc1)
	acceptedIngStore.DeleteClusterNSObj(updatedSvc1.Cluster, updatedSvc1.Namespace, updatedSvc1.Name)
	addKeyToIngestionQueue(DefNS, key1)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedSvc1.Hostname, false)
	verifyGsGraph(t, updatedSvc1, true, 1, false)

	key2 := GetSvcKey(gslbutils.ObjectDelete, updatedSvc2)
	acceptedIngStore.DeleteClusterNSObj(updatedSvc2.Cluster, updatedSvc2.Namespace, updatedSvc2.Name)
	addKeyToIngestionQueue(DefNS, key2)
	ok, msg = waitAndVerify(t, utils.ADMIN_NS+"/"+updatedSvc2.Hostname, false)
	verifyGsGraph(t, updatedSvc2, true, 0, false)
}
