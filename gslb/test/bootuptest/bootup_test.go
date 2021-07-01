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

package bootuptest_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	amkovmwarecomv1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/api/v1alpha1"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/api"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/apiserver"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/ingestion"
)

var cfg1 *rest.Config
var cfg2 *rest.Config

var k8sClient1 client.Client
var k8sClient2 client.Client

var testEnv1 *envtest.Environment
var testEnv2 *envtest.Environment

var testScheme *runtime.Scheme

const (
	Cluster1                 = "cluster1"
	Cluster2                 = "cluster2"
	TestAMKOVersion          = "1.4.2"
	TestAMKODifferentVersion = "1.5.1"
	TestAMKOClusterName      = "test-amko-cluster"
	TestGSLBSecret           = "gslb-config-secret"
	AMKOCRDs                 = "../../../helm/amko/crds"
	TestGCName               = "test-gc"
	TestGDPName              = "test-gdp"
	TestLeaderIP             = "10.10.10.10"
	AviSystem                = "avi-system"
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

func BuildAndCreateTestKubeConfig(kClient1, kClient2 client.Client) {
	user1 := Cluster1 + "-user"
	user2 := Cluster2 + "-user"

	kData := ClustersKubeConfig{}
	Expect(yaml.Unmarshal([]byte(KubeConfigData), &kData)).Should(Succeed())

	kData.Clusters = []ClusterData{
		{
			Cluster: ClusterServerData{
				CAData: base64.StdEncoding.EncodeToString([]byte(testEnv1.Config.CAData)),
				Server: testEnv1.Config.Host,
			},
			Name: Cluster1,
		},
		{
			Cluster: ClusterServerData{
				CAData: base64.StdEncoding.EncodeToString([]byte(testEnv2.Config.CAData)),
				Server: testEnv2.Config.Host,
			},
			Name: Cluster2,
		},
	}

	kData.Contexts = []KubeContextData{
		{
			Context: ContextData{
				Cluster: Cluster1,
				User:    user1,
			},
			Name: Cluster1,
		},
		{
			Context: ContextData{
				Cluster: Cluster2,
				User:    user2,
			},
			Name: Cluster2,
		},
	}

	kData.Users = []UserData{
		{
			Name: user1,
			User: UserID{
				ClientCert: base64.StdEncoding.EncodeToString([]byte(testEnv1.Config.CertData)),
				ClientKey:  base64.StdEncoding.EncodeToString([]byte(testEnv1.Config.KeyData)),
			},
		},
		{
			Name: user2,
			User: UserID{
				ClientCert: base64.StdEncoding.EncodeToString([]byte(testEnv2.Config.CertData)),
				ClientKey:  base64.StdEncoding.EncodeToString([]byte(testEnv2.Config.KeyData)),
			},
		},
	}

	// generate a string out of kubeCfg
	kubeCfgData, err := yaml.Marshal(kData)
	Expect(err).NotTo(HaveOccurred())

	// create the "avi-system" namespace
	nsObj1 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: AviSystem,
		},
	}
	kClient1.Create(context.Background(), &nsObj1)
	nsObj2 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: AviSystem,
		},
	}
	kClient2.Create(context.Background(), &nsObj2)

	Expect(os.Setenv("GSLB_CONFIG", string(kubeCfgData))).Should(Succeed())
}

func getTestAMKOClusterObj(currentContext string, isLeader bool) amkovmwarecomv1alpha1.AMKOCluster {
	return amkovmwarecomv1alpha1.AMKOCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestAMKOClusterName,
			Namespace: AviSystem,
		},
		Spec: amkovmwarecomv1alpha1.AMKOClusterSpec{
			ClusterContext: currentContext,
			IsLeader:       isLeader,
			Clusters:       []string{Cluster1, Cluster2},
		},
	}
}

