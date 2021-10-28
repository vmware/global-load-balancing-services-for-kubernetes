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
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

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
