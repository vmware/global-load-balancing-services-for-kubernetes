package hacloud

import (
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type clusterDetails struct {
	clustername string
	kubeconfig  string
	kubeapi     string
	informers   *containerutils.Informers
}

type K8sinformers struct {
	cs kubernetes.Interface
}

type ClusterCache struct {
	clustername string
	shardPrefix string
	numShards   int
	vsipCache   *containerutils.AviCache
}

//var VSIPetailsClusterCache *containerutils.AviMultiCache

func BuildContextConfig(kubeconfigPath, context string) (*restclient.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

var AviCtrls map[string]AviController

func Initialize() {
	var clusterNames []string

	clusterdetails := loadClusterAccess()
	clients := make(map[string]*kubernetes.Clientset)
	AviCtrls = make(map[string]AviController)

	registeredInformers := []string{containerutils.ServiceInformer, containerutils.PodInformer, containerutils.EndpointInformer, containerutils.SecretInformer, containerutils.IngressInformer, containerutils.RouteInformer}
	informersArg := make(map[string]interface{})
	informersArg[containerutils.INFORMERS_INSTANTIATE_ONCE] = true

	for _, cluster := range clusterdetails {
		cfg, err := BuildContextConfig(cluster.kubeconfig, cluster.clustername)
		if err != nil {
			containerutils.AviLog.Infof("Error in connecting to kubernetes API %v", err.Error())
			continue
		} else {
			containerutils.AviLog.Infof("Successfully connected to kubernetes API")
		}
		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			containerutils.AviLog.Infof("Error in creating k8s clientset %v", err.Error())
			continue
		}
		oshiftClient, err := oshiftclient.NewForConfig(cfg)
		if err != nil {
			containerutils.AviLog.Infof("Error in creating openshift clientset %v", err.Error())
			continue
		}
		informersArg[containerutils.INFORMERS_OPENSHIFT_CLIENT] = oshiftClient
		informerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{ClientSet: kubeClient}, registeredInformers, informersArg)
		clusterNames = append(clusterNames, cluster.clustername)
		clients[cluster.clustername] = kubeClient
		AviCtrls[cluster.clustername] = GetAviController(cluster.clustername, informerInstance)
	}
	stopCh := containerutils.SetupSignalHandler()
	for clustername, c := range AviCtrls {
		c.SetupEventHandlers(K8sinformers{cs: clients[clustername]})
		c.Start(stopCh)
	}
	//ingestionQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	//ingestionQueue.SyncFunc = SyncFromIngestionLayer
	//ingestionQueue.Run(stopCh)

}

// Stub Code: using static cluster details - to be read from crd
func loadClusterAccess() []clusterDetails {
	var clusterdetails []clusterDetails
	clusterdetails = append(clusterdetails, clusterDetails{"kubernetes-admin@kubernetes", "/tmp/kubeconfigA", "", nil})
	return clusterdetails
}
