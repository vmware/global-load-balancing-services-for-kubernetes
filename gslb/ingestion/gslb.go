/*
* [2013] - [2020] Avi Networks Incorporated
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

package ingestion

import (
	"errors"
	"flag"
	"os"
	"sync"
	"time"

	"amko/gslb/gslbutils"
	"amko/gslb/nodes"

	gslbcs "amko/pkg/client/clientset/versioned"

	"github.com/avinetworks/container-lib/utils"
	"github.com/golang/glog"
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	gslbalphav1 "amko/pkg/apis/avilb/v1alpha1"
	gslbscheme "amko/pkg/client/clientset/versioned/scheme"
	gslbinformers "amko/pkg/client/informers/externalversions"
	gslblisters "amko/pkg/client/listers/avilb/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	avicache "amko/gslb/cache"

	avirest "amko/gslb/rest"
	aviretry "amko/gslb/retry"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type kubeClusterDetails struct {
	clusterName string
	kubeconfig  string
	kubeapi     string
	informers   *utils.Informers
}

type K8SInformers struct {
	Cs kubernetes.Interface
}

type ClusterCache struct {
	clusterName string
}

type GSLBConfigAddfn func(obj interface{})

var (
	masterURL         string
	kubeConfig        string
	insideCluster     bool
	membersKubeConfig string
	stopCh            <-chan struct{}
	cacheOnce         sync.Once
	informerTimeout   int64
)

func GetStopChannel() <-chan struct{} {
	return stopCh
}

func SetInformerListTimeout(val int64) {
	informerTimeout = val
}

type GSLBConfigController struct {
	kubeclientset kubernetes.Interface
	gslbclientset gslbcs.Interface
	gslbLister    gslblisters.GSLBConfigLister
	gslbSynced    cache.InformerSynced
	workqueue     workqueue.RateLimitingInterface
	recorder      record.EventRecorder
}

func (gslbController *GSLBConfigController) Cleanup() {
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "cleaning up the entire GSLB configuration")
}

func (gslbController *GSLBConfigController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "starting the workers")
	<-stopCh
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "shutting down the workers")
	return nil
}

func initFlags() {
	gslbutils.Logf("object: main, msg: %s", "initializing the flags")
	defKubeConfig := os.Getenv("HOME") + "/.kube/config"
	flag.StringVar(&kubeConfig, "kubeconfig", defKubeConfig, "Path to kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the kubernetes API server. Overrides any value in kubeconfig. Overrides any value in kubeconfig, only required if out-of-cluster.")
	gslbutils.Logf("master: %s, kubeconfig: %s, msg: %s", masterURL, kubeConfig, "fetched from cmd")
}

// GetNewController builds the GSLB Controller which has an informer for GSLB Config object
func GetNewController(kubeclientset kubernetes.Interface, gslbclientset gslbcs.Interface,
	gslbInformerFactory gslbinformers.SharedInformerFactory,
	AddGSLBConfigFunc GSLBConfigAddfn) *GSLBConfigController {

	gslbInformer := gslbInformerFactory.Avilb().V1alpha1().GSLBConfigs()
	// Create event broadcaster
	gslbscheme.AddToScheme(scheme.Scheme)
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "gslb-controller"})

	gslbController := &GSLBConfigController{
		kubeclientset: kubeclientset,
		gslbclientset: gslbclientset,
		gslbLister:    gslbInformer.Lister(),
		gslbSynced:    gslbInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "gslb-configs"),
		recorder:      recorder,
	}
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "setting up event handlers")
	// Event handler for when GSLB Config change
	gslbInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: AddGSLBConfigFunc,
		// Update not allowed for the GSLB Cluster Config object
		DeleteFunc: func(obj interface{}) {
			// Cleanup everything
			gslbController.Cleanup()
		},
	})
	return gslbController
}

// IsGSLBConfigValid returns true if the the GSLB Config object was created
// in "avi-system" namespace.
// TODO: Validate the controllers inside the config object
func IsGSLBConfigValid(obj interface{}) (*gslbalphav1.GSLBConfig, error) {
	config := obj.(*gslbalphav1.GSLBConfig)
	if config.ObjectMeta.Namespace == gslbutils.AVISystem {
		return config, nil
	}
	return nil, errors.New("invalid gslb config, namespace can only be avi-system")
}

func PublishChangeToRestLayer(gsKey interface{}, sharedQ *utils.WorkerQueue) {
	aviCacheKey, ok := gsKey.(avicache.TenantName)
	if !ok {
		gslbutils.Errf("CacheKey: %v, msg: cache key malformed, not publishing to rest layer", gsKey)
		return
	}
	nodes.PublishKeyToRestLayer(aviCacheKey.Tenant, aviCacheKey.Name, aviCacheKey.Name+"/"+aviCacheKey.Tenant, sharedQ)
}

// CacheRefreshRoutine fetches the objects in the AVI controller and finds out
// the delta between the existing and the new objects.
func CacheRefreshRoutine() {
	gslbutils.Logf("starting AVI cache refresh...\ncreating a new AVI cache")

	newAviCache := avicache.PopulateCache(false)
	existingAviCache := avicache.GetAviCache()

	sharedQ := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	// The refresh cycle builds a new set of AVI objects in `newAviCache` and compares them with
	// the existing avi cache. If a discrepancy is found, we just write the key to layer 3.
	for key, obj := range existingAviCache.Cache {
		existingGSObj, ok := obj.(*avicache.AviGSCache)
		if !ok {
			gslbutils.Errf("CacheKey: %v, CacheObj: %v, msg: existing GSLB Object in avi cache malformed", key, existingGSObj)
			continue
		}
		newGS, found := newAviCache.AviCacheGet(key)
		if !found {
			existingAviCache.AviCacheAdd(key, nil)
			PublishChangeToRestLayer(key, sharedQ)
			continue
		}
		newGSObj, ok := newGS.(*avicache.AviGSCache)
		if !ok {
			gslbutils.Warnf("CacheKey: %v, CacheObj: %v, msg: new GSLB object in avi cache malformed, will update", key,
				newGSObj)
			continue
		}
		if existingGSObj.CloudConfigCksum != newGSObj.CloudConfigCksum {
			gslbutils.Logf("CacheKey: %v, CacheObj: %v, msg: GSLB Service has changed in AVI, will update", key, obj)
			// First update the newly fetched avi cache in the existing avi cache key
			existingAviCache.AviCacheAdd(key, newGSObj)
			PublishChangeToRestLayer(key, sharedQ)
		}
	}

	gslbutils.Logf("AVI Cache refresh done")
}

// GenerateKubeConfig reads the kubeconfig given through the environment variable
// decodes it and then writes to a temporary file.
func GenerateKubeConfig() error {
	membersKubeConfig = os.Getenv("GSLB_CONFIG")
	if membersKubeConfig == "" {
		utils.AviLog.Error.Fatal("GSLB_CONFIG environment variable not set, exiting...")
		return errors.New("GSLB_CONFIG environment variable not set, exiting")
	}
	f, err := os.Create(gslbutils.GSLBKubePath)
	if err != nil {
		return errors.New("Error in creating file: " + err.Error())
	}

	_, err = f.WriteString(membersKubeConfig)
	if err != nil {
		return errors.New("Error in writing to config file: " + err.Error())
	}
	return nil
}

func parseControllerDetails(gc *gslbalphav1.GSLBConfig) {
	// Read the gslb leader's credentials
	leaderIP := gc.Spec.GSLBLeader.ControllerIP
	leaderVersion := gc.Spec.GSLBLeader.ControllerVersion
	leaderSecret := gc.Spec.GSLBLeader.Credentials

	if leaderIP == "" || leaderVersion == "" || leaderSecret == "" {
		gslbutils.Errf("controllerIP: %s, controllerVersion: %s, credentials: %s, msg: Invalid GSLB leader configuration",
			leaderIP, leaderVersion, leaderSecret)
		return
	}

	secretObj, err := gslbutils.GlobalKubeClient.CoreV1().Secrets(gslbutils.AVISystem).Get(leaderSecret, metav1.GetOptions{})
	if err != nil || secretObj == nil {
		gslbutils.Errf("Error in fetching leader controller secret %s in namespace %s, can't initialize controller",
			leaderSecret, gslbutils.AVISystem)
		return
	}
	ctrlUsername := secretObj.Data["username"]
	ctrlPassword := secretObj.Data["password"]
	gslbutils.NewAviControllerConfig(string(ctrlUsername), string(ctrlPassword), leaderIP, leaderVersion)
}

// AddGSLBConfigObject parses the gslb config object and starts informers
// for the member clusters.
func AddGSLBConfigObject(obj interface{}) {
	if gslbutils.IsGSLBConfigSet() {
		gslbutils.Errf("GSLB configuration is set already, can't change it. Delete and re-create the GSLB config object.")
		return
	}

	gc, err := IsGSLBConfigValid(obj)
	if err != nil {
		gslbutils.Warnf("ns: %s, gslbConfig: %s, msg: %s, %s", gc.ObjectMeta.Namespace, gc.ObjectMeta.Name,
			"invalid format", err)
		return
	}
	gslbutils.Logf("ns: %s, gslbConfig: %s, msg: %s", gc.ObjectMeta.Namespace, gc.ObjectMeta.Name,
		"got an add event")

	// parse and set the controller environment variables
	parseControllerDetails(gc)

	cacheRefreshInterval := gc.Spec.RefreshInterval
	if cacheRefreshInterval <= 0 {
		gslbutils.Logf("Invalid refresh interval provided, will set it to default %d seconds", gslbutils.DefaultRefreshInterval)
		cacheRefreshInterval = gslbutils.DefaultRefreshInterval
	}
	gslbutils.Logf("Cache refresh interval: %d seconds", cacheRefreshInterval)
	// Secret created with name: "gslb-config-secret" and environment variable to set is
	// GSLB_CONFIG.
	err = GenerateKubeConfig()
	if err != nil {
		utils.AviLog.Error.Fatalf("Error in generating the kubeconfig file: %s", err.Error())
	}
	aviCtrlList := InitializeGSLBClusters(gslbutils.GSLBKubePath, gc.Spec.MemberClusters)
	cacheOnce.Do(func() {
		gslbutils.GSLBConfigObj = gc
	})

	// TODO: Change the GSLBConfig CRD to take full sync interval as an input and fetch that
	// value before going into full sync
	// boot up time cache population
	avicache.PopulateCache(true)
	// Initialize a periodic worker running full sync
	refreshWorker := gslbutils.NewFullSyncThread(gslbutils.DefaultRefreshInterval)
	refreshWorker.SyncFunction = CacheRefreshRoutine
	go refreshWorker.Run()

	gcChan := gslbutils.GetGSLBConfigObjectChan()
	*gcChan <- true

	// Start the informers for the member controllers
	for _, aviCtrl := range aviCtrlList {
		aviCtrl.Start(stopCh)
	}

	// GSLB Configuration successfully done
	gslbutils.SetGSLBConfig(true)
}

// Initialize initializes the first controller which looks for GSLB Config
func Initialize() {
	initFlags()
	flag.Parse()
	stopCh = utils.SetupSignalHandler()
	// Check if we are running inside kubernetes
	cfg, err := rest.InClusterConfig()
	if err != nil {
		gslbutils.Warnf("object: main, msg: %s, %s", "not running inside kubernetes cluster", err)
	} else {
		gslbutils.Logf("object: main, msg: %s", "running inside kubernetes cluster, won't use config files")
		insideCluster = true
	}
	if insideCluster == false {
		cfg, err = clientcmd.BuildConfigFromFlags(masterURL, kubeConfig)
		gslbutils.Logf("masterURL: %s, kubeconfigPath: %s, msg=%s", masterURL, kubeConfig,
			"built from flags")
		if err != nil {
			gslbutils.Logf("object: main, msg: %s, %s", "error building kubeconfig", err)
		}
	}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		utils.AviLog.Error.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	gslbutils.GlobalKubeClient = kubeClient
	gslbClient, err := gslbcs.NewForConfig(cfg)
	if err != nil {
		utils.AviLog.Error.Fatalf("Error building gslb config clientset: %s", err.Error())
	}

	SetInformerListTimeout(120)
	ingestionQueueParams := utils.WorkerQueue{NumWorkers: 1, WorkqueueName: utils.ObjectIngestionLayer}
	graphQueueParams := utils.WorkerQueue{NumWorkers: utils.NumWorkersGraph, WorkqueueName: utils.GraphLayer}
	slowRetryQParams := utils.WorkerQueue{NumWorkers: 1, WorkqueueName: gslbutils.SlowRetryQueue, SlowSyncTime: gslbutils.SlowSyncTime}
	fastRetryQParams := utils.WorkerQueue{NumWorkers: 1, WorkqueueName: gslbutils.FastRetryQueue}

	utils.SharedWorkQueue(ingestionQueueParams, graphQueueParams, slowRetryQParams, fastRetryQParams)

	// Set workers for layer 2
	ingestionSharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	ingestionSharedQueue.SyncFunc = nodes.SyncFromIngestionLayer
	ingestionSharedQueue.Run(stopCh)

	// Set workers for layer 3 (REST layer)
	graphSharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	graphSharedQueue.SyncFunc = avirest.SyncFromNodesLayer
	graphSharedQueue.Run(stopCh)

	// Set up retry Queue
	slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
	slowRetryQueue.SyncFunc = aviretry.SyncFromRetryLayer
	slowRetryQueue.Run(stopCh)
	fastRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.FastRetryQueue)
	fastRetryQueue.SyncFunc = aviretry.SyncFromRetryLayer
	fastRetryQueue.Run(stopCh)

	// kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	gslbInformerFactory := gslbinformers.NewSharedInformerFactory(gslbClient, time.Second*30)

	gslbController := GetNewController(kubeClient, gslbClient, gslbInformerFactory,
		AddGSLBConfigObject)

	// Start the informer for the GDP controller
	gslbInformer := gslbInformerFactory.Avilb().V1alpha1().GSLBConfigs()

	go gslbInformer.Informer().Run(stopCh)

	gslbutils.Logf("waiting for a GSLB config object to be added")

	// Wait till a GSLB config object is added
	tmpChan := gslbutils.GetGSLBConfigObjectChan()
	<-*tmpChan

	gdpCtrl := InitializeGDPController(kubeClient, gslbClient, gslbInformerFactory, AddGDPObj,
		UpdateGDPObj, DeleteGDPObj)

	// Start the informer for the GDP controller
	gdpInformer := gslbInformerFactory.Avilb().V1alpha1().GlobalDeploymentPolicies()
	go gdpInformer.Informer().Run(stopCh)

	if err = gslbController.Run(stopCh); err != nil {
		utils.AviLog.Error.Fatalf("Error running GSLB controller: %s\n", err.Error())
	}

	if err := gdpCtrl.Run(stopCh); err != nil {
		utils.AviLog.Error.Fatalf("Error running GDP controller: %s\n", err)
	}
}

// BuildContextConfig builds the kubernetes/openshift context config
func BuildContextConfig(kubeconfigPath, context string) (*restclient.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

func InformersToRegister(oclient *oshiftclient.Clientset, kclient *kubernetes.Clientset) []string {
	allInformers := []string{}
	_, err := oclient.RouteV1().Routes("").List(metav1.ListOptions{TimeoutSeconds: &informerTimeout})
	if err == nil {
		// Openshift cluster with route support, we will just add service informer
		allInformers = append(allInformers, utils.RouteInformer)
	} else {
		// Kubernetes cluster
		_, ingErr := kclient.NetworkingV1beta1().Ingresses("").List(metav1.ListOptions{TimeoutSeconds: &informerTimeout})
		if ingErr == nil {
			// CoreV1 Ingress
			allInformers = append(allInformers, utils.CoreV1IngressInformer)
		} else {
			allInformers = append(allInformers, utils.ExtV1IngressInformer)
		}
	}

	allInformers = append(allInformers, utils.ServiceInformer)
	return allInformers
}

// InitializeGSLBClusters initializes the GSLB member clusters
func InitializeGSLBClusters(membersKubeConfig string, memberClusters []gslbalphav1.MemberCluster) []*GSLBMemberController {
	clusterDetails := loadClusterAccess(membersKubeConfig, memberClusters)
	clients := make(map[string]*kubernetes.Clientset)

	informersArg := make(map[string]interface{})

	aviCtrlList := make([]*GSLBMemberController, 0)
	for _, cluster := range clusterDetails {
		gslbutils.Logf("cluster: %s, msg: %s", cluster.clusterName, "initializing")
		cfg, err := BuildContextConfig(cluster.kubeconfig, cluster.clusterName)
		if err != nil {
			gslbutils.Warnf("cluster: %s, msg: %s, %s", cluster.clusterName, "error in connecting to kubernetes API",
				err)
			continue
		} else {
			gslbutils.Logf("cluster: %s, msg: %s", cluster.clusterName, "successfully connected to kubernetes API")
		}
		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			gslbutils.Warnf("cluster: %s, msg: %s, %s", cluster.clusterName, "error in creating kubernetes clientset",
				err)
			continue
		}
		oshiftClient, err := oshiftclient.NewForConfig(cfg)
		if err != nil {
			gslbutils.Warnf("cluster: %s, msg: %s, %s", cluster.clusterName, "error in creating openshift clientset")
			continue
		}
		informersArg[utils.INFORMERS_OPENSHIFT_CLIENT] = oshiftClient
		informersArg[utils.INFORMERS_INSTANTIATE_ONCE] = false
		registeredInformers := InformersToRegister(oshiftClient, kubeClient)
		if len(registeredInformers) == 0 {
			gslbutils.Errf("No informers available for this cluster %s, returning", cluster.clusterName)
			continue
		}
		gslbutils.Logf("Informers for cluster %s: %v", cluster.clusterName, registeredInformers)
		informerInstance := utils.NewInformers(utils.KubeClientIntf{
			ClientSet: kubeClient},
			registeredInformers,
			informersArg)
		clients[cluster.clusterName] = kubeClient
		aviCtrl := GetGSLBMemberController(cluster.clusterName, informerInstance)
		aviCtrl.SetupEventHandlers(K8SInformers{Cs: clients[cluster.clusterName]})
		aviCtrlList = append(aviCtrlList, &aviCtrl)
	}
	return aviCtrlList
}

func loadClusterAccess(membersKubeConfig string, memberClusters []gslbalphav1.MemberCluster) []kubeClusterDetails {
	var clusterDetails []kubeClusterDetails
	for _, memberCluster := range memberClusters {
		clusterDetails = append(clusterDetails, kubeClusterDetails{memberCluster.ClusterContext,
			membersKubeConfig, "", nil})
		gslbutils.Logf("cluster: %s, msg: %s", memberCluster.ClusterContext, "loaded cluster access")
	}
	return clusterDetails
}
