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

package ingestion

import (
	filter "amko/gslb/gdp_filter"
	"amko/gslb/gslbutils"
	"amko/gslb/k8sobjects"

	"github.com/avinetworks/container-lib/utils"
	containerutils "github.com/avinetworks/container-lib/utils"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func AddLBSvcEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {
	acceptedLBSvcStore := gslbutils.GetAcceptedLBSvcStore()
	rejectedLBSvcStore := gslbutils.GetRejectedLBSvcStore()
	gf := filter.GetGlobalFilter()
	gslbutils.Logf("Adding svc handler")
	svcEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*corev1.Service)
			// Don't add this svc if this is not of type LB,
			// or if no IP is allocated it's status
			if !isSvcTypeLB(svc) {
				gslbutils.Logf("cluster: %s, ns:%s, svc %s, msg: type not lb", c.name, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
				return
			}
			svcMeta, ok := k8sobjects.GetSvcMeta(svc, c.name)
			if !ok {
				gslbutils.Logf("cluster: %s, msg: could not get meta object for service: %s, ns: %s",
					c.name, svc.ObjectMeta.Name, svc.ObjectMeta.Namespace)
				return
			}
			if gf == nil || !gf.ApplyFilter(svcMeta, c.name) {
				AddOrUpdateLBSvcStore(rejectedLBSvcStore, svc, c.name)
				gslbutils.Logf("cluster: %s, ns: %s, svc: %s, msg: %s\n", c.name,
					svc.ObjectMeta.Namespace, svc.ObjectMeta.Name, "rejected ADD svc key because it couldn't pass through filter")
				return
			}
			AddOrUpdateLBSvcStore(acceptedLBSvcStore, svc, c.name)
			publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, svc.ObjectMeta.Namespace,
				svc.ObjectMeta.Name, gslbutils.ObjectAdd, c.workqueue)
		},
		DeleteFunc: func(obj interface{}) {
			svc, ok := obj.(*corev1.Service)
			if !ok {
				containerutils.AviLog.Errorf("object type is not Svc")
				return
			}
			if !isSvcTypeLB(svc) {
				return
			}
			DeleteFromLBSvcStore(acceptedLBSvcStore, svc, c.name)
			DeleteFromLBSvcStore(rejectedLBSvcStore, svc, c.name)
			publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, svc.ObjectMeta.Namespace,
				svc.ObjectMeta.Name, gslbutils.ObjectDelete, c.workqueue)
			return
		},
		UpdateFunc: func(old, curr interface{}) {
			oldSvc := old.(*corev1.Service)
			svc := curr.(*corev1.Service)
			if oldSvc.ResourceVersion != svc.ResourceVersion {
				svcMeta, ok := k8sobjects.GetSvcMeta(svc, c.name)
				if !ok || gf == nil || !isSvcTypeLB(svc) || !gf.ApplyFilter(svcMeta, c.name) {
					// See if the svc was already accepted, if yes, need to delete the key
					fetchedObj, ok := acceptedLBSvcStore.GetClusterNSObjectByName(c.name,
						oldSvc.ObjectMeta.Namespace, oldSvc.ObjectMeta.Name)
					if !ok {
						// Nothing to be done, just add to the rejected svc store
						AddOrUpdateLBSvcStore(rejectedLBSvcStore, svc, c.name)
						return
					}
					// Else, move this svc from accepted to rejected store, and add
					// a key for this svc to the queue
					multiClusterSvcName := c.name + "/" + svc.ObjectMeta.Namespace + "/" + svc.ObjectMeta.Name
					MoveObjs([]string{multiClusterSvcName}, acceptedLBSvcStore, rejectedLBSvcStore, gslbutils.SvcType)
					fetchedSvc := fetchedObj.(k8sobjects.SvcMeta)
					// Add a DELETE key for this svc
					publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, fetchedSvc.Namespace,
						fetchedSvc.Name, gslbutils.ObjectDelete, c.workqueue)
					return
				}
				AddOrUpdateLBSvcStore(acceptedLBSvcStore, svc, c.name)
				// If the svc was already part of rejected store, we need to remove from
				// this svc from the rejected store.
				rejectedLBSvcStore.DeleteClusterNSObj(c.name, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
				// Add the key for this svc to the queue.
				publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, svc.ObjectMeta.Namespace,
					svc.ObjectMeta.Name, gslbutils.ObjectUpdate, c.workqueue)
			}
		},
	}
	return svcEventHandler
}

