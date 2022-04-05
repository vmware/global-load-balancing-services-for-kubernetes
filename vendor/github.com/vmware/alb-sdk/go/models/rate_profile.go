// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// RateProfile rate profile
// swagger:model RateProfile
type RateProfile struct {

	// Action to perform upon rate limiting.
	// Required: true
	Action *RateLimiterAction `json:"action"`

	// Maximum number of connections or requests or packets to be let through instantaneously. Allowed values are 10-2500. Special values are 0- automatic. Field deprecated in 18.2.9.
	BurstSz *int32 `json:"burst_sz,omitempty"`

	// Maximum number of connections or requests or packets. Allowed values are 1-1000000000. Special values are 0- unlimited. Field deprecated in 18.2.9.
	Count *int32 `json:"count,omitempty"`

	// Explicitly tracks an attacker across rate periods. Allowed in Basic(Allowed values- false) edition, Enterprise edition.
	ExplicitTracking *bool `json:"explicit_tracking,omitempty"`

	// Enable fine granularity. Allowed in Basic(Allowed values- false) edition, Enterprise edition.
	FineGrain *bool `json:"fine_grain,omitempty"`

	// HTTP cookie name. Field introduced in 17.1.1. Allowed in Basic edition, Enterprise edition.
	HTTPCookie *string `json:"http_cookie,omitempty"`

	// HTTP header name. Field introduced in 17.1.1. Allowed in Basic edition, Enterprise edition.
	HTTPHeader *string `json:"http_header,omitempty"`

	// Time value in seconds to enforce rate count. Allowed values are 1-300. Field deprecated in 18.2.9. Unit is SEC.
	Period *int32 `json:"period,omitempty"`

	// The rate limiter configuration for this rate profile. Field introduced in 18.2.9.
	RateLimiter *RateLimiter `json:"rate_limiter,omitempty"`
}
