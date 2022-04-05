// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// SwitchoverEventDetails switchover event details
// swagger:model SwitchoverEventDetails
type SwitchoverEventDetails struct {

	// from_se_name of SwitchoverEventDetails.
	FromSeName *string `json:"from_se_name,omitempty"`

	// ip of SwitchoverEventDetails.
	IP *string `json:"ip,omitempty"`

	// ip6 of SwitchoverEventDetails.
	Ip6 *string `json:"ip6,omitempty"`

	// Reason for switchover. Field introduced in 21.1.3.
	Reason *string `json:"reason,omitempty"`

	// to_se_name of SwitchoverEventDetails.
	ToSeName *string `json:"to_se_name,omitempty"`

	// vs_name of SwitchoverEventDetails.
	VsName *string `json:"vs_name,omitempty"`

	// Unique object identifier of vs.
	VsUUID *string `json:"vs_uuid,omitempty"`
}
