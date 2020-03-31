/*
* [2013] - [2020] Avi Networks Incorporated
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

package k8sobjects

import (
	gdpv1alpha1 "amko/pkg/apis/avilb/v1alpha1"
)

// Interface for k8s/openshift objects(e.g. route, service, ingress) with minimal information
type MetaObject interface {
	GetType() string
	GetName() string
	GetNamespace() string

	SanityCheck(gdpv1alpha1.MatchRule) bool

	GlobOperate(gdpv1alpha1.MatchRule) bool
	EqualOperate(gdpv1alpha1.MatchRule) bool
	NotEqualOperate(gdpv1alpha1.MatchRule) bool
}
