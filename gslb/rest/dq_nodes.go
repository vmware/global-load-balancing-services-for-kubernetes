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
	"time"

	avicache "github.com/avinetworks/amko/gslb/cache"

	"github.com/avinetworks/amko/gslb/gslbutils"
	"github.com/avinetworks/amko/gslb/nodes"

	"github.com/avinetworks/sdk/go/clients"
	avimodels "github.com/avinetworks/sdk/go/models"
	"github.com/avinetworks/sdk/go/session"
	"github.com/davecgh/go-spew/spew"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

const (
	ControllerNotLeaderErr      = "Config Operations can be done ONLY on leader"
	ControllerInMaintenanceMode = "GSLB system is in maintenance mode."
)

var restLayer *RestOperations
var restOnce sync.Once

type RestOperations struct {
	cache             *avicache.AviCache
	hmCache           *avicache.AviHmCache
	aviRestPoolClient *utils.AviRestClientPool
}

func NewRestOperations(cache *avicache.AviCache, hmCache *avicache.AviHmCache, aviRestPoolClient *utils.AviRestClientPool) *RestOperations {
	restOnce.Do(func() {
		restLayer = &RestOperations{cache: cache, hmCache: hmCache, aviRestPoolClient: aviRestPoolClient}
	})
	return restLayer
}

func (restOp *RestOperations) DqNodes(key string) {
	gslbutils.Logf("key: %s, msg: starting rest layer sync", key)
	// got the key from graph layer, let's fetch the model
	// if the key is only in the delete cache, then set deleteOp to true, else false
	deleteOp := false

	var deleteAviModelIntf interface{}
	ok, aviModelIntf := nodes.SharedAviGSGraphLister().Get(key)
	if !ok {
		gslbutils.Logf("key: %s, msg: %s", key, "no model found for the key in the model cache")
		ok, deleteAviModelIntf = nodes.SharedDeleteGSGraphLister().Get(key)
		if !ok || deleteAviModelIntf == nil {
			gslbutils.Logf("key: %s, msg: %s, deleteAviModelIntf: %v", key, "no model found for the key in the delete cache",
				deleteAviModelIntf)
			return
		}
		deleteOp = true
	}

	var aviModel *nodes.AviGSObjectGraph
	if deleteOp {
		aviModel = deleteAviModelIntf.(*nodes.AviGSObjectGraph)
	} else {
		if aviModelIntf == nil {
			gslbutils.Errf("key: %s, msg: aviModelIntf is nil", key)
			return
		}
		aviModel = aviModelIntf.(*nodes.AviGSObjectGraph)
	}

	tenant, gsName := utils.ExtractNamespaceObjectName(key)
	gsKey := avicache.TenantName{Tenant: tenant, Name: gsName}
	gsCacheObj := restOp.getGSCacheObj(gsKey, key)

	ct := aviModel.GetRetryCounter()
	if ct <= 0 {
		aviModel.SetRetryCounter()
		gslbutils.Logf("key: %s, msg: retry counter exhausted, resetting counter", key)
		return
	}
	aviModel.DecrementRetryCounter()

	if deleteOp {
		gslbutils.Logf("key: %s, msg: %s", key, "no model found, will delete the GslbService")
		if gsCacheObj == nil {
			gslbutils.Errf("key: %s, msg: %s", key, "no cache object for this GS was found, can't delete")
			return
		}
		restOp.deleteGSOper(gsCacheObj, tenant, key)
		return
	}

	gslbutils.Logf("key: %s, msg: GslbService will be created/updated", key)
	if aviModel == nil {
		gslbutils.Warnf("key: %s, msg: %s", key, "no model exists for this GslbService")
		return
	}
	restOp.RestOperation(gsName, tenant, aviModel, gsCacheObj, key)
}

