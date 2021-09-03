/*
 * Copyright 2021 VMware, Inc.
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

package third_party_vips

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	avicache "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	amkorest "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/rest"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	KubeBuilderAssetsEnv = "KUBEBUILDER_ASSETS"
	// list indices for k8s cluster, openshift cluster and config cluster (where AMKO is running)
	// Config cluster and K8s cluster are same here
	K8s           = 0
	ConfigCluster = 0
	Oshift        = 1
	MaxClusters   = 2
	// AMKO CRD directory
	AmkoCRDs = "../../../../helm/amko/crds"

	AviSystemNS     = "avi-system"
	AviSecret       = "avi-secret"
	GslbConfigName  = "test-gc"
	GDPName         = "test-gdp"
	K8sContext      = "k8s"
	OshiftContext   = "oshift"
	Hostname        = "hostname"
	Path            = "path"
	TlsTrue         = true
	TlsFalse        = false
	DefaultPriority = 10
)

var (
	cfgs                 []*rest.Config
	clusterClients       []*kubernetes.Clientset
	testEnvs             []*envtest.Environment
	stopCh               <-chan struct{}
	apiURL               string
	ingestionKeyChan     chan string
	graphKeyChan         chan string
	oshiftClient         *oshiftclient.Clientset
	KubeBuilderAssetsVal string
	routeCRD             apiextensionv1beta1.CustomResourceDefinition
	hrCRD                apiextensionv1beta1.CustomResourceDefinition
	defaultPath          = []string{"/"}
)

var appLabel map[string]string = map[string]string{"key": "value"}

func BuildLBServiceObj(t *testing.T, name, ns string, hostIPs map[string]string, port int32) *corev1.Service {
	svcObj := &corev1.Service{}
	svcObj.Namespace = ns
	svcObj.Name = name

	svcObj.Spec.Type = "LoadBalancer"
	ports := corev1.ServicePort{
		Protocol: "TCP",
		Port:     port,
		TargetPort: intstr.IntOrString{
			Type:   0,
			IntVal: port,
		},
	}
	svcObj.Spec.Ports = []corev1.ServicePort{ports}
	svcObj.Spec.Selector = map[string]string{
		"app": "lb-app",
	}

	for ingHost, ingIP := range hostIPs {
		svcObj.Status.LoadBalancer.Ingress = append(svcObj.Status.LoadBalancer.Ingress,
			corev1.LoadBalancerIngress{
				IP:       ingIP,
				Hostname: ingHost,
			})

	}
	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	svcObj.Labels = labelMap
	return svcObj
}

func BuildIngressObj(name, ns, svc, cname string, hostIPs map[string]string, paths []string, withStatus bool, secretName string) *networkingv1.Ingress {
	ingObj := &networkingv1.Ingress{}
	ingObj.Namespace = ns
	ingObj.Name = name

	if paths == nil {
		paths = []string{"/"}
	}
	var ingPaths []networkingv1.HTTPIngressPath
	var pathType networkingv1.PathType = "ImplementationSpecific"
	for _, path := range paths {
		ingPath := networkingv1.HTTPIngressPath{
			Path:     path,
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: svc,
					Port: networkingv1.ServiceBackendPort{
						Number: 8080,
					},
				},
			},
		}
		ingPaths = append(ingPaths, ingPath)
	}

	var hosts []string
	for ingHost, ingIP := range hostIPs {
		hosts = append(hosts, ingHost)
		ingObj.Spec.Rules = append(ingObj.Spec.Rules, networkingv1.IngressRule{
			Host: ingHost,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: ingPaths,
				},
			},
		})
		if !withStatus {
			continue
		}
		ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
			IP:       ingIP,
			Hostname: ingHost,
		})
	}
	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	ingObj.Labels = labelMap
	if secretName != "" {
		if len(ingObj.Spec.TLS) == 0 {
			ingObj.Spec.TLS = make([]networkingv1.IngressTLS, 0)
		}
		ingObj.Spec.TLS = append(ingObj.Spec.TLS, networkingv1.IngressTLS{
			Hosts:      hosts,
			SecretName: secretName,
		})
	}

	return ingObj
}

func BuildRouteObj(name, ns, svc, cname, host, ip, path string, withStatus bool) *routev1.Route {
	if path == "" {
		path = "/"
	}
	routeObj := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: routev1.RouteSpec{
			Host: host,
			Path: path,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: svc,
			},
		},
	}

	if withStatus {
		routeObj.Status = routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Conditions: []routev1.RouteIngressCondition{
						{
							Message: ip,
						},
					},
					RouterName: "ako-test",
					Host:       host,
				},
			},
		}
	}

	labelMap := make(map[string]string)
	labelMap["key"] = "value"
	routeObj.Labels = labelMap

	return routeObj
}

func getAnnotations(hostNames []string) map[string]string {
	annot := map[string]string{
		"ako.vmware.com/controller-cluster-uuid": "cluster-XXXXX",
		"ako.vmware.com/host-fqdn-vs-uuid-map":   "",
	}

	hostVS := map[string]string{}
	for _, host := range hostNames {
		hostVS[host] = "virtualservice-" + host
	}
	jsonData, _ := json.Marshal(hostVS)
	annot["ako.vmware.com/host-fqdn-vs-uuid-map"] = string(jsonData)
	return annot
}

func buildk8sSecret(ns string) *corev1.Secret {
	secretObj := corev1.Secret{}
	secretObj.Name = "test-secret"
	secretObj.Namespace = ns
	secretObj.Data = make(map[string][]byte)
	secretObj.Data["tls.crt"] = []byte("")
	secretObj.Data["tls.key"] = []byte("")
	return &secretObj
}

func deletek8sSecret(t *testing.T, kc *kubernetes.Clientset, ns, name string) {
	err := kc.CoreV1().Secrets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Fatalf("error in deleting secret object %s/%s: %v", ns, name, err)
	}
	t.Logf("deleted secret object %s/%s", ns, name)
}

func k8sGetIngress(t *testing.T, kc *kubernetes.Clientset, name, ns, cname string) *networkingv1.Ingress {
	t.Logf("Fetching ingress %s/%s in cluster: %s", ns, name, cname)
	obj, err := kc.NetworkingV1().Ingresses(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error in getting ingress %s/%s in cluster %s: %v", ns, name, cname, err)
	}
	return obj
}

func k8sCleanupIngressStatus(t *testing.T, kc *kubernetes.Clientset, cname string, ingObj *networkingv1.Ingress) *networkingv1.Ingress {
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": nil,
	})
	updatedIng, err := kc.NetworkingV1().Ingresses(ingObj.Namespace).Patch(context.TODO(), ingObj.Name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in updating ingress %s/%s in cluster %s: %v", ingObj.Namespace, ingObj.Name, cname, err)
	}
	patchPayloadJson := map[string]interface{}{
		"metadata": map[string]map[string]string{
			"annotations": nil,
		},
	}
	patchPayloadBytes, _ := json.Marshal(patchPayloadJson)
	updatedIng, err = kc.NetworkingV1().Ingresses(ingObj.Namespace).Patch(context.TODO(), ingObj.Name, types.MergePatchType, patchPayloadBytes, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("error in updating ingress %s/%s in cluster %s: %v", ingObj.Namespace, ingObj.Name, cname, err)
	}
	return updatedIng
}

func k8sAddLBService(t *testing.T, kc *kubernetes.Clientset, name, ns string, hostIPs map[string]string, port int32) *corev1.Service {
	svcObj := BuildLBServiceObj(t, name, ns, hostIPs, port)
	hostname := name + "." + ns + "." + ingestion_test.TestDomain1
	svcObj.Annotations = getAnnotations([]string{hostname})

	svc, err := kc.CoreV1().Services(ns).Create(context.TODO(), svcObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service : %v", err)
	}

	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": svcObj.Status,
	})

	svc, err = kc.CoreV1().Services(ns).Patch(context.TODO(), name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in patching service: %v", err)
	}
	return svc
}

func k8sAddIngress(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, cname string,
	hostIPs map[string]string, paths []string, tls bool) *networkingv1.Ingress {

	secreName := "test-secret"
	if tls {
		secretObj := buildk8sSecret(ns)
		_, err := kc.CoreV1().Secrets(ns).Create(context.TODO(), secretObj, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("error in creating secret object %v: %v", secretObj, err)
		}
		t.Cleanup(func() {
			deletek8sSecret(t, kc, secretObj.Namespace, secretObj.Name)
		})
	}
	var ingObj *networkingv1.Ingress
	if tls {
		ingObj = BuildIngressObj(name, ns, svc, cname, hostIPs, paths, true, secreName)
	} else {
		ingObj = BuildIngressObj(name, ns, svc, cname, hostIPs, paths, true, "")
	}
	t.Logf("built an ingress object with name: %s, ns: %s, cname: %s", ns, name, cname)
	var hostnames []string
	for _, r := range ingObj.Spec.Rules {
		hostnames = append(hostnames, r.Host)
	}
	ingObj.Annotations = getAnnotations(hostnames)
	_, err := kc.NetworkingV1().Ingresses(ns).Create(context.TODO(), ingObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": ingObj.Status,
	})

	_, err = kc.NetworkingV1().Ingresses(ns).Patch(context.TODO(), name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in patching ingress: %v", err)
	}
	t.Logf("ingress object successfully created with name: %s, ns: %s, cname: %s", ns, name, cname)
	return ingObj
}

func oshiftAddRoute(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, cname, host,
	ip, path string, tls bool) *routev1.Route {
	routeObj := BuildRouteObj(name, ns, svc, cname, host, ip, path, true)
	if tls {
		routeObj.Spec.TLS = &routev1.TLSConfig{
			Termination:   routev1.TLSTerminationEdge,
			Certificate:   "cert",
			Key:           "key",
			CACertificate: "ca-cert",
		}
	}
	t.Logf("built a route object with name: %s, ns: %s and cname: %s", name, ns, cname)
	// applying annotations
	hostname := routeObj.Spec.Host
	routeObj.Annotations = getAnnotations([]string{hostname})
	newObj, err := oshiftClient.RouteV1().Routes(ns).Create(context.TODO(), routeObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Couldn't create route obj: %v, err: %v", routeObj, err)
	}
	t.Logf("route object successfully created with name: %s, ns: %s, cname: %s", ns, name, cname)
	return newObj
}

func k8sDeleteIngress(t *testing.T, kc *kubernetes.Clientset, name string, ns string) {
	err := kc.NetworkingV1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in deleting ingress: %v", err)
	}
}

func oshiftDeleteRoute(t *testing.T, kc *kubernetes.Clientset, name string, ns string) {
	err := oshiftClient.RouteV1().Routes(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Couldn't delete route obj: %v, err: %v", name, err)
	}
}

func k8sDeleteService(t *testing.T, kc *kubernetes.Clientset, name string, ns string) {
	err := kc.CoreV1().Services(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in deleting service: %v", err)
	}
}

func k8sUpdateIngress(t *testing.T, kc *kubernetes.Clientset, name, ns, svc string, hostIPs map[string]string, paths []string) *networkingv1.Ingress {
	ingress, err := kc.NetworkingV1().Ingresses(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error in getting ingress: %v", err)
	}
	var ingPaths []networkingv1.HTTPIngressPath
	var pathType networkingv1.PathType = "ImplementationSpecific"
	ingress.Spec.Rules = []networkingv1.IngressRule{}
	ingress.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{}
	for _, path := range paths {
		ingPath := networkingv1.HTTPIngressPath{
			Path:     path,
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: svc,
					Port: networkingv1.ServiceBackendPort{
						Number: 8080,
					},
				},
			},
		}
		ingPaths = append(ingPaths, ingPath)
	}

	var hosts []string
	for ingHost, ingIP := range hostIPs {
		hosts = append(hosts, ingHost)
		ingress.Spec.Rules = append(ingress.Spec.Rules, networkingv1.IngressRule{
			Host: ingHost,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: ingPaths,
				},
			},
		})
		ingress.Status.LoadBalancer.Ingress = append(ingress.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{
			IP:       ingIP,
			Hostname: ingHost,
		})
	}
	var hostnames []string
	for _, r := range ingress.Spec.Rules {
		hostnames = append(hostnames, r.Host)
	}
	ingress.Annotations = getAnnotations(hostnames)
	if ingress.Spec.TLS != nil && len(ingress.Spec.TLS) > 0 {
		ingress.Spec.TLS[0].Hosts = hostnames
	}
	_, err = kc.NetworkingV1().Ingresses(ns).Update(context.TODO(), ingress, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("error in updating ingress: %v", err)
	}
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": ingress.Status,
	})
	_, err = kc.NetworkingV1().Ingresses(ns).Patch(context.TODO(), name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in patching ingress: %v", err)
	}
	return ingress
}

func oshiftUpdateRoute(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, host,
	ip, path string) *routev1.Route {
	route, err := oshiftClient.RouteV1().Routes(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error in getting route: %v", err)
	}
	route.Spec.Host = host
	route.Spec.Path = path

	route.Status.Ingress[0].Host = host
	route.Annotations = getAnnotations([]string{host})
	_, err = oshiftClient.RouteV1().Routes(ns).Update(context.TODO(), route, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("error in updating ingress: %v", err)
	}
	return route
}

func k8sUpdateLBServicePort(t *testing.T, kc *kubernetes.Clientset, name, ns string, port int32) *corev1.Service {
	svc, err := kc.CoreV1().Services(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting service : %v", err)
	}
	svc.Spec.Ports[0].Port = port

	_, err = kc.CoreV1().Services(ns).Update(context.TODO(), svc, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("error updating service : %v", err)
	}
	return svc
}

func BuildAddAndVerifyAppSelectorTestGDP(t *testing.T) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	gdpObj := GetTestDefaultGDPObject()
	gdpObj.Spec.MatchRules.AppSelector = gdpalphav2.AppSelector{
		Label: appLabel,
	}
	gdpObj.Spec.MatchClusters = []gdpalphav2.ClusterProperty{
		{Cluster: K8sContext}, {Cluster: OshiftContext},
	}
	return AddAndVerifyTestGDPSuccess(t, gdpObj)
}

func BuildAddAndVerifyPoolPriorityTestGDP(t *testing.T, trafficSplit []gdpalphav2.TrafficSplitElem) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	gdpObj := GetTestDefaultGDPObject()
	gdpObj.Spec.MatchRules.AppSelector = gdpalphav2.AppSelector{
		Label: appLabel,
	}
	gdpObj.Spec.MatchClusters = []gdpalphav2.ClusterProperty{
		{Cluster: K8sContext}, {Cluster: OshiftContext},
	}

	// add pool priority
	gdpObj.Spec.TrafficSplit = trafficSplit
	return AddAndVerifyTestGDPSuccess(t, gdpObj)
}

func BuildIngressKeyAndVerify(t *testing.T, timeoutExpected bool, op, cname, ns, name, hostname string) {
	expectedKey := ingestion_test.GetIngressKey(op, cname, ns, name, hostname)
	t.Logf("key: %s, msg: will verify key", expectedKey)
	passed, errStr := ingestion_test.WaitAndVerify(t, []string{expectedKey}, timeoutExpected, ingestionKeyChan)
	if !passed {
		t.Fatalf(errStr)
	}
}

func BuildRouteKeyAndVerify(t *testing.T, timeoutExpected bool, op, cname, ns, name string) {
	expectedKey := ingestion_test.GetRouteKey(op, cname, ns, name)
	t.Logf("key: %s, msg: will verify key", expectedKey)
	passed, errStr := ingestion_test.WaitAndVerify(t, []string{expectedKey}, timeoutExpected, ingestionKeyChan)
	if !passed {
		t.Fatalf(errStr)
	}
}

func GetTestDefaultGDPObject() *gdpalphav2.GlobalDeploymentPolicy {
	matchRules := gdpalphav2.MatchRules{}
	matchClusters := []gdpalphav2.ClusterProperty{}
	return &gdpalphav2.GlobalDeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: AviSystemNS,
			Name:      GDPName,
		},
		Spec: gdpalphav2.GDPSpec{
			MatchRules:    matchRules,
			MatchClusters: matchClusters,
		},
	}
}

func AddTestGDP(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	newGdpObj, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(gdp.Namespace).Create(context.TODO(),
		gdp, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	t.Logf("created new GDP object %s in %s namespace", newGdpObj.Name, newGdpObj.Namespace)
	return newGdpObj, nil
}

func UpdateTestGDP(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	newGdpObj, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(gdp.Namespace).Update(context.TODO(),
		gdp, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	t.Logf("updated GDP object %s in %s namespace", newGdpObj.Name, newGdpObj.Namespace)
	return newGdpObj, nil
}

func GetTestGDP(t *testing.T, name, ns string) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	gdpObj, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(ns).Get(context.TODO(),
		name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	t.Logf("fetched GDP object %s in %s namespace", gdpObj.Name, gdpObj.Namespace)
	return gdpObj, nil
}

func VerifyGDPStatus(t *testing.T, ns, name, status string) {
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() string {
		gdpObj, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			t.Logf("failed to fetch GDP object: %v", err)
			return ""
		}
		return gdpObj.Status.ErrorStatus
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(status), "GDP status must be equal to %s", status)
}

func AddAndVerifyTestGDPSuccess(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	newGdpObj, err := AddTestGDP(t, gdp)
	if err != nil {
		return nil, err
	}
	VerifyGDPStatus(t, newGdpObj.Namespace, newGdpObj.Name, "success")
	return newGdpObj, nil
}

func UpdateAndVerifyTestGDPPrioritySuccess(t *testing.T, name, ns string, trafficSplit []gdpalphav2.TrafficSplitElem) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	oldGdpObj, err := GetTestGDP(t, name, ns)
	if err != nil {
		t.Fatal(err)
	}
	oldGdpObj.Spec.TrafficSplit = trafficSplit
	newGdpObj, err := UpdateTestGDP(t, oldGdpObj)
	if err != nil {
		return nil, err
	}
	VerifyGDPStatus(t, newGdpObj.Namespace, newGdpObj.Name, "success")
	return newGdpObj, nil
}

func AddAndVerifyTestGDPStatus(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy, status string) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	newGdpObj, err := AddTestGDP(t, gdp)
	if err != nil {
		return nil, err
	}
	VerifyGDPStatus(t, newGdpObj.Namespace, newGdpObj.Name, status)
	return newGdpObj, nil
}

func GetTestGSGraphFromName(t *testing.T, gsName string) *nodes.AviGSObjectGraph {
	gsList := nodes.SharedAviGSGraphLister()
	key := utils.ADMIN_NS + "/" + gsName
	found, gsObj := gsList.Get(key)
	if !found {
		t.Logf("error in fetching GS for key %s", key)
		return nil
	}
	gsGraph := gsObj.(*nodes.AviGSObjectGraph)
	return gsGraph.GetCopy()
}

func BuildTestPathHmNames(hostname string, paths []string, tls bool) []string {
	httpType := "http"
	if tls {
		httpType = "https"
	}
	hmNames := []string{}
	for _, p := range paths {
		hmName := "amko--" + gslbutils.EncodeHMName(httpType+"--"+hostname+"--"+p)
		hmNames = append(hmNames, hmName)
	}
	return hmNames
}

func BuildTestNonPathHmNames(hostname string) string {
	return nodes.HmNamePrefix + gslbutils.EncodeHMName(hostname)
}

func BuildExpectedPathHM(host string, paths []string, tls bool) nodes.HealthMonitor {
	protocol := "http"
	if tls {
		protocol = "https"
	}
	pathHMs := []nodes.PathHealthMonitorDetails{}
	for _, path := range paths {
		pathHm := nodes.PathHealthMonitorDetails{
			Name:            nodes.HmNamePrefix + gslbutils.EncodeHMName(protocol+"--"+host+"--"+path),
			IngressProtocol: protocol,
			Path:            path,
		}
		pathHMs = append(pathHMs, pathHm)
	}
	return nodes.HealthMonitor{
		Name:       "",
		HMProtocol: "",
		Port:       0,
		Type:       nodes.PathHM,
		PathHM:     pathHMs,
	}
}

func BuildExpectedNonPathHmDescription(host string) nodes.HealthMonitor {
	return nodes.HealthMonitor{
		Name:       nodes.HmNamePrefix + gslbutils.EncodeHMName(host),
		HMProtocol: "",
		Port:       0,
		Type:       nodes.NonPathHM,
		PathHM:     nil,
	}
}

func BuildExpectedPathHmDescriptionString(host string, path []string, tls bool) []string {
	hm := BuildExpectedPathHM(host, path, tls)
	descList := []string{}
	for _, pathHm := range hm.PathHM {
		descList = append(descList, pathHm.GetPathHMDescription(host))
	}
	return descList
}

func BuildExpectedNonPathHmDescriptionString(host string) string {
	hm := BuildExpectedNonPathHmDescription(host)
	return hm.GetHMDescription(host)[0]
}

func compareHmRefs(t *testing.T, expectedHmRefs, fetchedHmRefs []string) bool {
	for idx, h := range expectedHmRefs {
		if h != fetchedHmRefs[idx] {
			t.Logf("hm ref didn't match, expected list: %v, fetched list: %v", expectedHmRefs, fetchedHmRefs)
			return false
		}
	}
	return true
}

func verifyGSMembers(t *testing.T, expectedMembers []nodes.AviGSK8sObj, name string, tenant string,
	hmRefs []string, sitePersistenceRef *string, ttl *int, pa *gslbalphav1.PoolAlgorithmSettings, paths []string, tls bool, port *int32) bool {

	gs := GetTestGSGraphFromName(t, name)
	if gs == nil {
		t.Logf("GS Graph is nil, this is unexpected")
		return false
	}
	members := gs.MemberObjs
	if len(members) != len(expectedMembers) {
		t.Logf("length of members don't match")
		return false
	}

	if hmRefs != nil && len(hmRefs) != 0 {
		sort.Strings(hmRefs)
		if !strings.HasPrefix(hmRefs[0], "amko--") {
			// hm not created by amko
			fetchedHmRefs := gs.HmRefs
			sort.Strings(fetchedHmRefs)
			if len(hmRefs) != len(fetchedHmRefs) {
				t.Logf("length of hm refs don't match, expected: %v, got: %v", hmRefs, fetchedHmRefs)
				return false
			}

			if len(hmRefs) != 0 && !compareHmRefs(t, hmRefs, fetchedHmRefs) {
				return false
			}
		} else if paths != nil {
			// path based HMs
			fetchedPathHM := gs.Hm.PathHM
			expectedPathHM := BuildExpectedPathHM(name, paths, tls).PathHM
			if len(expectedPathHM) != len(fetchedPathHM) {
				t.Logf("expected path hm lenght doesnt match fetched path hm length, expected path hm: %v, fetched path hm : %v",
					expectedPathHM, fetchedPathHM)
				return false
			}
			matchedMembersLen := 0
			for _, fetchedPathHm := range fetchedPathHM {
				for _, expectedPathHm := range expectedPathHM {
					if fetchedPathHm.Name == expectedPathHm.Name && fetchedPathHm.Path == expectedPathHm.Path &&
						fetchedPathHm.IngressProtocol == expectedPathHm.IngressProtocol {
						matchedMembersLen = matchedMembersLen + 1
					}
				}
			}
			if matchedMembersLen != len(fetchedPathHM) {
				t.Logf("expected path hms and fetched path hms donot match, expected path hm : %v, fetched path hm %v",
					expectedPathHM, fetchedPathHM)
				return false
			}
		} else {
			// non path based HM
			if gs.Hm.Name != hmRefs[0] {
				t.Logf("hm names do not match, expected : %s, got : %s", hmRefs[0], gs.Hm.Name)
				return false
			}
			if port != nil && *port != gs.Hm.Port {
				t.Logf("hm port do not match, expected : %d, got : %d", *port, gs.Hm.Port)
				return false
			}
		}
	}
	if sitePersistenceRef != nil {
		if gs.SitePersistenceRef == nil {
			t.Logf("Site persistence ref should not be nil, expected value: %s", *sitePersistenceRef)
			return false
		}
		if *sitePersistenceRef != *gs.SitePersistenceRef {
			t.Logf("Site persistence should be %s, it is %s", *sitePersistenceRef, *gs.SitePersistenceRef)
			return false
		}
	} else {
		if gs.SitePersistenceRef != nil {
			t.Logf("Site persistence ref should be nil, it is %s", *gs.SitePersistenceRef)
			return false
		}
	}

	if ttl != nil {
		if gs.TTL == nil {
			t.Logf("TTL should not be nil")
			return false
		}
		if *gs.TTL != *ttl {
			t.Logf("TTL values should be equal, expected: %d, fetched: %d", *ttl, *gs.TTL)
			return false
		}
	} else {
		if gs.TTL != nil {
			t.Logf("TTL value should be nil, it is %d", *gs.TTL)
			return false
		}
	}

	if !reflect.DeepEqual(pa, gs.GslbPoolAlgorithm) {
		expected := spew.Sprintf("%v", pa)
		got := spew.Sprintf("%v", gs.GslbPoolAlgorithm)
		t.Logf("Pool algorithm settings don't match, expected: %v, got: %v", expected, got)
	}

	for _, e := range expectedMembers {
		for _, m := range members {
			if e.Cluster != m.Cluster || e.Namespace != m.Namespace || e.Name != m.Name {
				continue
			}
			if e.IPAddr != m.IPAddr {
				t.Logf("IP address don't match, expected: %s, fetched: %s", e.IPAddr, m.IPAddr)
				return false
			}
			if e.ControllerUUID != m.ControllerUUID {
				t.Logf("Controller UUIDs don't match for member, expected: %s, fetched: %s", e.ControllerUUID,
					m.ControllerUUID)
				return false
			}
			if e.IsPassthrough != m.IsPassthrough {
				t.Logf("IsPassthrough don't match for member, expected: %v, fetched: %v", e.IsPassthrough,
					m.IsPassthrough)
				return false
			}
			if e.Weight != m.Weight {
				t.Logf("Weight for members don't match, expected: %d, fetched: %d", e.Weight, m.Weight)
				return false
			}
			if e.Priority != m.Priority {
				t.Logf("Priorities for members don't match, expected: %d, fetched: %d", e.Priority, m.Priority)
				return false
			}
			if e.TLS != m.TLS {
				t.Logf("TLS for members don't match, expected: %v, fetched: %v", e.TLS, m.TLS)
				return false
			}
			if e.VirtualServiceUUID != m.VirtualServiceUUID {
				t.Logf("VS UUIDs should match, expected: %v, fetched: %v", e.VirtualServiceUUID,
					m.VirtualServiceUUID)
				return false
			}
		}
	}
	return true
}

func getTestGSMemberFromIng(t *testing.T, ingObj *networkingv1.Ingress, cname string,
	weight int32, priority int32) nodes.AviGSK8sObj {
	vsUUIDs := make(map[string]string)
	if err := json.Unmarshal([]byte(ingObj.Annotations[k8sobjects.VSAnnotation]), &vsUUIDs); err != nil {
		t.Fatalf("error in getting annotations from ingress object %v: %v", ingObj.Annotations, err)
	}
	hostName := ingObj.Spec.Rules[0].Host
	var tls bool
	if len(ingObj.Spec.TLS) != 0 {
		tls = true
	}

	paths := []string{}
	for _, rule := range ingObj.Spec.Rules {
		if rule.Host == hostName {
			if rule.HTTP == nil || rule.HTTP.Paths == nil || len(rule.HTTP.Paths) == 0 {
				paths = append(paths, "/")
				continue
			}
			for _, p := range rule.HTTP.Paths {
				paths = append(paths, p.Path)
			}
		}
	}
	return getTestGSMember(cname, gslbutils.IngressType, ingObj.Name, ingObj.Namespace,
		ingObj.Status.LoadBalancer.Ingress[0].IP, vsUUIDs[hostName],
		ingObj.Annotations[k8sobjects.ControllerAnnotation],
		true, false, tls, paths, weight, priority)
}

func getTestGSMemberFromRoute(t *testing.T, routeObj *routev1.Route, cname string,
	weight int32, priority int32) nodes.AviGSK8sObj {
	vsUUIDs := make(map[string]string)
	if err := json.Unmarshal([]byte(routeObj.Annotations[k8sobjects.VSAnnotation]), &vsUUIDs); err != nil {
		t.Fatalf("error in getting annotations from ingress object %v: %v", routeObj.Annotations, err)
	}
	hostName := routeObj.Spec.Host
	var tls bool
	if routeObj.Spec.TLS != nil {
		tls = true
	}
	paths := []string{routeObj.Spec.Path}

	return getTestGSMember(cname, gslbutils.RouteType, routeObj.Name, routeObj.Namespace,
		routeObj.Status.Ingress[0].Conditions[0].Message, vsUUIDs[hostName],
		routeObj.Annotations[k8sobjects.ControllerAnnotation],
		true, false, tls, paths, weight, priority)
}

func getTestGSMemberFromMultiPathRoute(t *testing.T, routeObjList []*routev1.Route, cname string,
	weight int32, priority int32) []nodes.AviGSK8sObj {
	var gsMemberList []nodes.AviGSK8sObj

	for _, routeObj := range routeObjList {
		vsUUIDs := make(map[string]string)
		if err := json.Unmarshal([]byte(routeObj.Annotations[k8sobjects.VSAnnotation]), &vsUUIDs); err != nil {
			t.Fatalf("error in getting annotations from route object %v: %v", routeObj.Annotations, err)
		}
		var tls bool
		if routeObj.Spec.TLS != nil {
			tls = true
		}
		gsMemberList = append(gsMemberList, getTestGSMember(cname, gslbutils.RouteType, routeObj.Name, routeObj.Namespace,
			routeObj.Status.Ingress[0].Conditions[0].Message, vsUUIDs[routeObj.Spec.Host],
			routeObj.Annotations[k8sobjects.ControllerAnnotation],
			true, false, tls, []string{routeObj.Spec.Path}, weight, priority))
	}
	return gsMemberList
}

func getTestGSMemberFromSvc(t *testing.T, svcObj *corev1.Service, cname string,
	weight int32, priority int32) nodes.AviGSK8sObj {
	vsUUIDs := make(map[string]string)
	if err := json.Unmarshal([]byte(svcObj.Annotations[k8sobjects.VSAnnotation]), &vsUUIDs); err != nil {
		t.Fatalf("error in getting annotations from ingress object %v: %v", svcObj.Annotations, err)
	}
	hostName := svcObj.Status.LoadBalancer.Ingress[0].Hostname

	return getTestGSMember(cname, gslbutils.SvcType, svcObj.Name, svcObj.Namespace,
		svcObj.Status.LoadBalancer.Ingress[0].IP, vsUUIDs[hostName],
		svcObj.Annotations[k8sobjects.ControllerAnnotation],
		true, false, false, []string{}, weight, priority)
}

func getTestGSMember(cname, objType, name, ns, ipAddr, vsUUID, controllerUUID string,
	syncVIPOnly, isPassthrough, tls bool, paths []string, weight int32, priority int32) nodes.AviGSK8sObj {
	return nodes.AviGSK8sObj{
		Cluster:            cname,
		ObjType:            objType,
		Name:               name,
		Namespace:          ns,
		IPAddr:             ipAddr,
		VirtualServiceUUID: vsUUID,
		ControllerUUID:     controllerUUID,
		SyncVIPOnly:        syncVIPOnly,
		IsPassthrough:      isPassthrough,
		TLS:                tls,
		Paths:              paths,
		Weight:             weight,
		Priority:           priority,
	}
}

func buildGSLBHostRule(name, ns, gsFqdn string, sitePersistence *gslbalphav1.SitePersistence,
	hmRefs []string, ttl *int) *gslbalphav1.GSLBHostRule {
	return &gslbalphav1.GSLBHostRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: gslbalphav1.GSLBHostRuleSpec{
			SitePersistence:   sitePersistence,
			HealthMonitorRefs: hmRefs,
			Fqdn:              gsFqdn,
			TTL:               ttl,
		},
	}
}

func deleteGSLBHostRule(t *testing.T, name, ns string) {
	err := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBHostRules(ns).Delete(context.TODO(),
		name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Fatalf("error in deleting gslb hostrule %s/%s: %v", ns, name, err)
	}
}

func addGSLBHostRule(t *testing.T, name, ns, gsFqdn string, hmRefs []string,
	sitePersistence *gslbalphav1.SitePersistence, ttl *int,
	status, errMsg string) *gslbalphav1.GSLBHostRule {

	gslbHR := buildGSLBHostRule(name, ns, gsFqdn, sitePersistence, hmRefs, ttl)
	newObj, err := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBHostRules(ns).Create(context.TODO(),
		gslbHR, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating a GSLB Host Rule object %v: %v", gslbHR, err)
	}
	t.Cleanup(func() {
		deleteGSLBHostRule(t, name, ns)
	})

	VerifyGSLBHostRuleStatus(t, ns, name, status, errMsg)
	return newObj
}

func updateGSLBHostRule(t *testing.T, gslbHRObj *gslbalphav1.GSLBHostRule, status, errMsg string) *gslbalphav1.GSLBHostRule {
	newObj, err := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBHostRules(gslbHRObj.Namespace).Update(context.TODO(),
		gslbHRObj, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("error in creating a GSLB Host Rule object %v: %v", gslbHRObj, err)
	}
	VerifyGSLBHostRuleStatus(t, gslbHRObj.Namespace, gslbHRObj.Name, status, errMsg)
	return newObj
}

func getGSLBHostRule(t *testing.T, name, ns string) *gslbalphav1.GSLBHostRule {
	obj, err := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBHostRules(ns).Get(context.TODO(),
		name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error in getting GSLB HostRule %s/%s: %v", ns, name, err)
	}
	return obj
}

func VerifyGSLBHostRuleStatus(t *testing.T, ns, name, status, errMsg string) {
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() bool {
		gslbHR, err := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBHostRules(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to fetch GSLBHostRule object %s/%s: %v", ns, name, err)
		}
		if gslbHR.Status.Status != status || gslbHR.Status.Error != errMsg {
			t.Logf("GSLB HostRule, expected status: %s, got: %s", status, gslbHR.Status.Status)
			t.Logf("GSLB HostRule, expected err: %s, got: %s", errMsg, gslbHR.Status.Error)
			return false
		}
		return true
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(true), "GSLB Host Rule status should match")
}

func GetTestGSFromRestCache(t *testing.T, gsName string) *avicache.AviGSCache {
	restLayerF := amkorest.NewRestOperations(nil, nil, nil)
	gsKey := avicache.TenantName{Tenant: utils.ADMIN_NS, Name: gsName}
	key := utils.ADMIN_NS + "/" + gsName
	gsObj := restLayerF.GetGSCacheObj(gsKey, key)
	if gsObj == nil {
		t.Logf("error in fetching GS from the rest cache for key: %v", gsKey)
		return nil
	}
	return gsObj
}

func verifyGSMembersInRestLayer(t *testing.T, expectedMembers []nodes.AviGSK8sObj, name string, tenant string,
	hmRefs []string, sitePersistenceRef *string, ttl *int, pa *gslbalphav1.PoolAlgorithmSettings, paths []string, tls bool) bool {

	gs := GetTestGSFromRestCache(t, name)
	if gs == nil {
		t.Logf("GS Graph is nil, this is unexpected")
		return false
	}
	members := gs.Members
	if len(members) != len(expectedMembers) {
		t.Logf("length of members don't match")
		return false
	}
	if hmRefs != nil && len(hmRefs) != 0 {
		sort.Strings(hmRefs)
		fetchedHmRefs := gs.HealthMonitor
		sort.Strings(fetchedHmRefs)
		if len(hmRefs) != len(fetchedHmRefs) {
			t.Logf("length of hm names don't match, expected: %v, got: %v", hmRefs, fetchedHmRefs)
			return false
		}
		if !compareHmRefs(t, hmRefs, fetchedHmRefs) {
			return false
		}
		fetchedHMObjs := amkorest.GetHMCacheObjFromGSCache(gs)
		if paths != nil {
			expectedHmDesc := BuildExpectedPathHmDescriptionString(name, paths, tls)
			hmDesc := []string{}
			for _, gsHm := range fetchedHMObjs {
				hmDesc = append(hmDesc, gsHm.Description)
			}
			if len(expectedHmDesc) != len(hmDesc) {
				t.Logf("length of hm descriptions dont match, expected: %v, got: %v", expectedHmDesc, hmDesc)
				return false
			}
			sort.Strings(expectedHmDesc)
			sort.Strings(hmDesc)
			for idx := range hmDesc {
				if hmDesc[idx] != expectedHmDesc[idx] {
					t.Logf("hm descriptions dont match, expected: %v, got: %v", expectedHmDesc, hmDesc)
					return false
				}
			}
		} else {
			for _, gsHm := range fetchedHMObjs {
				if gsHm.Description != BuildExpectedNonPathHmDescriptionString(name) {
					t.Logf("hm descriptions dont match")
				}
			}
		}

	}

	memberProperties := make(map[string]interface{})

	for _, e := range members {
		w := strconv.Itoa(int(e.Weight))
		m := e.Controller + "/" + e.IPAddr + "/" + e.VsUUID + "/" + w
		memberProperties[m] = struct{}{}
	}

	for _, e := range expectedMembers {
		w := strconv.Itoa(int(e.Weight))
		m := e.ControllerUUID + "/" + e.IPAddr + "/" + e.VirtualServiceUUID + "/" + w
		if _, ok := memberProperties[m]; !ok {
			t.Logf("members don't match, expected: %v, got: %v", m, memberProperties)
			return false
		}
	}
	return true
}

// Used to get union set of multi paths of ingress/route with the same host
func GetUniquePaths(paths []string) []string {
	uniquePathsSet := make(map[string]struct{})
	for _, path := range paths {
		uniquePathsSet[path] = struct{}{}
	}
	uniquePaths := []string{}
	for path := range uniquePathsSet {
		uniquePaths = append(uniquePaths, path)
	}
	return uniquePaths
}

// Can be used to get unqiue members when ingress/route have multi paths
func GetUniqueMembers(members []nodes.AviGSK8sObj) []nodes.AviGSK8sObj {
	uniqueMembers := []nodes.AviGSK8sObj{}
	for _, member := range members {
		exists := false
		for _, umem := range uniqueMembers {
			if member.IPAddr == umem.IPAddr {
				exists = true
				break
			}
		}
		if !exists {
			uniqueMembers = append(uniqueMembers, member)
		}
	}
	return uniqueMembers
}
