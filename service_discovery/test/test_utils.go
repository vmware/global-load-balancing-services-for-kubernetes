/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package test

import (
	"context"
	"fmt"
	"strconv"
	"time"

	b64 "encoding/base64"
	"encoding/json"

	. "github.com/onsi/gomega"
	amkovmwarecomv1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	sdutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	amkov1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var testEnv1 *envtest.Environment
var testEnv2 *envtest.Environment
var mgmtTestEnv *envtest.Environment

const (
	MemberCluster1          = "cluster1"
	MemberCluster2          = "cluster2"
	TestGSLBSecret          = "gslb-config-secret"
	TestMemberClusterSecret = "tenant-clusters-secret"
	AMKOCRDs                = "../../helm/amko/crds"
	TestClustersetName      = "test-clusterset"
)

// member cluster 1's constants:
const (
	Cluster1TestSvc       = "cluster1-svc1"
	Cluster1TestNS        = "blue"
	Cluster1TestSvcPort   = 80
	Cluster1TestNodePort  = 31000
	Cluster1TestSvc2      = "cluster1-svc2"
	Cluster1TestNS2       = "red"
	Cluster1TestSvcPort2  = 443
	Cluster1TestNodePort2 = 31001
	Cluster1Node1         = "10.10.10.10"
	Cluster1Node2         = "10.10.10.11"
	Cluster1Node1Name     = "cluster1-node1"
	Cluster1Node2Name     = "cluster2-node2"
)

// member cluster 2's constants:
const (
	Cluster2TestSvc       = "cluster2-svc1"
	Cluster2TestSvcPort   = 90
	Cluster2TestNS        = "green"
	Cluster2TestNodePort  = 32000
	Cluster2TestSvc2      = "cluster2-svc2"
	Cluster2TestNS2       = "yellow"
	Cluster2TestSvcPort2  = 8443
	Cluster2TestNodePort2 = 32001
	Cluster2Node1         = "10.10.20.10"
	Cluster2Node2         = "10.10.20.11"
	Cluster2Node1Name     = "cluster2-node1"
	Cluster2Node2Name     = "cluster2-node2"
)

const KubeConfigData = `
apiVersion: v1
clusters: []
contexts: []
kind: Config
preferences: {}
users: []
`

type ClustersKubeConfig struct {
	APIVersion string            `yaml:"apiVersion"`
	Clusters   []ClusterData     `yaml:"clusters"`
	Contexts   []KubeContextData `yaml:"contexts"`
	Kind       string            `yaml:"kind"`
	Users      []UserData        `yaml:"users"`
}

type ClusterData struct {
	Cluster ClusterServerData `yaml:"cluster"`
	Name    string            `yaml:"name"`
}

type ClusterServerData struct {
	CAData string `yaml:"certificate-authority-data"`
	Server string `yaml:"server"`
}

type KubeContextData struct {
	Context ContextData `yaml:"context"`
	Name    string      `yaml:"name"`
}

type ContextData struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

type UserData struct {
	Name string `yaml:"name"`
	User UserID `yaml:"user"`
}

type UserID struct {
	ClientCert string `yaml:"client-certificate-data"`
	ClientKey  string `yaml:"client-key-data"`
}

func BuildAndCreateTestKubeConfig(k8sClient1, k8sClient2, mgmtClient *kubernetes.Clientset) {
	user1 := MemberCluster1 + "-user"
	user2 := MemberCluster2 + "-user"

	// kData := make(map[string]interface{})
	kData := ClustersKubeConfig{}
	Expect(yaml.Unmarshal([]byte(KubeConfigData), &kData)).Should(Succeed())

	kData.Clusters = []ClusterData{
		{
			Cluster: ClusterServerData{
				CAData: b64.StdEncoding.EncodeToString([]byte(testEnv1.Config.CAData)),
				Server: testEnv1.Config.Host,
			},
			Name: MemberCluster1,
		},
		{
			Cluster: ClusterServerData{
				CAData: b64.StdEncoding.EncodeToString([]byte(testEnv2.Config.CAData)),
				Server: testEnv2.Config.Host,
			},
			Name: MemberCluster2,
		},
	}

	kData.Contexts = []KubeContextData{
		{
			Context: ContextData{
				Cluster: MemberCluster1,
				User:    user1,
			},
			Name: MemberCluster1,
		},
		{
			Context: ContextData{
				Cluster: MemberCluster2,
				User:    user2,
			},
			Name: MemberCluster2,
		},
	}

	kData.Users = []UserData{
		{
			Name: user1,
			User: UserID{
				ClientCert: b64.StdEncoding.EncodeToString([]byte(testEnv1.Config.CertData)),
				ClientKey:  b64.StdEncoding.EncodeToString([]byte(testEnv1.Config.KeyData)),
			},
		},
		{
			Name: user2,
			User: UserID{
				ClientCert: b64.StdEncoding.EncodeToString([]byte(testEnv2.Config.CertData)),
				ClientKey:  b64.StdEncoding.EncodeToString([]byte(testEnv2.Config.KeyData)),
			},
		},
	}

	// generate a string out of kubeCfg
	kubeCfgData, err := yaml.Marshal(kData)
	Expect(err).NotTo(HaveOccurred())

	// create the "avi-system" namespace
	nsObj := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: sdutils.AviSystemNS,
		},
	}

	_, err = mgmtClient.CoreV1().Namespaces().Create(context.TODO(), &nsObj, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	// build a secret object
	secretObj := BuildTestTenantSecretObj(kubeCfgData)
	mgmtClient.CoreV1().Secrets(sdutils.AviSystemNS).Create(context.TODO(),
		secretObj, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
}

func BuildTestTenantSecretObj(kubeCfgData []byte) *corev1.Secret {
	// the tenant cluster secret object is expected to have the kubeconfig data mapped to
	// the key: "clusters"
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestMemberClusterSecret,
			Namespace: sdutils.AviSystemNS,
		},
		StringData: map[string]string{
			"clusters": string(kubeCfgData),
		},
	}
}

