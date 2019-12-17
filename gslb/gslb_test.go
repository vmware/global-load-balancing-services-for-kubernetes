package gslb

import (
	"fmt"
	"os"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
	v1 "k8s.io/api/core/v1"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	oshiftfake "github.com/openshift/client-go/route/clientset/versioned/fake"
)

var kubeClient *k8sfake.Clientset
var keyChan chan string
var oshiftClient *oshiftfake.Clientset

func syncFuncForTest(key string) error {
	keyChan <- key
	return nil
}

func setupQueue(stopCh <-chan struct{}) {
	ingestionQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	ingestionQueue.SyncFunc = syncFuncForTest
	ingestionQueue.Run(stopCh)
}

func TestMain(m *testing.M) {
	setUp()
	ret := m.Run()
	os.Exit(ret)
}

func setUp() {
	kubeClient = k8sfake.NewSimpleClientset()
	oshiftClient = oshiftfake.NewSimpleClientset()
	informersArg := make(map[string]interface{})
	informersArg[containerutils.INFORMERS_OPENSHIFT_CLIENT] = oshiftClient

	registeredInformers := []string{containerutils.IngressInformer, containerutils.RouteInformer}
	informerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{kubeClient}, registeredInformers, informersArg)
	ctrl := GetAviController("cluster1", informerInstance)
	stopCh := containerutils.SetupSignalHandler()
	ctrl.Start(stopCh)
	keyChan = make(chan string)
	ctrl.SetupEventHandlers(K8SInformers{kubeClient})
	setupQueue(stopCh)
}

func waitAndVerify(t *testing.T, key string, timeoutExpected bool) (bool, string) {
	waitChan := make(chan interface{})
	go func() {
		time.Sleep(10 * time.Second)
		waitChan <- 1
	}()

	select {
	case data := <-keyChan:
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
		if timeoutExpected {
			return true, "Success"
		}
		return false, "timed out waiting for " + key
	}
	return true, ""
}

func addAndTestIngress(t *testing.T, name string, ns string, svcName string, ip string, hostname string, timeoutExpected bool) (bool, string) {
	actualKey := "Ingress/" + "cluster1/" + ns + "/" + name
	msg := ""
	lbstatus := make([]v1.LoadBalancerIngress, 2)
	lbstatus[0].IP = ip
	lbstatus[0].Hostname = hostname

	ingr := &extensionv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "10",
		},
		Spec: extensionv1beta1.IngressSpec{
			Backend: &extensionv1beta1.IngressBackend{
				ServiceName: svcName,
			},
		},
		Status: extensionv1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: lbstatus,
			},
		},
	}
	_, err := kubeClient.ExtensionsV1beta1().Ingresses(ns).Create(ingr)
	if err != nil {
		msg = fmt.Sprintf("%s: %v", "error in adding ingress", err)
		return false, msg
	}
	fmt.Println("actual key: " + actualKey)
	return waitAndVerify(t, actualKey, timeoutExpected)
}

func updateAndTestIngress(t *testing.T, name string, ns string, svc string, ip string, hostname string) (bool, string) {
	actualKey := "Ingress/" + "cluster1/" + ns + "/" + name
	msg := ""
	lbstatus := make([]v1.LoadBalancerIngress, 2)
	lbstatus[0].IP = ip
	lbstatus[0].Hostname = hostname
	ingr := &extensionv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "11",
		},
		Spec: extensionv1beta1.IngressSpec{
			Backend: &extensionv1beta1.IngressBackend{
				ServiceName: svc,
			},
		},
		Status: extensionv1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: lbstatus,
			},
		},
	}
	_, err := kubeClient.ExtensionsV1beta1().Ingresses(ns).Update(ingr)
	if err != nil {
		msg = fmt.Sprintf("%s: %v", "error in adding ingress", err)
		return false, msg
	}
	return waitAndVerify(t, actualKey, false)
}

func TestIngress(t *testing.T) {
	ok, msg := addAndTestIngress(t, "test-ingr1", "test-ns", "test-svc", "10.10.10.10", "avivantage", false)
	if !ok {
		t.Fatalf("error: %s", msg)
	}
	ok, msg = updateAndTestIngress(t, "test-ingr1", "test-ns", "test-svc2", "10.10.10.10", "avivantage")
	if !ok {
		t.Fatalf("error: %s", msg)
	}

	ok, msg = addAndTestIngress(t, "test-ingr2", "another-ns", "test-svc3", "", "", true)
	if !ok {
		t.Fatalf("error: %s", msg)
	}

	ok, msg = updateAndTestIngress(t, "test-ingr2", "another-ns", "test-svc3", "10.10.10.10", "avivantage")
	if !ok {
		t.Fatalf("error: %s", msg)
	}
}

func addAndTestRoute(t *testing.T, name string, ns string, host string, svc string, ip string, timeoutExpected bool) (bool, string) {
	actualKey := "Route/cluster1/" + ns + "/" + name
	routeStatus := make([]routev1.RouteIngress, 2)
	conditions := make([]routev1.RouteIngressCondition, 2)
	conditions[0].Message = ip
	routeStatus[0].Conditions = conditions
	routeExample := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "100",
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

	_, err := oshiftClient.RouteV1().Routes(ns).Create(routeExample)
	if err != nil {
		t.Fatalf("error in adding route: %v", err)
	}
	return waitAndVerify(t, actualKey, timeoutExpected)
}

func updateAndTestRoute(t *testing.T, name string, ns string, host string, svc string, ip string) (bool, string) {
	actualKey := "Route/cluster1/" + ns + "/" + name
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

	_, err := oshiftClient.RouteV1().Routes(ns).Update(routeExample)
	if err != nil {
		t.Fatalf("error in updating route: %v", err)
	}
	return waitAndVerify(t, actualKey, false)
}

func TestRoute(t *testing.T) {
	ok, msg := addAndTestRoute(t, "test-route", "test-ns", "foo.avi.com", "avi-svc", "10.10.10.10", false)
	if !ok {
		t.Fatalf("error: %s", msg)
	}
	ok, msg = updateAndTestRoute(t, "test-route", "test-ns", "foo.avi.com", "avi-svc2", "10.10.10.10")
	if !ok {
		t.Fatalf("error: %s", msg)
	}
	ok, msg = addAndTestRoute(t, "test-route2", "test-ns", "bar.avi.com", "avi-svc", "", true)
	if !ok {
		t.Fatalf("error: %s", msg)
	}
	ok, msg = updateAndTestRoute(t, "test-route2", "test-ns", "bar.avi.com", "avi-svc", "10.10.10.10")
	if !ok {
		t.Fatalf("error: %s", msg)
	}
}
