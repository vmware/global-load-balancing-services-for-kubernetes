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

package custom_fqdn

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	ingestion_test "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/ingestion"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"
	akov1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	hrcs "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
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
	AmkoCRDs   = "../../../../helm/amko/crds"
	AkoCRDs    = "../../crds/ako"
	oshiftCRDs = "../../crds/oshift"

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
	routeCRD             apiextensionv1.CustomResourceDefinition
	hrCRD                apiextensionv1.CustomResourceDefinition
)

var appLabel map[string]string = map[string]string{"key": "value"}

func BuildIngressObj(name, ns, svc, cname string, hostIPs map[string]string, withStatus bool, secretName string) *networkingv1.Ingress {
	ingObj := &networkingv1.Ingress{}
	ingObj.Namespace = ns
	ingObj.Name = name

	var hosts []string
	for ingHost, ingIP := range hostIPs {
		hosts = append(hosts, ingHost)
		ingObj.Spec.Rules = append(ingObj.Spec.Rules, networkingv1.IngressRule{
			Host: ingHost,
		})
		if !withStatus {
			continue
		}
		ingObj.Status.LoadBalancer.Ingress = append(ingObj.Status.LoadBalancer.Ingress, networkingv1.IngressLoadBalancerIngress{
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

func k8sAddIngress(t *testing.T, kc *kubernetes.Clientset, name, ns, svc, cname string,
	hostIPs map[string]string, tls bool) *networkingv1.Ingress {

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
	t.Logf("route created %+v", newObj)
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": routeObj.Status,
	})
	newObj, err = oshiftClient.RouteV1().Routes(ns).Patch(context.TODO(), routeObj.Name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("Couldn't update route obj: %v, err: %v", newObj, err)
	}
	t.Logf("route updated %+v", newObj)

	t.Logf("route object successfully created with name: %s, ns: %s, cname: %s", ns, name, cname)
	return newObj
}

func k8sDeleteIngress(t *testing.T, kc *kubernetes.Clientset, name string, ns string) {
	err := kc.NetworkingV1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
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
	newGdpObj, err := gslbutils.AMKOControlConfig().GDPClientset().AmkoV1alpha2().GlobalDeploymentPolicies(gdp.Namespace).Create(context.TODO(),
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
		gdpObj, err := gslbutils.AMKOControlConfig().GDPClientset().AmkoV1alpha2().GlobalDeploymentPolicies(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to fetch GDP object: %v", err)
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

func AddAndVerifyTestGDPFailure(t *testing.T, gdp *gdpalphav2.GlobalDeploymentPolicy, status string) (*gdpalphav2.GlobalDeploymentPolicy, error) {
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

// extraArgs can have the following additional parameters:
// 1. tls
// 2. hmTemplate
// the sequence must be followed to maintain the API.
func verifyGSMembers(t *testing.T, expectedMembers []nodes.AviGSK8sObj, name, tenant string,
	hmRefs []string, sitePersistenceRef *string, ttl *int, expectedDomainNames []string, extraArgs ...interface{}) bool {

	var tls bool
	if len(extraArgs) > 2 {
		t.Fatalf("extraArgs for verifyGSMembers given unsupported number of parameters")
	}
	if len(extraArgs) == 1 {
		tls = extraArgs[0].(bool)
	}
	var hmTemplate *string = nil
	if len(extraArgs) == 2 {
		hmTemplate = extraArgs[1].(*string)
	}

	gs := GetTestGSGraphFromName(t, name)
	if gs == nil {
		t.Logf("GS Graph is nil, this is unexpected")
		return false
	}
	members := gs.MemberObjs
	if len(members) != len(expectedMembers) {
		t.Logf("length of members don't match, expectedMembers: %v, members: %v", expectedMembers, members)
		return false
	}

	if !gslbutils.SetEqual(expectedDomainNames, gs.DomainNames) {
		t.Logf("GS Domain names didn't match, expected: %v, got: %v", expectedDomainNames, gs.DomainNames)
		return false
	}

	if hmTemplate != nil {
		if *gs.HmTemplate != *hmTemplate {
			t.Logf("hm template didn't match, expected: %s, fetched: %s", *hmTemplate, *gs.HmTemplate)
			return false
		}
	} else if len(hmRefs) != 0 {
		sort.Strings(hmRefs)
		fetchedHmRefs := gs.HmRefs
		sort.Strings(fetchedHmRefs)
		if len(hmRefs) != len(fetchedHmRefs) {
			t.Logf("length of hm refs don't match")
			return false
		}
		for idx, h := range hmRefs {
			if h != fetchedHmRefs[idx] {
				t.Logf("hm ref didn't match, expected list: %v, fetched list: %v", hmRefs, fetchedHmRefs)
				return false
			}
		}
	} else {
		// default HM(s)
		if tls {
			if gs.Hm.HMProtocol != gslbutils.SystemGslbHealthMonitorHTTPS {
				t.Logf("hm protocol didn't match, expected: %s, got: %s", gslbutils.SystemGslbHealthMonitorHTTPS,
					gs.Hm.HMProtocol)
				return false
			}
		} else {
			if gs.Hm.HMProtocol != gslbutils.SystemGslbHealthMonitorHTTP {
				t.Logf("hm protocol didn't match, expected: %s, got: %s", gslbutils.SystemGslbHealthMonitorHTTP,
					gs.Hm.HMProtocol)
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

func verifyGSDoesNotExist(t *testing.T, name string) bool {
	gs := GetTestGSGraphFromName(t, name)
	return gs == nil
}

func getTestGSMemberFromIng(t *testing.T, ingObj *networkingv1.Ingress, cname string,
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
	err := gslbutils.AMKOControlConfig().GSLBClientset().AmkoV1alpha1().GSLBHostRules(ns).Delete(context.TODO(),
		name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Fatalf("error in deleting gslb hostrule %s/%s: %v", ns, name, err)
	}
}

func addGSLBHostRule(t *testing.T, name, ns, gsFqdn string, hmRefs []string,
	sitePersistence *gslbalphav1.SitePersistence, ttl *int,
	status, errMsg string) *gslbalphav1.GSLBHostRule {

	gslbHR := buildGSLBHostRule(name, ns, gsFqdn, sitePersistence, hmRefs, ttl)
	newObj, err := gslbutils.AMKOControlConfig().GSLBClientset().AmkoV1alpha1().GSLBHostRules(ns).Create(context.TODO(),
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
	newObj, err := gslbutils.AMKOControlConfig().GSLBClientset().AmkoV1alpha1().GSLBHostRules(gslbHRObj.Namespace).Update(context.TODO(),
		gslbHRObj, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("error in creating a GSLB Host Rule object %v: %v", gslbHRObj, err)
	}
	VerifyGSLBHostRuleStatus(t, gslbHRObj.Namespace, gslbHRObj.Name, status, errMsg)
	return newObj
}

func getGSLBHostRule(t *testing.T, name, ns string) *gslbalphav1.GSLBHostRule {
	obj, err := gslbutils.AMKOControlConfig().GSLBClientset().AmkoV1alpha1().GSLBHostRules(ns).Get(context.TODO(),
		name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error in getting GSLB HostRule %s/%s: %v", ns, name, err)
	}
	return obj
}

func VerifyGSLBHostRuleStatus(t *testing.T, ns, name, status, errMsg string) {
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() bool {
		gslbHR, err := gslbutils.AMKOControlConfig().GSLBClientset().AmkoV1alpha1().GSLBHostRules(ns).Get(context.TODO(), name, metav1.GetOptions{})
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

func getDefaultHostRule(name, ns, lfqdn, status string) *akov1alpha1.HostRule {
	return &akov1alpha1.HostRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: akov1alpha1.HostRuleSpec{
			VirtualHost: akov1alpha1.HostRuleVirtualHost{
				Fqdn: lfqdn,
			},
		},
		Status: akov1alpha1.HostRuleStatus{
			Status: status,
		},
	}
}

func getHostRuleForCustomFqdn(name, ns, lfqdn, gfqdn, status string) *akov1alpha1.HostRule {
	hr := getDefaultHostRule(name, ns, lfqdn, status)
	hr.Spec.VirtualHost.Gslb = akov1alpha1.HostRuleGSLB{
		Fqdn: gfqdn,
	}
	return hr
}

func getDefaultAliases(objType string) []string {
	return []string{
		objType + "_alias1" + ".avi.com",
		objType + "_alias2" + ".avi.com",
		objType + "_alias3" + ".avi.com",
	}
}

func getHostRuleWithAliases(name, objType, ns, lfqdn, status string, aliases []string) *akov1alpha1.HostRule {
	if aliases == nil {
		aliases = getDefaultAliases(objType)
	}
	hr := getDefaultHostRule(name, ns, lfqdn, status)
	hr.Spec.VirtualHost.Aliases = aliases
	return hr
}

func getHostRuleWithAliasesForCustomFqdn(name, objType, ns, lfqdn, gfqdn, status string, aliases []string, includeAliases bool) *akov1alpha1.HostRule {
	if aliases == nil {
		aliases = getDefaultAliases(objType)
	}
	hr := getHostRuleForCustomFqdn(name, ns, lfqdn, gfqdn, status)
	hr.Spec.VirtualHost.Gslb.IncludeAliases = includeAliases
	hr.Spec.VirtualHost.Aliases = aliases
	return hr
}

func getDefaultExpectedDomainNames(gsName string, hrObjList []*akov1alpha1.HostRule) []string {
	aliasList := []string{}
	for _, hr := range hrObjList {
		aliasList = append(aliasList, hr.Spec.VirtualHost.Aliases...)
	}
	aliasSet := sets.NewString(aliasList...)
	return aliasSet.Insert(gsName).List()
}

func deleteHostRule(t *testing.T, cluster int, name, ns string) {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}

	err = hrClient.AkoV1alpha1().HostRules(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Fatalf("error in deleting hostrule for cluster %d: %v", cluster, err)
	}
}

func createHostRule(t *testing.T, cluster int, hr *akov1alpha1.HostRule) *akov1alpha1.HostRule {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}

	newHr, err := hrClient.AkoV1alpha1().HostRules(hr.Namespace).Create(context.TODO(), hr, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error in creating hostrule for cluster %d: %v", cluster, err)
	}
	updateHostRuleStatus(t, cluster, hr)
	t.Cleanup(func() {
		deleteHostRule(t, cluster, newHr.Name, newHr.Namespace)
	})
	return newHr
}

func updateHostRuleStatus(t *testing.T, cluster int, hr *akov1alpha1.HostRule) *akov1alpha1.HostRule {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}
	patchPayload, _ := json.Marshal(map[string]interface{}{
		"status": hr.Status,
	})
	newHr, err := hrClient.AkoV1alpha1().HostRules(hr.Namespace).Patch(context.TODO(), hr.Name, types.MergePatchType, patchPayload, metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatalf("error in updating the status of hostrule for cluster %d: %v", cluster, err)
	}
	return newHr
}

func updateHostRule(t *testing.T, cluster int, hr *akov1alpha1.HostRule) *akov1alpha1.HostRule {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}

	newHr, err := hrClient.AkoV1alpha1().HostRules(hr.Namespace).Update(context.TODO(), hr, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("error in updating hostrule for cluster %d: %v", cluster, err)
	}
	return newHr
}

func getTestHostRule(t *testing.T, cluster int, name, ns string) *akov1alpha1.HostRule {
	hrClient, err := hrcs.NewForConfig(cfgs[cluster])
	if err != nil {
		t.Fatalf("error in getting hostrule client for cluster %d: %v", cluster, err)
	}

	hr, err := hrClient.AkoV1alpha1().HostRules(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error in getting hostrule %s/%s: %v", ns, name, err)
	}
	return hr
}

func DeleteTestGDP(t *testing.T, ns, name string) error {
	err := gslbutils.AMKOControlConfig().GDPClientset().AmkoV1alpha2().GlobalDeploymentPolicies(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	t.Logf("deleted GDP %s in %s namespace", name, ns)
	return nil
}
