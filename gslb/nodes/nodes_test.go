package nodes

import (
	"fmt"
	"os"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	acceptedRouteStore.AddOrUpdate(routeExample, cname, ns, name)
	addKeyToIngestionQueue(ns, key)
}

func TestGSGraphs(t *testing.T) {
	gslbutils.AcceptedRouteStore = gslbutils.NewClusterStore()
	gslbutils.RejectedRouteStore = gslbutils.NewClusterStore()
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