func (restOp *RestOperations) createUpdateGSHm(aviGSGraph *nodes.AviGSObjectGraph, gsCacheObj *avicache.AviGSCache, key string,
	gsKey avicache.TenantName) {

	var hmOperation *utils.RestOp
	if aviGSGraph.IsHmTypeCustom() == false {
		gslbutils.Debugf("key: %s, gsKey: %v, msg: default health monitor to be used")
		return
	}
	// HM POST or DELETE-POST (PUT)?
	gslbutils.Debugf(spew.Sprintf("key: %s, gsKey: %s, aviGSGraph: %v, msg: determining a post/put on the hm", key, gsKey, *aviGSGraph))
	hm := restOp.getGSHmCacheObj(aviGSGraph.Name, aviGSGraph.Tenant, key)
	if hm != nil {
		// PUT required?
		hmCksum := aviGSGraph.GetHmChecksum()
		gslbutils.Debugf(spew.Sprintf("key: %s, gsKey: %s, aviGSGraph: %s, hmChecksum: %d, hmCloudConfigChecksum: %d, msg: will check if hm needs to change",
			key, gsKey, *aviGSGraph, hmCksum, hm.CloudConfigCksum))
		if hmCksum != hm.CloudConfigCksum {
			// PUT required
			hmOperation = restOp.AviGsHmBuild(aviGSGraph, utils.RestPut, hm, key)
			restOp.ExecuteRestAndPopulateCache(hmOperation, gsKey, key)
		}
		// no changes required
	} else {
		// POST operation
		gslbutils.Debugf(spew.Sprintf("key: %s, gsKey: %s, aviGSGraph: %v, msg: will build a post call for hm", key, gsKey, *aviGSGraph))
		hmOperation = restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, hm, key)
		restOp.ExecuteRestAndPopulateCache(hmOperation, gsKey, key)
	}
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
		if aviGSGraph.IsHmTypeCustom() {
			hm := restOp.getGSHmCacheObj(aviGSGraph.Name, aviGSGraph.Tenant, key)
			if hm != nil {
				hmCksum := aviGSGraph.GetHmChecksum()
				gslbutils.Debugf(spew.Sprintf("key: %s, gsKey: %s, aviGSGraph: %v, hmChecksum: %d, hmCloudConfigChecksum: %d, msg: will check if hm needs to change",
					key, gsKey, *aviGSGraph, hmCksum, hm.CloudConfigCksum))
				if hm.CloudConfigCksum != hmCksum {
					// update gs, delete hm, create new hm and update gs
					op := restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key, false)
					restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
					if op.Err != nil {
						gslbutils.Debugf("key: %s, gsKey: %s, msg: error in rest operation: %v", key, gsKey, op)
						return
					}
					op = restOp.AviGsHmDel(hm.UUID, utils.ADMIN_NS, key, hm.Name)
					restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
					if op.Err != nil {
						gslbutils.Debugf("key: %s, gsKey: %s, error in rest operation: %v", key, gsKey, op)
						return
					}
					op = restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key)
					restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
					if op.Err != nil {
						gslbutils.Debugf("key: %s, gsKey: %s, error in rest operation: %v", key, gsKey, op)
						return
					}
					op = restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key, true)
					restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
					if op.Err != nil {
						gslbutils.Debugf("key: %s, gsKey: %s, error in rest operation: %v", key, gsKey, op)
						return
					}
				}
			} else {
				// HM needs to be created
				op := restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key)
				restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
				if op.Err != nil {
					gslbutils.Debugf("key: %s, gsKey: %s, error in rest operation: %v", key, gsKey, op)
					return
				}
				op = restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key, true)

				restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
				if op.Err != nil {
					gslbutils.Debugf("key: %s, gsKey: %s, error in rest operation: %v", key, gsKey, op)
					return
				}
			}
		}

		cksum = aviGSGraph.GetChecksum()
		// check if the GS needs an update
		if gsCacheObj.CloudConfigCksum == cksum {
			gslbutils.Debugf("key: %s, GSLBService: %s, msg: the checksums are same for the GSLB service, existing: %d, new: %d, ignoring",
				key, gsName, gsCacheObj.CloudConfigCksum, cksum)
			return
		}
		gslbutils.Debugf("key: %s, GSLBService: %s, oldCksum: %d, newCksum: %d, msg: %s", key, gsName,
			gsCacheObj.CloudConfigCksum, cksum, "checksums are different for the GSLB Service")
		// it should be a PUT call
		operation = restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key, true)
		gslbutils.Debugf(spew.Sprintf("gsKey: %s, restOps: %v, operation: %v", gsKey, restOps, operation))
	} else {
		// its a post operation
		// first see if we need a new health monitor
		hm := restOp.getGSHmCacheObj(aviGSGraph.Name, aviGSGraph.Tenant, key)
		if aviGSGraph.IsHmTypeCustom() {
			if hm == nil && aviGSGraph.IsHmTypeCustom() {
				// create a new health monitor
				op := restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key)
				restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
				if op.Err != nil {
					gslbutils.Debugf("key: %s, gsKey: %s, error in rest operation: %v")
					return
				}
				gslbutils.Debugf("key: %s, gsKey: %s, msg: no new hm required")
			} else {
				// a health monitor already exists, see if we need to re-create it
				hmCksum := aviGSGraph.GetHmChecksum()
				gslbutils.Debugf(spew.Sprintf("key: %s, gsKey: %s, aviGSGraph: %s, hmChecksum: %d, hmCloudConfigChecksum: %d, msg: will check if hm needs to change",
					key, gsKey, *aviGSGraph, hmCksum, hm.CloudConfigCksum))
				if hm.CloudConfigCksum != hmCksum {
					// delete hm, create new hm and update gs
					op := restOp.AviGsHmDel(hm.UUID, utils.ADMIN_NS, key, hm.Name)
					restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
					if op.Err != nil {
						gslbutils.Debugf("key: %s, gsKey: %s, error in rest operation: %v", key, gsKey, op)
						return
					}
					op = restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key)
					restOp.ExecuteRestAndPopulateCache(op, gsKey, key)
					if op.Err != nil {
						gslbutils.Debugf("key: %s, gsKey: %s, error in rest operation: %v", key, gsKey, op)
						return
					}
				}
			}
		}
		gslbutils.Logf("key: %s, operation: POST, msg: GS not found in cache", key)
		operation = restOp.AviGSBuild(aviGSGraph, utils.RestPost, nil, key, true)
	}
	operation.ObjName = aviGSGraph.Name
	restOp.ExecuteRestAndPopulateCache(operation, gsKey, key)
}