func BuildAndCreateTestClusterset(mgmtAmkoClient *amkov1.Clientset) {
	cs := amkovmwarecomv1alpha1.ClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestClustersetName,
			Namespace: sdutils.AviSystemNS,
		},
		Spec: amkovmwarecomv1alpha1.ClusterSetSpec{
			SecretName: TestMemberClusterSecret,
			Clusters: []amkovmwarecomv1alpha1.ClusterConfig{
				{
					Context: MemberCluster1,
				},
				{
					Context: MemberCluster2,
				},
			},
		},
	}
	_, err := mgmtAmkoClient.AmkoV1alpha1().ClusterSets(sdutils.AviSystemNS).Create(context.TODO(),
		&cs, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
}

func getTestMCIObj(mciName string, configs []amkovmwarecomv1alpha1.BackendConfig) *amkovmwarecomv1alpha1.MultiClusterIngress {
	return &amkovmwarecomv1alpha1.MultiClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mciName,
			Namespace: sdutils.AviSystemNS,
		},
		Spec: amkovmwarecomv1alpha1.MultiClusterIngressSpec{
			Hostname:   "abc.avi.com",
			SecretName: "test-secret",
			Config:     configs,
		},
	}
}

func getTestBackendDefaultConfigs() []amkovmwarecomv1alpha1.BackendConfig {
	return []amkovmwarecomv1alpha1.BackendConfig{
		{
			Path:           "/foo",
			ClusterContext: MemberCluster1,
			Weight:         50,
			Service: amkovmwarecomv1alpha1.Service{
				Name:      Cluster1TestSvc,
				Port:      Cluster1TestSvcPort,
				Namespace: Cluster1TestNS,
			},
		},
		{
			Path:           "/bar",
			ClusterContext: MemberCluster2,
			Weight:         50,
			Service: amkovmwarecomv1alpha1.Service{
				Name:      Cluster2TestSvc,
				Port:      Cluster2TestSvcPort,
				Namespace: Cluster2TestNS,
			},
		},
	}
}

func getTestSvc(svcName, ns string, port, nodePort int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Spec: corev1.ServiceSpec{
			Type: "NodePort",
			Ports: []corev1.ServicePort{
				{
					Port:     port,
					NodePort: nodePort,
				},
			},
		},
	}
}

func AddNodesForCluster(kubeClient *kubernetes.Clientset, node1IP, node2IP, node1Name, node2Name string) {
	node := getTestNode(node1Name, node1IP)
	_, err := kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	node = getTestNode(node2Name, node2IP)
	_, err = kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func getTestNode(nodeName, nodeIP string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Spec: corev1.NodeSpec{
			PodCIDR: "192.168.1.1/24",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: nodeIP,
				},
				{
					Type:    corev1.NodeHostName,
					Address: nodeName,
				},
			},
		},
	}
}

func VerifyServiceImport(ctx context.Context, cname string, mgmtClient *amkov1.Clientset, obj *corev1.Service,
	kubeClient *kubernetes.Clientset, newMCIObj *amkovmwarecomv1alpha1.MultiClusterIngress, nodes []string,
	excludePorts []int32) {

	Eventually(func() string {
		return IsServiceImportExpected(ctx, mgmtClient, obj, cname, nodes, excludePorts)
	}, 5*time.Second, 1*time.Second).Should(Equal("success"))
}

