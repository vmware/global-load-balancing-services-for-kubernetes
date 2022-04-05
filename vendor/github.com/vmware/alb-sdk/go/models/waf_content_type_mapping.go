// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// WafContentTypeMapping waf content type mapping
// swagger:model WafContentTypeMapping
type WafContentTypeMapping struct {

	// Request Content-Type. When it is equal to request Content-Type header value, the specified request_body_parser is used. Field introduced in 21.1.3.
	// Required: true
	ContentType *string `json:"content_type"`

	// Request body parser. Enum options - WAF_REQUEST_PARSER_URLENCODED, WAF_REQUEST_PARSER_MULTIPART, WAF_REQUEST_PARSER_JSON, WAF_REQUEST_PARSER_XML, WAF_REQUEST_PARSER_HANDLE_AS_STRING, WAF_REQUEST_PARSER_DO_NOT_PARSE. Field introduced in 21.1.3.
	// Required: true
	RequestBodyParser *string `json:"request_body_parser"`
}