func AviRestOperateWrapper(restOp *RestOperations, aviClient *clients.AviClient, operation *utils.RestOp) error {
	restTimeoutChan := make(chan error, 1)

	go func() {
		err := restOp.aviRestPoolClient.AviRestOperate(aviClient, []*utils.RestOp{operation})
		restTimeoutChan <- err
	}()

	select {
	case err := <-restTimeoutChan:
		return err
	case <-time.After(gslbutils.RestTimeoutSecs * time.Second):
		gslbutils.Errf(spew.Sprintf("operation: %v, err: rest timeout occured", operation))
		return errors.New("rest timeout occured")
	}
}

func (restOp *RestOperations) ExecuteRestAndPopulateCache(operation *utils.RestOp, gsKey avicache.TenantName, key string) {
	// Choose a AVI client based on the model name hash. This would ensure that the same worker queue processes updates for a
	// given GS everytime.
	bkt := utils.Bkt(key, gslbutils.NumRestWorkers)
	gslbutils.Logf("key: %s, queue: %d, msg: processing in rest queue", key, bkt)

	if len(restOp.aviRestPoolClient.AviClient) > 0 {
		aviClient := restOp.aviRestPoolClient.AviClient[bkt]
		err := AviRestOperateWrapper(restOp, aviClient, operation)
		gslbutils.Debugf("key: %s, queue: %d, msg: avi rest operate wrapper response, %v", key, bkt, err)
		if err != nil {
			gslbutils.Errf("key: %s, queue: %d, msg: rest operation error: %s", key, bkt, err)
			if err.Error() == "rest timeout occured" {
				gslbutils.Errf("key: %s, queue: %d, msg: got a rest timeout", key, bkt)
				restOp.PublishKeyToRetryLayer(gsKey, err)
				return
			}
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
			switch operation.Model {
			case "HealthMonitor":
				restOp.AviGSHmCacheAdd(operation, key)
			case "GSLBService":
				restOp.AviGSCacheAdd(operation, key)
			default:
				gslbutils.Errf("key: %s, method: %s, model: %s, msg: invalid model", key, operation.Method,
					operation.Model)
			}
		} else {
			switch operation.Model {
			case "HealthMonitor":
				restOp.AviGSHmCacheDel(restOp.hmCache, operation, key)
			case "GSLBService":
				restOp.AviGSCacheDel(restOp.cache, operation, key)
			default:
				gslbutils.Errf("key: %s, method: %s, model: %s, msg: invalid model", key, operation.Method,
					operation.Model)
			}
		}
	}
}

