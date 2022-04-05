// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// ProcessInfo process info
// swagger:model ProcessInfo
type ProcessInfo struct {

	// Current Process ID. Field introduced in 21.1.1.
	CurrentProcessID *float64 `json:"current_process_id,omitempty"`

	// Total memory usage of process in KBs. Field introduced in 21.1.1.
	CurrentProcessMemUsage *float64 `json:"current_process_mem_usage,omitempty"`

	// Number of times the process has been in current ProcessMode. Field introduced in 21.1.1.
	IntimationCount *float64 `json:"intimation_count,omitempty"`

	// Memory limit for process in KBs. Field introduced in 21.1.1.
	MemoryLimit *float64 `json:"memory_limit,omitempty"`

	// Current usage trend of process memory. Enum options - UPWARD, DOWNWARD, NEUTRAL. Field introduced in 21.1.1.
	MemoryTrendUsage *string `json:"memory_trend_usage,omitempty"`

	// Current mode of process. Enum options - REGULAR, DEBUG, DEGRADED, STOP. Field introduced in 21.1.1.
	ProcessMode *string `json:"process_mode,omitempty"`

	// Percentage of memory used out of given limits. Field introduced in 21.1.1.
	ThresholdPercent *float64 `json:"threshold_percent,omitempty"`
}
