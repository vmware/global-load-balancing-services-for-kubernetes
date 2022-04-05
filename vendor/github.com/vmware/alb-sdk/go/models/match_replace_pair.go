// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// MatchReplacePair match replace pair
// swagger:model MatchReplacePair
type MatchReplacePair struct {

	// String to be matched. Field deprecated in 21.1.3.
	MatchString *string `json:"match_string,omitempty"`

	// Replacement string. Field deprecated in 21.1.3.
	ReplacementString *ReplaceStringVar `json:"replacement_string,omitempty"`
}
