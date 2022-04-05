// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// ContentRewriteProfile content rewrite profile
// swagger:model ContentRewriteProfile
type ContentRewriteProfile struct {

	// Strings to be matched and replaced with on the request body. This should be configured when request_rewrite_enabled is set to true. This is currently not supported. Field deprecated in 21.1.3.
	ReqMatchReplacePair []*MatchReplacePair `json:"req_match_replace_pair,omitempty"`

	// Enable rewrite on request body. This is not currently supported. Field deprecated in 21.1.3.
	RequestRewriteEnabled *bool `json:"request_rewrite_enabled,omitempty"`

	// Enable rewrite on response body. Field deprecated in 21.1.3.
	ResponseRewriteEnabled *bool `json:"response_rewrite_enabled,omitempty"`

	// Rewrite only content types listed in this *string group. Content types not present in this list are not rewritten. It is a reference to an object of type StringGroup.
	RewritableContentRef *string `json:"rewritable_content_ref,omitempty"`

	// Strings to be matched and replaced with on the response body. This should be configured when response_rewrite_enabled is set to true. Field deprecated in 21.1.3.
	RspMatchReplacePair []*MatchReplacePair `json:"rsp_match_replace_pair,omitempty"`

	// Content Rewrite rules to be enabled on theresponse body. Field introduced in 21.1.3. Maximum of 1 items allowed.
	RspRewriteRules []*RspContentRewriteRule `json:"rsp_rewrite_rules,omitempty"`
}