func filterAndAddIngressMeta(ingressHostMetaObjs []k8sobjects.IngressHostMeta, c *GSLBMemberController, gf *filter.GlobalFilter,
	acceptedIngStore, rejectedIngStore *gslbutils.ClusterStore, numWorkers uint32) {
	for _, ihm := range ingressHostMetaObjs {
		if ihm.IPAddr == "" || ihm.Hostname == "" {
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s\n",
				c.name, ihm.Namespace, ihm.IngName,
				"rejected ADD ingress because IP address/Hostname not found in status field")
			continue
		}
		if gf == nil || !gf.ApplyFilter(ihm, c.name) {
			AddOrUpdateIngressStore(rejectedIngStore, ihm, c.name)
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s, ing: %v\n", c.name, ihm.Namespace,
				ihm.ObjName, "rejected ADD ingress key because it couldn't pass through the filter", ihm)
			continue
		}
		AddOrUpdateIngressStore(acceptedIngStore, ihm, c.name)
		publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name,
			ihm.Namespace, ihm.ObjName, gslbutils.ObjectAdd, c.workqueue)
	}
}

func deleteIngressMeta(ingressHostMetaObjs []k8sobjects.IngressHostMeta, c *GSLBMemberController, acceptedIngStore,
	rejectedIngStore *gslbutils.ClusterStore, numWorkers uint32) {
	for _, ihm := range ingressHostMetaObjs {
		DeleteFromIngressStore(acceptedIngStore, ihm, c.name)
		DeleteFromIngressStore(rejectedIngStore, ihm, c.name)
		publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name,
			ihm.Namespace, ihm.ObjName, gslbutils.ObjectDelete, c.workqueue)
	}
}

