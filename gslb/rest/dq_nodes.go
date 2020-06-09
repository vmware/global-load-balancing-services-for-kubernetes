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

package rest

import (
	"errors"
	"strings"
	"sync"

	avicache "amko/gslb/cache"

	"amko/gslb/gslbutils"
	"amko/gslb/nodes"

	"github.com/avinetworks/container-lib/utils"
	avimodels "github.com/avinetworks/sdk/go/models"
	"github.com/avinetworks/sdk/go/session"
	"github.com/davecgh/go-spew/spew"
)

const (
	ControllerNotLeaderErr = "Config Operations can be done ONLY on leader"
)

var restLayer *RestOperations
var restOnce sync.Once

type RestOperations struct {
	cache             *avicache.AviCache
	aviRestPoolClient *utils.AviRestClientPool
}

func NewRestOperations(cache *avicache.AviCache, aviRestPoolClient *utils.AviRestClientPool) *RestOperations {
	restOnce.Do(func() {
		restLayer = &RestOperations{cache: cache, aviRestPoolClient: aviRestPoolClient}
	})
	return restLayer
}

func (restOp *RestOperations) DqNodes(key string) {
	gslbutils.Logf("key: %s, msg: starting rest layer sync", key)
	// got the key from graph layer, let's fetch the model
	ok, aviModelIntf := nodes.SharedAviGSGraphLister().Get(key)
	if !ok {
		gslbutils.Logf("key: %s, msg: %s", key, "no model found for the key")
		return
	}

	tenant, gsName := utils.ExtractNamespaceObjectName(key)
	gsKey := avicache.TenantName{Tenant: tenant, Name: gsName}
	gsCacheObj := restOp.getGSCacheObj(gsKey, key)
	if aviModelIntf == nil {
		gslbutils.Errf("key: %s, msg: model found is nil, unexpected error", key)
		return
	}

	aviModel := aviModelIntf.(*nodes.AviGSObjectGraph)
	ct := aviModel.GetRetryCounter()
	if ct <= 0 {
		aviModel.SetRetryCounter()
		gslbutils.Logf("key: %s, msg: retry counter exhausted, resetting counter", key)
		return
	}
	aviModel.DecrementRetryCounter()

	if aviModel.MembersLen() == 0 {
		if gsCacheObj != nil {
			gslbutils.Logf("key: %s, msg: %s", key, "no model found, this is a GS deletion case")
			// Delete case can have two sub-cases:
			// 1. Just delete the IP if present
			// 2. Delete the GS object only if this is the last IP present.
			restOp.deleteGSOper(gsCacheObj, tenant, key)
		}
		return
	}

	gslbutils.Logf("key: %s, msg: GS create/update", key)
	if aviModel == nil {
		gslbutils.Warnf("key: %s, msg: %s", key, "no gslbservice in the model")
		return
	}
	restOp.RestOperation(gsName, tenant, aviModel, gsCacheObj, key)
}

func (restOp *RestOperations) RestOperation(gsName, tenant string, aviGSGraph *nodes.AviGSObjectGraph,
	gsCacheObj *avicache.AviGSCache, key string) {
	gsKey := avicache.TenantName{Tenant: tenant, Name: gsName}
	var operation *utils.RestOp

	if !gslbutils.IsControllerLeader() {
		gslbutils.Errf("key: %s, msg: can't execute rest operation as controller is not a leader", gsKey)
		return
	}
	if gsCacheObj != nil {
		var restOps []*utils.RestOp
		var cksum uint32
		cksum = aviGSGraph.GetChecksum()
		if gsCacheObj.CloudConfigCksum == cksum {
			gslbutils.Logf("key: %s, GSLBService: %s, msg: the checksums are same for the GSLB service, existing: %s, new: %s, ignoring",
				key, gsName, gsCacheObj.CloudConfigCksum, cksum)
			return
		}
		gslbutils.Logf("key: %s, GSLBService: %s, oldCksum: %s, newCksum: %s, msg: %s", key, gsName,
			gsCacheObj.CloudConfigCksum, cksum, "checksums are different for the GSLB Service")
		// it should be a PUT call
		operation = restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key)
		gslbutils.Logf("gsKey: %s, restOps: %v, operation: %v", gsKey, restOps, operation)
	} else {
		// its a post operation
		gslbutils.Logf("key: %s, operation: POST, msg: GS not found in cache", key)
		operation = restOp.AviGSBuild(aviGSGraph, utils.RestPost, nil, key)
	}
	operation.ObjName = aviGSGraph.Name
	restOp.ExecuteRestAndPopulateCache(operation, gsKey, key)
}

