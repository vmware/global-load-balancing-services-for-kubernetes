// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// AlertSyslogConfig alert syslog config
// swagger:model AlertSyslogConfig
type AlertSyslogConfig struct {

	// UNIX time since epoch in microseconds. Units(MICROSECONDS).
	// Read Only: true
	LastModified *string `json:"_last_modified,omitempty"`

	// Protobuf versioning for config pbs. Field introduced in 21.1.1.
	ConfigpbAttributes *ConfigPbAttributes `json:"configpb_attributes,omitempty"`

	// User defined description for alert syslog config.
	Description *string `json:"description,omitempty"`

	// A user-friendly name of the syslog notification.
	// Required: true
	Name *string `json:"name"`

	// The list of syslog servers.
	SyslogServers []*AlertSyslogServer `json:"syslog_servers,omitempty"`

	//  It is a reference to an object of type Tenant.
	TenantRef *string `json:"tenant_ref,omitempty"`

	// url
	// Read Only: true
	URL *string `json:"url,omitempty"`

	// Unique object identifier of the object.
	UUID *string `json:"uuid,omitempty"`
}
