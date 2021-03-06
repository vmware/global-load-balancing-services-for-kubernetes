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
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

func BuildIngressObj(name, ns, svc, cname string, hostIPs map[string]string, withStatus bool, secretName string) *networkingv1beta1.Ingress {
	ingObj := &networkingv1beta1.Ingress{}
	ingObj.Namespace = ns
	ingObj.Name = name

	var hosts []string
	for ingHost, ingIP := range hostIPs {
		hosts = append(hosts, ingHost)
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
	if secretName != "" {
		if len(ingObj.Spec.TLS) == 0 {
			ingObj.Spec.TLS = make([]networkingv1beta1.IngressTLS, 0)
		}
		ingObj.Spec.TLS = append(ingObj.Spec.TLS, networkingv1beta1.IngressTLS{
			Hosts:      hosts,
			SecretName: secretName,
		})
	}

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

func k8sGetIngress(t *testing.T, kc *kubernetes.Clientset, name, ns, cname string) *networkingv1beta1.Ingress {
	t.Logf("Fetching ingress %s/%s in cluster: %s", ns, name, cname)
	obj, err := kc.NetworkingV1beta1().Ingresses(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error in getting ingress %s/%s in cluster %s: %v", ns, name, cname, err)
	}
	return obj
}

func k8sCleanupIngressStatus(t *testing.T, kc *kubernetes.Clientset, cname string, ingObj *networkingv1beta1.Ingress) *networkingv1beta1.Ingress {
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": nil,
	})
	updatedIng, err := kc.NetworkingV1beta1().Ingresses(ingObj.Namespace).Patch(context.TODO(), ingObj.Name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in updating ingress %s/%s in cluster %s: %v", ingObj.Namespace, ingObj.Name, cname, err)
	}
	patchPayloadJson := map[string]interface{}{
		"metadata": map[string]map[string]string{
			"annotations": nil,
		},
	}
	patchPayloadBytes, _ := json.Marshal(patchPayloadJson)
	updatedIng, err = kc.NetworkingV1beta1().Ingresses(ingObj.Namespace).Patch(context.TODO(), ingObj.Name, types.MergePatchType, patchPayloadBytes, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("error in updating ingress %s/%s in cluster %s: %v", ingObj.Namespace, ingObj.Name, cname, err)
	}
	return updatedIng
}

func k8sAddIngress(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, cname string,
	hostIPs map[string]string, tls bool) *networkingv1beta1.Ingress {

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
	var ingObj *networkingv1beta1.Ingress
	if tls {
		ingObj = BuildIngressObj(name, ns, svc, cname, hostIPs, true, secreName)
	} else {
		ingObj = BuildIngressObj(name, ns, svc, cname, hostIPs, true, "")
	}
	t.Logf("built an ingress object with name: %s, ns: %s, cname: %s", ns, name, cname)
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
	ip string, tls bool) *routev1.Route {
	routeObj := BuildRouteObj(name, ns, svc, cname, host, ip, true)
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

func verifyGSMembers(t *testing.T, expectedMembers []nodes.AviGSK8sObj, name, tenant string,
	hmRefs []string, sitePersistenceRef *string, ttl *int, pa *gslbalphav1.PoolAlgorithmSettings) bool {

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

	sort.Strings(hmRefs)
	fetchedHmRefs := gs.HmRefs
	sort.Strings(fetchedHmRefs)
	if len(hmRefs) != len(fetchedHmRefs) {
		t.Logf("length of hm refs don't match, expected: %v, got: %v", hmRefs, fetchedHmRefs)
		return false
	}

	if len(hmRefs) != 0 {
		for idx, h := range hmRefs {
			if h != fetchedHmRefs[idx] {
				t.Logf("hm ref didn't match, expected list: %v, fetched list: %v", hmRefs, fetchedHmRefs)
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

func getTestGSMemberFromIng(t *testing.T, ingObj *networkingv1beta1.Ingress, cname string,
	weight int32) nodes.AviGSK8sObj {
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
		true, false, tls, paths, weight)
}

func getTestGSMemberFromRoute(t *testing.T, routeObj *routev1.Route, cname string,
	weight int32) nodes.AviGSK8sObj {
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
		true, false, tls, paths, weight)
}

func getTestGSMember(cname, objType, name, ns, ipAddr, vsUUID, controllerUUID string,
	syncVIPOnly, isPassthrough, tls bool, paths []string, weight int32) nodes.AviGSK8sObj {
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

func verifyGSMembersInRestLayer(t *testing.T, expectedMembers []nodes.AviGSK8sObj, name, tenant string,
	hmRefs []string, sitePersistenceRef *string, ttl *int, pa *gslbalphav1.PoolAlgorithmSettings) bool {

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

	sort.Strings(hmRefs)
	fetchedHmNames := gs.HealthMonitorNames
	sort.Strings(fetchedHmNames)
	if len(hmRefs) != len(fetchedHmNames) {
		t.Logf("length of hm names don't match, expected: %v, got: %v", hmRefs, fetchedHmNames)
		return false
	}

	if len(hmRefs) != 0 {
		for idx, h := range hmRefs {
			if h != fetchedHmNames[idx] {
				t.Logf("hm ref didn't match, expected list: %v, fetched list: %v", hmRefs, fetchedHmNames)
				return false
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
