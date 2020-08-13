package hacloud

import (
	"os"
	"testing"
	"time"

	k8sfake "k8s.io/client-go/kubernetes/fake"

	// To Do: add test for openshift route
	oshiftfake "github.com/openshift/client-go/route/clientset/versioned/fake"

	containerutils "github.com/avinetworks/ako/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"
)

var kubeClient *k8sfake.Clientset
var oshiftClient *oshiftfake.Clientset
var keyChan chan string

func syncFuncForTest(key string) error {
	keyChan <- key
	return nil
}

func setupQueue(stopCh <-chan struct{}) {
	ingestionQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	ingestionQueue.SyncFunc = syncFuncForTest
	ingestionQueue.Run(stopCh)
}

func waitAndverify(t *testing.T, key string) {
	waitChan := make(chan int)
	go func() {
		time.Sleep(5 * time.Second)
		waitChan <- 1
	}()

	select {
	case data := <-keyChan:
		if data != key {
			t.Fatalf("error in match expected: %v, got: %v", key, data)
		}
	case _ = <-waitChan:
		t.Fatalf("timed out waitig for %v", key)
	}
}

func TestMain(m *testing.M) {
	setUp()
	ret := m.Run()
	os.Exit(ret)
}

func setUp() {
	os.Setenv("INGRESS_API", "extensionv1")
	kubeClient = k8sfake.NewSimpleClientset()
	oshiftClient = oshiftfake.NewSimpleClientset()
	informersArg := make(map[string]interface{})
	informersArg[containerutils.INFORMERS_OPENSHIFT_CLIENT] = oshiftClient
	registeredInformers := []string{containerutils.ServiceInformer, containerutils.PodInformer, containerutils.EndpointInformer, containerutils.SecretInformer, containerutils.IngressInformer, containerutils.RouteInformer}
	informerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{kubeClient}, registeredInformers, informersArg)
	ctrl := GetAviController("cluster1", informerInstance)
	stopCh := containerutils.SetupSignalHandler()
	ctrl.Start(stopCh)
	keyChan = make(chan string)
	ctrl.SetupEventHandlers(K8sinformers{kubeClient})
	setupQueue(stopCh)
}

func TestSvc(t *testing.T) {
	svcExample := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "red-ns",
			Name:            "testsvc",
			ResourceVersion: "100",
		},
	}
	_, err := kubeClient.CoreV1().Services("red-ns").Create(svcExample)
	if err != nil {
		t.Fatalf("error in adding Service: %v", err)
	}
	waitAndverify(t, "Service/cluster1/red-ns/testsvc")

	svcExampleUpdated := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:                corev1.ServiceTypeLoadBalancer,
			HealthCheckNodePort: 80,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "red-ns",
			Name:            "testsvc",
			ResourceVersion: "101",
		},
	}
	_, err = kubeClient.CoreV1().Services("red-ns").Update(svcExampleUpdated)
	if err != nil {
		t.Fatalf("error in Updating Service: %v", err)
	}
	waitAndverify(t, "Service/cluster1/red-ns/testsvc")

	err = kubeClient.CoreV1().Services("red-ns").Delete("testsvc", nil)
	if err != nil {
		t.Fatalf("error in deleting Service: %v", err)
	}
	waitAndverify(t, "Service/cluster1/red-ns/testsvc")
}

func TestEndpoint(t *testing.T) {
	epExample := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "red-ns",
			Name:            "testep",
			ResourceVersion: "100",
		},
		Subsets: []corev1.EndpointSubset{},
	}
	_, err := kubeClient.CoreV1().Endpoints("red-ns").Create(epExample)
	if err != nil {
		t.Fatalf("error in adding ep: %v", err)
	}
	waitAndverify(t, "Endpoints/cluster1/red-ns/testep")

	epExampleUpdated := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "red-ns",
			Name:            "testep",
			ResourceVersion: "101",
			Annotations:     map[string]string{"foo": "bar"},
		},
		Subsets: []corev1.EndpointSubset{},
	}

	_, err = kubeClient.CoreV1().Endpoints("red-ns").Update(epExampleUpdated)
	if err != nil {
		t.Fatalf("error in updating ep: %v", err)
	}
	err = kubeClient.CoreV1().Endpoints("red-ns").Delete("testep", nil)
	if err != nil {
		t.Fatalf("error in deleting ep: %v", err)
	}
	waitAndverify(t, "Endpoints/cluster1/red-ns/testep")
}

func TestIngress(t *testing.T) {
	ingrExample := &extensionv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "red-ns",
			Name:            "testingr",
			ResourceVersion: "100",
		},
		Spec: extensionv1beta1.IngressSpec{
			Backend: &extensionv1beta1.IngressBackend{
				ServiceName: "testsvc",
			},
		},
	}
	_, err := kubeClient.ExtensionsV1beta1().Ingresses("red-ns").Create(ingrExample)
	if err != nil {
		t.Fatalf("error in adding Ingress: %v", err)
	}
	waitAndverify(t, "Ingress/cluster1/red-ns/testingr")

	ingrExampleUpdated := &extensionv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "red-ns",
			Name:            "testingr",
			ResourceVersion: "101",
		},
		Spec: extensionv1beta1.IngressSpec{
			Backend: &extensionv1beta1.IngressBackend{
				ServiceName: "testsvcupdated",
			},
		},
	}
	_, err = kubeClient.ExtensionsV1beta1().Ingresses("red-ns").Update(ingrExampleUpdated)
	if err != nil {
		t.Fatalf("error in updating Ingress: %v", err)
	}
	waitAndverify(t, "Ingress/cluster1/red-ns/testingr")

	err = kubeClient.ExtensionsV1beta1().Ingresses("red-ns").Delete("testingr", nil)
	if err != nil {
		t.Fatalf("error in deleting Ingress: %v", err)
	}
	waitAndverify(t, "Ingress/cluster1/red-ns/testingr")
}

func TestRoute(t *testing.T) {
	routeExample := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "red-ns",
			Name:            "testroute",
			ResourceVersion: "100",
		},
		Spec: routev1.RouteSpec{
			Host: "foo.com",
			To: routev1.RouteTargetReference{
				Name: "testsvc",
			},
		},
	}

	_, err := oshiftClient.RouteV1().Routes("red-ns").Create(routeExample)
	if err != nil {
		t.Fatalf("error in adding Route: %v", err)
	}
	waitAndverify(t, "Route/cluster1/red-ns/testroute")

	routeExampleUpdated := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "red-ns",
			Name:            "testroute",
			ResourceVersion: "101",
		},
		Spec: routev1.RouteSpec{
			Host: "foo.com",
			To: routev1.RouteTargetReference{
				Name: "testsvc2",
			},
		},
	}
	_, err = oshiftClient.RouteV1().Routes("red-ns").Update(routeExampleUpdated)
	if err != nil {
		t.Fatalf("error in updating Route: %v", err)
	}
	waitAndverify(t, "Route/cluster1/red-ns/testroute")

	err = oshiftClient.RouteV1().Routes("red-ns").Delete("testroute", nil)
	if err != nil {
		t.Fatalf("error in deleting Route: %v", err)
	}
	waitAndverify(t, "Route/cluster1/red-ns/testroute")
}
