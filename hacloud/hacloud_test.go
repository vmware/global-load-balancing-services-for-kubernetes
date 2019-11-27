package hacloud

import (
	"os"
	"testing"
	"time"

	k8sfake "k8s.io/client-go/kubernetes/fake"

	// To Do: add test for openshift route
	//oshiftfake "github.com/openshift/client-go/route/clientset/versioned/fake"

	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
	corev1 "k8s.io/api/core/v1"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var kubeClient *k8sfake.Clientset
var globalKey string

func syncFuncForTest(key string) error {
	globalKey = key
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
	registeredInformers := []string{containerutils.ServiceInformer, containerutils.PodInformer, containerutils.EndpointInformer, containerutils.SecretInformer, containerutils.IngressInformer}
	informerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{kubeClient}, registeredInformers)
	ctrl := GetAviController("cluster1", informerInstance)
	stopCh := containerutils.SetupSignalHandler()
	ctrl.Start(stopCh)
	ctrl.SetupEventHandlers(K8sinformers{kubeClient})
	setupQueue(stopCh)
}

func TestSvc(t *testing.T) {
	svcExample := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "red-ns",
			Name:      "testsvc",
		},
	}
	_, err := kubeClient.CoreV1().Services("red-ns").Create(svcExample)
	if err != nil {
		t.Fatalf("error in adding Service: %v", err)
	}
	time.Sleep(2 * time.Second)
	if globalKey != "Service/cluster1/red-ns/testsvc" {
		t.Fatalf("error in adding Service: %v", globalKey)
	}
	svcExampleUpdated := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:                corev1.ServiceTypeLoadBalancer,
			HealthCheckNodePort: 80,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "red-ns",
			Name:      "testsvc",
		},
	}
	_, err = kubeClient.CoreV1().Services("red-ns").Update(svcExampleUpdated)
	if err != nil {
		t.Fatalf("error in Updating Service: %v", err)
	}
	err = kubeClient.CoreV1().Services("red-ns").Delete("testsvc", nil)
	if err != nil {
		t.Fatalf("error in deleting Service: %v", err)
	}
}

func TestEndpoint(t *testing.T) {
	epExample := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "red-ns",
			Name:      "testep",
		},
		Subsets: []corev1.EndpointSubset{},
	}
	_, err := kubeClient.CoreV1().Endpoints("red-ns").Create(epExample)
	if err != nil {
		t.Fatalf("error in adding ep: %v", err)
	}
	time.Sleep(2 * time.Second)
	if globalKey != "Endpoints/cluster1/red-ns/testep" {
		t.Fatalf("error in adding ep: %v", globalKey)
	}

	epExampleUpdated := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "red-ns",
			Name:        "testep",
			Annotations: map[string]string{"foo": "bar"},
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
}

func TestIngress(t *testing.T) {
	ingrExample := &extensionv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "red-ns",
			Name:      "testingr",
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
	time.Sleep(2 * time.Second)
	if globalKey != "Ingress/cluster1/red-ns/testingr" {
		t.Fatalf("error in adding Ingress: %v", globalKey)
	}

	ingrExampleUpdated := &extensionv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "red-ns",
			Name:      "testingr",
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

	err = kubeClient.ExtensionsV1beta1().Ingresses("red-ns").Delete("testingr", nil)
	if err != nil {
		t.Fatalf("error in deleting Ingress: %v", err)
	}
}
