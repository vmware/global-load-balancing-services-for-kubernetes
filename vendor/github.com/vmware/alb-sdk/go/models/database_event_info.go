// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// DatabaseEventInfo database event info
// swagger:model DatabaseEventInfo
type DatabaseEventInfo struct {

	// Component of the database(e.g. metrics). Field introduced in 21.1.1.
	Component *string `json:"component,omitempty"`

	// Reported message of the event. Field introduced in 21.1.1.
	Message *string `json:"message,omitempty"`
}
