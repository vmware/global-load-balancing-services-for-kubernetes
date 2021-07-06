/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"fmt"
	"strings"

	amkov1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/api/v1alpha1"
)

const (
	// Status field types
	CurrentAMKOClusterValidationStatusType = 0
	ClusterContextsStatusType              = 1
	MemberValidationStatusType             = 2
	GSLBConfigFederationStatusType         = 3
	GDPFederationStatusType                = 4

	// Status field type values
	CurrentAMKOClusterValidationStatusField = "current AMKOCluster Validation"
	ClusterContextsStatusField              = "member cluster initialisation"
	MemberValidationStatusField             = "member cluster validation"
	GSLBConfigFederationStatusField         = "GSLBConfig Federation"
	GDPFederationStatusField                = "GDP Federation"

	StatusMsgInvalidAMKOCluster = "invalid AMKOCluster object"
	StatusMsgValidAMKOCluster   = "valid AMKOCluster object"

	StatusMsgClusterClientsInvalid     = "member cluster clients invalid"
	StatusMsgSomeClusterClientsInvalid = "some member cluster clients couldn't be fetched"
	StatusMsgClusterClientsSuccess     = "all cluster clients fetched"

	StatusMemberValidationFailure  = "validation of member clusters failed"
	StatusMembersInvalid           = "error in validating some member clusters"
	StatusMembersValidationSuccess = "validated all member clusters"

	StatusGSLBConfigFederationFailure     = "failure in federation"
	StatusSomeGSLBConfigFederationFailure = "error in federating to some clusters"
	StatusGSLBConfigFederationSuccess     = "federated to all valid clusters successfully"

	StatusGDPFederationFailure     = "failure in federation"
	StatusSomeGDPFederationFailure = "error in federating to some clusters"
	StatusGDPFederationSuccess     = "federated to all valid clusters successfully"

	StatusMsgFederationFailure = "failure in federating objects"
	StatusMsgFederationSuccess = "federation successful"
	StatusMsgNotALeader        = "won't federate objects"

	ErrMembersValidation = "failure in validating some members"
	ErrFederationFailure = "object couldn't be federated to all clusters"
	ErrInitClientContext = "error in initializing member custer context"

	AMKONotALeaderReason = "AMKO not a leader"
)

type StatusMsgRecord struct {
	statusType string
	allFailed  string
	someFailed string
	success    string
}

var statusMsgTypes map[int]*StatusMsgRecord = map[int]*StatusMsgRecord{
	CurrentAMKOClusterValidationStatusType: {
		statusType: CurrentAMKOClusterValidationStatusField,
		allFailed:  StatusMsgInvalidAMKOCluster,
		success:    StatusMsgValidAMKOCluster,
	},

	ClusterContextsStatusType: {
		statusType: ClusterContextsStatusField,
		allFailed:  StatusMsgClusterClientsInvalid,
		someFailed: StatusMsgSomeClusterClientsInvalid,
		success:    StatusMsgClusterClientsSuccess,
	},

	MemberValidationStatusType: {
		statusType: MemberValidationStatusField,
		allFailed:  StatusMsgClusterClientsInvalid,
		someFailed: StatusMembersInvalid,
		success:    StatusMembersValidationSuccess,
	},

	GSLBConfigFederationStatusType: {
		statusType: GSLBConfigFederationStatusField,
		allFailed:  StatusGSLBConfigFederationFailure,
		someFailed: StatusSomeGSLBConfigFederationFailure,
		success:    StatusGSLBConfigFederationSuccess,
	},

	GDPFederationStatusType: {
		statusType: GDPFederationStatusField,
		allFailed:  StatusGDPFederationFailure,
		someFailed: StatusSomeGDPFederationFailure,
		success:    StatusGDPFederationSuccess,
	},
}

func GetClusterErrMsg(errClusters []ClusterErrorMsg) string {
	var errList []string

	for _, m := range errClusters {
		errList = append(errList, m.err.Error())
	}

	return strings.Join(errList, ",")
}

func getStatusCondition(statusType int, statusMsg, reason string,
	errClusters []ClusterErrorMsg) (amkov1alpha1.AMKOClusterCondition, error) {

	statusMsgType, ok := statusMsgTypes[statusType]
	if !ok {
		return amkov1alpha1.AMKOClusterCondition{}, fmt.Errorf("error in identifying error type %d", statusType)
	}
	amkoClusterCondition := amkov1alpha1.AMKOClusterCondition{
		Type:   statusMsgType.statusType,
		Status: statusMsg,
	}

	if statusType == CurrentAMKOClusterValidationStatusType && reason == AMKONotALeaderReason {
		amkoClusterCondition.Reason = reason
		amkoClusterCondition.Status = StatusMsgNotALeader
		return amkoClusterCondition, nil
	}

	if len(errClusters) == 0 {
		// if there are no error clusters and the reason field is empty, then it
		// has to be a success message.
		// the reason will be non-empty only if there's a hard error, where the federator
		// has to stop and retry.
		if reason == "" {
			// success field
			amkoClusterCondition.Status = statusMsgType.success
			return amkoClusterCondition, nil
		}
		amkoClusterCondition.Reason = reason
		amkoClusterCondition.Status = statusMsgType.allFailed
		return amkoClusterCondition, nil
	}
	amkoClusterCondition.Reason = GetClusterErrMsg(errClusters)
	amkoClusterCondition.Status = statusMsgType.someFailed

	return amkoClusterCondition, nil
}

func getErrorMsg(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
