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

package k8sutils

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	amkov1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

const (
	ServiceImportFullIndexer    = "clusterNameNamespaceIndex"
	ServiceImportClusterIndexer = "clusterIndex"
)

type K8sServiceDiscoveryConfig struct {
	clientset       *kubernetes.Clientset
	amkov1Clientset *amkov1.Clientset
	clusters        []*K8sClusterConfig
}

var k8sCfg *K8sServiceDiscoveryConfig

func InitK8sServiceDiscoveryConfig(cfg *rest.Config) (*K8sServiceDiscoveryConfig, error) {
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error in initializing k8s service discovery config: %v", err)
	}
	amkoClient, err := amkov1.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error in initializing k8s service discovery config: %v", err)
	}

	k8sCfg = &K8sServiceDiscoveryConfig{
		clientset:       kubeClient,
		amkov1Clientset: amkoClient,
	}
	return k8sCfg, nil
}

func (sdc *K8sServiceDiscoveryConfig) SetClusterConfigs(cc []*K8sClusterConfig) {
	sdc.clusters = cc
}

func (sdc *K8sServiceDiscoveryConfig) GetClientset() *kubernetes.Clientset {
	return sdc.clientset
}

func (sdc *K8sServiceDiscoveryConfig) GetAmkoV1Clientset() *amkov1.Clientset {
	return sdc.amkov1Clientset
}

const (
	// SDKubePath is to be used for initializing member cluster clients, this constant
	// must not be used directly, IsKubePathSet() must be checked before using the below
	// constant
	SDKubePath = "/tmp/sd-kubeconfig"
)

var kubePathInit bool
var kubePathOnce sync.Once

// IsKubePathSet returns true if the kubeconfig exists in the SDKubePath
func IsKubePathSet() bool {
	kubePathOnce.Do(func() {
		_, err := os.Stat(SDKubePath)
		if err != nil {
			gslbutils.Errf("error in fetching file %s: %v", SDKubePath, err)
			return
		}
		kubePathInit = true
	})
	return kubePathInit
}

type K8sClusterConfig struct {
	name      string
	informers *containerutils.Informers
	workqueue []workqueue.RateLimitingInterface
}

// InitK8sClusterConfig initializes a kubernetes cluster client and informers.
// "name" must have the cluster context name.
func InitK8sClusterConfig(cname string) (*K8sClusterConfig, error) {
	informersArg := make(map[string]interface{})

	gslbutils.Logf("cluster: %s, msg: initializing clientset", cname)
	if !IsKubePathSet() {
		return nil, fmt.Errorf("can't initialize clientset for cluster %s, kubeconfig path is unset", cname)
	}
	cfg, err := BuildContextConfig(SDKubePath, cname)
	if err != nil {
		return nil, fmt.Errorf("error in building context config for %s: %v", cname, err)
	}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error in creating a clientset for %s: %v", cname, err)
	}

	informersArg[containerutils.INFORMERS_INSTANTIATE_ONCE] = false
	informersToStart, err := InformersToRegister(kubeClient, cname)
	if err != nil {
		return nil, fmt.Errorf("error in getting informers for %s: %v", cname, err)
	}
	informerInstance := containerutils.NewInformers(
		containerutils.KubeClientIntf{
			ClientSet: kubeClient,
		},
		informersToStart,
		informersArg,
	)
	return &K8sClusterConfig{
		name:      cname,
		informers: informerInstance,
		workqueue: containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer).Workqueue,
	}, nil
}

func (k8sCluster *K8sClusterConfig) Name() string {
	return k8sCluster.name
}

func (k8sCluster *K8sClusterConfig) ClientSet() kubernetes.Interface {
	return k8sCluster.informers.ClientSet
}

func (k8sCluster *K8sClusterConfig) ServiceInformer() coreinformers.ServiceInformer {
	return k8sCluster.informers.ServiceInformer
}