func IsServiceImportExpected(ctx context.Context, mgmtClient *amkov1.Clientset, svc *corev1.Service,
	cname string, nodes []string, excludePorts []int32) string {

	siName := cname + "--" + svc.GetNamespace() + "--" + svc.GetName()
	fmt.Printf("fetching service import: %s\n", siName)

	si, err := mgmtClient.AmkoV1alpha1().ServiceImports(sdutils.AviSystemNS).Get(ctx, siName, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("unexpected error in getting service import %s: %v", siName, err)
	}

	// check for cluster, namespace and name
	if si.Spec.Cluster != cname {
		return fmt.Sprintf("error in cluster match for service import, expected: %s, got: %s\n",
			cname, si.Spec.Cluster)
	}

	if si.Spec.Namespace != svc.GetNamespace() {
		return fmt.Sprintf("error in namespace match for service import, expected: %s, got: %s\n",
			svc.GetNamespace(), si.Spec.Namespace)
	}

	if si.Spec.Service != svc.GetName() {
		return fmt.Sprintf("error in namespace match for service import, expected: %s, got: %s\n",
			svc.GetName(), si.Spec.Service)
	}

	// check for endpoints
	// build a list of expected endpoints
	expectedEndpoints := map[string]interface{}{}
	for _, p := range svc.Spec.Ports {
		for _, n := range nodes {
			expectedEndpoints[strconv.Itoa(int(p.Port))+"-"+strconv.Itoa(int(p.NodePort))+"-"+n] = struct{}{}
		}
	}

	fetchedEndpoints := map[string]interface{}{}
	for _, sp := range si.Spec.SvcPorts {
		for _, ep := range sp.Endpoints {
			fetchedEndpoints[strconv.Itoa(int(sp.Port))+"-"+strconv.Itoa(int(ep.Port))+"-"+ep.IP] = struct{}{}
		}
	}

	if len(expectedEndpoints) != len(fetchedEndpoints) {
		return fmt.Sprintf("length of expected and fetched endpoints do not match, expected: %v, fetched: %v",
			expectedEndpoints, fetchedEndpoints)
	}

	for k := range expectedEndpoints {
		if _, ok := fetchedEndpoints[k]; !ok {
			return fmt.Sprintf("%s not found in fetched endpoints: %v", k, fetchedEndpoints)
		}
	}

	return "success"
}

func CreateTestNamespacesInMemberClusters(k8sClient1 *kubernetes.Clientset, k8sClient2 *kubernetes.Clientset) {
	ns := BuildTestNamespace(Cluster1TestNS)
	_, err := k8sClient1.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	ns = BuildTestNamespace(Cluster1TestNS2)
	_, err = k8sClient1.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	ns = BuildTestNamespace(Cluster2TestNS)
	_, err = k8sClient2.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	ns = BuildTestNamespace(Cluster2TestNS2)
	_, err = k8sClient2.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func BuildTestNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func VerifyServiceImportNotExists(ctx context.Context, cname string, mgmtClient *amkov1.Clientset, svc *corev1.Service) {
	siName := cname + "--" + svc.GetNamespace() + "--" + svc.GetName()
	Eventually(func() error {
		fmt.Printf("searching for service import: %s\n", siName)
		_, err := mgmtClient.AmkoV1alpha1().ServiceImports(sdutils.AviSystemNS).Get(ctx, siName, metav1.GetOptions{})
		return err
	}, 5*time.Second, 1*time.Second).Should(HaveOccurred())
}

func UpdateTestSvcPort(ctx context.Context, k8sClient *kubernetes.Clientset, ns, svcName string,
	port, nodePort int32) {

	spec := map[string]interface{}{}
	svc, err := k8sClient.CoreV1().Services(ns).Get(ctx, svcName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	svc.Spec.Ports[0].Port = port
	svc.Spec.Ports[0].NodePort = nodePort
	spec["ports"] = svc.Spec.Ports

	patchPayload, err := json.Marshal(map[string]map[string]interface{}{
		"spec": spec,
	})
	Expect(err).NotTo(HaveOccurred())
	_, err = k8sClient.CoreV1().Services(ns).Patch(ctx, svc.GetName(), types.MergePatchType, patchPayload, metav1.PatchOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func UpdateTestSvcType(ctx context.Context, k8sClient *kubernetes.Clientset, ns, svcName string,
	svcType string, nodePort int32) {

	spec := map[string]interface{}{}
	svc, err := k8sClient.CoreV1().Services(ns).Get(ctx, svcName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	spec["type"] = svcType
	if svcType == string(corev1.ServiceTypeClusterIP) {
		svc.Spec.Ports[0].NodePort = 0
	} else if svcType == string(corev1.ServiceTypeNodePort) {
		svc.Spec.Ports[0].NodePort = nodePort
	}
	spec["ports"] = svc.Spec.Ports
	patchPayload, err := json.Marshal(map[string]map[string]interface{}{
		"spec": spec,
	})
	Expect(err).NotTo(HaveOccurred())
	_, err = k8sClient.CoreV1().Services(ns).Patch(ctx, svc.GetName(), types.MergePatchType, patchPayload, metav1.PatchOptions{})
	Expect(err).NotTo(HaveOccurred())
}