func getTestGCObj() gslbalphav1.GSLBConfig {
	return gslbalphav1.GSLBConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestGCName,
			Namespace: AviSystem,
		},
		Spec: gslbalphav1.GSLBConfigSpec{
			GSLBLeader: gslbalphav1.GSLBLeader{
				Credentials:       "test-creds",
				ControllerVersion: "20.1.4",
				ControllerIP:      TestLeaderIP,
			},
			MemberClusters: []gslbalphav1.MemberCluster{
				{
					ClusterContext: Cluster1,
				},
				{
					ClusterContext: Cluster2,
				},
			},
			RefreshInterval: 3600,
			LogLevel:        "INFO",
		},
	}
}

func getTestGDPObject() gdpalphav2.GlobalDeploymentPolicy {
	label := make(map[string]string)
	label["key"] = "value"
	return gdpalphav2.GlobalDeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestGDPName,
			Namespace: AviSystem,
		},
		Spec: gdpalphav2.GDPSpec{
			MatchRules: gdpalphav2.MatchRules{
				AppSelector: gdpalphav2.AppSelector{
					Label: label,
				},
			},
			MatchClusters: []gdpalphav2.ClusterProperty{
				{
					Cluster: Cluster1,
				},
				{
					Cluster: Cluster2,
				},
			},
			TTL: getGDPTTLPtr(300),
		},
	}
}

func getGDPTTLPtr(val int) *int {
	ttl := val
	return &ttl
}

func createTestGCAndGDPObjs(ctx context.Context, k8sClient client.Client, gc *gslbalphav1.GSLBConfig, gdp *gdpalphav2.GlobalDeploymentPolicy) {
	Expect(k8sClient.Create(ctx, gc)).Should(Succeed())
	Expect(k8sClient.Create(ctx, gdp)).Should(Succeed())
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	var err error

	testEnv1 = &envtest.Environment{
		CRDDirectoryPaths:     []string{AMKOCRDs},
		ErrorIfCRDPathMissing: true,
	}
	cfg1, err = testEnv1.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg1).NotTo(BeNil())
	cfg1.Transport = nil

	testEnv2 = &envtest.Environment{
		CRDDirectoryPaths:     []string{AMKOCRDs},
		ErrorIfCRDPathMissing: true,
	}
	cfg2, err = testEnv2.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg2).NotTo(BeNil())
	cfg2.Transport = nil

	testScheme = runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(testScheme))
	utilruntime.Must(amkovmwarecomv1alpha1.AddToScheme(testScheme))
	utilruntime.Must(gslbalphav1.AddToScheme(testScheme))
	utilruntime.Must(gdpalphav2.AddToScheme(testScheme))

	k8sClient1, err = client.New(cfg1, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient1).NotTo(BeNil())

	k8sClient2, err = client.New(cfg2, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient2).NotTo(BeNil())

	// build and create the gslb members secret
	fmt.Fprintf(GinkgoWriter, "building and creating secret\n")
	BuildAndCreateTestKubeConfig(k8sClient1, k8sClient2)

	kdata := os.Getenv("GSLB_CONFIG")
	fmt.Fprintf(GinkgoWriter, "kubeconfig data: %v", kdata)

}, 60)

