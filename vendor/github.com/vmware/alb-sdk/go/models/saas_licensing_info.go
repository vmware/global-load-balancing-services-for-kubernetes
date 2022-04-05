// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// SaasLicensingInfo saas licensing info
// swagger:model SaasLicensingInfo
type SaasLicensingInfo struct {

	// Maximum service units limit for controller. Allowed values are 0-1000. Special values are 0 - infinite. Field introduced in 21.1.3.
	MaxServiceUnits *float64 `json:"max_service_units,omitempty"`

	// Minimum service units that always remain reserved on controller. Allowed values are 0-1000. Field introduced in 21.1.3.
	ReserveServiceUnits *float64 `json:"reserve_service_units,omitempty"`
}
