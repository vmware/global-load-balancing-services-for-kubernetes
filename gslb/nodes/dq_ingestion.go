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
	"fmt"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/store"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

func DeriveGSLBServiceName(hostname, cname string) string {
	if !gslbutils.GetCustomFqdnMode() {
		return hostname
	}
	fqdnMapping := gslbutils.GetFqdnMap()
	gsFqdn, err := fqdnMapping.GetGlobalFqdnForLocalFqdn(cname, hostname)
	if err != nil {
		gslbutils.Debugf("hostname: %s, msg: no global fqdn for this hostname", hostname)
		return hostname
	}
	return gsFqdn
}

func DeriveGSLBServiceDomainNames(gsName string) []string {
	gsDn := []string{gsName}
	gsDomainNameMap := gslbutils.GetDomainNameMap()
	gsDomainNames, err := gsDomainNameMap.GetDomainNamesForGS(gsName)
	if err != nil {
		gslbutils.Debugf("gsName: %s, msg: %v", gsName, err)
		// return the gsName as the domain names in case of an error
		return gsDn
	}
	gsDn = append(gsDn, gsDomainNames...)
	return gsDn
}

func PublishKeyToRestLayer(tenant, gsName, key string, sharedQueue *utils.WorkerQueue, extraArgs ...string) {
	// First see if there's another instance of the same model in the store
	modelName := tenant + "/" + gsName
	keyForBkt := modelName
	if len(extraArgs) == 1 {
		keyForBkt = tenant + "/" + extraArgs[0]
	}
	bkt := utils.Bkt(keyForBkt, sharedQueue.NumWorkers)
	sharedQueue.Workqueue[bkt].AddRateLimited(modelName)
	gslbutils.Logf("key: %s, modelName: %s, bkt: %d, msg: %s", key, modelName, bkt, "published key to rest layer")
}

func GetObjTrafficRatio(ns, cname string) int32 {
	globalFilter := gslbutils.GetGlobalFilter()
	if globalFilter == nil {
		// return default traffic ratio
		gslbutils.Errf("ns: %s, cname: %s, msg: global filter can't be nil at this stage", ns, cname)
		return 1
	}
	val, err := globalFilter.GetTrafficWeight(cname)
	if err != nil {
		gslbutils.Warnf("ns: %s, cname: %s, msg: error occured while fetching traffic info for this cluster, %s",
			ns, cname, err.Error())
		return 1
	}
	return val
}

func GetObjTrafficPriority(ns, cname string) int32 {
	globalFilter := gslbutils.GetGlobalFilter()
	if globalFilter == nil {
		// return default priority
		gslbutils.Errf("ns: %s, cname: %s, msg: global filter can't be nil at this stage", ns, cname)
		return 10
	}
	val, err := globalFilter.GetTrafficPriority(cname)
	if err != nil {
		gslbutils.Warnf("ns: %s, cname: %s, msg: error occured while fetching traffic priority info for this cluster, %s",
			ns, cname, err.Error())
		return 10
	}
	return val
}

