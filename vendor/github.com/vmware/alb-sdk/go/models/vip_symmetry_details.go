// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// VipSymmetryDetails vip symmetry details
// swagger:model VipSymmetryDetails
type VipSymmetryDetails struct {

	// Maximum number of SEs assigned across all Virtual Services sharing this VIP. Field introduced in 21.1.3.
	MaxNumSeAssigned *int32 `json:"max_num_se_assigned,omitempty"`

	// Maximum number of SEs requested across all Virtual Services sharing this VIP. Field introduced in 21.1.3.
	MaxNumSeRequested *int32 `json:"max_num_se_requested,omitempty"`

	// Minimum number of SEs assigned across all Virtual Services sharing this VIP. Field introduced in 21.1.3.
	MinNumSeAssigned *int32 `json:"min_num_se_assigned,omitempty"`

	// Minimum number of SEs requested across all Virtual Services sharing this VIP. Field introduced in 21.1.3.
	MinNumSeRequested *int32 `json:"min_num_se_requested,omitempty"`

	// Number of Virtual Services sharing VsVip. Field introduced in 21.1.3.
	NumVs *int32 `json:"num_vs,omitempty"`

	// Reason for symmetric/asymmetric shared VIP event. Field introduced in 21.1.3.
	Reason *string `json:"reason,omitempty"`

	// VIP ID. Field introduced in 21.1.3.
	VipID *string `json:"vip_id,omitempty"`

	// VsVip Name. Field introduced in 21.1.3.
	VsvipName *string `json:"vsvip_name,omitempty"`

	// VsVip UUID. Field introduced in 21.1.3.
	VsvipUUID *string `json:"vsvip_uuid,omitempty"`
}
