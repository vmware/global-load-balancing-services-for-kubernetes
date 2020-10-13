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

package nodes

import (
	"errors"
	"sync"

	"github.com/avinetworks/amko/gslb/gslbutils"
	"github.com/avinetworks/amko/gslb/k8sobjects"
	"github.com/davecgh/go-spew/spew"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

func DeriveGSLBServiceName(hostname string) string {
	// This function is a place-holder for deriving the GSLB service name
	// For now, the hostname of a route is the GSLB Service name
	return hostname
}

func PublishKeyToRestLayer(tenant, gsName, key string, sharedQueue *utils.WorkerQueue) {
	// First see if there's another instance of the same model in the store
	modelName := tenant + "/" + gsName
	bkt := utils.Bkt(modelName, sharedQueue.NumWorkers)
	sharedQueue.Workqueue[bkt].AddRateLimited(modelName)
	gslbutils.Logf("key: %s, modelName: %s, msg: %s", key, modelName, "published key to rest layer")
}

func GetObjTrafficRatio(ns, cname string) int32 {
	globalFilter := gslbutils.GetGlobalFilter()
	if globalFilter == nil {
		// return default traffic ratio
		gslbutils.Errf("ns: %s, cname: %s, msg: global filter can't be nil at this stage", ns, cname)
		return 1
	}
	val, err := globalFilter.GetTrafficWeight(ns, cname)
	if err != nil {
		gslbutils.Warnf("ns: %s, cname: %s, msg: error occured while fetching traffic info for this cluster, %s",
			ns, cname, err.Error())
		return 1
	}
	return val
}

func getObjFromStore(objType, cname, ns, objName, key, storeType string) interface{} {
	var store *gslbutils.ClusterStore
	switch objType {
	case gslbutils.RouteType:
		if storeType == gslbutils.AcceptedStore {
			store = gslbutils.GetAcceptedRouteStore()
		} else {
			store = gslbutils.GetRejectedRouteStore()
		}
		if store == nil {
			// Error state, the route store is not updated, so we can't do anything here
			gslbutils.Errf("key: %s, msg: %s", key, "accepted route store is empty, can't add route")
			return nil
		}
		break
	case gslbutils.IngressType:
		if storeType == gslbutils.AcceptedStore {
			store = gslbutils.GetAcceptedIngressStore()
		} else {
			store = gslbutils.GetRejectedIngressStore()
		}
		if store == nil {
			gslbutils.Errf("key: %s, msg: %s", key, "accepted ingress store is empty, can't add ingress")
			return nil
		}
		break

	case gslbutils.SvcType:
		if storeType == gslbutils.AcceptedStore {
			store = gslbutils.GetAcceptedLBSvcStore()
		} else {
			store = gslbutils.GetRejectedLBSvcStore()
		}
		if store == nil {
			gslbutils.Errf("key: %s, msg: %s", key, "accepted svc store is empty, can't add svc")
			return nil
		}
		break
	}
	obj, ok := store.GetClusterNSObjectByName(cname, ns, objName)
	if !ok {
		gslbutils.Warnf("key: %s, objName: %s, msg: error finding the object in the %s store", key,
			objName, storeType)
		return nil
	}
	return obj
}

func PublishAllGraphKeys() {
	agl := SharedAviGSGraphLister()
	keys := agl.GetAll()
	sharedQ := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	for _, key := range keys {
		bkt := utils.Bkt(key, sharedQ.NumWorkers)
		sharedQ.Workqueue[bkt].AddRateLimited(key)
		gslbutils.Logf("process: resyncNodes, modelName: %s, msg: published key to rest layer", key)
	}
}

func AddUpdateObjOperation(key, cname, ns, objType, objName string, wq *utils.WorkerQueue,
	fullSync bool, agl *AviGSGraphLister) {

	var prevChecksum, newChecksum uint32
	obj := getObjFromStore(objType, cname, ns, objName, key, gslbutils.AcceptedStore)
	if obj == nil {
		// error message already logged in the above function
		return
	}
	metaObj := obj.(k8sobjects.MetaObject)
	if metaObj.GetHostname() == "" {
		gslbutils.Errf("key: %s, msg: %s", key, "no hostname for object, not supported")
		return
	}
	if metaObj.GetIPAddr() == "" {
		// IP Address not found, no use adding this as a GS
		gslbutils.Errf("key: %s, msg: %s", key, "no IP address found for the object")
		return
	}
	// get the traffic ratio for this member
	memberWeight := GetObjTrafficRatio(ns, cname)
	gsName := DeriveGSLBServiceName(metaObj.GetHostname())
	modelName := utils.ADMIN_NS + "/" + gsName
	found, aviGS := agl.Get(modelName)
	if !found {
		gslbutils.Logf("key: %s, modelName: %s, msg: %s", key, modelName, "generating new model")
		aviGS = NewAviGSObjectGraph()
		// Note: For now, the hostname is used as a way to create the GSLB services. This is on the
		// assumption that the hostnames are same for a route across all clusters.
		aviGS.(*AviGSObjectGraph).ConstructAviGSGraph(gsName, key, metaObj, memberWeight)
		gslbutils.Debugf(spew.Sprintf("key: %s, gsName: %s, model: %v, msg: constructed new model", key, modelName,
			*(aviGS.(*AviGSObjectGraph))))
		agl.Save(modelName, aviGS.(*AviGSObjectGraph))
	} else {
		gsGraph := aviGS.(*AviGSObjectGraph)
		prevHmChecksum := gsGraph.GetHmChecksum()
		// since the object was found, fetch the current checksum
		prevChecksum = gsGraph.GetChecksum()
		// GSGraph found, so, only need to update the member of the GSGraph's GSNode
		aviGS.(*AviGSObjectGraph).UpdateGSMember(metaObj, memberWeight)
		// Get the new checksum after the updates
		newChecksum = gsGraph.GetChecksum()
		newHmChecksum := gsGraph.GetHmChecksum()

		gslbutils.Debugf("prevChecksum: %d, newChecksum: %d, prevHmChecksum: %d, newHmChecksum: %d, key: %s", prevChecksum,
			newChecksum, prevHmChecksum, newHmChecksum, key)

		if (prevChecksum == newChecksum) && (prevHmChecksum == newHmChecksum) {
			// Checksums are same, return
			gslbutils.Debugf(spew.Sprintf("key: %s, gsName: %s, model: %v, msg: %s", key, gsName, *gsGraph,
				"the model for this key has identical checksums"))
			return
		}
		aviGS.(*AviGSObjectGraph).SetRetryCounter()
		gslbutils.Debugf(spew.Sprintf("key: %s, gsName: %s, model: %v, msg: %s", key, gsName, *gsGraph,
			"updated the model"))
		agl.Save(modelName, aviGS.(*AviGSObjectGraph))
	}
	// Update the hostname in the RouteHostMap
	metaObj.UpdateHostMap(cname + "/" + ns + "/" + objName)

	if !fullSync || gslbutils.IsControllerLeader() {

		PublishKeyToRestLayer(utils.ADMIN_NS, gsName, key, wq)
	}
}

func GetNewObj(objType string) (k8sobjects.MetaObject, error) {
	switch objType {
	case gslbutils.RouteType:
		return k8sobjects.RouteMeta{}, nil
	case gslbutils.IngressType:
		return k8sobjects.IngressHostMeta{}, nil
	case gslbutils.SvcType:
		return k8sobjects.SvcMeta{}, nil
	default:
		return nil, errors.New("unrecognised object: " + objType)
	}
}

func deleteObjOperation(key, cname, ns, objType, objName string, wq *utils.WorkerQueue) {
	gslbutils.Logf("key: %s, objType: %s, msg: %s", key, objType, "recieved delete operation for object")

	metaObj, err := GetNewObj(objType)
	if err != nil {
		gslbutils.Errf("key: %s, msg: %s", key, err.Error())
		return
	}

	clusterObj := cname + "/" + ns + "/" + objName
	// TODO: revisit this section to see if we really need this, or can we make do with metaObj
	hostname := metaObj.GetHostnameFromHostMap(clusterObj)
	if hostname == "" {
		gslbutils.Logf("key: %s, msg: no hostname for the %s object", key, objType)
		return
	}
	gsName := hostname
	modelName := utils.ADMIN_NS + "/" + hostname

	deleteGs := false
	agl := SharedAviGSGraphLister()
	found, aviGS := agl.Get(modelName)
	if found {
		if aviGS == nil {
			gslbutils.Warnf("key: %s, msg: no avi graph found for this key", key)
			return
		}
		uniqueMembersLen := len(aviGS.(*AviGSObjectGraph).GetUniqueMemberObjs())
		aviGS.(*AviGSObjectGraph).DeleteMember(cname, ns, objName, objType)
		// delete the obj from the hostname map
		newUniqueMemberLen := len(aviGS.(*AviGSObjectGraph).GetUniqueMemberObjs())
		if uniqueMembersLen != newUniqueMemberLen {
			metaObj.DeleteMapByKey(clusterObj)
		}
		gslbutils.Debugf("key: %s, gsMembers: %d, msg: checking if its a GS deletion case", key,
			aviGS.(*AviGSObjectGraph).GetUniqueMemberObjs())
		if len(aviGS.(*AviGSObjectGraph).GetUniqueMemberObjs()) == 0 {
			deleteGs = true
		}
	} else {
		// avi graph not found, return
		gslbutils.Warnf("key: %s, msg: no gs key found in gs models", key)
		return
	}
	aviGS.(*AviGSObjectGraph).SetRetryCounter()
	if deleteGs {
		// add the object to the delete cache and remove from the model cache
		SharedDeleteGSGraphLister().Save(modelName, aviGS)
		SharedAviGSGraphLister().Delete(modelName)
	} else {
		SharedAviGSGraphLister().Save(modelName, aviGS)
	}
	if gslbutils.IsControllerLeader() {
		PublishKeyToRestLayer(utils.ADMIN_NS, gsName, key, wq)
	}
}

func isAcceptableObject(objType string) bool {
	return objType == gslbutils.RouteType || objType == gslbutils.IngressType || objType == gslbutils.SvcType
}

func DequeueIngestion(key string) {
	// The key format expected here is: operation/objectType/clusterName/Namespace/objName
	gslbutils.Logf("key: %s, msg: %s", key, "starting graph sync")
	objectOperation, objType, cname, ns, objName := gslbutils.ExtractMultiClusterKey(key)
	sharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	if !isAcceptableObject(objType) {
		gslbutils.Warnf("key: %s, msg: %s", key, "not an acceptable object, can't process")
		return
	}
	switch objectOperation {
	case gslbutils.ObjectAdd:
		AddUpdateObjOperation(key, cname, ns, objType, objName, sharedQueue, false, SharedAviGSGraphLister())
	case gslbutils.ObjectDelete:
		deleteObjOperation(key, cname, ns, objType, objName, sharedQueue)
	case gslbutils.ObjectUpdate:
		AddUpdateObjOperation(key, cname, ns, objType, objName, sharedQueue, false, SharedAviGSGraphLister())
	}
}

func SyncFromIngestionLayer(key string, wg *sync.WaitGroup) error {
	DequeueIngestion(key)
	return nil
}
