// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// NsxtClusters nsxt clusters
// swagger:model NsxtClusters
type NsxtClusters struct {

	// List of transport node clusters. Field introduced in 20.1.6. Allowed in Basic edition, Enterprise edition.
	ClusterIds []string `json:"cluster_ids,omitempty"`

	// Include or Exclude. Field introduced in 20.1.6. Allowed in Basic edition, Enterprise edition.
	Include *bool `json:"include,omitempty"`
}
