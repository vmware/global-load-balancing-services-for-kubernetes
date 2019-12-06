package gslb

import (
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

// BuildContextConfig builds the kubernetes/openshift context config
func BuildContextConfig(kubeconfigPath, context string) (*restclient.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

// Initialize initializes set of clusters
func Initialize() {
	clusterDetails := loadClusterAccess()
	clients := make(map[string]*kubernetes.Clientset)

	registeredInformers := []string{containerutils.IngressInformer, containerutils.RouteInformer}
	informersArg := make(map[string]interface{})

	stopCh := containerutils.SetupSignalHandler()

	for _, cluster := range clusterDetails {
		cfg, err := BuildContextConfig(cluster.kubeconfig, cluster.clusterName)
		if err != nil {
			containerutils.AviLog.Warning.Printf("Error in connecting to kubernetes API %v", err.Error())
			continue
		} else {
			containerutils.AviLog.Info.Printf("Successfully connected to kubernetes API")
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
		informerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{
			ClientSet: kubeClient},
			registeredInformers,
			informersArg)
		clients[cluster.clusterName] = kubeClient
		aviCtrl := GetAviController(cluster.clusterName, informerInstance)
		aviCtrl.SetupEventHandlers(K8SInformers{cs: clients[cluster.clusterName]})
		aviCtrl.Start(stopCh)
	}
}

func loadClusterAccess() []kubeClusterDetails {
	var clusterDetails []kubeClusterDetails
	containerutils.AviLog.Info.Printf("clusterDetails: %v", clusterDetails)
	clusterDetails = append(clusterDetails, kubeClusterDetails{"kubernetes-admin@kubernetes",
		"/tmp/kubeconfigA", "", nil})
	return clusterDetails
}