func getObjFromStore(objType, cname, ns, objName, key, storeType string) interface{} {
	var cstore *store.ClusterStore
	switch objType {
	case gslbutils.RouteType:
		if storeType == gslbutils.AcceptedStore {
			cstore = store.GetAcceptedRouteStore()
		} else {
			cstore = store.GetRejectedRouteStore()
		}
		if cstore == nil {
			// Error state, the route store is not updated, so we can't do anything here
			gslbutils.Errf("key: %s, msg: %s", key, "accepted route store is empty, can't add route")
			return nil
		}

	case gslbutils.IngressType:
		if storeType == gslbutils.AcceptedStore {
			cstore = store.GetAcceptedIngressStore()
		} else {
			cstore = store.GetRejectedIngressStore()
		}
		if cstore == nil {
			gslbutils.Errf("key: %s, msg: %s", key, "accepted ingress store is empty, can't add ingress")
			return nil
		}

	case gslbutils.SvcType:
		if storeType == gslbutils.AcceptedStore {
			cstore = store.GetAcceptedLBSvcStore()
		} else {
			cstore = store.GetRejectedLBSvcStore()
		}
		if cstore == nil {
			gslbutils.Errf("key: %s, msg: %s", key, "accepted svc store is empty, can't add svc")
			return nil
		}
	case gslbutils.MCIType:
		if storeType == gslbutils.AcceptedStore {
			cstore = store.GetAcceptedMultiClusterIngressStore()
		} else {
			cstore = store.GetRejectedMultiClusterIngressStore()
		}
		if cstore == nil {
			gslbutils.Errf("key: %s, msg: %s", key, "accepted/rejected multi-cluster ingress store is empty, can't add/delete multi-cluster ingress")
			return nil
		}
	}
	obj, ok := cstore.GetClusterNSObjectByName(cname, ns, objName)
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

func GetHmChecksum(objType string, gsGraph *AviGSObjectGraph) uint32 {
	var checksum uint32
	if objType == gslbutils.SvcType {
		checksum = gsGraph.GetHmChecksum(gsGraph.Hm.GetHMDescription(gsGraph.Name))
	} else {
		description := gsGraph.Hm.GetHMDescription(gsGraph.Name)
		checksum = gsGraph.GetHmChecksum(description)
	}
	return checksum
}

func AddUpdateGSLBHostRuleOperation(key, objType, objName string, wq *utils.WorkerQueue, agl *AviGSGraphLister) {
	modelName := utils.ADMIN_NS + "/" + objName
	found, aviGS := agl.Get(modelName)
	if !found {
		// no existing GS for the GS FQDN
		gslbutils.Logf("key: %s, msg: no GS for the GS FQDN in host rule, will return", key)
		return
	}
	gsGraph := aviGS.(*AviGSObjectGraph)
	prevHmChecksum := GetHmChecksum(objType, gsGraph)
	prevChecksum := gsGraph.GetChecksum()
	// update the GS graph
	aviGS.(*AviGSObjectGraph).UpdateAviGSGraphWithGSFqdn(objName, false, gsGraph.MemberObjs[0].TLS)
	newChecksum := gsGraph.GetChecksum()
	newHmChecksum := GetHmChecksum(objType, gsGraph)
	gslbutils.Debugf("prevChecksum: %d, newChecksum: %d, prevHmChecksum: %d, newHmChecksum: %d, key: %s", prevChecksum,
		newChecksum, prevHmChecksum, newHmChecksum, key)
	if (prevChecksum == newChecksum) && (prevHmChecksum == newHmChecksum) {
		// Checksums are same, return
		gslbutils.Debugf(spew.Sprintf("key: %s, gsName: %s, model: %v, msg: %s", key, objName, *gsGraph,
			"the model for this key has identical checksums"))
		return
	}
	aviGS.(*AviGSObjectGraph).SetRetryCounter()
	gslbutils.Debugf(spew.Sprintf("key: %s, gsName: %s, model: %v, msg: %s", key, objName, *gsGraph,
		"updated the model"))
	agl.Save(modelName, aviGS.(*AviGSObjectGraph))

	PublishKeyToRestLayer(utils.ADMIN_NS, objName, key, wq)
}

type memberFqdnList struct {
	memberFqdnMap map[string]string
	Lock          sync.RWMutex
}

var memberFqdns *memberFqdnList
var memberFqdnSyncOnce sync.Once

func GetMemberFqdnMap() *memberFqdnList {
	memberFqdnSyncOnce.Do(func() {
		memberFqdnList := &memberFqdnList{
			memberFqdnMap: make(map[string]string),
		}
		memberFqdns = memberFqdnList
	})
	return memberFqdns
}

func UpdateMemberFqdnMapping(metaObj k8sobjects.MetaObject, hostname, gsName string) {
	fqdnMap := GetMemberFqdnMap()
	key := metaObj.GetCluster() + "/" + metaObj.GetNamespace() + "/" + metaObj.GetName() + "/" + hostname
	fqdnMap.Lock.Lock()
	defer fqdnMap.Lock.Unlock()
	fqdnMap.memberFqdnMap[key] = gsName
}

func GSNameForMemberFqdn(cname, ns, objName, hostname string) (string, error) {
	fqdnMap := GetMemberFqdnMap()
	key := cname + "/" + ns + "/" + objName + "/" + hostname
	fqdnMap.Lock.RLock()
	defer fqdnMap.Lock.RUnlock()
	v, ok := fqdnMap.memberFqdnMap[key]
	if !ok {
		return "", fmt.Errorf("no GS name for this object %s", key)
	}
	return v, nil
}

func DeleteMemberFqdnMapping(metaObj k8sobjects.MetaObject, hostname, gsName string) {
	fqdnMap := GetMemberFqdnMap()
	key := metaObj.GetCluster() + "/" + metaObj.GetNamespace() + "/" + metaObj.GetName() + "/" + hostname
	fqdnMap.Lock.Lock()
	defer fqdnMap.Lock.Unlock()
	delete(fqdnMap.memberFqdnMap, key)
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
	gsName := DeriveGSLBServiceName(metaObj.GetHostname(), metaObj.GetCluster())
	gsDomainNames := DeriveGSLBServiceDomainNames(gsName)
	UpdateMemberFqdnMapping(metaObj, metaObj.GetHostname(), gsName)
	modelName := utils.ADMIN_NS + "/" + gsName
	found, aviGS := agl.Get(modelName)
	if !found {
		gslbutils.Logf("key: %s, modelName: %s, msg: %s", key, modelName, "generating new model")
		aviGS = NewAviGSObjectGraph()
		// Note: For now, the hostname is used as a way to create the GSLB services. This is on the
		// assumption that the hostnames are same for a route across all clusters.
		aviGS.(*AviGSObjectGraph).ConstructAviGSGraphFromMeta(gsName, key, metaObj, gsDomainNames)
		gslbutils.Debugf(spew.Sprintf("key: %s, gsName: %s, model: %v, msg: constructed new model", key, modelName,
			*(aviGS.(*AviGSObjectGraph))))
		agl.Save(modelName, aviGS.(*AviGSObjectGraph))
	} else {
		gsGraph := aviGS.(*AviGSObjectGraph)
		prevHmChecksum := GetHmChecksum(objType, gsGraph)
		// since the object was found, fetch the current checksum
		prevChecksum = gsGraph.GetChecksum()
		aviGS.(*AviGSObjectGraph).UpdateGSMemberFromMetaObj(metaObj, gsDomainNames)
		// Get the new checksum after the updates
		newChecksum = gsGraph.GetChecksum()
		newHmChecksum := GetHmChecksum(objType, gsGraph)

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
	case gslbutils.MCIType:
		return k8sobjects.MultiClusterIngressHostMeta{}, nil
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

	gsFqdn, err := GSNameForMemberFqdn(cname, ns, objName, hostname)
	if err != nil {
		gslbutils.Logf("key: %s, msg: no GS for the %s object: %v", key, objType, err)
	}
	DeleteMemberFqdnMapping(metaObj, hostname, gsFqdn)

	gsName := gsFqdn
	modelName := utils.ADMIN_NS + "/" + gsFqdn

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
		gslbutils.Debugf("key: %s, gsMembers: %v, msg: checking if its a GS deletion case", key,
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

func OperateOnK8sObject(key string) {
	objectOperation, objType, cname, ns, objName := gslbutils.ExtractMultiClusterKey(key)
	sharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	switch objectOperation {
	case gslbutils.ObjectAdd:
		AddUpdateObjOperation(key, cname, ns, objType, objName, sharedQueue, false, SharedAviGSGraphLister())
	case gslbutils.ObjectDelete:
		deleteObjOperation(key, cname, ns, objType, objName, sharedQueue)
	case gslbutils.ObjectUpdate:
		AddUpdateObjOperation(key, cname, ns, objType, objName, sharedQueue, false, SharedAviGSGraphLister())
	}

}

func OperateOnGSLBHostRule(key string) {
	_, objType, objName, err := gslbutils.ExtractGSLBHostRuleKey(key)
	if err != nil {
		gslbutils.Errf("key: %s, msg: couldn't parse the key for GSLBHostRule: %v", key, err)
		return
	}
	sharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	AddUpdateGSLBHostRuleOperation(key, objType, objName, sharedQueue, SharedAviGSGraphLister())
}

func DeleteGSOrGSMembers(aviGSGraph *AviGSObjectGraph, members []AviGSK8sObj, modelName string, agl *AviGSGraphLister,
	sharedQ *utils.WorkerQueue, key string) {
	gsName := aviGSGraph.Name
	for _, m := range members {
		aviGSGraph.DeleteMember(m.Cluster, m.Namespace, m.Name, m.ObjType)
	}
	if len(aviGSGraph.GetUniqueMemberObjs()) == 0 {
		SharedDeleteGSGraphLister().Save(modelName, aviGSGraph)
		agl.Delete(modelName)
	} else {
		agl.Save(gsName, aviGSGraph)
	}
	PublishKeyToRestLayer(utils.ADMIN_NS, gsName, key, sharedQ)
}

func DeleteAndAddGSGraphForFqdn(agl *AviGSGraphLister, oldFqdn, newFqdn, key, cname string) {
	sharedQ := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)

	// a Global Fqdn for a Local Fqdn was added, see if we need to delete the GS for local fqdn first
	var members []AviGSK8sObj
	modelName := utils.ADMIN_NS + "/" + oldFqdn
	found, aviGS := agl.Get(modelName)
	if found {
		aviGSGraph := aviGS.(*AviGSObjectGraph)
		// an existing GS Graph was found for the local fqdn, delete member(s) or the GS graph.
		gslbutils.Logf("key: %s, modelName: %s, msg: GS graph for fqdn %s exists, will evaluate the members",
			key, modelName, oldFqdn)
		members = aviGSGraph.GetGSMembersByCluster(cname)
		DeleteGSOrGSMembers(aviGSGraph, members, modelName, agl, sharedQ, key)
	}
	if len(members) == 0 {
		// there are no members which can be added to the new GS, return
		gslbutils.Logf("key: %s, oldFqdn: %s, newFqdn: %s, msg: no pending members to be added",
			key, oldFqdn, newFqdn)
		return
	}

	// a new GS object needs to be created or updated for the global fqdn
	newModelName := utils.ADMIN_NS + "/" + newFqdn
	found, aviGS = agl.Get(newModelName)
	if found {
		gslbutils.Logf("key: %s, gsName: %s, msg: GS for fqdn %s already exists, will update", key,
			aviGS.(*AviGSObjectGraph).Name, newFqdn)
		aviGSGraph := aviGS.(*AviGSObjectGraph)
		for _, m := range members {
			deleteMember := aviGSGraph.AddUpdateGSMember(m)
			if deleteMember {
				aviGSGraph.DeleteMember(m.Cluster, m.Namespace, m.Name, m.ObjType)
			}
		}
		agl.Save(newFqdn, aviGS.(*AviGSObjectGraph))
		PublishKeyToRestLayer(utils.ADMIN_NS, newFqdn, key, sharedQ, oldFqdn)
		return
	}

	aviGS = NewAviGSObjectGraph()
	aviGSGraph := aviGS.(*AviGSObjectGraph)
	aviGSGraph.ConstructAviGSGraphFromObjects(newFqdn, members, key)
	gslbutils.Debugf(spew.Sprintf("key: %s, gsName: %s, model: %v, msg: constructed new model", key, modelName,
		*(aviGSGraph)))
	agl.Save(newModelName, aviGS.(*AviGSObjectGraph))
	PublishKeyToRestLayer(utils.ADMIN_NS, newFqdn, key, sharedQ, oldFqdn)
}

func OperateOnHostRule(key string) {
	agl := SharedAviGSGraphLister()
	op, _, cname, _, lfqdn, gfqdn, err := gslbutils.ExtractMultiClusterHostRuleKey(key)
	if err != nil {
		gslbutils.Errf("key: %s, msg: couldn't parse the key for HostRule: %v", key, err)
		return
	}

	switch op {
	case gslbutils.ObjectAdd:
		// TODO: Might be unneccessary code
		fqdnMap := gslbutils.GetFqdnMap()
		fqdnMap.AddUpdateToFqdnMapping(gfqdn, lfqdn, cname)
		DeleteAndAddGSGraphForFqdn(agl, lfqdn, gfqdn, key, cname)

	case gslbutils.ObjectUpdate:
		fqdnMap := gslbutils.GetFqdnMap()
		// for an update operation, the fqdns have different meaning
		prevFqdn := lfqdn
		newFqdn := gfqdn
		lFqdnObjs, err := fqdnMap.GetLocalFqdnsForGlobalFqdn(prevFqdn)
		if err != nil {
			gslbutils.Logf("key: %s, cluster: %s, prevFqdn: %s, msg: error in fetching local fqdn mapping",
				key, cname, prevFqdn)
		}
		for _, f := range lFqdnObjs {
			if f.Cluster == cname {
				fqdnMap.DeleteFromFqdnMapping(prevFqdn, f.Fqdn, f.Cluster)
				fqdnMap.AddUpdateToFqdnMapping(newFqdn, f.Fqdn, f.Cluster)
			}
		}
		DeleteAndAddGSGraphForFqdn(agl, prevFqdn, newFqdn, key, cname)

	case gslbutils.ObjectDelete:
		fqdnMap := gslbutils.GetFqdnMap()
		fqdnMap.DeleteFromFqdnMapping(gfqdn, lfqdn, cname)
		DeleteAndAddGSGraphForFqdn(agl, gfqdn, lfqdn, key, cname)

	default:
		gslbutils.Errf("key: %s, msg: invalid HostRule operation: %s", key, op)
	}
}

func DequeueIngestion(key string) {
	// The key format expected here is: operation/objectType/clusterName/Namespace/objName
	gslbutils.Logf("key: %s, msg: %s", key, "starting graph sync")
	objType, err := gslbutils.GetObjectTypeFromKey(key)
	if err != nil {
		gslbutils.Errf("key: %s, msg: couldn't fetch the object type from key: %v", key, err)
		return
	}
	switch objType {
	case gslbutils.RouteType, gslbutils.IngressType, gslbutils.SvcType, gslbutils.MCIType:
		OperateOnK8sObject(key)
	case gslbutils.GSFQDNType:
		OperateOnGSLBHostRule(key)
	case gslbutils.HostRuleType:
		OperateOnHostRule(key)
	default:
		gslbutils.Errf("key: %s, msg: invalid object derived from key, won't process")
	}
}

func SyncFromIngestionLayer(key interface{}, wg *sync.WaitGroup) error {
	keyStr, ok := key.(string)
	if !ok {
		gslbutils.Errf("unexpected object type: expected string, got %T", key)
		return nil
	}
	DequeueIngestion(keyStr)
	return nil
}