func (restOp *RestOperations) ExecuteRestAndPopulateCache(operation *utils.RestOp, gsKey avicache.TenantName, key string) {
	// Choose a AVI client based on the model name hash. This would ensure that the same worker queue processes updates for a
	// given GS everytime.
	bkt := utils.Bkt(key, utils.NumWorkersGraph)
	gslbutils.Logf("key: %s, queue: %d, msg: processing in rest queue", key, bkt)
	if len(restOp.aviRestPoolClient.AviClient) > 0 {
		aviClient := restOp.aviRestPoolClient.AviClient[bkt]
		err := restOp.aviRestPoolClient.AviRestOperate(aviClient, []*utils.RestOp{operation})

		if err != nil {
			gslbutils.Errf("key: %s, msg: rest operation error: %s", key, err)
			webSyncErr, ok := err.(*utils.WebSyncError)
			if !ok {
				gslbutils.Errf("key: %s, msg: %s, err: %v", key, "got an improper web api error, returning", err)
				return
			}
			restOp.PublishKeyToRetryLayer(gsKey, webSyncErr.GetWebAPIError())
			return
		}
		// rest call executed successfully
		gslbutils.Logf("key: %s, msg: rest call executed successfully, will update cache", key)
		if operation.Err == nil && (operation.Method == utils.RestPost || operation.Method == utils.RestPut) {
			if operation.Model == "GSLBService" {
				restOp.AviGSCacheAdd(operation, key)
			} else {
				gslbutils.Errf("key: %s, method: %s, model: %s, msg: invalid model", key, operation.Method,
					operation.Model)
			}
		} else {
			if operation.Model == "GSLBService" {
				// delete call executed
				restOp.AviGSCacheDel(restOp.cache, operation, key)
			}
		}
	}
}

func (restOp *RestOperations) handleErrAndUpdateCache(errCode int, gsKey avicache.TenantName, key string) {
	if len(restOp.aviRestPoolClient.AviClient) <= 0 {
		gslbutils.Errf("invalid avi pool client configuration in restOp, key: %s", key)
		return
	}

	bkt := utils.Bkt(key, utils.NumWorkersGraph)
	aviclient := restOp.aviRestPoolClient.AviClient[bkt]

	switch errCode {
	case 409:
		// case where the object configuration is mis-represented in the in-memory cache
		// first fetch the object and then update it into the cache
		gslbutils.Logf("httpStatus: %d, gsKey: %v, will delete the avi cache key and re-populate", errCode,
			gsKey)
		restOp.cache.AviCacheDelete(gsKey)
		restOp.cache.AviObjGSCachePopulate(aviclient, gsKey.Name)
		return

	case 400, 404:
		// case where the object doesn't exist in Avi, delete that object
		gslbutils.Logf("httpStatus: %d, gsKey: %v, will delete the avi cache key", errCode, gsKey)
		restOp.cache.AviCacheDelete(gsKey)
		return
	}
	gslbutils.Logf("httpStatus: %d, gsKey: %v, unhandled error code, avi cache unchanged")
	return
}