func (restOp *RestOperations) handleErrAndUpdateCache(errCode int, gsKey avicache.TenantName, key string) {
	if len(restOp.aviRestPoolClient.AviClient) <= 0 {
		gslbutils.Errf("invalid avi pool client configuration in restOp, key: %s", key)
		return
	}

	bkt := utils.Bkt(key, gslbutils.NumRestWorkers)
	gslbutils.Logf("key: %s, queue: %s, msg: handling error and updating cache", key, bkt)
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

func setRetryCounterForGraph(key string) error {
	ok, aviModelIntf := nodes.SharedAviGSGraphLister().Get(key)
	if !ok {
		gslbutils.Warnf("key: %s, msg: %s", key, "no model found for this key in SharedAviGSGraphLister")
		// check in the delete cache
		ok, aviModelIntf = nodes.SharedDeleteGSGraphLister().Get(key)
		if !ok {
			gslbutils.Warnf("key: %s, msg: %s", key, "no model found for this key in SharedDeleteGSGraphLister")
			return errors.New("no model for this key")
		}
	}
	aviModel, ok := aviModelIntf.(*nodes.AviGSObjectGraph)
	if !ok {
		gslbutils.Errf("key: %s, msg: %s", key, "model malformed for this key")
		return errors.New("model malformed for this key")
	}
	aviModel.SetRetryCounter()
	return nil
}

func (restOp *RestOperations) PublishKeyToRetryLayer(gsKey avicache.TenantName, webApiErr error) {
	var bkt uint32
	bkt = 0

	key := gsKey.Tenant + "/" + gsKey.Name

	if webApiErr.Error() == "rest timeout occured" {
		gslbutils.Errf("gsKey: %v, msg: timeout occured while doing rest call", gsKey)
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published gskey to slow path retry queue", key)
		return
	}
	aviError, ok := webApiErr.(session.AviError)
	if !ok {
		gslbutils.Errf("error in parsing the web api error to avi error: %v", webApiErr)
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published gskey to slow path retry queue", key)
		return
	}

	gslbutils.Logf("key: %s, msg: Status code retrieved: %d", key, aviError.HttpStatusCode)
	switch aviError.HttpStatusCode {
	case 500, 501, 502, 503:
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published gskey to slow path retry queue", key)

	case 400:
		// check if the message contains: "not a leader"
		// if the controller is not the leader anymore, stop syncing from layer 3.
		if strings.Contains(*aviError.Message, ControllerNotLeaderErr) {
			gslbutils.Errf("can't execute operations on a non-leader controller, will wait for it to become a leader in the next full sync")
			gslbutils.SetControllerAsFollower()
			// don't retry
			return
		}
		if strings.Contains(*aviError.Message, ControllerInMaintenanceMode) {
			gslbutils.Errf("can't execute rest operations on a leader in maintenance mode, will retry")
			// will retry indefinitely for this error, so reset the retryCounter
			err := setRetryCounterForGraph(key)
			if err != nil {
				gslbutils.Errf("can't set the retry counter for this key, will re-sync in the next full sync")
				gslbutils.SetResyncRequired(true)
				return
			}
			// else, publish the key to slowRetryQueue
			slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
			slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
			gslbutils.Logf("key: %s, msg: Published gskey to slow path retry queue", key)
			return
		}
		gslbutils.Errf("can't handle error code 400: %s, won't retry", *aviError.Message)

	case 404, 409:
		// however, if this controller is still the leader, we should retry
		// for these error codes, we should first update the cache and put the key back to rest layer
		restOp.handleErrAndUpdateCache(aviError.HttpStatusCode, gsKey, key)
		fastRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.FastRetryQueue)
		fastRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published gskey to fast path retry queue", key)

	default:
		gslbutils.Warnf("key: %s, msg: unhandled status code %d", key, aviError.HttpStatusCode)
		gslbutils.SetResyncRequired(true)
	}
}

