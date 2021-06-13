/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AMKOClusterSpec defines the desired state of AMKOCluster
type AMKOClusterSpec struct {
	// IsLeader indicates whether this federator is running as part of the leader AMKO instance
	IsLeader bool `json:"isLeader,omitempty"`

	// Current cluster context wherever this AMKO is currently deployed
	ClusterContext string `json:"clusterContext,omitempty"`

	// Version of the AMKO instance
	Version string `json:"version,omitempty"`

	// Clusters contain the list of all clusters where the federation will happen
	Clusters []string `json:"clusters,omitempty"`
}

// AMKOClusterStatus defines the observed state of AMKOCluster
type AMKOClusterStatus struct {
	Conditions []AMKOClusterCondition `json:"conditions,omitempty"`
}

type AMKOClusterCondition struct {
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AMKOCluster is the Schema for the amkoclusters API
type AMKOCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AMKOClusterSpec   `json:"spec,omitempty"`
	Status AMKOClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AMKOClusterList contains a list of AMKOCluster
type AMKOClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AMKOCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AMKOCluster{}, &AMKOClusterList{})
}
