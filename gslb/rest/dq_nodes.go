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

package rest

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/apiserver"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"
	avicache "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"

	"github.com/davecgh/go-spew/spew"
	"github.com/vmware/alb-sdk/go/clients"
	avimodels "github.com/vmware/alb-sdk/go/models"
	"github.com/vmware/alb-sdk/go/session"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

const (
	ControllerNotLeaderErr      = "Config Operations can be done ONLY on leader"
	ControllerInMaintenanceMode = "GSLB system is in maintenance mode."
	GsGroupNamePrefix           = "amko-gs-group-"
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

func (restOp *RestOperations) deleteAllStaleHMsForGS(key string) {
	gslbutils.Debugf("key: %s, msg: checking if any stale health monitors present for this key", key)
	keySplit := strings.Split(key, "/")
	if len(keySplit) != 2 {
		gslbutils.Warnf("key: %s, msg: wrong key format, expecting the key to be <tenant>/<gsName>", key)
		return
	}
	tenant, gsName := keySplit[0], keySplit[1]
	hmObjs := restOp.hmCache.AviHmCacheGetHmsForGS(tenant, gsName)
	if len(hmObjs) == 0 {
		gslbutils.Debugf("key: %s, msg: no more health monitors for this key", key)
		return
	}
	gslbutils.Debugf("key: %s, msg: %d health monitors found for this key", key, len(hmObjs))
	for _, hmObj := range hmObjs {
		hmCacheObj, ok := hmObj.(*avicache.AviHmObj)
		if !ok {
			gslbutils.Warnf("key: %s, msg: hm cache object malformed", key)
			continue
		}
		err := restOp.deleteHmIfRequired(gsName, tenant, key, nil, avicache.TenantName{Tenant: tenant, Name: gsName}, hmCacheObj.Name)
		if err != nil {
			gslbutils.Errf("key: %s, msg: error in deleting hm for this key", key)
		}
	}
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
			// it could be that the key published was for a stale health monitor too, so remove all the
			// stale health monitor for this GS
			restOp.deleteAllStaleHMsForGS(key)
			return
		}
		deleteOp = true
	}

	var aviModel, aviModelCopy *nodes.AviGSObjectGraph

	if deleteOp {
		aviModel = deleteAviModelIntf.(*nodes.AviGSObjectGraph)
	} else {
		if aviModelIntf == nil {
			gslbutils.Errf("key: %s, msg: aviModelIntf is nil", key)
			return
		}
		aviModel = aviModelIntf.(*nodes.AviGSObjectGraph)
		aviModelCopy = aviModel.GetCopy()
	}

	tenant, gsName := utils.ExtractNamespaceObjectName(key)
	gsKey := avicache.TenantName{Tenant: tenant, Name: gsName}
	gsCacheObj := restOp.GetGSCacheObj(gsKey, key)

	ct := aviModel.GetRetryCounter()
	if ct <= 0 {
		aviModel.SetRetryCounter()
		gslbutils.Logf("key: %s, msg: retry counter exhausted, resetting counter", key)
		return
	}
	aviModel.DecrementRetryCounter()

	// Two ways a deletion can happen:
	// 1. We get the model in the delete cache and not in the regular model cache.
	// 2. Layer 2 might have pushed a key deciding its a UPDATE operation at the time, but before we
	//    get to Layer 3, layer 2 could have again set the members to 0.
	if deleteOp || (aviModelCopy != nil && aviModelCopy.MembersLen() == 0) {
		gslbutils.Logf("key: %s, msg: %s", key, "no model or members found, will delete the GslbService")
		if gsCacheObj == nil {
			gslbutils.Errf("key: %s, msg: %s", key, "no cache object for this GS was found, can't delete")
			// it could be that the key published was for a stale health monitor too, so remove all the
			// stale health monitor for this GS
			restOp.deleteAllStaleHMsForGS(key)
			return
		}
		restOp.deleteGSOper(gsCacheObj, tenant, key, aviModel)
		return
	}

	gslbutils.Logf("key: %s, msg: GslbService will be created/updated", key)
	if aviModelCopy == nil {
		gslbutils.Errf("key: %s, msg: %s", key, "unexpected error, no model exists for this GslbService")
		return
	}
	restOp.RestOperation(gsName, tenant, aviModelCopy, gsCacheObj, key)
}

func GetHMCacheObj(gsName string, key avicache.TenantName) avicache.AviHmObj {
	hmCache := cache.GetAviHmCache()
	hmObj, ok := hmCache.AviHmCacheGet(key)
	if !ok {
		gslbutils.Debugf("gs: %s, msg: health monitor %s not found", gsName, key.Name)
		return avicache.AviHmObj{}
	}
	hmCacheObj, ok := hmObj.(*avicache.AviHmObj)
	if !ok {
		gslbutils.Warnf("key: %s, msg: hm cache object malformed", gsName)
		return avicache.AviHmObj{}
	}
	return *hmCacheObj
}

func GetHMCacheObjFromGSCache(gsCacheObj *avicache.AviGSCache) []avicache.AviHmObj {
	hmCacheObjs := []avicache.AviHmObj{}
	for _, hmName := range gsCacheObj.HealthMonitor {
		key := avicache.TenantName{Tenant: gsCacheObj.Tenant, Name: hmName}
		hmCacheObjs = append(hmCacheObjs, GetHMCacheObj(gsCacheObj.Name, key))
	}
	return hmCacheObjs
}

