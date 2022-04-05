// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// NetworkService network service
// swagger:model NetworkService
type NetworkService struct {

	// UNIX time since epoch in microseconds. Units(MICROSECONDS).
	// Read Only: true
	LastModified *string `json:"_last_modified,omitempty"`

	//  It is a reference to an object of type Cloud. Field introduced in 18.2.5.
	CloudRef *string `json:"cloud_ref,omitempty"`

	// Protobuf versioning for config pbs. Field introduced in 21.1.1.
	ConfigpbAttributes *ConfigPbAttributes `json:"configpb_attributes,omitempty"`

	// Key value pairs for granular object access control. Also allows for classification and tagging of similar objects. Field deprecated in 20.1.5. Field introduced in 20.1.2. Maximum of 4 items allowed.
	Labels []*KeyValue `json:"labels,omitempty"`

	// List of labels to be used for granular RBAC. Field introduced in 20.1.5. Allowed in Basic edition, Essentials edition, Enterprise edition.
	Markers []*RoleFilterMatchLabel `json:"markers,omitempty"`

	// Name of the NetworkService. Field introduced in 18.2.5.
	// Required: true
	Name *string `json:"name"`

	// Routing Information of the NetworkService. Field introduced in 18.2.5.
	RoutingService *RoutingService `json:"routing_service,omitempty"`

	// Service Engine Group to which the service is applied. It is a reference to an object of type ServiceEngineGroup. Field introduced in 18.2.5.
	// Required: true
	SeGroupRef *string `json:"se_group_ref"`

	// Indicates the type of NetworkService. Enum options - ROUTING_SERVICE. Field introduced in 18.2.5.
	// Required: true
	ServiceType *string `json:"service_type"`

	//  It is a reference to an object of type Tenant. Field introduced in 18.2.5.
	TenantRef *string `json:"tenant_ref,omitempty"`

	// url
	// Read Only: true
	URL *string `json:"url,omitempty"`

	// UUID of the NetworkService. Field introduced in 18.2.5.
	UUID *string `json:"uuid,omitempty"`

	// VRF context to which the service is scoped. It is a reference to an object of type VrfContext. Field introduced in 18.2.5.
	// Required: true
	VrfRef *string `json:"vrf_ref"`
}
