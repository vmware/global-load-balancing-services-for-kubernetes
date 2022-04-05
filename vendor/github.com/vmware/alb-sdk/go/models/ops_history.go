// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// OpsHistory ops history
// swagger:model OpsHistory
type OpsHistory struct {

	// Duration of Upgrade operation in seconds. Field introduced in 20.1.4. Unit is SEC.
	Duration *int32 `json:"duration,omitempty"`

	// End time of Upgrade operation. Field introduced in 20.1.4.
	EndTime *string `json:"end_time,omitempty"`

	// Upgrade operation performed. Enum options - UPGRADE, PATCH, ROLLBACK, ROLLBACKPATCH, SEGROUP_RESUME. Field introduced in 20.1.4.
	Ops *string `json:"ops,omitempty"`

	// Patch after the upgrade operation. . Field introduced in 20.1.4.
	PatchVersion *string `json:"patch_version,omitempty"`

	// ServiceEngineGroup/SE events for upgrade operation. Field introduced in 20.1.4.
	SeUpgradeEvents []*SeUpgradeEvents `json:"se_upgrade_events,omitempty"`

	// SeGroup status for the upgrade operation. Field introduced in 20.1.4.
	SegStatus *SeGroupStatus `json:"seg_status,omitempty"`

	// Start time of Upgrade operation. Field introduced in 20.1.4.
	StartTime *string `json:"start_time,omitempty"`

	// Upgrade operation status. Field introduced in 20.1.4.
	State *UpgradeOpsState `json:"state,omitempty"`

	// Record of Pre/Post snapshot captured for current upgrade operation. It is a reference to an object of type StatediffOperation. Field introduced in 21.1.3.
	StatediffRef *string `json:"statediff_ref,omitempty"`

	// Controller events for Upgrade operation. Field introduced in 20.1.4.
	UpgradeEvents []*EventMap `json:"upgrade_events,omitempty"`

	// Image after the upgrade operation. Field introduced in 20.1.4.
	Version *string `json:"version,omitempty"`
}