func (restOp *RestOperations) getHmPathDiff(aviGSGraph *nodes.AviGSObjectGraph, gsCacheObj *avicache.AviGSCache) ([]string, []string) {
	hmNameList := aviGSGraph.GetHmPathNamesList()
	toBeAdded := []string{}
	toBeDeleted := []string{}

	if gsCacheObj == nil {
		gslbutils.Debugf("gsName: %s, pathList: %v, msg: gsCacheObj is nil, we will only create path based HMs", aviGSGraph.Name,
			hmNameList)
		toBeAdded = hmNameList
		return toBeAdded, toBeDeleted
	}

	// create a map of health monitors in the existing GS object and the new GS object
	existingHms := make(map[string]struct{})
	for _, hmName := range gsCacheObj.HealthMonitor {
		existingHms[hmName] = struct{}{}
	}
	newHms := make(map[string]struct{})
	for _, hmName := range hmNameList {
		newHms[hmName] = struct{}{}
	}

	for _, hmName := range hmNameList {
		if _, exists := existingHms[hmName]; !exists {
			toBeAdded = append(toBeAdded, hmName)
		}
	}
	existingHMObjs := GetHMCacheObjFromGSCache(gsCacheObj)
	for _, hmObj := range existingHMObjs {
		hmName := hmObj.Name
		if _, exists := newHms[hmName]; !exists {
			toBeDeleted = append(toBeDeleted, hmName)
		}
	}
	gslbutils.Debugf("gsName: %s, toBeAdded: %v, toBeDeleted: %v, msg: hms to be added and deleted", aviGSGraph.Name, toBeAdded,
		toBeDeleted)
	return toBeAdded, toBeDeleted
}

func (restOp *RestOperations) updateGsIfRequired(aviGSGraph *nodes.AviGSObjectGraph, gsCacheObj *avicache.AviGSCache,
	gsKey avicache.TenantName, key string) {
	gsName := gsCacheObj.Name
	cksum := aviGSGraph.GetChecksum()
	// check if the GS needs an update
	if gsCacheObj.CloudConfigCksum == cksum {
		gslbutils.Debugf("key: %s, GslbService: %s, msg: the checksums are same for the GSLB service, existing: %d, new: %d, ignoring",
			key, gsName, gsCacheObj.CloudConfigCksum, cksum)
		return
	}
	gslbutils.Debugf("key: %s, GSLBService: %s, oldCksum: %d, newCksum: %d, msg: %s", key, gsName,
		gsCacheObj.CloudConfigCksum, cksum, "checksums are different for the GSLB Service")
	// it should be a PUT call
	operation := restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key, true)
	gslbutils.Debugf(spew.Sprintf("gsKey: %s, operation: %v", gsKey, operation))
	restOp.ExecuteRestAndPopulateCache(operation, &gsKey, nil, key)
}

func (restOp *RestOperations) createOrDeletePathHm(aviGSGraph *nodes.AviGSObjectGraph, gsCacheObj *avicache.AviGSCache,
	key string, gsKey avicache.TenantName) error {
	// see if the health monitors are path based
	toBeAddedPathHms, toBeDelPathHms := restOp.getHmPathDiff(aviGSGraph, gsCacheObj)
	gslbutils.Debugf("key: %s, toBeAdded: %v, toBeDeleted: %v, msg: hms to be added/deleted", key, toBeAddedPathHms,
		toBeDelPathHms)

	if len(toBeAddedPathHms) != 0 {
		// we have to create path based HMs for these paths first
		for _, hmName := range toBeAddedPathHms {
			hmObj := restOp.getGSHmCacheObj(hmName, aviGSGraph.Tenant, key)
			if hmObj == nil {
				op := restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key, hmName)
				if op == nil {
					gslbutils.Errf("key: %s, msg: couldn't build a rest operation for health monitor, returning", key)
					return errors.New("couldn't build a rest operation")
				}
				hmKey := avicache.TenantName{Tenant: utils.ADMIN_NS, Name: hmName}
				restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
				if op.Err != nil {
					gslbutils.Errf("key: %s, hmKey: %v, msg: error while performing rest operation", key, hmKey)
					return op.Err
				}
			}
		}
	}
	if gsCacheObj == nil {
		// HMs are already created for a GS post operation, no HMs to be deleted, just return
		gslbutils.Debugf("key: %s, msg: HMs have been created for the GS post operation", key)
		return nil
	}
	// update GS, after adding the HMs and before deleting the HMs
	restOp.updateGsIfRequired(aviGSGraph, gsCacheObj, gsKey, key)
	if len(toBeDelPathHms) != 0 {
		// we have to delete path based HMs for these paths
		for _, hmName := range toBeDelPathHms {
			err := restOp.deleteHmIfRequired(gsCacheObj.Name, utils.ADMIN_NS, key, gsCacheObj, gsKey, hmName)
			if err != nil {
				// the key has been already published to the retry queue for an error event, so just return
				return errors.New("couldn't build a rest operation")
			}
		}
	}
	return nil
}