func (restOp *RestOperations) AviGsHmBuild(gsMeta *nodes.AviGSObjectGraph, restMethod utils.RestMethod,
	hmCacheObj *avicache.AviHmObj, key string) *utils.RestOp {
	gslbutils.Logf("key: %s, msg: creating rest operation for health monitor")
	monitorPort := gsMeta.Hm.Port
	hmName := gsMeta.Hm.Name
	hmProto := gsMeta.Hm.Protocol
	isFederated := true
	sendInterval := int32(10)
	description := "Custom GSLB health monitor created by AMKO"
	tenantRef := gslbutils.GetAviAdminTenantRef()

	aviGsHm := avimodels.HealthMonitor{
		IsFederated:  &isFederated,
		MonitorPort:  &monitorPort,
		Name:         &hmName,
		SendInterval: &sendInterval,
		Type:         &hmProto,
		Description:  &description,
		TenantRef:    &tenantRef,
	}

	if hmProto == gslbutils.SystemHealthMonitorTypeUDP {
		udpRequest := ""
		hmUDP := avimodels.HealthMonitorUDP{
			UDPRequest: &udpRequest,
		}
		aviGsHm.UDPMonitor = &hmUDP
	} else {
		tcpHalfOpen := false
		hmTCP := avimodels.HealthMonitorTCP{
			TCPHalfOpen: &tcpHalfOpen,
		}
		aviGsHm.TCPMonitor = &hmTCP
	}

	path := "/api/healthmonitor"

	operation := utils.RestOp{ObjName: gsMeta.Name, Path: path, Obj: aviGsHm, Tenant: gsMeta.Tenant, Model: "HealthMonitor",
		Version: gslbutils.GetAviConfig().Version}

	if restMethod == utils.RestPost {
		operation.Method = utils.RestPost
		gslbutils.Debugf(spew.Sprintf("key: %s, hmModel: %s, msg: GS rest operation %v\n", key, gsMeta.Hm, utils.Stringify(operation)))
		return &operation
	}
	operation.Path = path + hmCacheObj.UUID
	operation.Method = utils.RestPut
	gslbutils.Debugf(spew.Sprintf("key: %s, hmModel: %s, msg: GS rest operation %v\n", key, gsMeta.Hm, utils.Stringify(operation)))
	return &operation
}

func (restOp *RestOperations) AviGSBuild(gsMeta *nodes.AviGSObjectGraph, restMethod utils.RestMethod,
	cacheObj *avicache.AviGSCache, key string, hmRequired bool) *utils.RestOp {
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
	}

	if hmRequired {
		if gsMeta.Hm.Name == "" {
			gslbutils.Errf("gs %s doesn't have a health monitor", gsMeta.Name)
		}
		// Assign a health monitor, if present
		aviGslbSvc.HealthMonitorRefs = []string{"/api/healthmonitor?name=" + gsMeta.Hm.Name}
	}

	path := "/api/gslbservice/"

	operation := utils.RestOp{ObjName: gsMeta.Name, Path: path, Obj: aviGslbSvc, Tenant: gsMeta.Tenant, Model: "GSLBService",
		Version: gslbutils.GetAviConfig().Version}

	if restMethod == utils.RestPost {
		operation.Method = utils.RestPost
		gslbutils.Debugf(spew.Sprintf("key: %s, gsMeta: %v, msg: GS rest operation %v\n", key, *gsMeta, utils.Stringify(operation)))
		return &operation
	}
	// Else, its a PUT call
	operation.Path = path + cacheObj.Uuid
	operation.Method = utils.RestPut

	gslbutils.Debugf(spew.Sprintf("key: %s, gsMeta: %v, msg: GS rest operation %v\n", key, *gsMeta, utils.Stringify(operation)))
	return &operation
}