func (restOp *RestOperations) PublishKeyToRetryLayer(gsKey avicache.TenantName, webApiErr error) {
	var bkt uint32
	bkt = 0

	key := gsKey.Tenant + "/" + gsKey.Name

	aviError, ok := webApiErr.(session.AviError)
	if !ok {
		gslbutils.Errf("error in parsing the web api error to avi error: %v", webApiErr)
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		utils.AviLog.Infof("key: %s, msg: Published gskey to slow path retry queue", key)
		return
	}

	utils.AviLog.Infof("key: %s, msg: Status code retrieved: %d", key, aviError.HttpStatusCode)
	switch aviError.HttpStatusCode {
	case 500, 501, 502, 503:
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		utils.AviLog.Infof("key: %s, msg: Published gskey to slow path retry queue", key)

	case 400, 404, 409:
		// check if the message contains: "not a leader"
		// if the controller is not the leader anymore, stop syncing from layer 3.
		if aviError.HttpStatusCode == 400 && strings.Contains(*aviError.Message, ControllerNotLeaderErr) {
			gslbutils.Errf("can't execute operations on a non-leader controller, will wait for it to become a leader in the next full sync")
			gslbutils.SetControllerAsFollower()
			// don't retry
			return
		}
		// however, if this controller is still the leader, we should retry
		// for these error codes, we should first update the cache and put the key back to rest layer
		restOp.handleErrAndUpdateCache(aviError.HttpStatusCode, gsKey, key)
		fastRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.FastRetryQueue)
		fastRetryQueue.Workqueue[bkt].AddRateLimited(key)
		utils.AviLog.Infof("key: %s, msg: Published gskey to fast path retry queue", key)

	default:
		utils.AviLog.Infof("key: %s, msg: unhandled status code %d", key, aviError.HttpStatusCode)
	}
}

func (restOp *RestOperations) AviGSBuild(gsMeta *nodes.AviGSObjectGraph, restMethod utils.RestMethod,
	cacheObj *avicache.AviGSCache, key string) *utils.RestOp {
	gslbutils.Logf("key: %s, msg: creating rest operation", key)
	// description field needs references
	var gslbPoolMembers []*avimodels.GslbPoolMember
	var gslbSvcGroups []*avimodels.GslbPool
	memberObjs := gsMeta.GetUniqueMemberObjs()
	for _, member := range memberObjs {
		if member.IPAddr == "" {
			continue
		}
		enabled := true
		ipVersion := "V4"
		ipAddr := member.IPAddr
		ratio := member.Weight

		gslbPoolMember := avimodels.GslbPoolMember{
			Enabled: &enabled,
			IP:      &avimodels.IPAddr{Addr: &ipAddr, Type: &ipVersion},
			Ratio:   &ratio,
		}
		gslbPoolMembers = append(gslbPoolMembers, &gslbPoolMember)
	}
	// Now, build a GSLB pool
	algorithm := "GSLB_ALGORITHM_ROUND_ROBIN"
	poolEnabled := true
	poolName := gsMeta.Name + "-10"
	priority := int32(10)
	gslbPool := avimodels.GslbPool{
		Algorithm: &algorithm,
		Enabled:   &poolEnabled,
		Members:   gslbPoolMembers,
		Name:      &poolName,
		Priority:  &priority,
	}
	gslbSvcGroups = append(gslbSvcGroups, &gslbPool)

	// Now, build the GSLB service
	ctrlHealthStatusEnabled := true
	createdBy := gslbutils.AmkoUser
	// TODO: description to be appropriately filled
	gsEnabled := true
	healthMonitorScope := "GSLB_SERVICE_HEALTH_MONITOR_ALL_MEMBERS"
	isFederated := true
	minMembers := int32(0)
	gsName := gsMeta.Name
	poolAlgorithm := "GSLB_SERVICE_ALGORITHM_PRIORITY"
	resolveCname := false
	sitePersistenceEnabled := false
	tenantRef := gslbutils.GetAviAdminTenantRef()
	useEdnsClientSubnet := true
	wildcardMatch := false
	description := strings.Join(gsMeta.GetMemberObjList(), ",")

	hmRefs := []string{"/api/healthmonitor/?name=" + gslbutils.GSLBHealthMonitor}

	aviGslbSvc := avimodels.GslbService{
		ControllerHealthStatusEnabled: &ctrlHealthStatusEnabled,
		CreatedBy:                     &createdBy,
		DomainNames:                   gsMeta.DomainNames,
		Enabled:                       &gsEnabled,
		Groups:                        gslbSvcGroups,
		HealthMonitorScope:            &healthMonitorScope,
		IsFederated:                   &isFederated,
		MinMembers:                    &minMembers,
		Name:                          &gsName,
		PoolAlgorithm:                 &poolAlgorithm,
		ResolveCname:                  &resolveCname,
		SitePersistenceEnabled:        &sitePersistenceEnabled,
		UseEdnsClientSubnet:           &useEdnsClientSubnet,
		WildcardMatch:                 &wildcardMatch,
		TenantRef:                     &tenantRef,
		Description:                   &description,
		HealthMonitorRefs:             hmRefs,
	}

	path := "/api/gslbservice/"

	operation := utils.RestOp{ObjName: gsMeta.Name, Path: path, Obj: aviGslbSvc, Tenant: gsMeta.Tenant, Model: "GSLBService",
		Version: gslbutils.GetAviConfig().Version}

	if restMethod == utils.RestPost {
		operation.Method = utils.RestPost
		return &operation
	}
	// Else, its a PUT call
	operation.Path = path + cacheObj.Uuid
	operation.Method = utils.RestPut

	gslbutils.Logf(spew.Sprintf("key: %s, gsMeta: %s, msg: GS rest operation %v\n", key, *gsMeta, utils.Stringify(operation)))
	return &operation
}

