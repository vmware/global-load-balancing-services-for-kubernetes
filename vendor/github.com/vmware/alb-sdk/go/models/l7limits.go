// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// L7limits l7limits
// swagger:model L7limits
type L7limits struct {

	// Number of HTTPPolicies attached to a VS. Field introduced in 21.1.1.
	HTTPPoliciesPerVs *int32 `json:"http_policies_per_vs,omitempty"`

	// Number of Compression Filters. Field introduced in 21.1.1.
	NumCompressionFilters *int32 `json:"num_compression_filters,omitempty"`

	// Number of Custom strings per match/action. Field introduced in 21.1.1.
	NumCustomStr *int32 `json:"num_custom_str,omitempty"`

	// Number of Matches per Rule. Field introduced in 21.1.1.
	NumMatchesPerRule *int32 `json:"num_matches_per_rule,omitempty"`

	// Number of rules per HTTPRequest/HTTPResponse/HTTPSecurity Policy. Field introduced in 21.1.1.
	NumRulesPerHTTPPolicy *int32 `json:"num_rules_per_http_policy,omitempty"`

	// Number of Stringgroups/IPgroups per match. Field introduced in 21.1.1.
	NumStrgroupsPerMatch *int32 `json:"num_strgroups_per_match,omitempty"`

	// Number of implicit strings for Cacheable MIME types. Field introduced in 21.1.1.
	StrCacheMime *int32 `json:"str_cache_mime,omitempty"`

	// Number of String groups for Cacheable MIME types. Field introduced in 21.1.1.
	StrGroupsCacheMime *int32 `json:"str_groups_cache_mime,omitempty"`

	// Number of String groups for non Cacheable MIME types. Field introduced in 21.1.1.
	StrGroupsNoCacheMime *int32 `json:"str_groups_no_cache_mime,omitempty"`

	// Number of String groups for non Cacheable URI. Field introduced in 21.1.1.
	StrGroupsNoCacheURI *int32 `json:"str_groups_no_cache_uri,omitempty"`

	// Number of implicit strings for non Cacheable MIME types. Field introduced in 21.1.1.
	StrNoCacheMime *int32 `json:"str_no_cache_mime,omitempty"`

	// Number of implicit strings for non Cacheable URI. Field introduced in 21.1.1.
	StrNoCacheURI *int32 `json:"str_no_cache_uri,omitempty"`
}