func (restOp *RestOperations) getGSHmCacheObj(gsName, tenant string, key string) *avicache.AviHmObj {
	hmKey := avicache.TenantName{Tenant: tenant, Name: "amko-hm-" + gsName}
	hmObjIntf, found := restOp.hmCache.AviHmCacheGet(hmKey)
	if found {
		hmObj, ok := hmObjIntf.(*avicache.AviHmObj)
		if !ok {
			gslbutils.Warnf("key: %s, msg: %s", key, "invalid Health monitor object found, ignoring...")
			return nil
		}
		return hmObj
	}
	gslbutils.Logf("key: %s, hmKey: %v, msg: HM cache object not found", key, hmKey)
	return nil
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
	gslbutils.Debugf(spew.Sprintf("GSLB Service DELETE Restop %v\n", utils.Stringify(operation)))
	return &operation
}

func (restOp *RestOperations) AviGsHmDel(uuid string, tenant string, key string, hmName string) *utils.RestOp {
	path := "/api/healthmonitor/" + uuid
	gslbutils.Logf("name of HM to be deleted from the cache: %s", hmName)
	operation := utils.RestOp{ObjName: hmName, Path: path, Method: "DELETE", Tenant: tenant, Model: "HealthMonitor",
		Version: gslbutils.GetAviConfig().Version}
	gslbutils.Debugf(spew.Sprintf("Health Monitor DELETE Restop %s\n", utils.Stringify(operation)))
	return &operation
}

