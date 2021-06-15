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
	"net/http"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	routev1 "github.com/openshift/api/route/v1"

	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/ingestion"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	amkorest "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/rest"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/mockaviserver"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
	gslbcs "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned"
	gslbinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/informers/externalversions"
	gdpcs "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha2/clientset/versioned"
	gdpinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha2/informers/externalversions"
	akov1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestMain(m *testing.M) {
	setUp()
	ret := m.Run()
	cleanUp()
	os.Exit(ret)
}

func cleanUp() {
	for idx, testEnv := range testEnvs {
		if testEnv != nil {
			testEnv.Stop()
			gslbutils.Logf("cluster %d stopped", idx)
		}
	}
	// clear out the lists
	cfgs = nil
	clusterClients = nil
	testEnvs = nil
}

func createRouteCRD() {
	routeCRD = apiextensionv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "routes." + routev1.SchemeGroupVersion.Group,
		},
		Spec: apiextensionv1beta1.CustomResourceDefinitionSpec{
			Group:   routev1.SchemeGroupVersion.Group,
			Version: routev1.SchemeGroupVersion.Version,
			Scope:   apiextensionv1beta1.NamespaceScoped,
			Names: apiextensionv1beta1.CustomResourceDefinitionNames{
				Plural: "routes",
				Kind:   reflect.TypeOf(routev1.Route{}).Name(),
			},
		},
	}
}

func createHostRuleCRD() {
	hrCRD = apiextensionv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hostrules." + akov1alpha1.SchemeGroupVersion.Group,
		},
		Spec: apiextensionv1beta1.CustomResourceDefinitionSpec{
			Group:   akov1alpha1.SchemeGroupVersion.Group,
			Version: akov1alpha1.SchemeGroupVersion.Version,
			Scope:   apiextensionv1beta1.NamespaceScoped,
			Names: apiextensionv1beta1.CustomResourceDefinitionNames{
				Plural: "hostrules",
				Kind:   reflect.TypeOf(akov1alpha1.HostRule{}).Name(),
			},
		},
	}
}

func SetUpEnvClusters() {
	cfgs = make([]*rest.Config, MaxClusters)
	clusterClients = make([]*kubernetes.Clientset, MaxClusters)
	testEnvs = make([]*envtest.Environment, MaxClusters)
	createHostRuleCRD()

	testEnv1 := &envtest.Environment{
		CRDDirectoryPaths: []string{AmkoCRDs},
		CRDs: []client.Object{
			&hrCRD,
		},
		ErrorIfCRDPathMissing: true,
	}
	testEnvs[0] = testEnv1

	createRouteCRD()
	testEnv2 := &envtest.Environment{
		CRDs: []client.Object{
			&routeCRD,
			&hrCRD,
		},
		ErrorIfCRDPathMissing: true,
	}
	testEnvs[1] = testEnv2
}

func StartEnvClusters() {
	var err error
	for idx, testEnv := range testEnvs {
		cfgs[idx], err = testEnv.Start()
		if err != nil {
			gslbutils.Errf("error occured while starting test env cluster %d: %v", idx, err)
			CleanupAndExit()
		}
		gslbutils.Logf("started cluster %d", idx)
		clientTransport := &http.Transport{}
		cfgs[idx].Transport = clientTransport
	}
}

func SetUpClients() {
	for idx, cfg := range cfgs {
		cfg.Transport = nil
		clientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			gslbutils.Errf("error occured while fetching clientset for cluster %d: %v", idx, err)
			CleanupAndExit()
		}
		gslbutils.Logf("set up the clientset for cluster %d", idx)
		clusterClients[idx] = clientset
	}
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: AviSystemNS,
		},
	}
	clusterClients[ConfigCluster].CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
	oc, err := oshiftclient.NewForConfig(cfgs[Oshift])
	if err != nil {
		gslbutils.Errf("error occured while fetching the openshift client: %v", err)
		CleanupAndExit()
	}
	oshiftClient = oc
}

func SyncFromTestIngestionLayer(key string, wg *sync.WaitGroup) error {
	gslbutils.Logf("recieved key from ingestion layer: %s", key)
	ingestionKeyChan <- key

	return nil
}

func SyncFromTestNodesLayer(key string, wg *sync.WaitGroup) error {
	gslbutils.Logf("recieved key from graph layer: %s", key)

	return nil
}

