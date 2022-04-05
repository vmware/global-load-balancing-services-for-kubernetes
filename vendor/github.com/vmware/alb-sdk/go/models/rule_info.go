// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// RuleInfo rule info
// swagger:model RuleInfo
type RuleInfo struct {

	// URI hitted signature rule matches. Field introduced in 21.1.1.
	Matches []*Matches `json:"matches,omitempty"`

	// URI hitted signature rule group id. Field introduced in 21.1.1.
	RuleGroupID *string `json:"rule_group_id,omitempty"`

	// URI hitted signature rule id. Field introduced in 21.1.1.
	RuleID *string `json:"rule_id,omitempty"`
}
