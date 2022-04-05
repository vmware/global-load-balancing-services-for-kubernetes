// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// FalsePositiveDetails false positive details
// swagger:model FalsePositiveDetails
type FalsePositiveDetails struct {

	// false positive result details. Field introduced in 21.1.1.
	FalsePositiveResults []*FalsePositiveResult `json:"false_positive_results,omitempty"`

	// vs id for this false positive details. Field introduced in 21.1.1.
	VsUUID *string `json:"vs_uuid,omitempty"`
}
