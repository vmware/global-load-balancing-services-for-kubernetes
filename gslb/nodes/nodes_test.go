package nodes

import (
	"fmt"
	"os"
	"testing"
	"time"

	"amko/gslb/gslbutils"
	"amko/gslb/k8sobjects"

	"github.com/avinetworks/container-lib/utils"
	routev1 "github.com/openshift/api/route/v1"
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
	ingestionQueue.SyncFunc = SyncFromIngestionLayer
	ingestionQueue.Run(testStopCh)

	graphQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	graphQueue.SyncFunc = graphSyncFuncForTest
	graphQueue.Run(testStopCh)
}

func setUp() {
	testStopCh = utils.SetupSignalHandler()
	keyChan = make(chan string)

	setupQueue(testStopCh)
}

func graphSyncFuncForTest(key string) error {
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
		fmt.Printf("Expected key: %s, got data: %s\n", key, data)
		if timeoutExpected {
			// if the timeout is expected, then there shouldn't be anything on this channel
			if data != "" {
				errMsg := "Unexpected data: %s" + data
				return false, errMsg
			}
		}
		if data == key {
			return true, ""
		}
	case _ = <-waitChan:
		fmt.Println("waiting for timeout")
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

func addAndTestRoute(t *testing.T, name string, ns string, host string, svc string, ip string, cname string, acceptedRouteStore *gslbutils.ClusterStore) {
	key := gslbutils.ObjectAdd + "/" + "Route/" + cname + "/" + ns + "/" + name
	routeStatus := make([]routev1.RouteIngress, 2)
	conditions := make([]routev1.RouteIngressCondition, 2)
	conditions[0].Message = ip
	routeStatus[0].Conditions = conditions
	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	routeExample := k8sobjects.RouteMeta{
		Name:      name,
		Namespace: ns,
		Labels:    labelMap,
		Hostname:  host,
		IPAddr:    ip,
	}
	acceptedRouteStore.AddOrUpdate(routeExample, cname, ns, name)
	addKeyToIngestionQueue(ns, key)
}

func TestGSGraphs(t *testing.T) {
	gslbutils.AcceptedRouteStore = gslbutils.GetAcceptedRouteStore()
	gslbutils.RejectedRouteStore = gslbutils.GetRejectedRouteStore()
	hostname := "abc.avi.com"
	addAndTestRoute(t, "foo-test-route1", "default", hostname, "foo-svc1", "10.10.10.10", "foo", gslbutils.AcceptedRouteStore)
	ok, msg := waitAndVerify(t, "admin"+"/"+hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	addAndTestRoute(t, "bar-test-route2", "default", hostname, "bar-svc1", "10.10.10.20", "bar", gslbutils.AcceptedRouteStore)
	ok, msg = waitAndVerify(t, "admin"+"/"+hostname, false)
	if !ok {
		t.Fatalf("%s", msg)
	}
	// delete one of the routes
	addKeyToIngestionQueue("default", "DELETE/Route/foo/default/foo-test-route1")
	ok, msg = waitAndVerify(t, "admin"+"/"+hostname, false)

	// add an invalid route
	hostname = "xyz.avi.com"
	addAndTestRoute(t, "invalid-route", "default", hostname, "test-svc", "", "test-cluster", gslbutils.AcceptedRouteStore)
	ok, msg = waitAndVerify(t, "admin"+"/"+hostname, true)
}
