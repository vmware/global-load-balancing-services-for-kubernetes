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
package retry

import (
	"amko/gslb/nodes"

	"github.com/avinetworks/container-lib/utils"

	avicache "amko/gslb/cache"
)

func SyncFromRetryLayer(key string) error {
	// Retrieve the Key and note the time.
	utils.AviLog.Info.Printf("Retry layer - Retrieved the key: %s", key)
	tenant, gsName := utils.ExtractNamespaceObjectName(key)
	// Fetch the cache
	aviObjCache := avicache.GetAviCache()

	aviRestClientPool := avicache.SharedAviClients()
	gsKey := avicache.TenantName{Tenant: tenant, Name: gsName}

	// Delete the key from cache and then populate cache for the key from avi object
	aviObjCache.AviCacheDelete(gsKey)
	aviObjCache.AviObjOneGSCachePopulate(aviRestClientPool.AviClient[0], gsName)

	// At this point, we can re-enqueue the key back to the rest layer.
	sharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)

	nodes.PublishKeyToRestLayer(tenant, gsName, "retry", sharedQueue)
	return nil
}
