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

package nodes

import (
	filter "amko/gslb/gdp_filter"
	"amko/gslb/gslbutils"
	"amko/gslb/k8sobjects"

	"github.com/avinetworks/container-lib/utils"
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
	globalFilter := filter.GetGlobalFilter()
	if globalFilter == nil {
		// return default traffic ratio
		gslbutils.Errf("ns: %s, cname: %s, msg: global filter can't be nil at this stage", ns, cname)
		return 1
	}
	val := globalFilter.GetTrafficWeight(ns, cname)
	if val < 0 {
		gslbutils.Warnf("ns: %s, cname: %s, msg: traffic weight wasn't defined for this object", ns, cname)
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
		gslbutils.Warnf("key: %s, objName: %s, msg: %s", key, objName,
			"error finding the object in the accepted store")
		return nil
	}
	return obj
}

func AddUpdateObjOperation(key, cname, ns, objType, objName string, wq *utils.WorkerQueue, fullSync bool, agl *AviGSGraphLister) {
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
		agl.Save(modelName, aviGS.(*AviGSObjectGraph))
	} else {
		// since the object was found, fetch the current checksum
		prevChecksum = aviGS.(*AviGSObjectGraph).GetChecksum()
		// GSGraph found, so, only need to update the member of the GSGraph's GSNode
		aviGS.(*AviGSObjectGraph).UpdateGSMember(metaObj, memberWeight)
		// Get the new checksum after the updates
		newChecksum = aviGS.(*AviGSObjectGraph).GetChecksum()
		if prevChecksum == newChecksum {
			// Checksums are same, return
			gslbutils.Logf("key: %s, model: %s, msg: %s", key, modelName,
				"the model for this key has identical checksums")
			return
		}
		agl.Save(modelName, aviGS.(*AviGSObjectGraph))
	}
	// Update the hostname in the RouteHostMap
	metaObj.UpdateHostMap(cname + "/" + ns + "/" + objName)

	if !fullSync {
		PublishKeyToRestLayer(utils.ADMIN_NS, gsName, key, wq)
	}
}

func deleteObjOperation(key, cname, ns, objType, objName string, wq *utils.WorkerQueue) {
	gslbutils.Logf("key: %s, msg: %s", key, "recieved delete operation for route")

	deleteOp := true
	clusterObj := cname + "/" + ns + "/" + objName
	obj := getObjFromStore(objType, cname, ns, objName, key, gslbutils.AcceptedStore)
	if obj == nil {
		obj = getObjFromStore(objType, cname, ns, objName, key, gslbutils.RejectedStore)
		if obj == nil {
			gslbutils.Errf("key: %s, msg: %s", key, "error finding the object in the accepted/rejected route store")
			return
		}
	}
	metaObj := obj.(k8sobjects.MetaObject)
	// TODO: revisit this section to see if we really need this, or can we make do with metaObj
	hostName := metaObj.GetHostnameFromHostMap(clusterObj)
	if hostName == "" {
		gslbutils.Logf("key: %s, msg: no hostname for the %s object", key, objType)
		return
	}
	gsName := hostName
	modelName := utils.ADMIN_NS + "/" + hostName

	agl := SharedAviGSGraphLister()
	found, aviGS := agl.Get(modelName)
	if found {
		// Check the no. of members in this model, if its the last one, its a delete, else its an update
		if aviGS.(*AviGSObjectGraph).MembersLen() > 1 {
			deleteOp = false
		} else {
			deleteOp = true
		}
		aviGS.(*AviGSObjectGraph).DeleteMember(metaObj.GetCluster(), metaObj.GetNamespace(),
			metaObj.GetName(), metaObj.GetType())
	} else {
		// avi graph not found, return
		gslbutils.Warnf("key: %s, msg: no avi graph found for this key", key)
		return
	}
	// Also, now delete this route name from the host map
	metaObj.DeleteMapByKey(clusterObj)

	if deleteOp {
		// if its a model delete
		SharedAviGSGraphLister().Save(modelName, nil)
		// SharedAviGSGraphLister().Delete(modelName)
		bkt := utils.Bkt(modelName, wq.NumWorkers)
		wq.Workqueue[bkt].AddRateLimited(modelName)
	} else {
		SharedAviGSGraphLister().Save(modelName, aviGS.(*AviGSObjectGraph))
		PublishKeyToRestLayer(utils.ADMIN_NS, gsName, key, wq)
	}
	gslbutils.Logf("key: %s, modelName: %s, msg: %s", key, gsName, "published key to rest layer")
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

func SyncFromIngestionLayer(key string) error {
	DequeueIngestion(key)
	return nil
}
