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

package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	amkovmwarecomv1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/api/v1alpha1"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Federator Suite",
		[]Reporter{})
}

var cfg1 *rest.Config
var cfg2 *rest.Config

var k8sClient1 client.Client
var k8sClient2 client.Client

var ctx context.Context
var cancel context.CancelFunc

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	// test envs for both cluster1 and cluster2
	testEnv1 = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases"), AMKOCRDs},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg1, err = testEnv1.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg1).NotTo(BeNil())

	testEnv2 = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases"), AMKOCRDs},
		ErrorIfCRDPathMissing: true,
	}
	cfg2, err = testEnv2.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg2).NotTo(BeNil())

	err = amkovmwarecomv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	testScheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(testScheme))
	utilruntime.Must(amkovmwarecomv1alpha1.AddToScheme(testScheme))
	utilruntime.Must(gslbalphav1.AddToScheme(testScheme))
	utilruntime.Must(gdpalphav2.AddToScheme(testScheme))

	// get k8s clients for cluster1 and cluster2
	k8sClient1, err = client.New(cfg1, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient1).NotTo(BeNil())

	k8sClient2, err = client.New(cfg2, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient2).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg1, ctrl.Options{
		Scheme: testScheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&AMKOClusterReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)

	Expect(err).ToNot(HaveOccurred())

	// build and create the gslb members secret
	fmt.Fprintf(GinkgoWriter, "building and creating secret\n")
	BuildAndCreateTestKubeConfig(k8sClient1, k8sClient2)

	kdata := os.Getenv("GSLB_CONFIG")
	fmt.Fprintf(GinkgoWriter, "kubeconfig data: %v", kdata)

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

}, 60)

var _ = Describe("Federator Validation", func() {
	amkoCluster := getTestAMKOClusterObj(Cluster1, true)
	gcObj := getTestGCObj()
	gdpObj := getTestGDPObject()

	// Given a federator
	//   when an AMKOCluster object is added with empty version
	//     status of AMKOCluster object should contain error
	//     no federation should happen
	Context("when an AMKOCluster object's version field is empty", func() {
		Specify("AMKOCluster's federation status should indicate an error", func() {
			By("Creating a new AMKOCluster object with an empty version field")
			ctx := context.Background()
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			amkoCluster.Spec.Version = ""
			Expect(k8sClient1.Create(ctx, &amkoCluster)).Should(Succeed())
			VerifyTestAMKOClusterStatus(k8sClient1,
				CurrentAMKOClusterValidationStatusField,
				StatusMsgInvalidAMKOCluster,
				"version field can't be empty in AMKOCluster object")
		})

		It("should not federate any objects on member clusters", func() {
			TestGCGDPNotFederated(k8sClient2)
		})

		Specify("deletion of AMKOCluster with invalid version is successful", func() {
			ctx := context.Background()
			Expect(k8sClient1.Delete(ctx, &amkoCluster)).Should(Succeed())
			deleteTestGCAndGDPObj(ctx, k8sClient1, &gcObj, &gdpObj)
		})
	})

	//   when an AMKOCluster object is added with an invalid cluster context
	//     status of AMKOCluster object should contain error
	//     no federation should happen
	//
	Context("when an AMKOCluster object has an invalid clusterContext", func() {
		Specify("AMKOCluster's federation status should indicate an error", func() {
			By("Creating a new AMKOCluster object with a different clusterContext")
			ctx := context.Background()
			gcObj.ObjectMeta.ResourceVersion = ""
			gdpObj.ObjectMeta.ResourceVersion = ""
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			amkoCluster = getTestAMKOClusterObj("invalid-context", true)
			Expect(k8sClient1.Create(ctx, &amkoCluster)).Should(Succeed())
			VerifyTestAMKOClusterStatus(k8sClient1,
				CurrentAMKOClusterValidationStatusField,
				StatusMsgValidAMKOCluster, "")
			VerifyTestAMKOClusterStatus(k8sClient1,
				ClusterContextsStatusField,
				StatusMsgClusterClientsInvalid,
				"error in initialising member cluster contexts: current cluster context invalid-context not part of member clusters")
		})

		It("should not federate any objects on member clusters", func() {
			TestGCGDPNotFederated(k8sClient2)
		})

		Specify("deletion of AMKOCluster with invalid cluster context is successful", func() {
			ctx := context.Background()
			Expect(k8sClient1.Delete(ctx, &amkoCluster)).Should(Succeed())
			deleteTestGCAndGDPObj(ctx, k8sClient1, &gcObj, &gdpObj)
		})
	})

	//   when an AMKOCluster object is added with an invalid cluster list
	//     status of AMKOCluster object should contain error
	//     no federation should happen
	Context("when an AMKOCluster object has invalid cluster list", func() {
		Specify("the AMKOCluster's federation status indicates an error", func() {
			By("Creating a new AMKOCluster object with a different clusterContext")
			ctx := context.Background()
			gcObj.ObjectMeta.ResourceVersion = ""
			gdpObj.ObjectMeta.ResourceVersion = ""
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			amkoCluster = getTestAMKOClusterObj("cluster1", true)
			amkoCluster.Spec.Clusters = append(amkoCluster.Spec.Clusters, "invalid-cluster")
			Expect(k8sClient1.Create(ctx, &amkoCluster)).Should(Succeed())
			VerifyTestAMKOClusterStatus(k8sClient1, ClusterContextsStatusField,
				StatusMsgClusterClientsInvalid,
				"error in initialising member cluster contexts: error in building context config for kubernetes cluster invalid-cluster: context \"invalid-cluster\" does not exist")
		})

		It("should not federate any objects on member clusters", func() {
			TestGCGDPNotFederated(k8sClient2)
		})

		Specify("deletion of AMKOCluster with invalid cluster list is successful", func() {
			ctx := context.Background()
			Expect(k8sClient1.Delete(ctx, &amkoCluster)).Should(Succeed())
			deleteTestGCAndGDPObj(ctx, k8sClient1, &gcObj, &gdpObj)
		})
	})
})

