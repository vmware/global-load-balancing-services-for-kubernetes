// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// ScheduledScaling scheduled scaling
// swagger:model ScheduledScaling
type ScheduledScaling struct {

	// Scheduled autoscale duration (in hours). Allowed values are 1-24. Field introduced in 21.1.1. Unit is HOURS.
	AutoscalingDuration *int32 `json:"autoscaling_duration,omitempty"`

	// The cron expression describing desired time for the scheduled autoscale. Field introduced in 21.1.1.
	CronExpression *string `json:"cron_expression,omitempty"`

	// Desired number of servers during scheduled intervals, it may cause scale-in or scale-out based on the value. Field introduced in 21.1.1.
	DesiredCapacity *int32 `json:"desired_capacity,omitempty"`

	// Enables the scheduled autoscale. Field introduced in 21.1.1.
	Enable *bool `json:"enable,omitempty"`

	// Scheduled autoscale end date in ISO8601 format, said day will be included in scheduled and have to be in future and greater than start date. Field introduced in 21.1.1.
	EndDate *string `json:"end_date,omitempty"`

	// Deprecated.Frequency of the Scheduled autoscale. Enum options - ONCE, EVERY_DAY, EVERY_WEEK, EVERY_MONTH. Field deprecated in 21.1.3. Field introduced in 21.1.1.
	Recurrence *string `json:"recurrence,omitempty"`

	// Maximum number of simultaneous scale-in/out servers for scheduled autoscale. If this value is 0, regular autoscale policy dictates this. . Field introduced in 21.1.1.
	ScheduleMaxStep *int32 `json:"schedule_max_step,omitempty"`

	// Scheduled autoscale start date in ISO8601 format, said day will be included in scheduled and have to be in future. Field introduced in 21.1.1.
	StartDate *string `json:"start_date,omitempty"`
}
