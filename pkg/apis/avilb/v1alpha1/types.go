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
	GSLBLeader      GSLBLeader      `json:"gslbLeader,omitempty"`
	MemberClusters  []MemberCluster `json:"memberClusters,omitempty"`
	GSLBNameSource  string          `json:"globalServiceNameSource,omitempty"`
	DomainNames     []string        `json:"domainNames,omitempty"`
	RefreshInterval int             `json:"refreshInterval,omitempty"`
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
	MatchRules     []MatchRule        `json:"matchRules,omitempty"`
	MatchClusters  []MemberCluster    `json:"matchClusters,omitempty"`
	GSLBConfigName string             `json:"gslbConfig,omitempty"`
	TrafficSplit   []TrafficSplitElem `json:"trafficSplit,omitempty"`
	// LBAlgorithm is for determination of a server inside a pool and is different
	// from the algorithm used to determine a pool inside a GSLB service
	LBAlgorithm string `json:"lbAlgorithm,omitempty"`
}

// MatchRule is the match criteria needed to select the kubernetes/openshift objects.
type MatchRule struct {
	Object string `json:"object,omitempty"`
	Hosts  []Host `json:"hosts,omitempty"`
	Label  Label  `json:"label,omitempty"`
	Op     string `json:"op,omitempty"`
}

// Objects on which rules will be applied
const (
	// RouteObj only applies to openshift Routes
	RouteObj = "ROUTE"
	// IngressObj applies to K8S Ingresses
	IngressObj = "INGRESS"
	// LBSvc applies to service type LoadBalancer
	LBSvcObj = "LBSVC"
)

// Host holds the hostname, it could be a glob expression too.
type Host struct {
	HostName string `json:"host,omitempty"`
}

// Label is a Kubernetes/Openshift label with a key-value pair.
type Label struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitmempty"`
}

// Operators required for selecting k8s/openshift objects
const (
	// EqualsOp ensures an exact match
	EqualsOp = "EQUALS"
	// GlobOp ensures a Glob match
	GlobOp = "GLOB"
	// NotequalsOp ensures exact no match
	NotequalsOp = "NOTEQUALS"
)

// TrafficSplitElem determines how much traffic to be routed to a cluster.
type TrafficSplitElem struct {
	// Cluster is the cluster context
	Cluster string `json:"cluster,omitempty"`
	Weight  uint32 `json:"weight,omitempty"`
}

// GDPStatus gives the current status of the policy object.
type GDPStatus struct {
	MatchState  string `json:"matchState,omitempty"`
	ErrorStatus string `json:"errorStatus,omitempty"`
}

// Set of constants for GSLB Load Balancing algorithm, decide the member inside a pool
// to be picked.
// TODO: Support GSLB_ALGORITHM_CONSISTENT_HASH.
const (
	GSLBAlgoRoundRobin = "GSLB_ALGORITHM_ROUND_ROBIN"
	GSLBAlgoGeo        = "GSLB_ALGORITHM_GEO"
	GSLBAlgoTopology   = "GSLB_ALGORITHM_TOPOLOGY"
)