func (restOp *RestOperations) createOrUpdateNonPathHm(aviGSGraph *nodes.AviGSObjectGraph, gsCacheObj *avicache.AviGSCache,
	gsKey avicache.TenantName, key string) error {
	hms := GetHMCacheObjFromGSCache(gsCacheObj)
	if len(hms) != 0 {
		hm := hms[0]
		hmKey := avicache.TenantName{Tenant: utils.ADMIN_NS, Name: hm.Name}
		hmCksum := aviGSGraph.GetHmChecksum(aviGSGraph.Hm.GetHMDescription(aviGSGraph.Name))
		gslbutils.Debugf(spew.Sprintf("key: %s, hmKey: %v, aviGSGraph: %v, hmChecksum: %d, hmCloudConfigChecksum: %d, msg: will check if hm needs to change",
			key, hmKey, *aviGSGraph, hmCksum, hm.CloudConfigCksum))
		if hm.CloudConfigCksum != hmCksum {
			// update gs, delete hm, create new hm and update gs
			op := restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key, false)
			restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
			if op.Err != nil {
				gslbutils.Errf("key: %s, hmKey: %s, msg: error in rest operation: %v", key, hmKey, op)
				return op.Err
			}
			op = restOp.AviGsHmDel(hm.UUID, utils.ADMIN_NS, key, hm.Name)
			restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
			if op.Err != nil {
				gslbutils.Errf("key: %s, hmKey: %s, error in rest operation: %v", key, hmKey, op)
				return op.Err
			}
			hmObj := restOp.getGSHmCacheObj(aviGSGraph.Hm.Name, aviGSGraph.Tenant, key)
			if hmObj == nil {
				op = restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key, "")
				restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
				if op.Err != nil {
					gslbutils.Errf("key: %s, hmKey: %s, error in rest operation: %v", key, hmKey, op)
					return op.Err
				}
			}
			op = restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key, true)
			restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
			if op.Err != nil {
				gslbutils.Errf("key: %s, hmKey: %s, error in rest operation: %v", key, hmKey, op)
				return op.Err
			}
		} else {
			gslbutils.Debugf(spew.Sprintf("key: %s, hmKey: %s, aviGSGraph: %v, hmChecksum: %d, hmCloudConfigChecksum: %d, msg: no change in HM required",
				key, hmKey, *aviGSGraph, hmCksum, hm.CloudConfigCksum))
		}
	} else {
		// HM needs to be created
		op := restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key, "")
		if op == nil {
			gslbutils.Errf("key: %s, error in building avi hm object, won't retry", key)
			return errors.New("error in building avi hm object")
		}
		hmKey := avicache.TenantName{Tenant: utils.ADMIN_NS, Name: op.ObjName}
		restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
		if op.Err != nil {
			gslbutils.Errf("key: %s, hmKey: %v, error in rest operation: %v", key, hmKey, op)
			return op.Err
		}
		op = restOp.AviGSBuild(aviGSGraph, utils.RestPut, gsCacheObj, key, true)

		restOp.ExecuteRestAndPopulateCache(op, &gsKey, nil, key)
		if op.Err != nil {
			gslbutils.Errf("key: %s, gsKey: %v, error in rest operation: %v", key, gsKey, op)
			return op.Err
		}
	}
	return nil
}

func (restOp *RestOperations) RestOperation(gsName, tenant string, aviGSGraph *nodes.AviGSObjectGraph,
	gsCacheObj *avicache.AviGSCache, key string) {
	gsKey := avicache.TenantName{Tenant: tenant, Name: gsName}
	var operation *utils.RestOp
	pathNames := aviGSGraph.GetHmPathNamesList()

	if !gslbutils.IsControllerLeader() {
		gslbutils.Errf("key: %s, msg: can't execute rest operation as controller is not a leader", gsKey)
		return
	}
	var err error
	if gsCacheObj != nil {
		if len(pathNames) > 0 {
			// path based default HMs
			err = restOp.createOrDeletePathHm(aviGSGraph, gsCacheObj, key, gsKey)
		} else if aviGSGraph.Hm.HMProtocol != "" {
			// non-path based default Hms
			err = restOp.createOrUpdateNonPathHm(aviGSGraph, gsCacheObj, gsKey, key)
		} else {
			// user provided custom HM Refs
			gslbutils.Logf("key: %s, msg: user provided HM refs will be attached to GS", gsKey)
			restOp.updateGsIfRequired(aviGSGraph, gsCacheObj, gsKey, key)
			restOp.deleteAllStaleHMsForGS(gsKey.Tenant + "/" + gsKey.Name)
		}
		if err != nil {
			// the key for this graph would have been already published to the retry queue, so just return
			return
		}
		// PUT on GS if required
		restOp.updateGsIfRequired(aviGSGraph, gsCacheObj, gsKey, key)
		return
	}
	// its a POST operation for a GS
	// first, see if we need new health monitor(s)

	gslbutils.Debugf("key: %s, pathList: %v, msg: path based HM name list of the GS", key, pathNames)
	if len(pathNames) > 0 {
		// path based HMs
		err = restOp.createOrDeletePathHm(aviGSGraph, gsCacheObj, key, gsKey)
		if err != nil {
			gslbutils.Errf("key: %s, pathList: %v, msg: got an error for creating/deleting path based hms, %s", key,
				pathNames, err.Error())
			return
		}
	} else {
		// non-path based HMs (System-GSLB-TCP/UDP)
		hm := restOp.getGSHmCacheObj(aviGSGraph.Hm.Name, aviGSGraph.Tenant, key)
		if hm == nil {
			if aviGSGraph.IsHmTypeCustom(aviGSGraph.Hm.Name) {
				// create a new health monitor
				op := restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key, "")
				if op == nil {
					gslbutils.Errf("key: %s, gsKey: %s, msg: couldn't build a rest operation for health monitor, returning",
						key, gsKey)
					// won't retry in this case as this was a case of bad model we recieved from layer 2
					return
				}
				hmKey := avicache.TenantName{Tenant: aviGSGraph.Tenant, Name: aviGSGraph.Hm.Name}
				restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
				if op.Err != nil {
					gslbutils.Errf("key: %s, hmKey: %v, error in rest operation: %v", key, hmKey, op.Err.Error())
					return
				}
				gslbutils.Debugf("key: %s, hmKey: %v, msg: no new hm required", key, hmKey)
			}
			gslbutils.Debugf("key: %s, gsKey: %v, msg: nothing to be done for default HM", key, gsKey)
		} else {
			// a health monitor already exists, see if we need to re-create it
			hmCksum := aviGSGraph.GetHmChecksum(aviGSGraph.Hm.GetHMDescription(aviGSGraph.Name))
			gslbutils.Debugf(spew.Sprintf("key: %s, gsKey: %s, aviGSGraph: %s, hmChecksum: %d, hmCloudConfigChecksum: %d, msg: will check if hm needs to change",
				key, gsKey, *aviGSGraph, hmCksum, hm.CloudConfigCksum))
			if hm.CloudConfigCksum != hmCksum {
				// delete hm, create new hm and update gs
				hmKey := avicache.TenantName{Tenant: utils.ADMIN_NS, Name: hm.Name}
				op := restOp.AviGsHmDel(hm.UUID, utils.ADMIN_NS, key, hm.Name)
				restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
				if op.Err != nil {
					gslbutils.Errf("key: %s, hmKey: %s, error in rest operation: %v", key, hmKey, op)
					return
				}
				op = restOp.AviGsHmBuild(aviGSGraph, utils.RestPost, nil, key, "")
				restOp.ExecuteRestAndPopulateCache(op, nil, &hmKey, key)
				if op.Err != nil {
					gslbutils.Errf("key: %s, hmKey: %s, error in rest operation: %v", key, hmKey, op)
					return
				}
			}
		}
	}

	gslbutils.Logf("key: %s, operation: POST, msg: GS not found in cache", key)
	operation = restOp.AviGSBuild(aviGSGraph, utils.RestPost, nil, key, true)

	operation.ObjName = aviGSGraph.Name
	restOp.ExecuteRestAndPopulateCache(operation, &gsKey, nil, key)
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