var _ = Describe("Federation Operation", func() {
	amkoCluster1 := getTestAMKOClusterObj(Cluster1, true)
	amkoCluster2 := getTestAMKOClusterObj(Cluster2, false)
	gcObj := getTestGCObj()
	gdpObj := getTestGDPObject()

	// Given a federator
	//   when a valid AMKOCluster object is added on both clusters
	//     status should reflect federation success
	//     GC and GDP objects should be federated on the other cluster (POST)
	//     GC updates should be federated on the other cluster (PUT)
	//     GDP updates should be federated on the other cluster (PUT)
	Context("when a valid AMKOCluster object is added to both clusters", func() {
		Specify("AMKOCluster's federation status should indicate success", func() {
			By("Creating a valid AMKOCluster object on both the clusters")
			ctx := context.Background()
			gcObj.ObjectMeta.ResourceVersion = ""
			gdpObj.ObjectMeta.ResourceVersion = ""
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			Expect(k8sClient1.Create(ctx, &amkoCluster1)).Should(Succeed())
			Expect(k8sClient2.Create(ctx, &amkoCluster2)).Should(Succeed())
			VerifySuccessForAllStatusFields(k8sClient1)
		})

		It("should federate GC and GDP objects on member clusters", func() {
			TestGCGDPExist(k8sClient2)
		})

		It("should federate UUID in GC to cluster2", func() {
			Eventually(func() string {
				var obj gslbalphav1.GSLBConfig
				Expect(k8sClient2.Get(context.TODO(),
					types.NamespacedName{
						Name:      gcObj.Name,
						Namespace: gcObj.Namespace},
					&obj)).Should(Succeed())
				return obj.Annotations["amko.vmware.com/amko-uuid"]
			}, 5*time.Second, 1*time.Second).Should(Equal("3e328a5c-a717-11ed-a422-0a580a80025b"))
		})

		It("should federate GC updates to cluster2", func() {
			By("updating the GC object on cluster1")
			ctx := context.Background()
			gcObj.Spec.RefreshInterval = 999
			Expect(k8sClient1.Update(ctx, &gcObj)).Should(Succeed())
			Eventually(func() int {
				var obj gslbalphav1.GSLBConfig
				Expect(k8sClient2.Get(context.TODO(),
					types.NamespacedName{
						Name:      gcObj.Name,
						Namespace: gcObj.Namespace},
					&obj)).Should(Succeed())
				return obj.Spec.RefreshInterval
			}, 5*time.Second, 1*time.Second).Should(Equal(999))
		})

		It("should federate GDP updates to cluster2", func() {
			By("updating the GDP object on cluster1")
			ctx := context.Background()
			ttl := 1000
			gdpObj.Spec.TTL = &ttl
			Expect(k8sClient1.Update(ctx, &gdpObj)).Should(Succeed())
			Eventually(func() int {
				var obj gdpalphav2.GlobalDeploymentPolicy
				Expect(k8sClient2.Get(context.TODO(),
					types.NamespacedName{
						Name:      gdpObj.Name,
						Namespace: gdpObj.Namespace},
					&obj)).Should(Succeed())
				return *obj.Spec.TTL
			}, 5*time.Second, 1*time.Second).Should(Equal(1000))
		})

		Specify("deletion of AMKOCluster, GC and GDP is successful", func() {
			CleanupTestObjects(k8sClient1, k8sClient2, &amkoCluster1, &amkoCluster2,
				&gcObj, &gdpObj)
		})
	})

	// when a valid AMKOCluster object is added to both the clusters, but there's a version mismatch
	//   status in cluster1 should indicate failure
	//   no federation
	//   update to the cluster2's AMKOCluster version to match the versions should enable federation
	Context("when a valid AMKOCluster object is added to both clusters but with version mismatch", func() {
		Specify("AMKOCluster's federation status should indicate failure", func() {
			By("Creating a AMKOCluster objects on both the clusters with different versions")
			ctx := context.Background()
			// re-init the objects
			gcObj = getTestGCObj()
			gdpObj = getTestGDPObject()
			amkoCluster1 = getTestAMKOClusterObj(Cluster1, true)
			amkoCluster2 = getTestAMKOClusterObj(Cluster2, false)
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			amkoCluster2.Spec.Version = TestAMKODifferentVersion
			Expect(k8sClient1.Create(ctx, &amkoCluster1)).Should(Succeed())
			Expect(k8sClient2.Create(ctx, &amkoCluster2)).Should(Succeed())
			VerifyTestAMKOClusterStatus(k8sClient1, CurrentAMKOClusterValidationStatusField,
				StatusMsgValidAMKOCluster, "")
			VerifyTestAMKOClusterStatus(k8sClient1, ClusterContextsStatusField,
				StatusMsgClusterClientsSuccess, "")
			VerifyTestAMKOClusterStatus(k8sClient1, MemberValidationStatusField,
				StatusMembersInvalid, "version mismatch, current AMKO: "+
					TestAMKOVersion+", AMKO in cluster cluster2: "+TestAMKODifferentVersion)
		})

		It("should not federate GC and GDP objects on member clusters", func() {
			TestGCGDPNotFederated(k8sClient2)
		})

		Specify("cluster1's AMKOCluster status should indicate success, if version is matched on cluster2", func() {
			ctx := context.Background()
			amkoCluster2.Spec.Version = TestAMKOVersion
			Expect(k8sClient2.Update(ctx, &amkoCluster2)).Should(Succeed())
			VerifySuccessForAllStatusFields(k8sClient1)
		})

		It("should now federate GC and GDP objects on member clusters", func() {
			TestGCGDPExist(k8sClient2)
		})

		Specify("deletion of AMKOCluster, GC and GDP is successful", func() {
			CleanupTestObjects(k8sClient1, k8sClient2, &amkoCluster1, &amkoCluster2,
				&gcObj, &gdpObj)
		})
	})

	// when a valid AMKOCluster object is added on cluster1 and no AMKOCluster object in cluster2
	//   status should reflect federation failure
	//   GC and GDP objects should not be federated on the other cluster
	//   creating an AMKOCluster object on cluster2 should start the federation
	Context("when an AMKOCluster object is added to cluster1 but no AMKOCluster on cluster2", func() {
		Specify("AMKOCluster's federation status should indicate failure", func() {
			By("Creating an AMKOCluster object on cluster1")
			ctx := context.Background()
			// re-init the objects
			gcObj = getTestGCObj()
			gdpObj = getTestGDPObject()
			amkoCluster1 = getTestAMKOClusterObj(Cluster1, true)
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			Expect(k8sClient1.Create(ctx, &amkoCluster1)).Should(Succeed())
			VerifyTestAMKOClusterStatus(k8sClient1, CurrentAMKOClusterValidationStatusField,
				StatusMsgValidAMKOCluster, "")
			VerifyTestAMKOClusterStatus(k8sClient1, ClusterContextsStatusField,
				StatusMsgClusterClientsSuccess, "")
			VerifyTestAMKOClusterStatus(k8sClient1, MemberValidationStatusField,
				StatusMembersInvalid, "no AMKOCluster object present in cluster cluster2, can't federate")
		})

		It("should not federate GC and GDP objects on member clusters", func() {
			TestGCGDPNotFederated(k8sClient2)
		})

		Specify("AMKOCluster's federation status should indicate success on adding an AMKOCluster on cluster2", func() {
			ctx := context.Background()
			amkoCluster2 = getTestAMKOClusterObj(Cluster2, false)
			Expect(k8sClient2.Create(ctx, &amkoCluster2)).Should(Succeed())
			VerifySuccessForAllStatusFields(k8sClient1)
		})

		It("should now federate GC and GDP objects on member clusters", func() {
			TestGCGDPExist(k8sClient2)
		})

		Specify("deletion of AMKOCluster, GC and GDP is successful", func() {
			CleanupTestObjects(k8sClient1, k8sClient2, &amkoCluster1, &amkoCluster2,
				&gcObj, &gdpObj)
		})
	})

	// when a valid AMKOCluster object is added to both cluster1 and cluster2, both are leader
	//   status should reflect federation failure
	//   GC and GDP objects should not be federated on the other cluster
	//   update to the cluster2's AMKOCluster leader field to false should re-enable federation
	Context("AMKOClusters on both cluster1 and cluster2 are leaders", func() {
		Specify("AMKOCluster's federation status should indicate failure", func() {
			By("Creating an AMKOCluster object on cluster1 and cluster2 as leaders")
			ctx := context.Background()
			// re-init the objects
			gcObj = getTestGCObj()
			gdpObj = getTestGDPObject()
			amkoCluster1 = getTestAMKOClusterObj(Cluster1, true)
			amkoCluster2 = getTestAMKOClusterObj(Cluster2, true)
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			Expect(k8sClient1.Create(ctx, &amkoCluster1)).Should(Succeed())
			Expect(k8sClient2.Create(ctx, &amkoCluster2)).Should(Succeed())
			VerifyTestAMKOClusterStatus(k8sClient1, CurrentAMKOClusterValidationStatusField,
				StatusMsgValidAMKOCluster, "")
			VerifyTestAMKOClusterStatus(k8sClient1, ClusterContextsStatusField,
				StatusMsgClusterClientsSuccess, "")
			VerifyTestAMKOClusterStatus(k8sClient1, MemberValidationStatusField,
				StatusMsgClusterClientsInvalid, "AMKO in cluster cluster2 is also a leader, conflicting state")
		})

		It("should not federate GC and GDP objects on member clusters", func() {
			TestGCGDPNotFederated(k8sClient2)
		})

		Specify("AMKOCluster's federation status should indicate success on changing cluster2's AMKOCluster to follower", func() {
			ctx := context.Background()
			amkoCluster2.Spec.IsLeader = false
			Expect(k8sClient2.Update(ctx, &amkoCluster2)).Should(Succeed())
			VerifySuccessForAllStatusFields(k8sClient1)
		})

		It("should now federate GC and GDP objects on member clusters", func() {
			TestGCGDPExist(k8sClient2)
		})

		Specify("deletion of AMKOCluster, GC and GDP is successful", func() {
			ctx := context.Background()
			Expect(k8sClient1.Delete(ctx, &amkoCluster1)).Should(Succeed())
			deleteTestGCAndGDPObj(ctx, k8sClient1, &gcObj, &gdpObj)
			Expect(k8sClient2.Delete(ctx, &amkoCluster2)).Should(Succeed())
			deleteTestGCAndGDPObj(ctx, k8sClient2, &gcObj, &gdpObj)
		})
	})
})