func filterAndUpdateIngressMeta(oldIngMetaObjs, newIngMetaObjs []k8sobjects.IngressHostMeta, c *GSLBMemberController,
	gf *filter.GlobalFilter, acceptedIngStore, rejectedIngStore *gslbutils.ClusterStore, numWorkers uint32) {

	for _, ihm := range oldIngMetaObjs {
		// Check whether this exists in the new ingressHost list, if not, we need
		// to delete this ingressHost object
		newIhm, found := ihm.IngressHostInList(newIngMetaObjs)
		if !found {
			// ingressHost doesn't exist anymore, delete this ingressHost object
			_, isAccepted := acceptedIngStore.GetClusterNSObjectByName(c.name, ihm.Namespace,
				ihm.ObjName)
			DeleteFromIngressStore(acceptedIngStore, ihm, c.name)
			DeleteFromIngressStore(rejectedIngStore, ihm, c.name)
			// If part of accepted store, only then publish the delete key
			if isAccepted {
				publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name,
					ihm.Namespace, ihm.ObjName, gslbutils.ObjectDelete, c.workqueue)
			}
			continue
		}
		// ingressHost exists, check if that got updated
		if ihm.GetIngressHostCksum() == newIhm.GetIngressHostCksum() {
			// no changes, just continue
			continue
		}
		// there are changes, need to send an update key, but first apply the filter
		if gf == nil || !gf.ApplyFilter(newIhm, c.name) {
			// See if the ingressHost was already accepted, if yes, need to delete the key
			fetchedObj, ok := acceptedIngStore.GetClusterNSObjectByName(c.name,
				ihm.Namespace, ihm.ObjName)
			if !ok {
				// Nothing to be done, just add to the rejected ingress store
				AddOrUpdateIngressStore(rejectedIngStore, newIhm, c.name)
				continue
			}
			// Else, move this ingressHost from accepted to rejected store, and add
			// a delete key for this ingressHost to the queue
			multiClusterIngHostName := newIhm.GetClusterKey()
			MoveObjs([]string{multiClusterIngHostName}, acceptedIngStore, rejectedIngStore, gslbutils.IngressType)
			fetchedIngHost := fetchedObj.(k8sobjects.IngressHostMeta)
			// Add a DELETE key for this ingHost
			publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, fetchedIngHost.Cluster,
				fetchedIngHost.Namespace, fetchedIngHost.ObjName, gslbutils.ObjectDelete,
				c.workqueue)
			continue
		}
		// check if the object existed in the acceptedIngStore
		oper := gslbutils.ObjectAdd
		if _, ok := acceptedIngStore.GetClusterNSObjectByName(c.name, newIhm.Namespace, newIhm.ObjName); ok {
			oper = gslbutils.ObjectUpdate
		}
		// ingHost passed through the filter, need to send an update key
		// if the ingHost was already part of rejected store, we need to move this ingHost
		// from the rejected to accepted store
		AddOrUpdateIngressStore(acceptedIngStore, newIhm, c.name)
		rejectedIngStore.DeleteClusterNSObj(c.name, ihm.Namespace, ihm.GetIngressHostMetaKey())
		// Add the key for this ingHost to the queue
		publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name, ihm.Namespace, ihm.ObjName,
			oper, c.workqueue)
		continue
	}
	// Check if there are any new ingHost objects, if yes, we have to add those
	for _, ihm := range newIngMetaObjs {
		_, found := ihm.IngressHostInList(oldIngMetaObjs)
		if found {
			continue
		}
		// only the new ones will be considered, because the old ones
		// have been taken care of already
		// Add this ingressHost object
		if ihm.IPAddr == "" || ihm.Hostname == "" {
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s",
				c.name, ihm.Namespace, ihm.ObjName,
				"rejected ADD ingress because IP address/Hostname not found in status field")
			continue
		}
		if gf == nil || !gf.ApplyFilter(ihm, c.name) {
			AddOrUpdateIngressStore(rejectedIngStore, ihm, c.name)
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s\n", c.name, ihm.Namespace,
				ihm.ObjName, "rejected ADD ingress key because it couldn't pass through the filter")
			continue
		}
		AddOrUpdateIngressStore(acceptedIngStore, ihm, c.name)
		publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name,
			ihm.Namespace, ihm.ObjName, gslbutils.ObjectAdd, c.workqueue)
		continue
	}
}

func AddIngressEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {
	gf := filter.GetGlobalFilter()
	acceptedIngStore := gslbutils.GetAcceptedIngressStore()
	rejectedIngStore := gslbutils.GetRejectedIngressStore()

	gslbutils.Logf("Adding Ingress handler")
	ingressEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ingr, ok := utils.ToNetworkingIngress(obj)
			if !ok {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to networking/v1beta1 ingress")
				return
			}
			// Don't add this ingr if there's no status field present or no IP is allocated in this
			// status field
			ingressHostMetaObjs := k8sobjects.GetIngressHostMeta(ingr, c.name)
			filterAndAddIngressMeta(ingressHostMetaObjs, c, gf, acceptedIngStore, rejectedIngStore, numWorkers)
		},
		DeleteFunc: func(obj interface{}) {
			ingr, ok := utils.ToNetworkingIngress(obj)
			if !ok {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to networking/v1beta1 ingress")
				return
			}
			// Delete from all ingress stores
			ingressHostMetaObjs := k8sobjects.GetIngressHostMeta(ingr, c.name)
			deleteIngressMeta(ingressHostMetaObjs, c, acceptedIngStore, rejectedIngStore, numWorkers)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldIngr, okOld := utils.ToNetworkingIngress(old)
			ingr, okNew := utils.ToNetworkingIngress(curr)
			if !okOld || !okNew {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to networking/v1beta1 ingress")
				return
			}
			if oldIngr.ResourceVersion != ingr.ResourceVersion {
				oldIngMetaObjs := k8sobjects.GetIngressHostMeta(oldIngr, c.name)
				newIngMetaObjs := k8sobjects.GetIngressHostMeta(ingr, c.name)
				filterAndUpdateIngressMeta(oldIngMetaObjs, newIngMetaObjs, c, gf, acceptedIngStore, rejectedIngStore,
					numWorkers)
			}
		},
	}
	return ingressEventHandler
}

func AddRouteEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {
	gf := filter.GetGlobalFilter()
	acceptedRouteStore := gslbutils.GetAcceptedRouteStore()
	rejectedRouteStore := gslbutils.GetRejectedRouteStore()
	routeEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			route := obj.(*routev1.Route)
			// Don't add this route if there's no status field present or no IP is allocated in this
			// status field
			// TODO: See if we can change rejectRoute to Graph layer.
			if _, ok := gslbutils.RouteGetIPAddr(route); !ok {
				gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: %s\n", c.name,
					route.ObjectMeta.Namespace, route.ObjectMeta.Name, "rejected ADD route key because IP address not found")
				return
			}
			routeMeta := k8sobjects.GetRouteMeta(route, c.name)
			if gf == nil || !gf.ApplyFilter(routeMeta, c.name) {
				AddOrUpdateRouteStore(rejectedRouteStore, route, c.name)
				gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: %s\n", c.name,
					route.ObjectMeta.Namespace, route.ObjectMeta.Name, "rejected ADD route key because it couldn't pass through filter")
				return
			}
			AddOrUpdateRouteStore(acceptedRouteStore, route, c.name)
			publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, route.ObjectMeta.Namespace,
				route.ObjectMeta.Name, gslbutils.ObjectAdd, c.workqueue)
		},
		DeleteFunc: func(obj interface{}) {
			route, ok := obj.(*routev1.Route)
			if !ok {
				containerutils.AviLog.Errorf("object type is not route")
				return
			}
			// Delete from all route stores
			DeleteFromRouteStore(acceptedRouteStore, route, c.name)
			DeleteFromRouteStore(rejectedRouteStore, route, c.name)
			publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, route.ObjectMeta.Namespace,
				route.ObjectMeta.Name, gslbutils.ObjectDelete, c.workqueue)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldRoute := old.(*routev1.Route)
			route := curr.(*routev1.Route)
			if oldRoute.ResourceVersion != route.ResourceVersion {
				routeMeta := k8sobjects.GetRouteMeta(route, c.name)
				if gf == nil || !gf.ApplyFilter(routeMeta, c.name) {
					// See if the route was already accepted, if yes, need to delete the key
					fetchedObj, ok := acceptedRouteStore.GetClusterNSObjectByName(c.name,
						oldRoute.ObjectMeta.Namespace, oldRoute.ObjectMeta.Name)
					if !ok {
						// Nothing to be done, just add to the rejected route store
						AddOrUpdateRouteStore(rejectedRouteStore, route, c.name)
						return
					}
					// Else, move this route from accepted to rejected store, and add
					// a key for this route to the queue
					multiClusterRouteName := c.name + "/" + route.ObjectMeta.Namespace + "/" + route.ObjectMeta.Name
					MoveObjs([]string{multiClusterRouteName}, acceptedRouteStore, rejectedRouteStore, "Route")
					fetchedRoute := fetchedObj.(k8sobjects.RouteMeta)
					// Add a DELETE key for this route
					publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, fetchedRoute.Namespace,
						fetchedRoute.Name, gslbutils.ObjectDelete, c.workqueue)
					return
				}
				AddOrUpdateRouteStore(acceptedRouteStore, route, c.name)
				// If the route was already part of rejected store, we need to remove from
				// this route from the rejected store.
				rejectedRouteStore.DeleteClusterNSObj(c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
				// Add the key for this route to the queue.
				publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, route.ObjectMeta.Namespace,
					route.ObjectMeta.Name, gslbutils.ObjectUpdate, c.workqueue)
			}
		},
	}
	return routeEventHandler
}

func publishKeyToGraphLayer(numWorkers uint32, objType, cname, namespace, name, op string, wq []workqueue.RateLimitingInterface) {
	key := gslbutils.MultiClusterKey(op, objType, cname, namespace, name)
	bkt := containerutils.Bkt(namespace, numWorkers)
	wq[bkt].AddRateLimited(key)
	gslbutils.Logf("cluster: %s, ns: %s, objType: %s, op: %s, objName: %s, msg: added %s key ",
		cname, namespace, objType, op, name, key)
}