func SetUpTestWorkerQueues() {
	gslbutils.SetWaitGroupMap()
	numIngestionWorkers := utils.NumWorkersIngestion
	ingestionQueueParams := utils.WorkerQueue{NumWorkers: numIngestionWorkers, WorkqueueName: utils.ObjectIngestionLayer}
	graphQueueParams := utils.WorkerQueue{NumWorkers: gslbutils.NumRestWorkers, WorkqueueName: utils.GraphLayer}

	utils.SharedWorkQueue(&ingestionQueueParams, &graphQueueParams)

	ingestionSharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	ingestionSharedQueue.SyncFunc = nodes.SyncFromIngestionLayer
	ingestionSharedQueue.Run(stopCh, gslbutils.GetWaitGroupFromMap(gslbutils.WGIngestion))

	// Set workers for layer 3 (REST layer)
	graphSharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	graphSharedQueue.SyncFunc = amkorest.SyncFromNodesLayer
	// graphSharedQueue.SyncFunc = SyncFromTestNodesLayer
	graphSharedQueue.Run(stopCh, gslbutils.GetWaitGroupFromMap(gslbutils.WGGraph))
}

func SetUpAMKOConfigs() {
	gslbutils.SetTestMode(true)
	os.Setenv("MOCK_DATA_DIR", "../../avimockobjects/")
	os.Setenv("GSLB_CONFIG", "test-data")

	gslbutils.GlobalKubeClient = clusterClients[ConfigCluster]
	gslbClient, err := gslbcs.NewForConfig(cfgs[ConfigCluster])
	if err != nil {
		gslbutils.Errf("error occured while creating a clientset for gslb: %v", err)
		CleanupAndExit()
	}
	gslbutils.GlobalGslbClient = gslbClient
	gdpClient, err := gdpcs.NewForConfig(cfgs[ConfigCluster])
	if err != nil {
		gslbutils.Errf("error occured while creating a clientset for gdp: %v", err)
		CleanupAndExit()
	}
	gslbutils.GlobalGdpClient = gdpClient

	gslbutils.PublishGDPStatus = true
	gslbutils.PublishGSLBStatus = true
	stopCh = utils.SetupSignalHandler()

	SetUpTestWorkerQueues()
	ingestion.SetInformerListTimeout(120)
	gslbInformerFactory := gslbinformers.NewSharedInformerFactory(gslbClient, time.Second*30)

	gslbController := ingestion.GetNewController(clusterClients[ConfigCluster], gslbClient,
		gslbInformerFactory, ingestion.AddGSLBConfigObject, GetTestEnvClustersAsGslbMembers)
	gslbInformer := gslbInformerFactory.Amko().V1alpha1().GSLBConfigs()
	go gslbInformer.Informer().Run(stopCh)

	gdpInformerFactory := gdpinformers.NewSharedInformerFactory(gdpClient, time.Second*30)
	gdpCtrl := ingestion.InitializeGDPController(clusterClients[ConfigCluster], gdpClient, gdpInformerFactory,
		ingestion.AddGDPObj, ingestion.UpdateGDPObj, ingestion.DeleteGDPObj)
	gdpInformer := gdpInformerFactory.Amko().V1alpha2().GlobalDeploymentPolicies()
	go gdpInformer.Informer().Run(stopCh)

	gslbhrCtrl := ingestion.InitializeGSLBHostRuleController(clusterClients[ConfigCluster],
		gslbClient, gslbInformerFactory, ingestion.AddGSLBHostRuleObj,
		ingestion.UpdateGSLBHostRuleObj, ingestion.DeleteGSLBHostRuleObj)

	gslbhrInformer := gslbInformerFactory.Amko().V1alpha1().GSLBHostRules()
	go gslbhrInformer.Informer().Run(stopCh)

	go ingestion.RunControllers(gslbController, gdpCtrl, gslbhrCtrl, stopCh)
}

func GetTestEnvClustersAsGslbMembers(arg1 string, arg2 []gslbalphav1.MemberCluster) ([]*ingestion.GSLBMemberController, error) {
	clients := make(map[string]*kubernetes.Clientset)

	testClustersContexts := []ingestion.KubeClusterDetails{
		ingestion.GetNewKubeClusterDetails(K8sContext, "", "", nil),
		ingestion.GetNewKubeClusterDetails(OshiftContext, "", "", nil),
	}

	memberClusterList := make([]*ingestion.GSLBMemberController, 0)
	for idx, c := range testClustersContexts {
		member, err := ingestion.InitializeMemberCluster(cfgs[idx], c, clients)
		if err != nil {
			return nil, err
		}
		gslbutils.Logf("test cluster: %s, informers set up", c.GetClusterContextName())
		memberClusterList = append(memberClusterList, member)
	}
	return memberClusterList, nil
}

func CleanupAndExit() {
	cleanUp()
	os.Exit(1)
}

func SetUpMockController() {
	mockaviserver.NewAviMockAPIServer()
	apiURL = mockaviserver.GetMockServerURL()
	gslbutils.Logf("test controller started, URL: %s", apiURL)
}