func (restOp *RestOperations) getGSCacheObj(gsKey avicache.TenantName, key string) *avicache.AviGSCache {
	gsCache, found := restOp.cache.AviCacheGet(gsKey)
	if found {
		gsCacheObj, ok := gsCache.(*avicache.AviGSCache)
		if !ok {
			gslbutils.Warnf("key: %s, msg: %s", key, "invalid GS object found, ignoring...")
			return nil
		}
		return gsCacheObj
	}
	gslbutils.Logf("key: %s, gsKey: %v, msg: GS cache object not found", key, gsKey)
	return nil
}

func (restOp *RestOperations) AviGSDel(uuid string, tenant string, key string, gsName string) *utils.RestOp {
	path := "/api/gslbservice/" + uuid
	gslbutils.Logf("name of the GS to be deleted from the cache: %s", gsName)
	operation := utils.RestOp{ObjName: gsName, Path: path, Method: "DELETE", Tenant: tenant, Model: "GSLBService",
		Version: gslbutils.GetAviConfig().Version}
	gslbutils.Logf(spew.Sprintf("GSLB Service DELETE Restop %v\n", utils.Stringify(operation)))
	return &operation
}

func (restOp *RestOperations) deleteGSOper(gsCacheObj *avicache.AviGSCache, tenant string, key string) {
	var restOps *utils.RestOp
	bkt := utils.Bkt(key, utils.NumWorkersGraph)
	aviclient := restOp.aviRestPoolClient.AviClient[bkt]
	if !gslbutils.IsControllerLeader() {
		gslbutils.Errf("key: %s, msg: %s", key, "can't execute rest operation, as controller is not a leader")
		return
	}
	if gsCacheObj != nil {
		operation := restOp.AviGSDel(gsCacheObj.Uuid, tenant, key, gsCacheObj.Name)
		restOps = operation
		err := restOp.aviRestPoolClient.AviRestOperate(aviclient, []*utils.RestOp{restOps})
		if err != nil {
			gslbutils.Warnf("key: %s, GSLBService: %s, msg: %s", key, gsCacheObj.Uuid,
				"failed to delete GSLB Service")
			webSyncErr, ok := err.(*utils.WebSyncError)
			if !ok {
				gslbutils.Errf("key: %s, GSLBService: %s, msg: %s", key, gsCacheObj.Uuid, "got an improper web api error, returning")
				return
			}
			gsKey := avicache.TenantName{Tenant: tenant, Name: gsCacheObj.Name}
			restOp.PublishKeyToRetryLayer(gsKey, webSyncErr.GetWebAPIError())
			return
		}
		// Clear all the cache objects which were deleted
		restOp.AviGSCacheDel(restOp.cache, operation, key)
		// delete the GS name from the layer 2 cache here
		gslbutils.Warnf("key: %s, msg: deleting key from the layer 2 cache", key)
		nodes.SharedAviGSGraphLister().Delete(key)
	}
}

