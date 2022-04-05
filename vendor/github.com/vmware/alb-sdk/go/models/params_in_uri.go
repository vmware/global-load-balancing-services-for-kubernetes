// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// ParamsInURI params in URI
// swagger:model ParamsInURI
type ParamsInURI struct {

	// Params info in hitted signature rule which has ARGS match element. Field introduced in 21.1.1.
	ParamInfo []*ParamInURI `json:"param_info,omitempty"`
}
