package gslb

import (
	"encoding/base64"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/golang/glog"
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
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
)

const (
	// GSLBKubePath is a temporary path to put the kubeconfig
	GSLBKubePath = "/tmp/gslb-kubeconfig"
	//AVISystem is the namespace where everything AVI related is created
	AVISystem = "avi-system"
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
	utils.AviLog.Warning.Print("Cleaning up the entire GSLB configuration...")
}

func (gslbController *GSLBConfigController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	containerutils.AviLog.Info.Print("Starting the workers for gslb controller...")
	<-stopCh
	containerutils.AviLog.Info.Print("Shutting down the workers for gslb controller")
	return nil
}

func initFlags() {
	utils.AviLog.Info.Print("initializing the flags...")
	defKubeConfig := os.Getenv("HOME") + "/.kube/config"
	flag.StringVar(&kubeConfig, "kubeconfig", defKubeConfig, "Path to kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the kubernetes API server. Overrides any value in kubeconfig. Overrides any value in kubeconfig, only required if out-of-cluster.")
	utils.AviLog.Info.Printf("Master: %s, kubeconfig: %s", masterURL, kubeConfig)
}

// GetNewController builds the GSLB Controller which has an informer for GSLB Config object
func GetNewController(kubeclientset kubernetes.Interface, gslbclientset gslbcs.Interface,
	gslbInformerFactory gslbinformers.SharedInformerFactory,
	AddGSLBConfigFunc GSLBConfigAddfn) *GSLBConfigController {

	gslbInformer := gslbInformerFactory.Avilb().V1alpha1().GSLBConfigs()
	// Create event broadcaster
	gslbscheme.AddToScheme(scheme.Scheme)
	utils.AviLog.Info.Print("Creating event broadcaster for GSLB config controller")
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
	utils.AviLog.Info.Print("Setting up event handlers for GSLB Config controller")
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
func IsGSLBConfigValid(obj interface{}) *gslbalphav1.GSLBConfig {
	config := obj.(*gslbalphav1.GSLBConfig)
	if config.ObjectMeta.Namespace == AVISystem {
		return config
	}
	return nil
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
	f, err := os.Create(GSLBKubePath)
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
	gc := IsGSLBConfigValid(obj)
	if gc == nil {
		utils.AviLog.Warning.Printf("GSLB object not recognised, ignoring...")
		return
	}

	// Secret created with name: "gslb-config-secret" and environment variable to set is
	// GSLB_CONFIG.
	err := GenerateKubeConfig()
	if err != nil {
		utils.AviLog.Error.Fatalf("Error in generating the kubeconfig file: %s", err.Error())
	}
	aviCtrlList := InitializeGSLBClusters(GSLBKubePath, gc.Spec.MemberClusters)
	for _, aviCtrl := range aviCtrlList {
		aviCtrl.Start(stopCh)
	}
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
		utils.AviLog.Warning.Printf("We are not running inside kubernetes cluster: %s", err.Error())
	} else {
		utils.AviLog.Info.Printf("We are running inside a kubernetes cluster, won't use config files.")
		insideCluster = true
	}
	if insideCluster == false {
		cfg, err = clientcmd.BuildConfigFromFlags(masterURL, kubeConfig)
		utils.AviLog.Info.Printf("master: %s, kubeconfig: %s", masterURL, kubeConfig)
		if err != nil {
			utils.AviLog.Error.Fatalf("Error building kubeconfig: %s", err.Error())
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
		utils.AviLog.Info.Printf("Initializing for cluster context: %s", cluster.clusterName)
		cfg, err := BuildContextConfig(cluster.kubeconfig, cluster.clusterName)
		if err != nil {
			containerutils.AviLog.Warning.Printf("Error in connecting to kubernetes API %v", err.Error())
			continue
		} else {
			containerutils.AviLog.Info.Printf("Successfully connected to kubernetes API for cluster context: %s", cluster.clusterName)
		}
		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			containerutils.AviLog.Warning.Printf("Error in creating kubernetes clientset %v", err.Error())
			continue
		}
		oshiftClient, err := oshiftclient.NewForConfig(cfg)
		if err != nil {
			containerutils.AviLog.Info.Printf("Error in creating openshift clientset %v", err.Error())
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
		containerutils.AviLog.Info.Printf("Loaded cluster access for %s", memberCluster.ClusterContext)
	}
	return clusterDetails
}
