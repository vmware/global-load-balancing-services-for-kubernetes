// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// NetworkSecurityPolicy network security policy
// swagger:model NetworkSecurityPolicy
type NetworkSecurityPolicy struct {

	// UNIX time since epoch in microseconds. Units(MICROSECONDS).
	// Read Only: true
	LastModified *string `json:"_last_modified,omitempty"`

	// Checksum of cloud configuration for Network Sec Policy. Internally set by cloud connector.
	CloudConfigCksum *string `json:"cloud_config_cksum,omitempty"`

	// Protobuf versioning for config pbs. Field introduced in 21.1.1.
	ConfigpbAttributes *ConfigPbAttributes `json:"configpb_attributes,omitempty"`

	// Creator name.
	CreatedBy *string `json:"created_by,omitempty"`

	// User defined description for the object.
	Description *string `json:"description,omitempty"`

	// Geo database. It is a reference to an object of type GeoDB. Field introduced in 21.1.1.
	GeoDbRef *string `json:"geo_db_ref,omitempty"`

	// Network Security Policy is created and modified by internal modules only. Should not be modified by users. Field introduced in 21.1.1.
	Internal *bool `json:"internal,omitempty"`

	// IP reputation database. It is a reference to an object of type IPReputationDB. Field introduced in 20.1.1. Allowed in Basic edition, Essentials edition, Enterprise edition.
	IPReputationDbRef *string `json:"ip_reputation_db_ref,omitempty"`

	// Key value pairs for granular object access control. Also allows for classification and tagging of similar objects. Field deprecated in 20.1.5. Field introduced in 20.1.2. Maximum of 4 items allowed.
	Labels []*KeyValue `json:"labels,omitempty"`

	// List of labels to be used for granular RBAC. Field introduced in 20.1.5. Allowed in Basic edition, Essentials edition, Enterprise edition.
	Markers []*RoleFilterMatchLabel `json:"markers,omitempty"`

	// Name of the object.
	Name *string `json:"name,omitempty"`

	// Placeholder for description of property rules of obj type NetworkSecurityPolicy field type str  type object
	Rules []*NetworkSecurityRule `json:"rules,omitempty"`

	//  It is a reference to an object of type Tenant.
	TenantRef *string `json:"tenant_ref,omitempty"`

	// url
	// Read Only: true
	URL *string `json:"url,omitempty"`

	// Unique object identifier of the object.
	UUID *string `json:"uuid,omitempty"`
}
