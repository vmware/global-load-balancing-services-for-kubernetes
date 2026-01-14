/*
 * Copyright © 2025 Broadcom Inc. and/or its subsidiaries. All Rights Reserved.
 * All Rights Reserved.
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*   http://www.apache.org/licenses/LICENSE-2.0
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*/

package controllers

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	amkov1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/api/v1alpha1"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"
)

// createTestAMKOCluster creates a test AMKOCluster object
func createUnitTestAMKOCluster(name, namespace, version, clusterContext string, isLeader bool) *amkov1alpha1.AMKOCluster {
	return &amkov1alpha1.AMKOCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: amkov1alpha1.AMKOClusterSpec{
			IsLeader:       isLeader,
			ClusterContext: clusterContext,
			Version:        version,
			Clusters:       []string{"cluster1", "cluster2"},
		},
	}
}

// createTestGSLBConfig creates a test GSLBConfig object
func createUnitTestGSLBConfig(name, namespace string) *gslbalphav1.GSLBConfig {
	return &gslbalphav1.GSLBConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gslbalphav1.GSLBConfigSpec{
			GSLBLeader: gslbalphav1.GSLBLeader{
				Credentials:       "test-creds",
				ControllerVersion: "20.1.4",
				ControllerIP:      "10.10.10.10",
			},
			RefreshInterval: 3600,
		},
	}
}

// createTestGDP creates a test GlobalDeploymentPolicy object
func createUnitTestGDP(name, namespace string) *gdpalphav2.GlobalDeploymentPolicy {
	ttl := 300
	return &gdpalphav2.GlobalDeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gdpalphav2.GDPSpec{
			TTL: &ttl,
		},
	}
}

