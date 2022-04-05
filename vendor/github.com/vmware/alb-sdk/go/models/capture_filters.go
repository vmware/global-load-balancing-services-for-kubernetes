// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// CaptureFilters capture filters
// swagger:model CaptureFilters
type CaptureFilters struct {

	// Per packet IP filter. Matches with source and destination address. Curently not applicable for DebugServiceEngine. Field introduced in 18.2.5.
	CaptureIP *DebugIPAddr `json:"capture_ip,omitempty"`

	// Capture filter for SE IPC. Not applicable for Debug Virtual Service. Field introduced in 18.2.5.
	CaptureIpc *CaptureIPC `json:"capture_ipc,omitempty"`

	// Destination Port range filter. Field introduced in 18.2.5.
	DstPortEnd *int32 `json:"dst_port_end,omitempty"`

	// Destination Port range filter. Field introduced in 18.2.5.
	DstPortStart *int32 `json:"dst_port_start,omitempty"`

	// Ethernet Proto filter. Enum options - ETH_TYPE_IPV4. Field introduced in 18.2.5.
	EthProto *string `json:"eth_proto,omitempty"`

	// IP Proto filter. Support for TCP only for now. Enum options - IP_TYPE_TCP. Field introduced in 18.2.5.
	IPProto *string `json:"ip_proto,omitempty"`

	// Source Port filter. Field introduced in 18.2.5.
	SrcPort *int32 `json:"src_port,omitempty"`

	// Source Port range end filter. If specified, the source port filter will be a range. The filter range will be between src_port and src_port_range_end. Field introduced in 21.1.1.
	SrcPortRangeEnd *int32 `json:"src_port_range_end,omitempty"`

	// TCP ACK flag filter. Field introduced in 18.2.5.
	TCPAck *bool `json:"tcp_ack,omitempty"`

	// TCP FIN flag filter. Field introduced in 18.2.5.
	TCPFin *bool `json:"tcp_fin,omitempty"`

	// TCP PUSH flag filter. Field introduced in 18.2.5.
	TCPPush *bool `json:"tcp_push,omitempty"`

	// TCP SYN flag filter. Field introduced in 18.2.5.
	TCPSyn *bool `json:"tcp_syn,omitempty"`
}
