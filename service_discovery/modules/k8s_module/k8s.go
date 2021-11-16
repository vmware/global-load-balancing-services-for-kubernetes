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

package k8s_module

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	mciinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/informers/externalversions"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/bootup"
	clusterset "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/clusterset"
	k8sutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/k8s_utils"
	mciutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/mci_utils"
	serviceimport "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/service_import"
	sdutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	masterURL     string
	kubeConfig    string
	insideCluster bool
)

func Init() {
	initFlags()
	flag.Parse()

	if logfilePath := os.Getenv("SERVICE_DISCOVERY_LOG_FILE_PATH"); logfilePath != "" {
		flag.Lookup("log_dir").Value.Set(logfilePath)
	} else {
		flag.Lookup("logtostderr").Value.Set("true")
	}

	stopCh := containerutils.SetupSignalHandler()
	// Check if we are running inside a kubernetes cluster
	cfg, err := rest.InClusterConfig()
	if err != nil {
		gslbutils.Warnf("object: main, msg: %s, %s", "not running inside a kubernetes cluster", err)
	} else {
		gslbutils.Logf("object: main, msg: %s", "running inside a kubernetes cluster, won't use config files")
		insideCluster = true
	}

	if !insideCluster {
		cfg, err = clientcmd.BuildConfigFromFlags(masterURL, kubeConfig)
		gslbutils.Logf("masterURL: %s, kubeconfigPath: %s, msg: %s", masterURL, kubeConfig, "built from flags")
		if err != nil {
			gslbutils.LogAndPanic(err.Error() + ", error building kubeconfig")
		}
	}

	InitServiceDiscoveryConfigAndInformers(cfg, stopCh)

	<-stopCh
	gslbutils.Logf("service discovery is exiting")
}

func InitServiceDiscoveryConfigAndInformers(cfg *rest.Config, stopCh <-chan struct{}) {
	k8sSDConfig, err := k8sutils.InitK8sServiceDiscoveryConfig(cfg)
	if err != nil {
		gslbutils.Errf("%v", err)
		panic(err.Error())
	}

	gslbutils.SetWaitGroupMap()

	InitQueues()

	// initialize clusterset
	clusterConfigs, err := GetClusterInfo(k8sSDConfig)
	if err != nil {
		gslbutils.Errf("error in getting data from clusterset: %v", err)
		log.Panic(err.Error())
	}
	k8sSDConfig.SetClusterConfigs(clusterConfigs)

	k8sutils.InitSharedClusterList(clusterConfigs)
	k8sutils.RunSharedClusterInformers(stopCh)

	mciInformerFactory := mciinformers.NewSharedInformerFactory(k8sSDConfig.GetAmkoV1Clientset(), time.Second*30)
	mciCtrl := mciutils.InitializeMCIController(k8sSDConfig.GetClientset(), k8sSDConfig.GetAmkoV1Clientset(),
		mciInformerFactory, k8sutils.GetClusterListStr(clusterConfigs))
	mciInformer := mciInformerFactory.Amko().V1alpha1().MultiClusterIngresses()

	siCtrl := serviceimport.InitializeServiceImportController(k8sSDConfig.GetClientset(), k8sSDConfig.GetAmkoV1Clientset(),
		mciInformerFactory)

	// initialize the handler for layer 2 (service import objects)
	serviceimport.InitServiceImportHandler(k8sSDConfig.GetAmkoV1Clientset(), clusterset.GetClusterList(clusterConfigs),
		siCtrl)
	go siCtrl.Informer.Run(stopCh)

	err = bootup.BootupSync(clusterConfigs, k8sSDConfig.GetAmkoV1Clientset())
	if err != nil {
		gslbutils.LogAndPanic("error while bootup sync: " + err.Error())
	}

	go mciInformer.Informer().Run(stopCh)
	siCtrl.Informer.AddEventHandler(serviceimport.ServiceImportEventHandlers(4))

	go RunControllers(mciCtrl, siCtrl, stopCh)
	RunQueues(stopCh)

	k8sutils.AddEventHandlersToClusterInformers(sdutils.NumIngestionWorkers)
}

func InitQueues() {
	gslbutils.Logf("initializing queues")
	ingestionQueueParams := containerutils.WorkerQueue{
		NumWorkers:    sdutils.NumIngestionWorkers,
		WorkqueueName: containerutils.ObjectIngestionLayer,
	}
	wq := containerutils.SharedWorkQueue(&ingestionQueueParams)

	ingestionSharedQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	ingestionSharedQueue.SyncFunc = serviceimport.SyncFromIngestionLayer
	gslbutils.Logf("length of workqueue: %d, numworkers: %d", len(wq.GetQueueByName(containerutils.ObjectIngestionLayer).Workqueue),
		ingestionSharedQueue.NumWorkers)
}

func RunQueues(stopCh <-chan struct{}) {
	ingestionSharedQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	ingestionSharedQueue.Run(stopCh, gslbutils.GetWaitGroupFromMap(gslbutils.WGIngestion))
}

func RunControllers(mciCtrl *mciutils.MCIController, siCtrl *serviceimport.ServiceImportController, stopCh <-chan struct{}) {
	if err := mciCtrl.Run(stopCh); err != nil {
		gslbutils.Logf("error running MCI controller: %v", err)
		log.Panic("error running MCI controller")
	}
	if err := siCtrl.Run(stopCh); err != nil {
		gslbutils.Logf("error running Service Import controller: %v", err)
		log.Panic("error running Service Import controller")
	}
}

func GetClusterInfo(sd *k8sutils.K8sServiceDiscoveryConfig) ([]*k8sutils.K8sClusterConfig, error) {
	csList, err := sd.GetAmkoV1Clientset().AmkoV1alpha1().ClusterSets(sdutils.AviSystemNS).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error while fetching clusterset list: %v", err)
	}
	if len(csList.Items) > 1 {
		return nil, fmt.Errorf("only one clusterset allowed in this cluster")
	}
	if len(csList.Items) == 0 {
		return nil, fmt.Errorf("no clusterset available, returning")
	}
	cs := csList.Items[0].DeepCopy()
	clusterConfigs, err := clusterset.ValidateClusterset(cs, sd.GetClientset())
	if err != nil {
		return nil, fmt.Errorf("error in validating clusterset: %v", err)
	}
	return clusterConfigs, nil
}

func initFlags() {
	gslbutils.Logf("initializing the flags")
	defKubeConfig := os.Getenv("HOME") + "/.kube/config"
	flag.StringVar(&kubeConfig, "kubeconfigpath", defKubeConfig, "Path to kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the kubernetes API server. Overrides any value in kubeconfig, only required if out-of-cluster")
	gslbutils.Logf("master: %s, kubeconfig: %s, msg: fetched from cmd", masterURL, kubeConfig)
}
