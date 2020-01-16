package nodes

import (
	routev1 "github.com/openshift/api/route/v1"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
	"sync"
)

// RouteHostMap stores a mapping between cluster+ns+route to it's hostname
type RouteHostMap struct {
	HostMap map[string]string
	Lock    sync.Mutex
}

var rhMapInit sync.Once
var rhMap RouteHostMap

func getRouteHostMap() *RouteHostMap {
	rhMapInit.Do(func() {
		rhMap.HostMap = make(map[string]string)
	})
	return &rhMap
}

func DeriveGSLBServiceName(route *routev1.Route) string {
	hostName := route.Spec.Host
	// For now, the hostname of a route is the GSLB Service name
	return hostName
}

func publishKeyToRestLayer(aviGSGraph *AviGSObjectGraph, tenant, gsName, key string, sharedQueue *utils.WorkerQueue) {
	// First see if there's another instance of the same model in the store
	modelName := tenant + "/" + gsName
	SharedAviGSGraphLister().Save(modelName, aviGSGraph)
	bkt := utils.Bkt(modelName, sharedQueue.NumWorkers)
	sharedQueue.Workqueue[bkt].AddRateLimited(modelName)
	gslbutils.Logf("key: %s, modelName: %s, msg: %s", key, modelName, "published key to rest layer")
}

func addUpdateRouteOperation(key, cname, ns, objName string, wq *utils.WorkerQueue) {
	var prevChecksum, newChecksum uint32
	if gslbutils.AcceptedRouteStore == nil {
		// Error state, the route store is not updated, so we can't do anything here
		gslbutils.Errf("key: %s, msg: %s", key, "accepted route store is empty, can't add route")
		return
	}
	obj, ok := gslbutils.AcceptedRouteStore.GetClusterNSObjectByName(cname, ns, objName)
	if !ok {
		gslbutils.Errf("key: %s, msg: %s", key, "error finding the object in the accepted route store")
		return
	}
	route := obj.(*routev1.Route)
	hostName := route.Spec.Host
	if hostName == "" {
		gslbutils.Errf("key: %s, msg: %s", key, "no hostname for route object, not supported")
		return
	}
	ipAddr, ok := gslbutils.RouteGetIPAddr(route)
	if !ok {
		// IP Address not found, no use adding this as a GS
		gslbutils.Errf("key: %s, msg: %s", key, "no IP address found for the route")
		return
	}
	gsName := DeriveGSLBServiceName(route)
	modelName := utils.ADMIN_NS + "/" + gsName
	found, aviGS := SharedAviGSGraphLister().Get(modelName)
	if !found {
		gslbutils.Logf("key: %s, modelName: %s, msg: %s", key, modelName, "generating new model")
		aviGS = NewAviGSObjectGraph()
		aviGS.(*AviGSObjectGraph).ConstructAviGSNode(gsName, key, hostName, ipAddr)
		aviGS.(*AviGSObjectGraph).BuildAviGSGraph(key, hostName)
	} else {
		// since the object was found, fetch the previous checksum
		prevChecksum = aviGS.(*AviGSObjectGraph).GetChecksum()
		// GSGraph found, so, only need to update the member of the GSGraph's GSNode
		aviGS.(*AviGSObjectGraph).GSNode.UpdateMember(ipAddr)
		newChecksum = aviGS.(*AviGSObjectGraph).GetChecksum()
		if prevChecksum == newChecksum {
			// Checksums are same, return
			gslbutils.Logf("key: %s, model: %s, msg: %s", key, modelName,
				"the model for this key has identical checksums")
			return
		}
	}
	// Update the hostname in the RouteHostMap
	routeHostMap := getRouteHostMap()
	routeHostMap.Lock.Lock()
	routeHostMap.HostMap[cname+"/"+ns+"/"+objName] = hostName
	routeHostMap.Lock.Unlock()

	publishKeyToRestLayer(aviGS.(*AviGSObjectGraph), utils.ADMIN_NS, gsName, key, wq)
}

func deleteRouteOperation(key, cname, ns, objName string, wq *utils.WorkerQueue) {
	gslbutils.Logf("key: %s, msg: %s", key, "recieved delete operation for route")
	routeHostMap := getRouteHostMap()
	routeHostMap.Lock.Lock()
	defer routeHostMap.Lock.Unlock()
	clusterRoute := cname + "/" + ns + "/" + objName
	hostName, ok := rhMap.HostMap[clusterRoute]
	if !ok {
		gslbutils.Logf("key: %s, msg: %s", key, "no hostname for the route object")
		return
	}
	// Also, now delete this route name from the host map
	delete(routeHostMap.HostMap, clusterRoute)
	gsName := ns + "/" + hostName
	SharedAviGSGraphLister().Save(gsName, nil)
	bkt := utils.Bkt(gsName, wq.NumWorkers)
	wq.Workqueue[bkt].AddRateLimited(gsName)
	gslbutils.Logf("key: %s, modelName: %s, msg: %s", key, gsName, "published key to rest layer")
}

func DequeueIngestion(key string) {
	// The key format expected here is: operation/objectType/clusterName/Namespace/objName
	gslbutils.Logf("key: %s, msg: %s", key, "starting graph sync")
	objectOperation, objType, cname, ns, objName := gslbutils.ExtractMultiClusterKey(key)
	sharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	if objType != gslbutils.RouteType {
		gslbutils.Warnf("key: %s, msg: %s", key, "not an acceptable object, can't process")
		return
	}
	switch objectOperation {
	case gslbutils.ObjectAdd:
		addUpdateRouteOperation(key, cname, ns, objName, sharedQueue)
	case gslbutils.ObjectDelete:
		deleteRouteOperation(key, cname, ns, objName, sharedQueue)
	case gslbutils.ObjectUpdate:
		addUpdateRouteOperation(key, cname, ns, objName, sharedQueue)
	}
}

func SyncFromIngestionLayer(key string) error {
	DequeueIngestion(key)
	return nil
}
