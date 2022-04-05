// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// BotConfigIPReputation bot config IP reputation
// swagger:model BotConfigIPReputation
type BotConfigIPReputation struct {

	// Whether IP reputation-based Bot detection is enabled. Field introduced in 21.1.1.
	Enabled *bool `json:"enabled,omitempty"`

	// The UUID of the IP reputation DB to use. It is a reference to an object of type IPReputationDB. Field introduced in 21.1.1.
	IPReputationDbRef *string `json:"ip_reputation_db_ref,omitempty"`

	// The system-provided mapping from IP reputation types to Bot types. It is a reference to an object of type BotIPReputationTypeMapping. Field introduced in 21.1.1.
	SystemIPReputationMappingRef *string `json:"system_ip_reputation_mapping_ref,omitempty"`
}
