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
	"testing"

	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

	AviSystemNS    = "avi-system"
	AviSecret      = "avi-secret"
	GslbConfigName = "test-gc"
	GDPName        = "test-gdp"
	K8sContext     = "k8s"
	OshiftContext  = "oshift"
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
)

var appLabel map[string]string = map[string]string{"key": "value"}

func BuildIngressObj(name, ns, svc, cname string, hostIPs map[string]string, withStatus bool) *networkingv1beta1.Ingress {
	ingObj := &networkingv1beta1.Ingress{}
	ingObj.Namespace = ns
	ingObj.Name = name

	for ingHost, ingIP := range hostIPs {
		ingObj.Spec.Rules = append(ingObj.Spec.Rules, networkingv1beta1.IngressRule{
			Host: ingHost,
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
	return ingObj
}

func BuildRouteObj(name, ns, svc, cname, host, ip string, withStatus bool) *routev1.Route {
	routeObj := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: routev1.RouteSpec{
			Host: host,
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

func k8sAddIngress(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, cname string,
	hostIPs map[string]string) *networkingv1beta1.Ingress {

	ingObj := BuildIngressObj(name, ns, svc, cname, hostIPs, true)
	t.Logf("built an ingress object with name: %s, ns: %s, cname: %s", ns, name, cname)
	t.Logf("ingress: %v", ingObj)
	var hostnames []string
	for _, r := range ingObj.Spec.Rules {
		hostnames = append(hostnames, r.Host)
	}
	ingObj.Annotations = getAnnotations(hostnames)
	_, err := kc.NetworkingV1beta1().Ingresses(ns).Create(context.TODO(), ingObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": ingObj.Status,
	})

	_, err = kc.NetworkingV1beta1().Ingresses(ns).Patch(context.TODO(), name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in patching ingress: %v", err)
	}
	t.Logf("ingress object successfully created with name: %s, ns: %s, cname: %s", ns, name, cname)
	return ingObj
}

func oshiftAddRoute(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, cname, host,
	ip string) *routev1.Route {
	routeObj := BuildRouteObj(name, ns, svc, cname, host, ip, true)
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
	err := kc.NetworkingV1beta1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("error in creating ingress: %v", err)
	}
}

func oshiftDeleteRoute(t *testing.T, kc *kubernetes.Clientset, name string, ns string) {
	err := oshiftClient.RouteV1().Routes(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Couldn't delete route obj: %v, err: %v", name, err)
	}
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

func VerifyGDPStatus(t *testing.T, ns, name, status string) {
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() string {
		gdpObj, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			t.Errorf("failed to fetch GDP object: %v", err)
			return ""
		}
		return gdpObj.Status.ErrorStatus
	}).Should(gomega.Equal(status))
}

func AddAndVerifyTestGDPSuccess(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy) (*gdpalphav2.GlobalDeploymentPolicy, error) {
	newGdpObj, err := AddTestGDP(t, gdp)
	if err != nil {
		return nil, err
	}
	VerifyGDPStatus(t, newGdpObj.Namespace, newGdpObj.Name, "success")
	return newGdpObj, nil
}
