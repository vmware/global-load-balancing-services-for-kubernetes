// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// CustomIPAMDNSProfile custom ipam Dns profile
// swagger:model CustomIpamDnsProfile
type CustomIPAMDNSProfile struct {

	// UNIX time since epoch in microseconds. Units(MICROSECONDS).
	// Read Only: true
	LastModified *string `json:"_last_modified,omitempty"`

	// Protobuf versioning for config pbs. Field introduced in 21.1.1.
	ConfigpbAttributes *ConfigPbAttributes `json:"configpb_attributes,omitempty"`

	// Name of the Custom IPAM DNS Profile. Field introduced in 17.1.1.
	// Required: true
	Name *string `json:"name"`

	// Parameters that are always passed to the IPAM/DNS script. Field introduced in 17.1.1.
	ScriptParams []*CustomParams `json:"script_params,omitempty"`

	// Script URI of form controller //ipamdnsscripts/<file-name>, file-name must have a .py extension and conform to PEP8 naming convention. Field introduced in 17.1.1.
	// Required: true
	ScriptURI *string `json:"script_uri"`

	//  It is a reference to an object of type Tenant. Field introduced in 17.1.1.
	TenantRef *string `json:"tenant_ref,omitempty"`

	// url
	// Read Only: true
	URL *string `json:"url,omitempty"`

	//  Field introduced in 17.1.1.
	UUID *string `json:"uuid,omitempty"`
}