var _ = Describe("AMKOClusterReconciler Unit Tests", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("UpdateAMKOClusterStatus", func() {
		It("should add initial status condition", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			updatedCluster := amkoCluster.DeepCopy()

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
				StatusMsgValidAMKOCluster, "", nil, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(1))
			Expect(updatedCluster.Status.Conditions[0].Type).To(Equal(CurrentAMKOClusterValidationStatusField))
			Expect(updatedCluster.Status.Conditions[0].Status).To(Equal(StatusMsgValidAMKOCluster))
		})

		It("should update existing status condition", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			amkoCluster.Status.Conditions = []amkov1alpha1.AMKOClusterCondition{
				{
					Type:   CurrentAMKOClusterValidationStatusField,
					Status: StatusMsgInvalidAMKOCluster,
					Reason: "test reason",
				},
			}
			updatedCluster := amkoCluster.DeepCopy()

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
				StatusMsgValidAMKOCluster, "", nil, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(1))
			Expect(updatedCluster.Status.Conditions[0].Type).To(Equal(CurrentAMKOClusterValidationStatusField))
			Expect(updatedCluster.Status.Conditions[0].Status).To(Equal(StatusMsgValidAMKOCluster))
		})

		It("should add new condition when different type", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			amkoCluster.Status.Conditions = []amkov1alpha1.AMKOClusterCondition{
				{
					Type:   CurrentAMKOClusterValidationStatusField,
					Status: StatusMsgValidAMKOCluster,
				},
			}
			updatedCluster := amkoCluster.DeepCopy()

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, ClusterContextsStatusType,
				StatusMsgClusterClientsSuccess, "", nil, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(2))
		})

		It("should handle error clusters in status", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			updatedCluster := amkoCluster.DeepCopy()
			errClusters := []ClusterErrorMsg{
				{
					cname: "cluster2",
					err:   errors.New("test error"),
				},
			}

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, ClusterContextsStatusType,
				"", "", errClusters, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(1))
			Expect(updatedCluster.Status.Conditions[0].Type).To(Equal(ClusterContextsStatusField))
			Expect(updatedCluster.Status.Conditions[0].Reason).To(ContainSubstring("test error"))
		})

		It("should handle not a leader status", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", false)
			updatedCluster := amkoCluster.DeepCopy()

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
				StatusMsgNotALeader, AMKONotALeaderReason, nil, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(1))
			Expect(updatedCluster.Status.Conditions[0].Status).To(Equal(StatusMsgNotALeader))
			Expect(updatedCluster.Status.Conditions[0].Reason).To(Equal(AMKONotALeaderReason))
		})

		It("should handle hard error with reason", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			updatedCluster := amkoCluster.DeepCopy()

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
				StatusMsgInvalidAMKOCluster, "version field can't be empty", nil, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(1))
			Expect(updatedCluster.Status.Conditions[0].Status).To(Equal(StatusMsgInvalidAMKOCluster))
			Expect(updatedCluster.Status.Conditions[0].Reason).To(Equal("version field can't be empty"))
		})
	})

	Context("IsObjAMKOClusterType", func() {
		It("should return true for AMKOCluster type", func() {
			result := IsObjAMKOClusterType(ctx, "test-amko-cluster")
			Expect(result).To(BeTrue())
		})

		It("should return false for GC suffix", func() {
			result := IsObjAMKOClusterType(ctx, "test-gc"+GCSuffix)
			Expect(result).To(BeFalse())
		})

		It("should return false for GDP suffix", func() {
			result := IsObjAMKOClusterType(ctx, "test-gdp"+GDPSuffix)
			Expect(result).To(BeFalse())
		})

		It("should return true for name without special suffix", func() {
			result := IsObjAMKOClusterType(ctx, "my-amko-cluster-name")
			Expect(result).To(BeTrue())
		})
	})

	Context("Helper Functions", func() {
		It("should create test AMKOCluster with correct fields", func() {
			cluster := createUnitTestAMKOCluster("test", AviSystemNS, "1.0.0", "cluster1", true)

			Expect(cluster.Name).To(Equal("test"))
			Expect(cluster.Namespace).To(Equal(AviSystemNS))
			Expect(cluster.Spec.Version).To(Equal("1.0.0"))
			Expect(cluster.Spec.ClusterContext).To(Equal("cluster1"))
			Expect(cluster.Spec.IsLeader).To(BeTrue())
			Expect(cluster.Spec.Clusters).To(HaveLen(2))
		})

		It("should create test GSLBConfig with correct fields", func() {
			gc := createUnitTestGSLBConfig("test-gc", AviSystemNS)

			Expect(gc.Name).To(Equal("test-gc"))
			Expect(gc.Namespace).To(Equal(AviSystemNS))
			Expect(gc.Spec.GSLBLeader.ControllerIP).To(Equal("10.10.10.10"))
			Expect(gc.Spec.RefreshInterval).To(Equal(3600))
		})

		It("should create test GDP with correct fields", func() {
			gdp := createUnitTestGDP("test-gdp", AviSystemNS)

			Expect(gdp.Name).To(Equal("test-gdp"))
			Expect(gdp.Namespace).To(Equal(AviSystemNS))
			Expect(*gdp.Spec.TTL).To(Equal(300))
		})
	})

	Context("Status Condition Logic", func() {
		It("should properly format status with multiple conditions", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			updatedCluster := amkoCluster.DeepCopy()

			// Add first condition
			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
				StatusMsgValidAMKOCluster, "", nil, updatedCluster)
			Expect(err).ToNot(HaveOccurred())

			// Add second condition
			err = (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, ClusterContextsStatusType,
				StatusMsgClusterClientsSuccess, "", nil, updatedCluster)
			Expect(err).ToNot(HaveOccurred())

			// Add third condition
			err = (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, MemberValidationStatusType,
				StatusMembersValidationSuccess, "", nil, updatedCluster)
			Expect(err).ToNot(HaveOccurred())

			Expect(updatedCluster.Status.Conditions).To(HaveLen(3))
			Expect(updatedCluster.Status.Conditions[0].Type).To(Equal(CurrentAMKOClusterValidationStatusField))
			Expect(updatedCluster.Status.Conditions[1].Type).To(Equal(ClusterContextsStatusField))
			Expect(updatedCluster.Status.Conditions[2].Type).To(Equal(MemberValidationStatusField))
		})

		It("should update only the specific condition type", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			amkoCluster.Status.Conditions = []amkov1alpha1.AMKOClusterCondition{
				{
					Type:   CurrentAMKOClusterValidationStatusField,
					Status: StatusMsgValidAMKOCluster,
				},
				{
					Type:   ClusterContextsStatusField,
					Status: StatusMsgClusterClientsSuccess,
				},
			}
			updatedCluster := amkoCluster.DeepCopy()

			// Update only the first condition
			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
				StatusMsgInvalidAMKOCluster, "test error", nil, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(2))
			Expect(updatedCluster.Status.Conditions[0].Status).To(Equal(StatusMsgInvalidAMKOCluster))
			Expect(updatedCluster.Status.Conditions[0].Reason).To(Equal("test error"))
			// Second condition should remain unchanged
			Expect(updatedCluster.Status.Conditions[1].Status).To(Equal(StatusMsgClusterClientsSuccess))
		})
	})

	Context("Error Handling", func() {
		It("should handle multiple error clusters", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			updatedCluster := amkoCluster.DeepCopy()
			errClusters := []ClusterErrorMsg{
				{
					cname: "cluster2",
					err:   errors.New("error in cluster2"),
				},
				{
					cname: "cluster3",
					err:   errors.New("error in cluster3"),
				},
			}

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, MemberValidationStatusType,
				"", "", errClusters, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(1))
			Expect(updatedCluster.Status.Conditions[0].Reason).To(ContainSubstring("error in cluster2"))
			Expect(updatedCluster.Status.Conditions[0].Reason).To(ContainSubstring("error in cluster3"))
		})

		It("should handle empty error clusters list", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			updatedCluster := amkoCluster.DeepCopy()
			errClusters := []ClusterErrorMsg{}

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, MemberValidationStatusType,
				"", "", errClusters, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.Conditions).To(HaveLen(1))
			Expect(updatedCluster.Status.Conditions[0].Status).To(Equal(StatusMembersValidationSuccess))
			Expect(updatedCluster.Status.Conditions[0].Reason).To(BeEmpty())
		})
	})

	Context("Status Type Validation", func() {
		It("should handle all valid status types", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)

			statusTypes := []int{
				CurrentAMKOClusterValidationStatusType,
				ClusterContextsStatusType,
				MemberValidationStatusType,
				GSLBConfigFederationStatusType,
				GDPFederationStatusType,
			}

			for _, statusType := range statusTypes {
				updatedCluster := amkoCluster.DeepCopy()
				err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, statusType,
					"", "", nil, updatedCluster)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("should return error for invalid status type", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			updatedCluster := amkoCluster.DeepCopy()

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, 999,
				"", "", nil, updatedCluster)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error in identifying error type"))
		})
	})

	Context("Deep Copy Behavior", func() {
		It("should not modify original cluster when updating status", func() {
			amkoCluster := createUnitTestAMKOCluster("test-amko-cluster", AviSystemNS, "1.0.0", "cluster1", true)
			originalConditionsLen := len(amkoCluster.Status.Conditions)
			updatedCluster := amkoCluster.DeepCopy()

			err := (&AMKOClusterReconciler{}).UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
				StatusMsgValidAMKOCluster, "", nil, updatedCluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(len(amkoCluster.Status.Conditions)).To(Equal(originalConditionsLen))
			Expect(len(updatedCluster.Status.Conditions)).To(Equal(originalConditionsLen + 1))
		})
	})
})
