/*
 * Copyright 2019-2020 VMware, Inc.
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

// GSLBConfig is the top-level type
type GSLBConfig struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec for GSLB Config
	Spec GSLBConfigSpec `json:"spec,omitempty"`
	// +optional
	Status GSLBConfigStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GSLBConfigList is a list of GSLBConfig resources
type GSLBConfigList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GSLBConfig `json:"items"`
}

// GSLBConfigSpec is the GSLB configuration
type GSLBConfigSpec struct {
	GSLBLeader          GSLBLeader      `json:"gslbLeader,omitempty"`
	MemberClusters      []MemberCluster `json:"memberClusters,omitempty"`
	RefreshInterval     int             `json:"refreshInterval,omitempty"`
	LogLevel            string          `json:"logLevel,omitempty"`
	UseCustomGlobalFqdn *bool           `json:"useCustomGlobalFqdn,omitempty"`
}

// GSLBLeader is the leader node in the GSLB cluster
type GSLBLeader struct {
	Credentials       string `json:"credentials,omitempty"`
	ControllerVersion string `json:"controllerVersion,omitempty"`
	ControllerIP      string `json:"controllerIP,omitempty"`
}

// MemberCluster defines a GSLB member cluster details
type MemberCluster struct {
	ClusterContext string `json:"clusterContext,omitempty"`
}

// GSLBConfigStatus represents the state and status message of the GSLB cluster
type GSLBConfigStatus struct {
	State string `json:"state,omitempty"`
}

// how the Global services are going to be named
const (
	GSNameType = "HOSTNAME"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GSLBConfigSpecList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GSLBConfigSpec `json:"items"`
}

// TrafficSplitElem determines how much traffic to be routed to a cluster.
type TrafficSplitElem struct {
	// Cluster is the cluster context
	Cluster  string `json:"cluster,omitempty"`
	Weight   uint32 `json:"weight,omitempty"`
	Priority uint32 `json:"priority,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// GSLBHostRule is the top-level type which allows a user to override certain
// fields of a GSLB Service.
type GSLBHostRule struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec for GSLB Config
	Spec GSLBHostRuleSpec `json:"spec,omitempty"`
	// +optional
	Status GSLBHostRuleStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GSLBHostRuleList is a list of GSLBHostRule resources
type GSLBHostRuleList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GSLBHostRule `json:"items"`
}

// GSLBHostRuleSpec defines all the properties of a GSLB Service that can be overriden
// by a user.
type GSLBHostRuleSpec struct {
	// Fqdn is the fqdn of the GSLB Service for which the below properties can be
	// changed.
	Fqdn string `json:"fqdn,omitempty"`
	// TTL is Time To Live in seconds. This tells a DNS resolver how long to hold this DNS
	// record.
	TTL *int `json:"ttl,omitempty"`

	// PoolAlgorithmSettings defines the properties for a Gslb Service pool algorithm
	PoolAlgorithmSettings *PoolAlgorithmSettings `json:"poolAlgorithmSettings,omitempty"`
	// To maintain stickiness to the site where the connection was initiated, the site persistence has
	// to be enabled and a profile ref has to be provided.
	SitePersistence *SitePersistence `json:"sitePersistence,omitempty"`

	// ThirdPartyMembers is a list of third party members site
	ThirdPartyMembers []ThirdPartyMember `json:"thirdPartyMembers,omitempty"`
	// HealthMonitorRefs is a list of custom health monitors which will monitor the
	// GSLB Service's pool members.
	HealthMonitorRefs []string `json:"healthMonitorRefs,omitempty"`
	// HealthMonitorTemplate is a custom Health Monitor template based on which the
	// health monitors will be created.
	HealthMonitorTemplate *string `json:"healthMonitorTemplate,omitempty"`
	// TrafficSplit defines the weightage of traffic that can be routed to each cluster.
	TrafficSplit []TrafficSplitElem `json:"trafficSplit,omitempty"`
}

// PoolAlgorithmSettings define a set of properties to select the Gslb Algorithm for a Gslb
// Service pool. This is to select the appropriate server in a Gslb Service pool.
type PoolAlgorithmSettings struct {
	LBAlgorithm       string       `json:"lbAlgorithm,omitempty"`
	HashMask          *int         `json:"hashMask,omitempty"`
	FallbackAlgorithm *GeoFallback `json:"geoFallback,omitempty"`
}

type GeoFallback struct {
	LBAlgorithm string `json:"lbAlgorithm,omitempty"`
	HashMask    *int   `json:"hashMask,omitempty"`
}

// SitePersistence has the required properties to enable site persistence for a GS.
// If it needs to be enabled, `Enabled` must be set to true, and a persistence profile
// ref has to be specified.
type SitePersistence struct {
	Enabled    bool   `json:"enabled,omitempty"`
	ProfileRef string `json:"profileRef,omitempty"`
}

// GSLBHostRuleStatus contains the current state of the GSLBHostRule resource. If the
// current state is rejected, then an error message is also shown in the Error field.
type GSLBHostRuleStatus struct {
	Error  string `json:"error,omitempty"`
	Status string `json:"status,omitempty"`
}

type ThirdPartyMember struct {
	VIP  string `json:"vip,omitempty"`
	Site string `json:"site,omitempty"`
}

const (
	PoolAlgorithmConsistentHash = "GSLB_ALGORITHM_CONSISTENT_HASH"
	PoolAlgorithmGeo            = "GSLB_ALGORITHM_GEO"
	PoolAlgorithmRoundRobin     = "GSLB_ALGORITHM_ROUND_ROBIN"
	PoolAlgorithmTopology       = "GSLB_ALGORITHM_TOPOLOGY"
)