func (k8sCluster *K8sClusterConfig) GetNodes() (*corev1.NodeList, error) {
	nodes, err := k8sCluster.informers.ClientSet.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// GetNodeFromInformer returns the IP of the node and the error (if any)
func (k8sCluster *K8sClusterConfig) GetNodeFromInformer(nodeName string) (string, error) {
	node, err := k8sCluster.informers.NodeInformer.Lister().Get(nodeName)
	if err != nil {
		return "", err
	}
	return GetNodeIP(node.Status)
}

func (k8sCluster *K8sClusterConfig) GetSvcFromInformer(ns, svcName string) (*corev1.Service, error) {
	svc, err := k8sCluster.informers.ServiceInformer.Lister().Services(ns).Get(svcName)
	if err != nil {
		gslbutils.Errf("cluster: %s, ns: %s, svc: %s, err: %v", k8sCluster.Name(),
			ns, svcName, err)
		return nil, err
	}
	return svc.DeepCopy(), nil
}

func (k8sCluster *K8sClusterConfig) GetWorkqueue() []workqueue.RateLimitingInterface {
	return k8sCluster.workqueue
}

var sharedClusterList map[string]*K8sClusterConfig

func InitSharedClusterList(clusterConfigs []*K8sClusterConfig) {
	sharedClusterList = make(map[string]*K8sClusterConfig)
	for _, cc := range clusterConfigs {
		sharedClusterList[cc.Name()] = cc
	}
}

func RunSharedClusterInformers(stopCh <-chan struct{}) {
	for cname, cc := range sharedClusterList {
		if cc.informers.ServiceInformer != nil {
			go cc.informers.ServiceInformer.Informer().Run(stopCh)
			gslbutils.Logf("cluster: %s, msg: started service informer", cname)
		}
		if cc.informers.NodeInformer != nil {
			go cc.informers.NodeInformer.Informer().Run(stopCh)
			gslbutils.Logf("cluster: %s, msg: started namespace informer", cname)
		}
	}
}

func AddEventHandlersToClusterInformers(numWorkers uint32) {
	for cname, cc := range sharedClusterList {
		if cc.informers.ServiceInformer != nil {
			cc.informers.ServiceInformer.Informer().AddEventHandler(SvcEventHandlers(numWorkers, cc))
			gslbutils.Logf("cluster: %s, msg: added service event handler", cname)
		}
		if cc.informers.NodeInformer != nil {
			cc.informers.NodeInformer.Informer().AddEventHandler(NodeEventHandlers(numWorkers, cc))
			gslbutils.Logf("cluster: %s, msg: added node event handler", cname)
		}
	}
}

func GetNodeInfoFromSharedClusters(cname, nodeName string) (string, error) {
	return sharedClusterList[cname].GetNodeFromInformer(nodeName)
}

func GetSvcInfoFromSharedClusters(cname, ns, svc string) (*corev1.Service, error) {
	return sharedClusterList[cname].GetSvcFromInformer(ns, svc)
}

func GetWorkqueueForCluster(cname string) []workqueue.RateLimitingInterface {
	return sharedClusterList[cname].GetWorkqueue()
}

func GetClusterListFromSharedClusters() []string {
	result := []string{}
	for cname, _ := range sharedClusterList {
		result = append(result, cname)
	}
	return result
}

func GetClusterListStr(clist []*K8sClusterConfig) []string {
	result := []string{}
	for _, c := range clist {
		result = append(result, c.Name())
	}
	return result
}

// GetClustersetKubeConfig fetches the clusterset's secret, validates the secret
// and returns the kubeconfig for the clusters in the clusterset
func GetClustersetKubeConfig(kubeClient *kubernetes.Clientset, secretName string) (string, error) {
	secretObj, err := kubeClient.CoreV1().Secrets(utils.AviSystemNS).Get(context.TODO(), secretName, v1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error in fetching clusterset secret: %s", err)
	}

	clusterData, present := secretObj.Data["clusters"]
	if !present {
		return "", fmt.Errorf("key \"clusters\" is missing from secret data, required for fetching cluster kubeconfig")
	}

	if string(secretObj.Data["clusters"]) == "" {
		return "", fmt.Errorf("data for key \"clusters\" is missing from secret data, required for fetching cluster kubeconfig")
	}
	return string(clusterData), nil
}

func GenerateKubeConfig(kubeConfigData string) error {
	f, err := os.Create(SDKubePath)
	if err != nil {
		return fmt.Errorf("error in creating file: %v", err)
	}

	_, err = f.WriteString(kubeConfigData)
	if err != nil {
		return fmt.Errorf("error in writing to config file: %v", err)
	}

	return nil
}

func InformersToRegister(kclient *kubernetes.Clientset, cname string) ([]string, error) {
	var informerTimeout int64 = 120
	_, err := kclient.CoreV1().Services("").List(context.TODO(), v1.ListOptions{TimeoutSeconds: &informerTimeout})
	if err != nil {
		return nil, fmt.Errorf("can't access /api/services for cluster %s, error: %v", cname, err)
	}
	allInformers := []string{containerutils.ServiceInformer, containerutils.NodeInformer}
	return allInformers, nil
}

// BuildContextConfig builds the kubernetes/openshift context config
func BuildContextConfig(kubeconfigPath, context string) (*restclient.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

func GetNodeIP(nodeStatus corev1.NodeStatus) (string, error) {
	for _, addr := range nodeStatus.Addresses {
		if addr.Type == "InternalIP" {
			return addr.Address, nil
		}
	}
	return "", fmt.Errorf("can't find node address")
}

func IsErrorTypeNotFound(err error) bool {
	return k8serrors.IsNotFound(err)
}
