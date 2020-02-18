package nodes

import (
	"sync"

	"gitlab.eng.vmware.com/orion/container-lib/utils"
	filter "gitlab.eng.vmware.com/orion/mcc/gslb/gdp_filter"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
)

type RouteIPHostname struct {
	IP       string
	Hostname string
}

// RouteHostMap stores a mapping between cluster+ns+route to it's hostname
type RouteHostMap struct {
	HostMap map[string]RouteIPHostname
	Lock    sync.Mutex
}

var rhMapInit sync.Once
var rhMap RouteHostMap

func getRouteHostMap() *RouteHostMap {
	rhMapInit.Do(func() {
		rhMap.HostMap = make(map[string]RouteIPHostname)
	})
	return &rhMap
}

func DeriveGSLBServiceName(route gslbutils.RouteMeta) string {
	hostName := route.Hostname
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

func GetRouteTrafficRatio(ns, cname string) int32 {
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
	route := obj.(gslbutils.RouteMeta)
	if route.Hostname == "" {
		gslbutils.Errf("key: %s, msg: %s", key, "no hostname for route object, not supported")
		return
	}
	if route.IPAddr == "" {
		// IP Address not found, no use adding this as a GS
		gslbutils.Errf("key: %s, msg: %s", key, "no IP address found for the route")
		return
	}
	// get the traffic ratio for this member
	memberWeight := GetRouteTrafficRatio(ns, cname)
	gsName := DeriveGSLBServiceName(route)
	modelName := utils.ADMIN_NS + "/" + gsName
	found, aviGS := SharedAviGSGraphLister().Get(modelName)
	if !found {
		gslbutils.Logf("key: %s, modelName: %s, msg: %s", key, modelName, "generating new model")
		aviGS = NewAviGSObjectGraph()
		// Note: For now, the hostname is used as a way to create the GSLB services. This is on the
		// assumption that the hostnames are same for a route across all clusters.
		aviGS.(*AviGSObjectGraph).ConstructAviGSGraph(gsName, key, route.Hostname, route.IPAddr, memberWeight)
	} else {
		// since the object was found, fetch the previous checksum
		prevChecksum = aviGS.(*AviGSObjectGraph).GetChecksum()
		// GSGraph found, so, only need to update the member of the GSGraph's GSNode
		aviGS.(*AviGSObjectGraph).UpdateMember(route.IPAddr, memberWeight)
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
	defer routeHostMap.Lock.Unlock()
	routeHostMap.HostMap[cname+"/"+ns+"/"+objName] = RouteIPHostname{
		IP:       route.IPAddr,
		Hostname: route.Hostname,
	}

	publishKeyToRestLayer(aviGS.(*AviGSObjectGraph), utils.ADMIN_NS, gsName, key, wq)
}

func deleteRouteOperation(key, cname, ns, objName string, wq *utils.WorkerQueue) {
	gslbutils.Logf("key: %s, msg: %s", key, "recieved delete operation for route")
	routeHostMap := getRouteHostMap()
	routeHostMap.Lock.Lock()
	deleteOp := true
	defer routeHostMap.Lock.Unlock()
	clusterRoute := cname + "/" + ns + "/" + objName
	ipHostName, ok := rhMap.HostMap[clusterRoute]
	if !ok {
		gslbutils.Logf("key: %s, msg: %s", key, "no hostname for the route object")
		return
	}
	// Also, now delete this route name from the host map
	gsName := ipHostName.Hostname
	modelName := utils.ADMIN_NS + "/" + ipHostName.Hostname

	found, aviGS := SharedAviGSGraphLister().Get(modelName)
	if found {
		// Check the no. of members in this model, if its the last one, its a delete, else its an update
		if len(aviGS.(*AviGSObjectGraph).Members) > 1 {
			deleteOp = false
		} else {
			deleteOp = true
		}
		if !aviGS.(*AviGSObjectGraph).DeleteMember(ipHostName.IP) {
			// No member found for this route
			gslbutils.Warnf("key: %s, msg: no member for this route", key)
			return
		}
	} else {
		// avi graph not found, return
		gslbutils.Warnf("key: %s, msg: no avi graph found for this key", key)
		return
	}
	delete(routeHostMap.HostMap, clusterRoute)

	if deleteOp {
		// if its a model delete
		SharedAviGSGraphLister().Save(modelName, nil)
		// SharedAviGSGraphLister().Delete(modelName)
		bkt := utils.Bkt(modelName, wq.NumWorkers)
		wq.Workqueue[bkt].AddRateLimited(modelName)
	} else {
		publishKeyToRestLayer(aviGS.(*AviGSObjectGraph), utils.ADMIN_NS, gsName, key, wq)
	}
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