func (restOp *RestOperations) deleteGSOper(gsCacheObj *avicache.AviGSCache, tenant string, key string) {
	var restOps *utils.RestOp
	bkt := utils.Bkt(key, gslbutils.NumRestWorkers)
	gslbutils.Logf("key: %s, queue: %d, msg: deleting GS object", key, bkt)
	aviclient := restOp.aviRestPoolClient.AviClient[bkt]
	if !gslbutils.IsControllerLeader() {
		gslbutils.Errf("key: %s, msg: %s", key, "can't execute rest operation, as controller is not a leader")
		gslbutils.UpdateGSLBConfigStatus(ControllerNotLeaderErr)
		return
	}
	gsName := gsCacheObj.Name
	gsKey := avicache.TenantName{Tenant: tenant, Name: gsCacheObj.Name}
	if gsCacheObj != nil {
		operation := restOp.AviGSDel(gsCacheObj.Uuid, tenant, key, gsCacheObj.Name)
		restOps = operation
		err := AviRestOperateWrapper(restOp, aviclient, restOps)
		gslbutils.Debugf("key: %s, GSLBService: %s, msg: avi rest operate wrapper response %v", key, gsCacheObj.Uuid, err)
		if err != nil {
			gslbutils.Errf("key: %s, GSLBService: %s, msg: %s", key, gsCacheObj.Uuid,
				"failed to delete GSLB Service")
			if err.Error() == "rest timeout occured" {
				gslbutils.Errf("key: %s, queue: %d, msg: got a rest timeout", key, bkt)
				restOp.PublishKeyToRetryLayer(gsKey, err)
				return
			}
			webSyncErr, ok := err.(*utils.WebSyncError)
			if !ok {
				gslbutils.Errf("key: %s, GSLBService: %s, msg: %s", key, gsCacheObj.Uuid, "got an improper web api error, returning")
				return
			}
			restOp.PublishKeyToRetryLayer(gsKey, webSyncErr.GetWebAPIError())
			return
		}
		// Clear all the cache objects which were deleted
		restOp.AviGSCacheDel(restOp.cache, operation, key)

		// delete the health monitor associated with this GS object
		// We are only deleting the Health monitor objects which were created by AMKO and not others.
		hmName := "amko-hm-" + gsName
		hmCacheObjIntf, found := restOp.hmCache.AviHmCacheGet(avicache.TenantName{Tenant: utils.ADMIN_NS, Name: hmName})
		if found {
			// health monitor found for this object, deleting
			hmCacheObj, ok := hmCacheObjIntf.(*avicache.AviHmObj)
			if ok {
				operation := restOp.AviGsHmDel(hmCacheObj.UUID, hmCacheObj.Tenant, key, hmCacheObj.Name)
				restOps = operation
				err := AviRestOperateWrapper(restOp, aviclient, restOps)
				if err != nil {
					gslbutils.Warnf("key: %s, HealthMonitor: %s, msg: %s", key, hmName, "failed to delete")
					if err.Error() == "rest timeout occured" {
						gslbutils.Errf("key: %s, queue: %d, msg: got a rest timeout", key, bkt)
						restOp.PublishKeyToRetryLayer(gsKey, err)
						return
					}

					webSyncErr, ok := err.(*utils.WebSyncError)
					if !ok {
						gslbutils.Errf("key: %s, GSLBService: %s, msg: %s", key, hmName, "got an improper web api error, returning")
						return
					}
					gsKey := avicache.TenantName{Tenant: tenant, Name: gsCacheObj.Name}
					restOp.PublishKeyToRetryLayer(gsKey, webSyncErr.GetWebAPIError())
					return
				}
				restOp.AviGSHmCacheDel(restOp.hmCache, operation, key)
			} else {
				gslbutils.Warnf("health monitor object %v malformed, can't delete", hmCacheObj)
			}
		}
		gslbutils.Warnf("key: %s, msg: deleting key from layer 2 delete cache", key)
		nodes.SharedDeleteGSGraphLister().Delete(key)
	}
}

func (restOp *RestOperations) AviGSHmCacheDel(hmCache *avicache.AviHmCache, op *utils.RestOp, key string) {
	hmKey := avicache.TenantName{Tenant: op.Tenant, Name: op.ObjName}
	gslbutils.Logf("key: %s, hmKey: %v, msg: deleting from gs hm cache", key, hmKey)
	hmCache.AviHmCacheDelete(hmKey)
}

func (restOp *RestOperations) AviGSCacheDel(gsCache *avicache.AviCache, op *utils.RestOp, key string) {
	gsKey := avicache.TenantName{Tenant: op.Tenant, Name: op.ObjName}
	gslbutils.Logf("key: %s, gsKey: %v, msg: deleting from gs cache", key, gsKey)
	gsCache.AviCacheDelete(gsKey)
}