var _ = Describe("AMKO member cluster event handling", func() {
	gcObj := getTestGCObj()
	gdpObj := getTestGDPObject()
	amkoCluster1 := getTestAMKOClusterObj(Cluster1, true)
	amkoCluster2 := getTestAMKOClusterObj(Cluster2, false)

	// We need a BeforeEach for this describe block, as we are restarting testenv
	// in the describe block - "AMKO bootup Validation"
	var _ = BeforeEach(func() {
		var err error
		// create a new instance of k8sClient2
		k8sClient2, err = client.New(cfg2, client.Options{Scheme: testScheme})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient2).NotTo(BeNil())

		BuildAndCreateTestKubeConfig(k8sClient1, k8sClient2)

		// Clean up existing amkoCluster objects
		k8sClient1.Delete(context.Background(), &amkoCluster1)
		k8sClient2.Delete(context.Background(), &amkoCluster2)
	}, 60)

	Context("when the leader field in member cluster changes", func() {
		It("AMKO should reboot", func() {
			By("Shutting Down AMKO api server")
			ctx := context.Background()

			// Create a fake API Server
			amkoAPIServer := api.FakeApiServer{
				Port: "1234",
			}
			amkoAPIServer.InitApi()
			apiserver.SetAmkoAPIServer(&amkoAPIServer)

			Expect(k8sClient1.Create(ctx, &amkoCluster1)).Should(Succeed())
			Expect(k8sClient2.Create(ctx, &amkoCluster2)).Should(Succeed())

			createTestGCAndGDPObjs(ctx, k8sClient1, &gcObj, &gdpObj)

			testScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(testScheme))
			utilruntime.Must(amkovmwarecomv1alpha1.AddToScheme(testScheme))
			k8sManager, err := ctrl.NewManager(cfg1, ctrl.Options{
				Scheme:             testScheme,
				MetricsBindAddress: ":7070",
			})
			Expect(err).ToNot(HaveOccurred())
			err = (&ingestion.AMKOClusterReconciler{
				Client: k8sManager.GetClient(),
				Scheme: k8sManager.GetScheme(),
			}).SetupWithManager(k8sManager)
			Expect(err).ToNot(HaveOccurred())

			go func() {
				if err := k8sManager.Start(ctx); err != nil {
					Expect(err).ToNot(HaveOccurred())
				}
			}()

			// Set isLeader to true is amkoCluster objects of second cluster, which should be handled by AMKO
			amkoCluster2.Spec.IsLeader = true
			Expect(k8sClient2.Update(ctx, &amkoCluster2)).Should(Succeed())

			// API server should be shut down
			Eventually(func() bool {
				return amkoAPIServer.Shutdown
			}, 10*time.Second).Should(Equal(true))
		})
	})

	var _ = AfterEach(func() {
		k8sClient1.Delete(context.Background(), &amkoCluster1)
		k8sClient2.Delete(context.Background(), &amkoCluster2)
	}, 60)
})

var _ = Describe("AMKO bootup Validation", func() {
	amkoCluster1 := getTestAMKOClusterObj(Cluster1, true)
	amkoCluster2 := getTestAMKOClusterObj(Cluster2, true)

	Context("Both Member clusters are leaders", func() {
		Specify("AMKO Should not boot up", func() {
			By("Checking AMKOCLuster object fields of all member clusters")
			Expect(k8sClient1.Create(context.Background(), &amkoCluster1)).Should(Succeed())
			Expect(k8sClient2.Create(context.Background(), &amkoCluster2)).Should(Succeed())
			ok, _ := ingestion.HandleBootup(cfg1)
			Expect(ok).Should(BeFalse())
		})

		Specify("deletion of AMKOCluster is successful", func() {
			Expect(k8sClient2.Delete(context.Background(), &amkoCluster2)).Should(Succeed())
		})
	})

	Context("One Member Cluster is leader and another is follower", func() {
		Specify("AMKO Should boot up", func() {
			By("Validating AMKOCLuster object fields of all member clusters")
			amkoCluster2 = getTestAMKOClusterObj(Cluster2, false)
			Expect(k8sClient2.Create(context.Background(), &amkoCluster2)).Should(Succeed())
			ok, _ := ingestion.HandleBootup(cfg1)
			Expect(ok).Should(BeTrue())
		})
	})

	Context("One Member Cluster is down", func() {
		Specify("AMKO Should boot up", func() {
			var err error
			err = testEnv2.Stop()
			Expect(err).To(BeNil())
			ok, _ := ingestion.HandleBootup(cfg1)
			Expect(ok).Should(BeTrue())

			// save the new config in cfg2 for future test cases
			cfg2, err = testEnv2.Start()
			Expect(err).To(BeNil())
		})
	})
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv1.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = testEnv2.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func TestBootuptest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bootuptest Suite")
}
