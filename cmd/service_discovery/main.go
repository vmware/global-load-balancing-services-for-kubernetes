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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	mcics "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned"
	mciinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/informers/externalversions"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/bootup"
	clusterset "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/clusterset"
	k8sutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/k8s_utils"
	mciutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/mci_utils"
	sdutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	masterURL     string
	kubeConfig    string
	insideCluster bool
)

type K8sServiceDiscoveryConfig struct {
	clientset    *kubernetes.Clientset
	mciClientset *mcics.Clientset
	clusters     []*k8sutils.K8sClusterConfig
}

func main() {
	InitModules()
}

func InitModules() {
	K8sInit()
}

// Initialize the k8s module
func K8sInit() {
	initFlags()
	flag.Parse()

	if logfilePath := os.Getenv("SERVICE_DISCOVERY_LOG_FILE_PATH"); logfilePath != "" {
		flag.Lookup("log_dir").Value.Set(logfilePath)
	} else {
		flag.Lookup("logtostderr").Value.Set("true")
	}

	stopCh := utils.SetupSignalHandler()
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

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		gslbutils.LogAndPanic("error building kubernetes clientset: " + err.Error())
	}

	gslbutils.SetWaitGroupMap()
	gslbutils.GlobalKubeClient = kubeClient

	mciClient, err := mcics.NewForConfig(cfg)
	if err != nil {
		gslbutils.LogAndPanic("error building mci clientset: " + err.Error())
	}
	sdConfig := K8sServiceDiscoveryConfig{
		clientset:    kubeClient,
		mciClientset: mciClient,
	}

	// initialize clusterset
	clusterConfigs, err := GetClusterInfo(&sdConfig, sdConfig.clientset)
	if err != nil {
		gslbutils.LogAndPanic("error in getting data from clusterset: " + err.Error())
	}
	sdConfig.clusters = clusterConfigs

	err = bootup.BootupSync(clusterConfigs, mciClient)
	if err != nil {
		gslbutils.LogAndPanic("error while bootup sync: " + err.Error())
	}

	mciInformerFactory := mciinformers.NewSharedInformerFactory(mciClient, time.Second*30)
	mciCtrl := mciutils.InitializeMCIController(kubeClient, mciClient, mciInformerFactory, k8sutils.GetClusterListStr(clusterConfigs))
	mciInformer := mciInformerFactory.Amko().V1alpha1().MultiClusterIngresses()
	go mciInformer.Informer().Run(stopCh)

	if err := mciCtrl.Run(stopCh); err != nil {
		gslbutils.LogAndPanic("error running MCI Controller: " + err.Error())
	}

	gslbutils.Logf("service discovery is exiting")
}

func GetClusterInfo(sd *K8sServiceDiscoveryConfig, kubeclient *kubernetes.Clientset) ([]*k8sutils.K8sClusterConfig, error) {
	csList, err := sd.mciClientset.AmkoV1alpha1().ClusterSets(sdutils.AviSystemNS).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error while fetching clusterset list: %v", err)
	}
	if len(csList.Items) != 1 {
		return nil, fmt.Errorf("error in getting clusterset: only one clusterset allowed in this cluster")
	}
	cs := csList.Items[0].DeepCopy()
	clusterConfigs, err := clusterset.ValidateClusterset(cs, kubeclient)
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