func (restOp *RestOperations) AviGSHmCacheAdd(operation *utils.RestOp, key string) error {
	if (operation.Err != nil) || (operation.Response == nil) {
		gslbutils.Warnf("key: %s, response: %s, msg: rest operation has err or no response for health monitor: %s", key,
			operation.Response, operation.Err)
		return errors.New("rest operation errored")
	}

	respElem, err := RestRespArrToObjByType(operation, "healthmonitor", key)
	if err != nil || respElem == nil {
		gslbutils.Warnf("key: %s, resp: %s, msg: unable to find health monitor object in resp", key, operation.Response)
		return errors.New("health monitor not found")
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
	hmType, ok := respElem["type"].(string)
	if !ok {
		gslbutils.Warnf("key: %s, resp: %s, msg: type not present in response", key, respElem)
		return errors.New("type not present in response")
	}
	portF, ok := respElem["monitor_port"].(float64)
	if !ok {
		gslbutils.Warnf("key: %s, resp: %s, msg: monitor_port not present in response", key, respElem)
		return errors.New("monitor port not present in response")
	}
	port := int32(portF)

	cksum := gslbutils.GetGSLBHmChecksum(name, hmType, port)
	k := avicache.TenantName{Tenant: operation.Tenant, Name: name}
	addNew := false
	hmCache, ok := restOp.hmCache.AviHmCacheGet(k)
	if ok {
		// hm exists, just update it
		hmCacheObj, found := hmCache.(*avicache.AviHmObj)
		if found {
			hmCacheObj.UUID = uuid
			hmCacheObj.CloudConfigCksum = cksum
			hmCacheObj.Name = name
			hmCacheObj.Type = hmType
			hmCacheObj.Port = port
			gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: updated HM cache\n", key, k,
				utils.Stringify(hmCacheObj)))
		} else {
			// new cache object should be added
			gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: HM cache obj malformed\n", key, k,
				utils.Stringify(hmCacheObj)))
			addNew = true
		}
	} else {
		addNew = true
	}

	if addNew {
		hmCacheObj := avicache.AviHmObj{
			Name:             name,
			Tenant:           operation.Tenant,
			UUID:             uuid,
			Type:             hmType,
			Port:             port,
			CloudConfigCksum: cksum,
		}
		restOp.hmCache.AviHmCacheAdd(k, &hmCacheObj)
		gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: added HM to the cache", key, k,
			utils.Stringify(hmCacheObj)))
	}

	return nil
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

	cksum, gsMembers, memberObjs, hm, err := avicache.GetDetailsFromAviGSLB(respElem)
	if err != nil {
		gslbutils.Errf("key: %s, resp: %v, msg: error in getting checksum for gslb svc: %s", key, respElem, err)
	}
	gslbutils.Debugf("key: %s, resp: %s, cksum: %d, msg: GS information", key, utils.Stringify(respElem), cksum)
	k := avicache.TenantName{Tenant: operation.Tenant, Name: name}
	gsCache, ok := restOp.cache.AviCacheGet(k)
	if ok {
		gsCacheObj, found := gsCache.(*avicache.AviGSCache)
		if found {
			gsCacheObj.Uuid = uuid
			gsCacheObj.CloudConfigCksum = cksum
			gsCacheObj.Members = gsMembers
			gsCacheObj.K8sObjects = memberObjs
			gsCacheObj.HealthMonitorName = hm
			gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: updated GS cache\n", key, k,
				utils.Stringify(gsCacheObj)))
		} else {
			// New cache object
			gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: GS Cache obj malformed\n"), key, k,
				utils.Stringify(gsCacheObj))
			gsCacheObj := avicache.AviGSCache{
				Name:              name,
				Tenant:            operation.Tenant,
				Uuid:              uuid,
				Members:           gsMembers,
				K8sObjects:        memberObjs,
				HealthMonitorName: hm,
				CloudConfigCksum:  cksum,
			}
			restOp.cache.AviCacheAdd(k, &gsCacheObj)
			gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: added GS to the cache", key, k,
				utils.Stringify(gsCacheObj)))
		}
	} else {
		// New cache object
		gsCacheObj := avicache.AviGSCache{
			Name:              name,
			Tenant:            operation.Tenant,
			Uuid:              uuid,
			Members:           gsMembers,
			K8sObjects:        memberObjs,
			HealthMonitorName: hm,
			CloudConfigCksum:  cksum,
		}
		restOp.cache.AviCacheAdd(k, &gsCacheObj)
		gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: added GS to the cache", key, k,
			utils.Stringify(gsCacheObj)))
	}

	return nil
}

func SyncFromNodesLayer(key string, wg *sync.WaitGroup) error {
	cache := avicache.GetAviCache()
	hmCache := avicache.GetAviHmCache()
	aviclient := avicache.SharedAviClients()
	restLayerF := NewRestOperations(cache, hmCache, aviclient)
	gslbutils.Debugf("key: %s, msg: processing for key in rest layer", key)
	restLayerF.DqNodes(key)
	gslbutils.Debugf("key: %s, msg: processing for key is done in rest layer", key)
	return nil
}
