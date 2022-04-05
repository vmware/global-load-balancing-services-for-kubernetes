// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// BotDetectionPolicy bot detection policy
// swagger:model BotDetectionPolicy
type BotDetectionPolicy struct {

	// UNIX time since epoch in microseconds. Units(MICROSECONDS).
	// Read Only: true
	LastModified *string `json:"_last_modified,omitempty"`

	// Allow the user to skip BotManagement for selected requests. Field introduced in 21.1.1.
	AllowList *BotAllowList `json:"allow_list,omitempty"`

	// Human-readable description of this Bot Detection Policy. Field introduced in 21.1.1.
	Description *string `json:"description,omitempty"`

	// The IP location configuration used in this policy. Field introduced in 21.1.1.
	// Required: true
	IPLocationDetector *BotConfigIPLocation `json:"ip_location_detector"`

	// The IP reputation configuration used in this policy. Field introduced in 21.1.1.
	// Required: true
	IPReputationDetector *BotConfigIPReputation `json:"ip_reputation_detector"`

	// The name of this Bot Detection Policy. Field introduced in 21.1.1.
	// Required: true
	Name *string `json:"name"`

	// System-defined rules for classification. It is a reference to an object of type BotMapping. Field introduced in 21.1.1.
	SystemBotMappingRef *string `json:"system_bot_mapping_ref,omitempty"`

	// The installation provides an updated ruleset for consolidating the results of different decider phases. It is a reference to an object of type BotConfigConsolidator. Field introduced in 21.1.1.
	SystemConsolidatorRef *string `json:"system_consolidator_ref,omitempty"`

	// The unique identifier of the tenant to which this policy belongs. It is a reference to an object of type Tenant. Field introduced in 21.1.1.
	TenantRef *string `json:"tenant_ref,omitempty"`

	// url
	// Read Only: true
	URL *string `json:"url,omitempty"`

	// The User-Agent configuration used in this policy. Field introduced in 21.1.1.
	// Required: true
	UserAgentDetector *BotConfigUserAgent `json:"user_agent_detector"`

	// User-defined rules for classification. These are applied before the system classification rules. If a rule matches, processing terminates and the system-defined rules will not run. It is a reference to an object of type BotMapping. Field introduced in 21.1.1.
	UserBotMappingRef *string `json:"user_bot_mapping_ref,omitempty"`

	// The user-provided ruleset for consolidating the results of different decider phases. This runs before the system consolidator. If it successfully sets a consolidation, the system consolidator will not change it. It is a reference to an object of type BotConfigConsolidator. Field introduced in 21.1.1.
	UserConsolidatorRef *string `json:"user_consolidator_ref,omitempty"`

	// A unique identifier to this Bot Detection Policy. Field introduced in 21.1.1.
	UUID *string `json:"uuid,omitempty"`
}