var _ = Describe("Federation Consolidation Operations", func() {
	amkoCluster1 := getTestAMKOClusterObj(Cluster1, true)
	amkoCluster2 := getTestAMKOClusterObj(Cluster2, false)
	gcObj := getTestGCObj()
	gdpObj := getTestGDPObject()

	alternateGCObj := getTestGCObj()
	alternateGCObj.Name = "alt-test-gc"
	alternateGDPObj := getTestGDPObject()
	alternateGDPObj.Name = "alt-test-gdp"

	//   when a valid AMKOCluster object is present on both clusters, but multiple GSLBConfig and GDP objects on the follower cluster
	//     status should reflect federation success
	//     federator should delete the non-relevant GSLBConfigs on follower cluster
	//     GDP object should be federated
	Context("when a valid AMKOCluster object is added to both clusters and cluster2 has 2 GSLBConfigs and 2 GDPs", func() {
		Specify("AMKOCluster's federation status should indicate success", func() {
			By("Creating a valid AMKOCluster object on both the clusters and an extra GSLBConfig on cluster2")
			ctx := context.Background()
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			Expect(k8sClient2.Create(ctx, &alternateGCObj)).Should(Succeed())
			Expect(k8sClient2.Create(ctx, &alternateGDPObj)).Should(Succeed())

			Expect(k8sClient1.Create(ctx, &amkoCluster1)).Should(Succeed())
			Expect(k8sClient2.Create(ctx, &amkoCluster2)).Should(Succeed())
			VerifySuccessForAllStatusFields(k8sClient1)
		})

		It("should give an error when the extra gslbconfig object is fetched from cluster 2", func() {
			ctx := context.Background()
			obj := gslbalphav1.GSLBConfig{}
			Expect(k8sClient2.Get(ctx, types.NamespacedName{
				Namespace: alternateGCObj.Namespace,
				Name:      alternateGCObj.Name,
			}, &obj)).ShouldNot(Succeed())
		})

		It("should give an error when the extra gdp object is fetched from cluster 2", func() {
			ctx := context.Background()
			obj := gdpalphav2.GlobalDeploymentPolicy{}
			Expect(k8sClient2.Get(ctx, types.NamespacedName{
				Namespace: alternateGDPObj.Namespace,
				Name:      alternateGDPObj.Name,
			}, &obj)).ShouldNot(Succeed())
		})

		Specify("one each of GC and GDP objects should be federated to present on cluster 2", func() {
			// this does number of object checks anyway
			TestGCGDPExist(k8sClient2)
		})

		It("should federate primary GC updates to cluster2", func() {
			// make sure that the updates to the primary GC object is still federated
			By("updating the GC object on cluster1")
			ctx := context.Background()
			gcObj.Spec.RefreshInterval = 999
			Expect(k8sClient1.Update(ctx, &gcObj)).Should(Succeed())
			Eventually(func() int {
				var obj gslbalphav1.GSLBConfig
				Expect(k8sClient2.Get(context.TODO(),
					types.NamespacedName{
						Name:      gcObj.Name,
						Namespace: gcObj.Namespace},
					&obj)).Should(Succeed())
				return obj.Spec.RefreshInterval
			}, 30*time.Second, 1*time.Second).Should(Equal(999))
		})

		It("should federate primary GDP updates to cluster2", func() {
			// make sure that the updates to the primary GDP object is still federated
			By("updating the GDP object on cluster1")
			ctx := context.Background()
			ttl := 1000
			gdpObj.Spec.TTL = &ttl
			Expect(k8sClient1.Update(ctx, &gdpObj)).Should(Succeed())
			Eventually(func() int {
				var obj gdpalphav2.GlobalDeploymentPolicy
				Expect(k8sClient2.Get(context.TODO(),
					types.NamespacedName{
						Name:      gdpObj.Name,
						Namespace: gdpObj.Namespace},
					&obj)).Should(Succeed())
				return *obj.Spec.TTL
			}, 30*time.Second, 1*time.Second).Should(Equal(1000))
		})

		Specify("deletion of AMKOCluster, GC and GDP is successful", func() {
			CleanupTestObjects(k8sClient1, k8sClient2, &amkoCluster1, &amkoCluster2,
				&gcObj, &gdpObj)
		})
	})

	//   when a valid AMKOCluster object is present on both clusters and GC/GDP is deleted
	//     status should reflect federation success
	//     federator should delete the only GC/GDP object on cluster2
	Context("when a valid AMKOCluster object is added to both clusters and GC/GDP is deleted", func() {
		Specify("AMKOCluster's federation status should indicate success", func() {
			By("Creating a valid AMKOCluster object on both the clusters")
			ctx := context.Background()
			gcObj.SetResourceVersion("")
			gdpObj.SetResourceVersion("")
			amkoCluster1.SetResourceVersion("")
			amkoCluster2.SetResourceVersion("")
			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)
			Expect(k8sClient1.Create(ctx, &amkoCluster1)).Should(Succeed())
			Expect(k8sClient2.Create(ctx, &amkoCluster2)).Should(Succeed())
			VerifySuccessForAllStatusFields(k8sClient1)
		})

		It("should federate GC and GDP objects on member clusters", func() {
			TestGCGDPExist(k8sClient2)
		})

		It("Should delete the GC on cluster2 when GC on cluster1 is deleted", func() {
			ctx := context.Background()
			Expect(k8sClient1.Delete(ctx, &gcObj)).Should(Succeed())

			Eventually(func() int {
				gcList := gslbalphav1.GSLBConfigList{}
				Expect(k8sClient2.List(ctx, &gcList, &client.ListOptions{
					Namespace: gcObj.Namespace,
				})).Should(Succeed())
				return len(gcList.Items)
			}, 5*time.Second, 1*time.Second).Should(BeZero())
		})

		It("Should delete the GDP on cluster2 when GDP on cluster1 is deleted", func() {
			ctx := context.Background()
			Expect(k8sClient1.Delete(ctx, &gdpObj)).Should(Succeed())

			Eventually(func() int {
				gdpList := gdpalphav2.GlobalDeploymentPolicyList{}
				Expect(k8sClient2.List(ctx, &gdpList, &client.ListOptions{
					Namespace: gdpObj.Namespace,
				})).Should(Succeed())
				return len(gdpList.Items)
			}, 5*time.Second, 1*time.Second).Should(BeZero())
		})

		Specify("deletion of AMKOCluster, GC and GDP is successful", func() {
			CleanupTestObjects(k8sClient1, k8sClient2, &amkoCluster1, &amkoCluster2,
				&gcObj, &gdpObj)
		})
	})
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv1.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = testEnv2.Stop()
	Expect(err).NotTo(HaveOccurred())
})
