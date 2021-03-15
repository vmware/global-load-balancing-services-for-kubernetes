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

package v1alpha2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// GlobalDeploymentPolicy is the top-level type: Global Deployment Policy
// encloses all the rules, actions and configuration required for deploying
// applications.
type GlobalDeploymentPolicy struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec for GSLB Config
	Spec GDPSpec `json:"spec,omitempty"`
	// +optional
	Status GDPStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GlobalDeploymentPolicyList is a list of GDP resources
type GlobalDeploymentPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GlobalDeploymentPolicy `json:"items"`
}

// GDPSpec encloses all the properties of a GDP object.
type GDPSpec struct {
	MatchRules    MatchRules         `json:"matchRules,omitempty"`
	MatchClusters []ClusterProperty  `json:"matchClusters,omitempty"`
	TrafficSplit  []TrafficSplitElem `json:"trafficSplit,omitempty"`
}

// ClusterProperty specifies all the properties required for a Cluster. Cluster is the cluster
// context name (already added as part of the GSLBConfig object).
// SyncVIPOnly will ask AMKO to sync only the third party vips for this cluster.
type ClusterProperty struct {
	Cluster     string `json:"cluster,omitempty"`
	SyncVipOnly bool   `json:"syncVipOnly,omitempty"`
}

// MatchRules is the match criteria needed to select the kubernetes/openshift objects.
type MatchRules struct {
	AppSelector       `json:"appSelector,omitempty"`
	NamespaceSelector `json:"namespaceSelector,omitempty"`
}

// AppSelector selects the applications based on their labels
type AppSelector struct {
	Label map[string]string `json:"label,omitempty"`
}

// NamespaceSelector selects the applications based on their labels
type NamespaceSelector struct {
	Label map[string]string `json:"label,omitempty"`
}

// Objects on which rules will be applied
const (
	// RouteObj only applies to openshift Routes
	RouteObj = "ROUTE"
	// IngressObj applies to K8S Ingresses
	IngressObj = "INGRESS"
	// LBSvc applies to service type LoadBalancer
	LBSvcObj = "LBSVC"
	// NSObj applies to namespaces
	NSObj = "Namespace"
)

// TrafficSplitElem determines how much traffic to be routed to a cluster.
type TrafficSplitElem struct {
	// Cluster is the cluster context
	Cluster string `json:"cluster,omitempty"`
	Weight  uint32 `json:"weight,omitempty"`
}

// GDPStatus gives the current status of the policy object.
type GDPStatus struct {
	ErrorStatus string `json:"errorStatus,omitempty"`
}
