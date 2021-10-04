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

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// MCI is the top-level type
type MCI struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec for MCI Config
	Spec MCISpec `json:"spec,omitempty"`
	// +optional
	Status MCIStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MCIList is a list of GSLBConfig resources
type MCIList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCI `json:"items"`
}

// MCISpec is the GSLB configuration
type MCISpec struct {
	Hostname   string          `json:"hostName,omitempty"`
	SecretName string          `json:"secretName,omitempty"`
	Config     []BackendConfig `json:"config,omitempty"`
}

// BackendConfig contains the parameters from the tenant clusters
type BackendConfig struct {
	Path           string    `json:"path,omitempty"`
	ClusterContext string    `json:"cluster,omitempty"`
	Weight         int       `json:"weight,omitempty"`
	Services       []Service `json:"service,omitempty"`
}

// Service contains the backend service configuration and endpoints
type Service struct {
	Name      string `json:"name,omitempty"`
	Port      int    `json:"port,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// MCIStatus represents the current status of the MCI object
type MCIStatus struct {
	LoadBalancer LoadBalancer          `json:"loadBalancer,omitempty"`
	Backends     []BackendObjectStatus `json:"backends,omitempty"`
}

// LoadBalancer status is updated by AKO in the MCI object. It contains the
// VIP fetched from the load balancer and the host fqdn this vip is mapped to
type LoadBalancer struct {
	Ingress []IngressStatus `json:"ingress,omitempty"`
}

// IngressStatus contains the ingress details required for the traffic
type IngressStatus struct {
	Hostname string `json:"hostName,omitempty"`
	IP       string `json:"ip,omitempty"`
}

// BackendObjectStatus contains the backend configuration for a given cluster type.
// For k8s clusters, it contains the cluster, namespace, service and the related
// endpoints
type BackendObjectStatus struct {
	ClusterContext string           `json:"clusterContext,omitempty"`
	Namespace      string           `json:"namespace,omitempty"`
	ServiceName    string           `json:"serviceName,omitempty"`
	Endpoints      []EndpointStatus `json:"endpoints,omitempty"`
}

// EndpointStatus contains the mapping of the ip address to the port number
type EndpointStatus struct {
	IP   string `json:"ip,omitempty"`
	Port int    `json:"port,omitempty"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// ClusterSet is the top-level type
type ClusterSet struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec for MCI Config
	Spec ClusterSetSpec `json:"spec,omitempty"`
	// +optional
	Status ClusterSetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterSetList is a list of GSLBConfig resources
type ClusterSetList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterSet `json:"items"`
}

// ClusterSetSpec has the configuration of the cluster list which form this set.
// It also has the secret which contains the kubeconfig for all the clusters defined
// in this set.
type ClusterSetSpec struct {
	Clusters   []ClusterConfig `json:"clusters,omitempty"`
	SecretName string          `json:"secretName,omitempty"`
}

// ClusterConfig has the contains the cluster context name.
type ClusterConfig struct {
	Context string `json:"context"`
}

// ClusterSetStatus has the status of the clusters
type ClusterSetStatus struct {
	ServiceDiscovery []ServiceDiscoveryStatus `json:"serviceDiscovery"`
}

// ServiceDiscoveryStatus contains the cluster and it's last status: connected or not.
type ServiceDiscoveryStatus struct {
	Cluster string `json:"cluster,omitempty"`
	Status  string `json:"status,omitempty"`
}
