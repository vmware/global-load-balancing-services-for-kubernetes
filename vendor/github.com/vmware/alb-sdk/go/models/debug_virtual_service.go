// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// DebugVirtualService debug virtual service
// swagger:model DebugVirtualService
type DebugVirtualService struct {

	// UNIX time since epoch in microseconds. Units(MICROSECONDS).
	// Read Only: true
	LastModified *string `json:"_last_modified,omitempty"`

	// Placeholder for description of property capture of obj type DebugVirtualService field type str  type boolean
	Capture *bool `json:"capture,omitempty"`

	// Per packet capture filters for Debug Virtual Service. Applies to both frontend and backend packets. Field introduced in 18.2.7.
	CaptureFilters *CaptureFilters `json:"capture_filters,omitempty"`

	// Placeholder for description of property capture_params of obj type DebugVirtualService field type str  type object
	CaptureParams *DebugVirtualServiceCapture `json:"capture_params,omitempty"`

	//  It is a reference to an object of type Cloud.
	CloudRef *string `json:"cloud_ref,omitempty"`

	// This option controls the capture of Health Monitor flows. Enum options - DEBUG_VS_HM_NONE, DEBUG_VS_HM_ONLY, DEBUG_VS_HM_INCLUDE.
	DebugHm *string `json:"debug_hm,omitempty"`

	// Filters all packets of a complete transaction (client and server side), based on client ip.
	DebugIP *DebugIPAddr `json:"debug_ip,omitempty"`

	// Dns debug options. Field introduced in 18.2.1.
	DNSOptions *DebugDNSOptions `json:"dns_options,omitempty"`

	// Placeholder for description of property flags of obj type DebugVirtualService field type str  type object
	Flags []*DebugVsDataplane `json:"flags,omitempty"`

	// Filters for latency audit. Supported only for ingress. Field introduced in 21.1.1.
	LatencyAuditFilters *CaptureFilters `json:"latency_audit_filters,omitempty"`

	// Name of the object.
	// Required: true
	Name *string `json:"name"`

	// Object sync debug options. Field introduced in 20.1.3.
	Objsync *DebugVirtualServiceObjSync `json:"objsync,omitempty"`

	// This option re-synchronizes flows between Active-Standby service engines for all the virtual services placed on them. It should be used with caution because as it can cause a flood between Active-Standby. Field introduced in 18.1.3,18.2.1.
	ResyncFlows *bool `json:"resync_flows,omitempty"`

	// Placeholder for description of property se_params of obj type DebugVirtualService field type str  type object
	SeParams *DebugVirtualServiceSeParams `json:"se_params,omitempty"`

	//  It is a reference to an object of type Tenant.
	TenantRef *string `json:"tenant_ref,omitempty"`

	// url
	// Read Only: true
	URL *string `json:"url,omitempty"`

	// Unique object identifier of the object.
	UUID *string `json:"uuid,omitempty"`
}