func (restOp *RestOperations) AviGSCacheDel(gsCache *avicache.AviCache, op *utils.RestOp, key string) {
	gsKey := avicache.TenantName{Tenant: op.Tenant, Name: op.ObjName}
	gslbutils.Logf("key: %s, gsKey: %v, msg: deleting gs cache", key, gsKey)
	gsCache.AviCacheDelete(gsKey)
}

func (restOp *RestOperations) AviGSCacheAdd(operation *utils.RestOp, key string) error {
	if (operation.Err != nil) || (operation.Response == nil) {
		gslbutils.Warnf("key: %s, response: %s, msg: rest operation has err or no response for GS: %s", key,
			operation.Response, operation.Err)
		return errors.New("rest operation errored")
	}

	respElem, err := RestRespArrToObjByType(operation, "gslbservice", key)
	if err != nil || respElem == nil {
		gslbutils.Warnf("key: %s, resp: %s, msg: unable to find GS object in resp", key, operation.Response)
		return errors.New("GS not found")
	}
	name, ok := respElem["name"].(string)
	if !ok {
		gslbutils.Warnf("key: %s, resp: %s, msg: name not present in response", key, respElem)
		return errors.New("name not present in response")
	}
	uuid, ok := respElem["uuid"].(string)
	if !ok {
		gslbutils.Warnf("key: %s, resp: %s, msg: uuid not present in response", key, respElem)
		return errors.New("uuid not present in response")
	}

	cksum, gsMembers, memberObjs, err := avicache.GetDetailsFromAviGSLB(respElem)
	if err != nil {
		gslbutils.Errf("key: %s, resp: %v, msg: error in getting checksum for gslb svc: %s", key, respElem, err)
	}
	gslbutils.Logf("key: %s, resp: %s, cksum: %d, msg: GS information", key, utils.Stringify(respElem), cksum)
	k := avicache.TenantName{Tenant: operation.Tenant, Name: name}
	gsCache, ok := restOp.cache.AviCacheGet(k)
	if ok {
		gsCacheObj, found := gsCache.(*avicache.AviGSCache)
		if found {
			gsCacheObj.Uuid = uuid
			gsCacheObj.CloudConfigCksum = cksum
			gsCacheObj.Members = gsMembers
			gsCacheObj.K8sObjects = memberObjs
			gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: updated GS cache\n", key, k,
				utils.Stringify(gsCacheObj)))
		} else {
			// New cache object
			gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: GS Cache obj malformed\n"), key, k,
				utils.Stringify(gsCacheObj))
			gsCacheObj := avicache.AviGSCache{
				Name:             name,
				Tenant:           operation.Tenant,
				Uuid:             uuid,
				Members:          gsMembers,
				K8sObjects:       memberObjs,
				CloudConfigCksum: cksum,
			}
			restOp.cache.AviCacheAdd(k, &gsCacheObj)
			gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: added GS to the cache", key, k,
				utils.Stringify(gsCacheObj)))
		}
	} else {
		// New cache object
		gsCacheObj := avicache.AviGSCache{
			Name:             name,
			Tenant:           operation.Tenant,
			Uuid:             uuid,
			Members:          gsMembers,
			K8sObjects:       memberObjs,
			CloudConfigCksum: cksum,
		}
		restOp.cache.AviCacheAdd(k, &gsCacheObj)
		gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: added GS to the cache", key, k,
			utils.Stringify(gsCacheObj)))
	}

	return nil
}

func SyncFromNodesLayer(key string) error {
	cache := avicache.GetAviCache()
	aviclient := avicache.SharedAviClients()
	restLayerF := NewRestOperations(cache, aviclient)
	restLayerF.DqNodes(key)
	return nil
}
