/*
 * Copyright 2019-2020 VMware, Inc.
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

package retry

import (
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

func SyncFromRetryLayer(key interface{}, wg *sync.WaitGroup) error {
	keyStr, ok := key.(string)
	if !ok {
		gslbutils.Errf("unexpected object type: expected string, got %T", key)
		return nil
	}
	// Retrieve the Key and note the time.
	gslbutils.Logf("key: %s, msg: Retrieved the key in Retry layer", key)
	tenant, gsName := utils.ExtractNamespaceObjectName(keyStr)

	// At this point, we re-enqueue the key back to the rest layer.
	sharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)

	nodes.PublishKeyToRestLayer(tenant, gsName, "retry", sharedQueue)
	return nil
}