func (restOp *RestOperations) ExecuteRestAndPopulateCache(operation *utils.RestOp, gsKey, hmKey *avicache.TenantName,
	key string) {
	// Choose a AVI client based on the model name hash. This would ensure that the same worker queue processes updates for a
	// given GS everytime.
	bkt := utils.Bkt(key, gslbutils.NumRestWorkers)
	gslbutils.Logf("key: %s, queue: %d, msg: processing in rest queue", key, bkt)

	if len(restOp.aviRestPoolClient.AviClient) > 0 {
		aviClient := restOp.aviRestPoolClient.AviClient[bkt]
		err := AviRestOperateWrapper(restOp, aviClient, operation)
		gslbutils.Debugf("key: %s, queue: %d, msg: avi rest operate wrapper response, %v", key, bkt, err)
		if err != nil {
			if err.Error() == "rest timeout occured" {
				gslbutils.Errf("key: %s, queue: %d, msg: got a rest timeout", key, bkt)
				restOp.PublishKeyToRetryLayer(gsKey, hmKey, err, key)
				return
			}
			webSyncErr, ok := err.(*utils.WebSyncError)
			if !ok {
				gslbutils.Errf("key: %s, msg: %s, err: %v", key, "got an improper web api error, returning", err)
				return
			}
			restOp.PublishKeyToRetryLayer(gsKey, hmKey, webSyncErr.GetWebAPIError(), key)
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

func (restOp *RestOperations) handleErrAndUpdateCacheForHm(errCode int, hmKey avicache.TenantName, key string) {
	if len(restOp.aviRestPoolClient.AviClient) <= 0 {
		gslbutils.Errf("invalid avi pool client configuration in restOp, key: %s", key)
		return
	}

	bkt := utils.Bkt(key, gslbutils.NumRestWorkers)
	gslbutils.Logf("key: %s, queue: %d, msg: handling error and updating cache", key, bkt)
	aviclient := restOp.aviRestPoolClient.AviClient[bkt]

	switch errCode {
	case 409:
		// case where the object configuration is mis-represented in the in-memory cache
		// first fetch the object and then update it into the cache
		gslbutils.Logf("httpStatus: %d, hmKey: %v, will delete the avi cache key and re-populate", errCode,
			hmKey)
		restOp.hmCache.AviHmCacheDelete(hmKey)
		restOp.hmCache.AviHmObjCachePopulate(aviclient, hmKey.Name)
		return

	case 404:
		// case where the object doesn't exist in Avi, delete that object
		gslbutils.Logf("httpStatus: %d, gsKey: %v, will delete the avi hm cache key", errCode, hmKey)
		restOp.hmCache.AviHmCacheDelete(hmKey)
		return
	}
	gslbutils.Logf("httpStatus: %d, hmKey: %v, unhandled error code, avi hm cache unchanged")
	return
}

func (restOp *RestOperations) handleErrAndUpdateCacheForGS(errCode int, gsKey avicache.TenantName, key string) {
	if len(restOp.aviRestPoolClient.AviClient) <= 0 {
		gslbutils.Errf("invalid avi pool client configuration in restOp, key: %s", key)
		return
	}

	bkt := utils.Bkt(key, gslbutils.NumRestWorkers)
	gslbutils.Logf("key: %s, queue: %d, msg: handling error and updating cache", key, bkt)
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

	case 404:
		// case where the object doesn't exist in Avi, delete that object
		gslbutils.Logf("httpStatus: %d, gsKey: %v, will delete the avi cache key", errCode, gsKey)
		restOp.cache.AviCacheDelete(gsKey)
		return
	}
	gslbutils.Logf("httpStatus: %d, gsKey: %v, unhandled error code, avi gs cache unchanged", errCode, gsKey)
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

func (restOp *RestOperations) PublishKeyToRetryLayer(gsKey, hmKey *avicache.TenantName, webApiErr error, key string) {
	var bkt uint32
	bkt = 0

	gslbutils.Debugf("key: %s, gsKey: %v, hmKey: %v, msg: evaluating whether to publish to retry queue",
		key, gsKey, hmKey)
	if webApiErr.Error() == "rest timeout occured" {
		gslbutils.Errf("gsKey: %v, hmKey: %v, msg: timeout occured while doing rest call", gsKey, hmKey)
		// if this error occurs, we will reset the error counter, so it keeps on retrying until it has
		// nothing to do for this key
		err := setRetryCounterForGraph(key)
		if err != nil {
			gslbutils.Errf("can't set the retry counter for this key, will re-sync in the next full sync")
			gslbutils.SetResyncRequired(true)
			return
		}
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published key to slow path retry queue", key)
		return
	}
	aviError, ok := webApiErr.(session.AviError)
	if !ok {
		gslbutils.Errf("error in parsing the web api error to avi error: %v", webApiErr)
		err := setRetryCounterForGraph(key)
		if err != nil {
			gslbutils.Errf("can't set the retry counter for this key, will re-sync in the next full sync")
			gslbutils.SetResyncRequired(true)
			return
		}
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.FastRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published key to fast path retry queue", key)
		return
	}

	gslbutils.Logf("key: %s, msg: Status code retrieved: %d", key, aviError.HttpStatusCode)
	switch aviError.HttpStatusCode {
	case 500, 501, 502, 503:
		// Server errors, so we should keep on retrying
		err := setRetryCounterForGraph(key)
		if err != nil {
			gslbutils.Errf("can't set the retry counter for this key, will re-sync in the next full sync")
			gslbutils.SetResyncRequired(true)
			return
		}
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published key to slow path retry queue", key)

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
			gslbutils.Logf("key: %s, msg: Published key to slow path retry queue", key)
			return
		}
		gslbutils.Errf("can't handle error code 400: %s, won't retry", *aviError.Message)

	case 404, 409:
		// however, if this controller is still the leader, we should retry
		// for these error codes, we should first update the cache and put the key back to rest layer
		if gsKey != nil {
			restOp.handleErrAndUpdateCacheForGS(aviError.HttpStatusCode, *gsKey, key)
		} else {
			restOp.handleErrAndUpdateCacheForHm(aviError.HttpStatusCode, *hmKey, key)
		}
		fastRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.FastRetryQueue)
		fastRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published gskey to fast path retry queue", key)

	case 401:
		if strings.Contains(*aviError.Message, "Invalid credentials") {
			gslbutils.Errf("key: %s, msg: credentials were invalid, shutting down API server", key)
			apiserver.GetAmkoAPIServer().ShutDown()
			return
		}
		gslbutils.Errf("key: %s, msg: error code 401, will retry", key)
		slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
		slowRetryQueue.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("key: %s, msg: Published key to slow path retry queue", key)
		return

	default:
		gslbutils.Warnf("key: %s, msg: unhandled status code %d", key, aviError.HttpStatusCode)
		// no retry, but this will be taken care of in the next full sync
		gslbutils.SetResyncRequired(true)
	}
}

func (restOp *RestOperations) AviGsHmBuild(gsMeta *nodes.AviGSObjectGraph, restMethod utils.RestMethod,
	hmCacheObj *avicache.AviHmObj, key string, pathHm string) *utils.RestOp {
	gslbutils.Logf("key: %s, gsName: %s, msg: creating rest operation for health monitor", key, gsMeta.Name)
	var hmName string
	var monitorPort int32
	var hmHTTP avimodels.HealthMonitorHTTP

	hmProto := gsMeta.Hm.HMProtocol
	isFederated := true
	allowDup := true
	tenantRef := gslbutils.GetAviAdminTenantRef()
	description := ""
	sendInterval := int32(10)
	receiveTimeout := int32(4)
	successfulChecks := int32(3)
	failedChecks := int32(3)

	aviGsHm := avimodels.HealthMonitor{
		IsFederated:            &isFederated,
		Name:                   &hmName,
		SendInterval:           &sendInterval,
		Type:                   &hmProto,
		Description:            &description,
		TenantRef:              &tenantRef,
		AllowDuplicateMonitors: &allowDup,
		ReceiveTimeout:         &receiveTimeout,
		SuccessfulChecks:       &successfulChecks,
		FailedChecks:           &failedChecks,
	}

	if pathHm != "" {
		// path based http/https health monitor
		description = nodes.GetDescriptionForPathHMName(pathHm, gsMeta)
		path := nodes.GetPathFromHmDescription(pathHm, description)
		if path == "" {
			gslbutils.Errf("key: %s, pathHm: %s, msg: malformed path HM name provided for hm build", key, pathHm)
			return nil
		}
		request := "HEAD " + path + " HTTP/1.0"
		httpResponseCodes := []string{"HTTP_2XX", "HTTP_3XX"}
		hmHTTP.HTTPRequest = &request
		hmHTTP.HTTPResponseCode = httpResponseCodes

		hmName = pathHm
		switch hmProto {
		case gslbutils.SystemGslbHealthMonitorHTTP:
			monitorPort = gslbutils.DefaultHTTPHealthMonitorPort
			aviGsHm.HTTPMonitor = &hmHTTP
		case gslbutils.SystemGslbHealthMonitorHTTPS:
			monitorPort = gslbutils.DefaultHTTPSHealthMonitorPort
			aviGsHm.HTTPSMonitor = &hmHTTP
		default:
			gslbutils.Errf("key: %s, msg: can't build a path based health monitor for an unknown protocol %s", key, hmProto)
			return nil
		}

	} else {
		description = gsMeta.Hm.GetHMDescription(gsMeta.Name)[0]
		hmName = gsMeta.Hm.Name
		monitorPort = gsMeta.Hm.Port
		switch hmProto {
		case gslbutils.SystemHealthMonitorTypeUDP:
			udpRequest := "created_by: amko, request string not required"
			hmUDP := avimodels.HealthMonitorUDP{
				UDPRequest: &udpRequest,
			}
			aviGsHm.UDPMonitor = &hmUDP
		case gslbutils.SystemHealthMonitorTypeTCP:
			tcpHalfOpen := false
			hmTCP := avimodels.HealthMonitorTCP{
				TCPHalfOpen: &tcpHalfOpen,
			}
			aviGsHm.TCPMonitor = &hmTCP
		default:
			gslbutils.Errf("key: %s, msg: can't build a health monitor for an unknown protocol %s", key, hmProto)
			return nil
		}
	}

	aviGsHm.MonitorPort = &monitorPort

	path := "/api/healthmonitor"

	operation := utils.RestOp{ObjName: gsMeta.Name, Path: path, Obj: aviGsHm, Tenant: gsMeta.Tenant, Model: "HealthMonitor",
		Version: gslbutils.GetAviConfig().Version}

	if restMethod == utils.RestPost {
		operation.Method = utils.RestPost
		gslbutils.Debugf(spew.Sprintf("key: %s, hmModel: %v, msg: HM rest operation %v\n", key, gsMeta.Hm, utils.Stringify(operation)))
		return &operation
	}
	operation.Path = path + hmCacheObj.UUID
	operation.Method = utils.RestPut
	gslbutils.Debugf(spew.Sprintf("key: %s, hmModel: %s, msg: HM rest operation %v\n", key, gsMeta.Hm, utils.Stringify(operation)))
	return &operation
}

func (restOp *RestOperations) getGSPoolAlgorithmSettings(gsMeta *nodes.AviGSObjectGraph) (*string, *int32, *string) {
	var lbAlgorithm string

	if gsMeta.GslbPoolAlgorithm == nil {
		lbAlgorithm = gslbalphav1.PoolAlgorithmRoundRobin
		return &lbAlgorithm, nil, nil
	}

	lbAlgorithm = gsMeta.GslbPoolAlgorithm.LBAlgorithm
	switch lbAlgorithm {
	case gslbalphav1.PoolAlgorithmRoundRobin, gslbalphav1.PoolAlgorithmTopology:
		return &lbAlgorithm, nil, nil

	case gslbalphav1.PoolAlgorithmConsistentHash:
		hashMask := int32(*gsMeta.GslbPoolAlgorithm.HashMask)
		return &lbAlgorithm, &hashMask, nil

	case gslbalphav1.PoolAlgorithmGeo:
		fa := gsMeta.GslbPoolAlgorithm.FallbackAlgorithm.LBAlgorithm
		if gsMeta.GslbPoolAlgorithm.FallbackAlgorithm.HashMask != nil {
			hashMask := int32(*gsMeta.GslbPoolAlgorithm.FallbackAlgorithm.HashMask)
			return &lbAlgorithm, &hashMask, &fa
		} else {
			return &lbAlgorithm, nil, &fa
		}
	}
	return nil, nil, nil
}

func buildGsPoolMember(member nodes.AviGSK8sObj, key string) *avimodels.GslbPoolMember {
	enabled := true
	ipVersion := "V4"
	ipAddr := member.IPAddr
	ratio := member.Weight
	clusterUUID := member.ControllerUUID
	vsUUID := member.VirtualServiceUUID

	gsPoolMember := avimodels.GslbPoolMember{
		Enabled: &enabled,
		Ratio:   &ratio,
		IP:      &avimodels.IPAddr{Addr: &ipAddr, Type: &ipVersion},
	}
	if !member.SyncVIPOnly {
		if clusterUUID != "" {
			gsPoolMember.ClusterUUID = &clusterUUID
		} else {
			gslbutils.Warnf("key: %s, cluster: %s, namespace: %s, member: %s, msg: %s",
				key, member.Cluster, member.Namespace, member.Name, "controller cluster UUID is empty, will try to update the GS member")
		}
		gsPoolMember.VsUUID = &vsUUID
	}
	return &gsPoolMember
}

func buildGsPool(gsMeta *nodes.AviGSObjectGraph, gsPoolMembers []*avimodels.GslbPoolMember, priority int32, restOp *RestOperations) *avimodels.GslbPool {
	poolEnabled := true
	poolName := GsGroupNamePrefix + strconv.Itoa(int(priority))
	minHealthMonUp := int32(2)
	poolAlgorithm, hashMask, fallback := restOp.getGSPoolAlgorithmSettings(gsMeta)
	return &avimodels.GslbPool{
		Algorithm:           poolAlgorithm,
		ConsistentHashMask:  hashMask,
		FallbackAlgorithm:   fallback,
		Enabled:             &poolEnabled,
		Members:             gsPoolMembers,
		Name:                &poolName,
		Priority:            &priority,
		MinHealthMonitorsUp: &minHealthMonUp,
	}
}

func buildGslbSvcGroups(gsMeta *nodes.AviGSObjectGraph, key string, restOp *RestOperations) []*avimodels.GslbPool {
	pools := []*avimodels.GslbPool{}
	poolPriorityMap := map[int32][]nodes.AviGSK8sObj{}
	// first group all members according to their priorities
	for _, member := range gsMeta.GetUniqueMemberObjs() {
		poolPriorityMap[member.Priority] = append(poolPriorityMap[member.Priority], member)
	}
	// build the pool list from the poolPriorityMap
	for priority, members := range poolPriorityMap {
		// each priority makes one pool with `members` as the pool members
		gsPoolMembers := []*avimodels.GslbPoolMember{}
		for _, m := range members {
			if m.IPAddr == "" {
				gslbutils.Warnf("GS pool member doesn't have an IP address: %v", m)
				continue
			}
			gsPoolMembers = append(gsPoolMembers, buildGsPoolMember(m, key))
		}
		if len(gsPoolMembers) == 0 {
			continue
		}
		// build a new pool for this priority
		pools = append(pools, buildGsPool(gsMeta, gsPoolMembers, priority, restOp))
	}
	return pools
}

func (restOp *RestOperations) AviGSBuild(gsMeta *nodes.AviGSObjectGraph, restMethod utils.RestMethod,
	cacheObj *avicache.AviGSCache, key string, hmRequired bool) *utils.RestOp {
	gslbutils.Logf("key: %s, msg: creating rest operation", key)

	// build the gslb service pools
	gslbSvcGroups := buildGslbSvcGroups(gsMeta, key, restOp)

	// Now, build the GSLB service
	ctrlHealthStatusEnabled := true
	createdBy := gslbutils.AmkoUser
	gsEnabled := true
	healthMonitorScope := "GSLB_SERVICE_HEALTH_MONITOR_ALL_MEMBERS"
	isFederated := true
	minMembers := int32(0)
	gsName := gsMeta.Name
	resolveCname := false
	tenantRef := gslbutils.GetAviAdminTenantRef()
	useEdnsClientSubnet := true
	wildcardMatch := false
	// description field needs references
	description := strings.Join(gsMeta.GetMemberObjList(), ",")
	var hmRefs []string
	if len(gsMeta.HmRefs) > 0 {
		copy(hmRefs, gsMeta.HmRefs)
	}

	gsAlgorithm := "GSLB_SERVICE_ALGORITHM_PRIORITY"
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
		PoolAlgorithm:                 &gsAlgorithm,
		ResolveCname:                  &resolveCname,
		UseEdnsClientSubnet:           &useEdnsClientSubnet,
		WildcardMatch:                 &wildcardMatch,
		TenantRef:                     &tenantRef,
		Description:                   &description,
	}

	var ttl int32
	if gsMeta.TTL != nil {
		ttl = int32(*gsMeta.TTL)
		aviGslbSvc.TTL = &ttl
	}

	if gsMeta.SitePersistenceRef != nil {
		sitePersistenceEnabled := true
		persistenceProfileRef := "/api/applicationpersistenceprofile?name=" + *gsMeta.SitePersistenceRef
		aviGslbSvc.SitePersistenceEnabled = &sitePersistenceEnabled
		aviGslbSvc.ApplicationPersistenceProfileRef = &persistenceProfileRef
	} else {
		sitePersistenceEnabled := false
		aviGslbSvc.SitePersistenceEnabled = &sitePersistenceEnabled
	}

	hmAPI := "/api/healthmonitor?name="

	// Add the default health monitor(s) only if custom health monitor refs are not provided
	if hmRequired && len(gsMeta.HmRefs) == 0 {
		// check if path based (HTTP(S)) HMs are required or just a single non-path based (TCP/UDP) HM
		if len(gsMeta.Hm.PathHM) == 0 {
			if gsMeta.Hm.Name == "" {
				gslbutils.Errf("gs %s doesn't have a health monitor", gsMeta.Name)
			}
			aviGslbSvc.HealthMonitorRefs = []string{hmAPI + gsMeta.Hm.Name}
		} else {
			aviGslbSvc.HealthMonitorRefs = []string{}
			for _, hmName := range gsMeta.Hm.PathHM {
				aviGslbSvc.HealthMonitorRefs = append(aviGslbSvc.HealthMonitorRefs, hmAPI+hmName.Name)
			}
		}
	} else if len(gsMeta.HmRefs) > 0 {
		minHmUp := int32(len(gsMeta.HmRefs) + 1)
		aviGslbSvc.Groups[0].MinHealthMonitorsUp = &minHmUp
		// Add the custom health monitors here
		aviGslbSvc.HealthMonitorRefs = []string{}
		for _, hmName := range gsMeta.HmRefs {
			aviGslbSvc.HealthMonitorRefs = append(aviGslbSvc.HealthMonitorRefs,
				hmAPI+hmName)
		}
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

func (restOp *RestOperations) getGSHmCacheObj(hmName, tenant string, key string) *avicache.AviHmObj {
	hmKey := avicache.TenantName{Tenant: tenant, Name: hmName}
	hmObjIntf, found := restOp.hmCache.AviHmCacheGet(hmKey)
	if found {
		hmObj, ok := hmObjIntf.(*avicache.AviHmObj)
		if !ok {
			gslbutils.Warnf("key: %s, hmKey: %s, msg: %s", key, hmKey, "invalid Health monitor object found, ignoring...")
			return nil
		}
		return hmObj
	}
	gslbutils.Logf("key: %s, hmKey: %v, msg: HM cache object not found", key, hmKey)
	return nil
}

func (restOp *RestOperations) GetGSCacheObj(gsKey avicache.TenantName, key string) *avicache.AviGSCache {
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

func (restOp *RestOperations) deleteHmIfRequired(gsName, tenant, key string, gsCacheObj *avicache.AviGSCache,
	gsKey avicache.TenantName, hmName string) error {
	var restOps *utils.RestOp

	bkt := utils.Bkt(key, gslbutils.NumRestWorkers)
	gslbutils.Logf("key: %s, hmName: %s, queue: %d, msg: deleting HM object", key, hmName, bkt)
	aviclient := restOp.aviRestPoolClient.AviClient[bkt]
	if !gslbutils.IsControllerLeader() {
		gslbutils.Errf("key: %s, msg: %s", key, "can't execute rest operation, as controller is not a leader")
		gslbutils.UpdateGSLBConfigStatus(ControllerNotLeaderErr)
		return nil
	}

	// passthrough health monitor is shared across all passthrough GSs and hence, won't be deleted
	if hmName == gslbutils.SystemGslbHealthMonitorPassthrough {
		gslbutils.Debugf("key: %s, hmName: %s, msg: won't delete the passthrough health monitor", key, hmName)
		return nil
	}
	hmCacheObjIntf, found := restOp.hmCache.AviHmCacheGet(avicache.TenantName{Tenant: utils.ADMIN_NS, Name: hmName})
	if !found {
		gslbutils.Warnf("key: %s, gsKey: %v, msg: health monitor object not found in the hm cache, can't delete",
			key, gsKey)
		// return nil because the hm object to be deleted was already deleted, while its not expected, this is not
		// necessarily an error
		return nil
	}
	// health monitor found for this object, deleting
	hmCacheObj, ok := hmCacheObjIntf.(*avicache.AviHmObj)
	if !ok {
		gslbutils.Warnf("key: %s, gsKey: %v, msg: health monitor object %v malformed, can't delete, won't retry",
			key, gsKey, hmCacheObj)
		return errors.New("hm cache object malformed")
	}
	hmKey := avicache.TenantName{Tenant: utils.ADMIN_NS, Name: hmName}
	operation := restOp.AviGsHmDel(hmCacheObj.UUID, hmCacheObj.Tenant, key, hmCacheObj.Name)
	restOps = operation
	err := AviRestOperateWrapper(restOp, aviclient, restOps)
	if err != nil {
		gslbutils.Warnf("key: %s, HealthMonitor: %s, msg: %s", key, hmName, "failed to delete, will retry")
		if err.Error() == "rest timeout occured" {
			gslbutils.Errf("key: %s, queue: %d, msg: got a rest timeout", key, bkt)
			restOp.PublishKeyToRetryLayer(nil, &hmKey, err, key)
			return err
		}

		webSyncErr, ok := err.(*utils.WebSyncError)
		if !ok {
			gslbutils.Errf("key: %s, HealthMonitor: %s, gsKey: %v, msg: %s", key, hmName,
				gsKey, "got an improper web api error, will publish to retry queue")
		}
		restOp.PublishKeyToRetryLayer(nil, &hmKey, webSyncErr.GetWebAPIError(), key)
		return err
	}
	gslbutils.Debugf("key: %s, gsKey: %v, msg: will delete the key from hm cache", key, gsKey)
	restOp.AviGSHmCacheDel(restOp.hmCache, operation, key)
	return nil
}

func (restOp *RestOperations) deleteGSOper(gsCacheObj *avicache.AviGSCache, tenant string, key string,
	gsGraph *nodes.AviGSObjectGraph) {
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
				restOp.PublishKeyToRetryLayer(&gsKey, nil, err, key)
				return
			}
			webSyncErr, ok := err.(*utils.WebSyncError)
			if !ok {
				gslbutils.Errf("key: %s, GSLBService: %s, msg: %s", key, gsCacheObj.Uuid,
					"got an improper web api error, publishing to retry queue")
			}
			restOp.PublishKeyToRetryLayer(&gsKey, nil, webSyncErr.GetWebAPIError(), key)
			return
		}

		// Clear all the cache objects which were deleted
		restOp.AviGSCacheDel(restOp.cache, operation, key)

		// if no HM refs for this GS, delete all HMs for this GS
		if len(gsGraph.HmRefs) == 0 {
			for _, hmName := range gsCacheObj.HealthMonitor {
				// check if this HM is created by AMKO, if not, don't try to remove it
				if !gslbutils.HMCreatedByAMKO(hmName) {
					continue
				}
				err = restOp.deleteHmIfRequired(gsName, tenant, key, gsCacheObj, gsKey, hmName)
				if err != nil {
					return
				}
			}
		} else {
			gslbutils.Debugf("key: %s, GSLBService: %s, msg: won't remove HM refs", key, gsGraph.Name)
		}
		gslbutils.Logf("key: %s, msg: deleting key from layer 2 delete cache", key)
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
	description, ok := respElem["description"].(string)
	if !ok {
		gslbutils.Warnf("key: %s, resp: %s, msg: description not present in response", key, respElem)
		return errors.New("description not present in response")
	}

	cksum := gslbutils.GetGSLBHmChecksum(hmType, port, []string{description})
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
			hmCacheObj.Description = description
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
			Description:      description,
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

	cksum, gsMembers, memberObjs, hms, err := avicache.GetDetailsFromAviGSLB(respElem)
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
			gsCacheObj.HealthMonitor = hms
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
				HealthMonitor:    hms,
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
			HealthMonitor:    hms,
			CloudConfigCksum: cksum,
		}
		restOp.cache.AviCacheAdd(k, &gsCacheObj)
		gslbutils.Logf(spew.Sprintf("key: %s, cacheKey: %v, value: %v, msg: added GS to the cache", key, k,
			utils.Stringify(gsCacheObj)))
	}

	return nil
}

func SyncFromNodesLayer(key interface{}, wg *sync.WaitGroup) error {
	keyStr, ok := key.(string)
	if !ok {
		gslbutils.Errf("unexpected object type: expected string, got %T", key)
		return nil
	}
	cache := avicache.GetAviCache()
	hmCache := avicache.GetAviHmCache()
	aviclient := avicache.SharedAviClients()
	restLayerF := NewRestOperations(cache, hmCache, aviclient)
	gslbutils.Debugf("key: %s, msg: processing for key in rest layer", key)
	restLayerF.DqNodes(keyStr)
	gslbutils.Debugf("key: %s, msg: processing for key is done in rest layer", key)
	return nil
}
