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

package clusterset

import (
	"fmt"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	csv1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	k8sutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/k8s_utils"
	"k8s.io/client-go/kubernetes"
)

// ValidateClusterset checks the Clusterset object and returns a list of initialized
// kubernetes clients for a list of clusters
func ValidateClusterset(cs *csv1alpha1.ClusterSet, kubeClient *kubernetes.Clientset) ([]*k8sutils.K8sClusterConfig, error) {
	if len(cs.Spec.Clusters) == 0 {
		return nil, fmt.Errorf("cluster list is empty")
	}
	if cs.Spec.SecretName == "" {
		return nil, fmt.Errorf("secret name is empty")
	}

	// get the clusterset kubeconfig data
	kubeConfigData, err := k8sutils.GetClustersetKubeConfig(kubeClient, cs.Spec.SecretName)
	if err != nil {
		return nil, fmt.Errorf("error in getting clusterset kubeconfig: %v", err)
	}

	// generate the kubeconfig file
	err = k8sutils.GenerateKubeConfig(kubeConfigData)
	if err != nil {
		return nil, fmt.Errorf("error in parsing secret and generating kubeconfig: %v", err)
	}

	clusters := []*k8sutils.K8sClusterConfig{}
	// initialize clientsets for all member clusters in clusterset
	for _, cc := range cs.Spec.Clusters {
		cluster, err := k8sutils.InitK8sClusterConfig(cc.Context)
		if err != nil {
			// error in initializing clientsets and informers for a cluster, continue
			gslbutils.Warnf("cluster: %s, msg: error in initializing cluster: %v", err)
			continue
		}
		clusters = append(clusters, cluster)
	}
	if len(clusters) == 0 {
		return nil, fmt.Errorf("error in initializing clusters, no valid clusters in clusterset")
	}

	return clusters, nil
}

func GetClusterList(clusterConfigs []*k8sutils.K8sClusterConfig) []string {
	clist := []string{}
	for _, c := range clusterConfigs {
		clist = append(clist, c.Name())
	}
	return clist
}
