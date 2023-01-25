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
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	amkovmwarecomv1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	amkov1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	k8smodule "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/modules/k8s_module"
	sdutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Service Discovery Suite",
		[]Reporter{})

}

// All of the test suites will use 2 member clusters, one management (config) cluster.
// The management/config cluster will host the service discovery process and will import
// objects from the member clusters 1 and 2. MCI objects and Clusterset object will be
// created in the management cluster.

var mgmtCfg *rest.Config
var cfg1 *rest.Config
var cfg2 *rest.Config

var mgmtK8sClient *kubernetes.Clientset
var mgmtAmkoClient *amkov1.Clientset
var k8sClient1 *kubernetes.Clientset
var k8sClient2 *kubernetes.Clientset

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	// test envs for both member clusters
	testEnv1 = &envtest.Environment{}

	var err error
	cfg1, err = testEnv1.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg1).NotTo(BeNil())

	testEnv2 = &envtest.Environment{}
	cfg2, err = testEnv2.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg2).NotTo(BeNil())

	// Initialize management cluster config
	mgmtTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{AMKOCRDs},
		ErrorIfCRDPathMissing: true,
	}
	mgmtCfg, err = mgmtTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(mgmtCfg).NotTo(BeNil())

	err = amkovmwarecomv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	testScheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(testScheme))
	utilruntime.Must(amkovmwarecomv1alpha1.AddToScheme(testScheme))

	// get k8s clients for member clusters: cluster1 and cluster2
	k8sClient1, err = kubernetes.NewForConfig(cfg1)
	// k8sClient1, err = client.New(cfg1, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient1).NotTo(BeNil())
	// add nodes for member cluster 1
	AddNodesForCluster(k8sClient1, Cluster1Node1, Cluster1Node2, Cluster1Node1Name, Cluster1Node2Name)

	k8sClient2, err = kubernetes.NewForConfig(cfg2)
	// k8sClient2, err = client.New(cfg2, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient2).NotTo(BeNil())
	AddNodesForCluster(k8sClient2, Cluster2Node1, Cluster2Node2, Cluster2Node1Name, Cluster2Node2Name)

	// get k8s client for mgmt cluster
	mgmtK8sClient, err = kubernetes.NewForConfig(mgmtCfg)
	// mgmtK8sClient, err = client.New(mgmtCfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(mgmtK8sClient).NotTo(BeNil())

	mgmtAmkoClient, err = amkov1.NewForConfig(mgmtCfg)
	Expect(err).NotTo(HaveOccurred())

	// initialize service discovery config and informers
	stopCh := containerutils.SetupSignalHandler()

	// build and create the tenant members secret and a clusterset object
	fmt.Fprintf(GinkgoWriter, "building and creating secret\n")
	BuildAndCreateTestKubeConfig(k8sClient1, k8sClient2, mgmtK8sClient)
	fmt.Fprintf(GinkgoWriter, "building and creating clusterset\n")
	BuildAndCreateTestClusterset(mgmtAmkoClient)

	CreateTestNamespacesInMemberClusters(k8sClient1, k8sClient2)
	k8smodule.InitServiceDiscoveryConfigAndInformers(mgmtCfg, stopCh)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := mgmtTestEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = testEnv1.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = testEnv2.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("Service Validation", func() {
	backendConfigs := getTestBackendDefaultConfigs()
	mciObj := getTestMCIObj("test-svc-mci1", backendConfigs)
	cluster1Svc := getTestSvc(Cluster1TestSvc, Cluster1TestNS, Cluster1TestSvcPort, Cluster1TestNodePort)
	cluster2Svc := getTestSvc(Cluster2TestSvc, Cluster2TestNS, Cluster2TestSvcPort, Cluster2TestNodePort)

	Context("Given an MCI object with services from both clusters", func() {
		It("should create a service import object with information from both the clusters", func() {
			By("Creating two new services in two different member clusters")
			ctx := context.Background()
			newMCIObj, err := mgmtAmkoClient.AkoV1alpha1().MultiClusterIngresses(sdutils.AviSystemNS).Create(ctx, mciObj, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			cluster1Svc, err := k8sClient1.CoreV1().Services(cluster1Svc.GetNamespace()).Create(ctx, cluster1Svc, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			cluster2Svc, err := k8sClient2.CoreV1().Services(cluster2Svc.GetNamespace()).Create(ctx, cluster2Svc, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			VerifyServiceImport(ctx, MemberCluster1, mgmtAmkoClient, cluster1Svc, k8sClient1, newMCIObj,
				[]string{Cluster1Node1, Cluster1Node2}, []int32{})
			VerifyServiceImport(ctx, MemberCluster2, mgmtAmkoClient, cluster2Svc, k8sClient2, newMCIObj,
				[]string{Cluster2Node1, Cluster2Node2}, []int32{})
		})

		It("should update the service import object corresponding to cluster1", func() {
			By("updating cluster1's service with a different node port")
			ctx := context.Background()
			cluster1Svc.Spec.Ports[0].NodePort = Cluster1TestNodePort2
			UpdateTestSvcPort(ctx, k8sClient1, cluster1Svc.GetNamespace(), cluster1Svc.GetName(), Cluster1TestSvcPort,
				Cluster1TestNodePort2)
			VerifyServiceImport(ctx, MemberCluster1, mgmtAmkoClient, cluster1Svc, k8sClient1, mciObj,
				[]string{Cluster1Node1, Cluster1Node2}, []int32{})
		})

		It("should delete the service import object corresponding to cluster2", func() {
			By("updating cluster2's service with a different port, which is not present in the MCI object")
			ctx := context.Background()
			// Cluster2TestSvcPort2 is not present in the filter of service discovery agent, as this
			// was never added in the MCI object, so the ServiceImport object will be deleted
			cluster2Svc.Spec.Ports[0].Port = Cluster2TestSvcPort2
			UpdateTestSvcPort(ctx, k8sClient2, cluster2Svc.GetNamespace(), cluster2Svc.GetName(), Cluster2TestSvcPort2,
				Cluster2TestNodePort)
			VerifyServiceImportNotExists(ctx, MemberCluster2, mgmtAmkoClient, cluster2Svc)
		})

		It("should create back the service import object corresponding to cluster2", func() {
			By("updating cluster1's service with a port which is present in the MCI object")
			ctx := context.Background()
			cluster2Svc.Spec.Ports[0].Port = Cluster2TestSvcPort
			UpdateTestSvcPort(ctx, k8sClient2, cluster2Svc.GetNamespace(), cluster2Svc.GetName(), Cluster2TestSvcPort,
				Cluster2TestNodePort)
			VerifyServiceImport(ctx, MemberCluster2, mgmtAmkoClient, cluster2Svc, k8sClient2, mciObj,
				[]string{Cluster2Node1, Cluster2Node2}, []int32{})
		})

		It("should delete service import object corresponding to cluster1", func() {
			By("updating the cluster1's service to ClusterIP type (only NodePort type must be accepted)")
			ctx := context.Background()
			cluster1Svc.Spec.Type = "ClusterIP"
			UpdateTestSvcType(ctx, k8sClient1, cluster1Svc.GetNamespace(), cluster1Svc.GetName(), "ClusterIP", 0)
			VerifyServiceImportNotExists(ctx, MemberCluster1, mgmtAmkoClient, cluster1Svc)
		})

		It("should create back the service import object corresponding to cluster1", func() {
			By("updating the cluster1's service to NodePort type (only NodePort type must be accepted)")
			ctx := context.Background()
			cluster1Svc.Spec.Type = "NodePort"
			cluster1Svc.Spec.Ports[0].NodePort = Cluster1TestNodePort
			UpdateTestSvcType(ctx, k8sClient1, cluster1Svc.GetNamespace(), cluster1Svc.GetName(), "NodePort",
				Cluster1TestNodePort)
			VerifyServiceImport(ctx, MemberCluster1, mgmtAmkoClient, cluster1Svc, k8sClient1, mciObj,
				[]string{Cluster1Node1, Cluster1Node2}, []int32{})
		})

		It("should remove the service import object's endpoints corresponding to cluster1's node1", func() {
			By("deleting one node from cluster1")
			ctx := context.Background()
			err := k8sClient1.CoreV1().Nodes().Delete(ctx, Cluster1Node1Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			VerifyServiceImport(ctx, MemberCluster1, mgmtAmkoClient, cluster1Svc, k8sClient1, mciObj,
				[]string{Cluster1Node2}, []int32{})
		})

		It("should add the service import object's endpoints corresponding to cluster1's node1", func() {
			By("adding the node back to cluster1")
			ctx := context.Background()
			nodeObj := getTestNode(Cluster1Node1Name, Cluster1Node1)
			_, err := k8sClient1.CoreV1().Nodes().Create(ctx, nodeObj, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			VerifyServiceImport(ctx, MemberCluster1, mgmtAmkoClient, cluster1Svc, k8sClient1, mciObj,
				[]string{Cluster1Node1, Cluster1Node2}, []int32{})
		})

		It("should delete service import object corresponding to cluster1", func() {
			By("deleting cluster1's service")
			ctx := context.Background()
			err := k8sClient1.CoreV1().Services(cluster1Svc.GetNamespace()).Delete(ctx, cluster1Svc.GetName(), metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			VerifyServiceImportNotExists(ctx, MemberCluster1, mgmtAmkoClient, cluster1Svc)
		})

		It("should delete service import object corresponding to cluster2", func() {
			By("deleting cluster2's service")
			ctx := context.Background()
			err := k8sClient2.CoreV1().Services(cluster2Svc.GetNamespace()).Delete(ctx, cluster2Svc.GetName(), metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			VerifyServiceImportNotExists(ctx, MemberCluster2, mgmtAmkoClient, cluster2Svc)
		})
	})
})
