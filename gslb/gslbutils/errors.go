/*
 * Copyright 2021 VMware, Inc.
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

package gslbutils

// Error codes
const (
	ObjectErrStatus     = 1
	ControllerErrStatus = 2
	ResponseParseStatus = 3
	FederatedErrStatus  = 4
)

type ControllerValidationError struct {
	errCode int
	msg     string
}

func (vErr ControllerValidationError) Error() string {
	if vErr.errCode < 5 && vErr.errCode > 0 {
		return vErr.msg
	}
	return "unknown status code"
}

func GetIngestionErrorForObjectNotFound(errMsg string) error {
	return ControllerValidationError{errCode: ObjectErrStatus, msg: errMsg}
}

func GetIngestionErrorForController(errMsg string) error {
	return ControllerValidationError{errCode: ControllerErrStatus, msg: errMsg}
}

func GetIngestionErrorForParsing(errMsg string) error {
	return ControllerValidationError{errCode: ResponseParseStatus, msg: errMsg}
}

func GetIngestionErrorForObjectNotFederated(errMsg string) error {
	return ControllerValidationError{errCode: FederatedErrStatus, msg: errMsg}
}

// IsControllerError returns true only if there was an issue in communicating with the controller.
func IsControllerError(err error) bool {
	vErr, ok := err.(ControllerValidationError)
	if !ok || vErr.errCode != ControllerErrStatus {
		return false
	}
	return true
}

// IsRetriableOnError returns true only if a retry is required
func IsRetriableOnError(err error) bool {
	// For errors other than object not federated, we will retry for everything else
	vErr, ok := err.(ControllerValidationError)
	if !ok {
		return false
	}
	if vErr.errCode == FederatedErrStatus {
		return false
	}
	return true
}
