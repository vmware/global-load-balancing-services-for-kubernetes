package ingestion

import (
	"encoding/base64"
	"errors"
	"flag"
	"os"
	"sync"
	"time"

	"github.com/golang/glog"
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/nodes"
	gslbcs "gitlab.eng.vmware.com/orion/mcc/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	gslbalphav1 "gitlab.eng.vmware.com/orion/mcc/pkg/apis/avilb/v1alpha1"
	gslbscheme "gitlab.eng.vmware.com/orion/mcc/pkg/client/clientset/versioned/scheme"
	gslbinformers "gitlab.eng.vmware.com/orion/mcc/pkg/client/informers/externalversions"
	gslblisters "gitlab.eng.vmware.com/orion/mcc/pkg/client/listers/avilb/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type kubeClusterDetails struct {
	clusterName string
	kubeconfig  string
	kubeapi     string
	informers   *containerutils.Informers
}

type K8SInformers struct {
	cs kubernetes.Interface
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
)

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

// GenerateKubeConfig reads the kubeconfig given through the environment variable
// decodes it and then writes to a temporary file.
func GenerateKubeConfig() error {
	membersKubeConfig = os.Getenv("GSLB_CONFIG")
	if membersKubeConfig == "" {
		utils.AviLog.Error.Fatal("GSLB_CONFIG environment variable not set, exiting...")
		return errors.New("GSLB_CONFIG environment variable not set, exiting")
	}
	membersData, err := base64.StdEncoding.DecodeString(membersKubeConfig)
	if err != nil {
		utils.AviLog.Error.Fatalf("Error in decoding the GSLB config data: %s", err)
		return errors.New("Error in decoding the GSLB config data: " + err.Error())
	}
	f, err := os.Create(gslbutils.GSLBKubePath)
	if err != nil {
		return errors.New("Error in creating file: " + err.Error())
	}

	_, err = f.WriteString(string(membersData))
	if err != nil {
		return errors.New("Error in writing to config file: " + err.Error())
	}
	return nil
}

// AddGSLBConfigObject parses the gslb config object and starts informers
// for the member clusters.
func AddGSLBConfigObject(obj interface{}) {
	utils.AviLog.Info.Print("adding gslb config object")
	gc, err := IsGSLBConfigValid(obj)
	if err != nil {
		gslbutils.Warnf("ns: %s, gslbConfig: %s, msg: %s, %s", gc.ObjectMeta.Namespace, gc.ObjectMeta.Name,
			"invalid format", err)
		return
	}
	gslbutils.Logf("ns: %s, gslbConfig: %s, msg: %s", gc.ObjectMeta.Namespace, gc.ObjectMeta.Name,
		"got an add event")
	// Secret created with name: "gslb-config-secret" and environment variable to set is
	// GSLB_CONFIG.
	err = GenerateKubeConfig()
	if err != nil {
		utils.AviLog.Error.Fatalf("Error in generating the kubeconfig file: %s", err.Error())
	}
	aviCtrlList := InitializeGSLBClusters(gslbutils.GSLBKubePath, gc.Spec.MemberClusters)
	for _, aviCtrl := range aviCtrlList {
		aviCtrl.Start(stopCh)
	}
	cacheOnce.Do(func() {
		gslbutils.GSLBConfigObj = gc
	})
}

// func GetKubernetesClient() (kubernetes.Interface, gslbcs.Clientset)

// Initialize initializes the first controller which looks for GSLB Config
func Initialize() {
	initFlags()
	flag.Parse()
	stopCh = containerutils.SetupSignalHandler()
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

	gslbClient, err := gslbcs.NewForConfig(cfg)
	if err != nil {
		utils.AviLog.Error.Fatalf("Error building gslb config clientset: %s", err.Error())
	}

	// Set workers for layer 2
	sharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	sharedQueue.SyncFunc = nodes.SyncFromIngestionLayer
	sharedQueue.Run(stopCh)

	// kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	gslbInformerFactory := gslbinformers.NewSharedInformerFactory(gslbClient, time.Second*30)

	gslbController := GetNewController(kubeClient, gslbClient, gslbInformerFactory,
		AddGSLBConfigObject)

	// Start the informer for the GDP controller
	gslbInformer := gslbInformerFactory.Avilb().V1alpha1().GSLBConfigs()
	go gslbInformer.Informer().Run(stopCh)

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

// InitializeGSLBClusters initializes the GSLB member clusters
func InitializeGSLBClusters(membersKubeConfig string, memberClusters []gslbalphav1.MemberCluster) []*GSLBMemberController {
	clusterDetails := loadClusterAccess(membersKubeConfig, memberClusters)
	clients := make(map[string]*kubernetes.Clientset)

	registeredInformers := []string{containerutils.IngressInformer, containerutils.RouteInformer}
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

		informersArg[containerutils.INFORMERS_OPENSHIFT_CLIENT] = oshiftClient
		informersArg[containerutils.INFORMERS_INSTANTIATE_ONCE] = false
		informerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{
			ClientSet: kubeClient},
			registeredInformers,
			informersArg)
		clients[cluster.clusterName] = kubeClient
		aviCtrl := GetGSLBMemberController(cluster.clusterName, informerInstance)
		aviCtrl.SetupEventHandlers(K8SInformers{cs: clients[cluster.clusterName]})
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
