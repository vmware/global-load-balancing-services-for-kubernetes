package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// GSLBConfigSpec is the top-level type
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

// GSLBConfig is the GSLB configuration
type GSLBConfigSpec struct {
	GSLBLeader     GSLBLeader      `json:"gslbLeader,omitempty"`
	MemberClusters []MemberCluster `json:"memberClusters,omitempty"`
	GSLBNameSource string          `json:"globalServiceNameSource,omitempty"`
	DomainNames    []string        `json:"domainNames,omitempty"`
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
	State string `json:`
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
