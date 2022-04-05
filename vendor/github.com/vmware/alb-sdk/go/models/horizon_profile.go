// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// HorizonProfile horizon profile
// swagger:model HorizonProfile
type HorizonProfile struct {

	// Horizon blast port of the UAG server. Field introduced in 21.1.3.
	BlastPort *int32 `json:"blast_port,omitempty"`

	// Horizon pcoip port of the UAG server. Field introduced in 21.1.3.
	PcoipPort *int32 `json:"pcoip_port,omitempty"`
}
