// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// WafCRS waf c r s
// swagger:model WafCRS
type WafCRS struct {

	// UNIX time since epoch in microseconds. Units(MICROSECONDS).
	// Read Only: true
	LastModified *string `json:"_last_modified,omitempty"`

	// Protobuf versioning for config pbs. Field introduced in 21.1.1.
	ConfigpbAttributes *ConfigPbAttributes `json:"configpb_attributes,omitempty"`

	// A short description of this ruleset. Field introduced in 18.1.1.
	// Required: true
	Description *string `json:"description"`

	// WAF Rules are sorted in groups based on their characterization. Field introduced in 18.1.1. Maximum of 64 items allowed.
	Groups []*WafRuleGroup `json:"groups,omitempty"`

	// Integrity protection value. Field introduced in 18.2.1.
	// Required: true
	Integrity *string `json:"integrity"`

	// List of labels to be used for granular RBAC. Field introduced in 20.1.6. Allowed in Basic edition, Essentials edition, Enterprise edition.
	Markers []*RoleFilterMatchLabel `json:"markers,omitempty"`

	// The name of this ruleset object. Field introduced in 18.2.1.
	// Required: true
	Name *string `json:"name"`

	// The release date of this version in RFC 3339 / ISO 8601 format. Field introduced in 18.1.1.
	// Required: true
	ReleaseDate *string `json:"release_date"`

	// Tenant that this object belongs to. It is a reference to an object of type Tenant. Field introduced in 18.2.1.
	TenantRef *string `json:"tenant_ref,omitempty"`

	// url
	// Read Only: true
	URL *string `json:"url,omitempty"`

	//  Field introduced in 18.1.1.
	UUID *string `json:"uuid,omitempty"`

	// The version of this ruleset object. Field introduced in 18.1.1.
	// Required: true
	Version *string `json:"version"`
}