func CreateAviSecretInConfigCluster() {
	secretObj := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AviSecret,
			Namespace: AviSystemNS,
		},
		Data: map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("admin"),
		},
	}
	_, err := clusterClients[ConfigCluster].CoreV1().Secrets(AviSystemNS).Create(context.TODO(), &secretObj, metav1.CreateOptions{})
	if err != nil {
		gslbutils.Errf("error in creating a secret: %v", err)
		CleanupAndExit()
	}
	gslbutils.Logf("created test secret object")
}

func setUp() {
	// Set the location of the api server and etcd binaries
	KubeBuilderAssetsVal = os.Getenv(KubeBuilderAssetsEnv)
	if KubeBuilderAssetsVal == "" {
		panic("kube builder assets directory not set, set the environment variable KUBEBUILDER_ASSETS and re-run")
	}

	ingestionKeyChan = make(chan string)
	graphKeyChan = make(chan string)

	// Set up the clusters
	SetUpEnvClusters()
	// Start the clusters
	StartEnvClusters()
	// Fetch the kube clients for all clusters
	SetUpClients()

	// Inialize AMKO configs
	SetUpAMKOConfigs()

	// Set up mock AVI controller API server
	SetUpMockController()

	// Create AVI secret
	CreateAviSecretInConfigCluster()

	// Setup Gslb Config object
	AddTestGslbConfigObject()
}

type forGomega struct {
}

func (f forGomega) Fatalf(format string, args ...interface{}) {
	gslbutils.Errf(format, args...)
	CleanupAndExit()
}

func GetTestGSLBConfigObject() *gslbalphav1.GSLBConfig {
	return &gslbalphav1.GSLBConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GslbConfigName,
			Namespace: AviSystemNS,
		},

		Spec: gslbalphav1.GSLBConfigSpec{
			GSLBLeader: gslbalphav1.GSLBLeader{
				Credentials:       AviSecret,
				ControllerVersion: "20.1.4",
				ControllerIP:      apiURL,
			},
			MemberClusters: []gslbalphav1.MemberCluster{
				{ClusterContext: K8sContext},
				{ClusterContext: OshiftContext},
			},
			RefreshInterval:     100,
			LogLevel:            "DEBUG",
			UseCustomGlobalFqdn: false,
		},
	}
}

func AddTestGslbConfigObject() {
	var f forGomega
	gcClient := gslbutils.GlobalGslbClient

	t := types.GomegaTestingT(f)
	g := gomega.NewGomegaWithT(t)
	gc := GetTestGSLBConfigObject()
	_, err := gcClient.AmkoV1alpha1().GSLBConfigs(AviSystemNS).Create(context.TODO(), gc,
		metav1.CreateOptions{})
	if err != nil {
		gslbutils.Errf("error in creating GSLBConfig object: %v", err)
		return
	}
	g.Eventually(func() string {
		gcObj, err := gcClient.AmkoV1alpha1().GSLBConfigs(AviSystemNS).Get(context.TODO(), GslbConfigName,
			metav1.GetOptions{})
		if err != nil {
			gslbutils.Errf("failed to fetch GSLBConfig object: %v", err)
			return ""
		}
		return gcObj.Status.State
	}).Should(gomega.Equal("success: gslb config accepted"))
}

func DeleteTestGDP(t *testing.T, ns, name string) error {
	err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	t.Logf("deleted GDP %s in %s namespace", name, ns)
	return nil
}

func TestGDP(t *testing.T) {
	aviSystemNS := "avi-system"
	ttl := 10
	testGDPObj := gdpalphav2.GlobalDeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "abc-gdp",
			Namespace: aviSystemNS,
		},
		Spec: gdpalphav2.GDPSpec{
			TTL: &ttl,
		},
	}
	newGdp, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies(aviSystemNS).Create(context.TODO(),
		&testGDPObj, metav1.CreateOptions{})
	if err != nil {
		gslbutils.Errf("error in creating GDP Object: %v", err)
		return
	}
	gslbutils.Logf("new gdp object: %v", newGdp)
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() string {
		gdpObj, err := gslbutils.GlobalGdpClient.AmkoV1alpha2().GlobalDeploymentPolicies("avi-system").Get(context.TODO(), "abc-gdp", metav1.GetOptions{})
		if err != nil {
			t.Errorf("failed to fetch GDP object: %v", err)
			return ""
		}
		return gdpObj.Status.ErrorStatus
	}).Should(gomega.Equal("success"))
	t.Cleanup(func() {
		DeleteTestGDP(t, "avi-system", "abc-gdp")
	})
}
