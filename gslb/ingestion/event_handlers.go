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

package ingestion

import (
	"fmt"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/filterstore"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/store"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"

	akov1alpha1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	akov1beta1 "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	filter "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/filter"
)

func AddLBSvcEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {
	acceptedLBSvcStore := store.GetAcceptedLBSvcStore()
	rejectedLBSvcStore := store.GetRejectedLBSvcStore()
	gslbutils.Logf("Adding svc handler")
	svcEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*corev1.Service)
			// Don't add this svc if this is not of type LB,
			// or if no IP is allocated it's status
			if !isSvcTypeLB(svc) {
				gslbutils.Debugf("cluster: %s, ns: %s, svc %s, msg: type not lb", c.name, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
				return
			}
			svcMeta, ok := k8sobjects.GetSvcMeta(svc, c.name)
			if !ok {
				gslbutils.Logf("cluster: %s, msg: could not get meta object for service: %s, ns: %s",
					c.name, svc.ObjectMeta.Name, svc.ObjectMeta.Namespace)
				return
			}
			if !gslbutils.CheckTenant(svc.Namespace, c.name, svcMeta.Tenant) {
				return
			}
			if !filter.ApplyFilter(filter.FilterArgs{
				Obj:     svcMeta,
				Cluster: c.name,
			}) {
				AddOrUpdateLBSvcStore(rejectedLBSvcStore, svc, c.name)
				gslbutils.Logf("cluster: %s, ns: %s, svc: %s, msg: %s\n", c.name,
					svc.ObjectMeta.Namespace, svc.ObjectMeta.Name, "rejected ADD svc key because it couldn't pass through filter")
				return
			}
			AddOrUpdateLBSvcStore(acceptedLBSvcStore, svc, c.name)
			publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, svc.ObjectMeta.Namespace,
				svc.ObjectMeta.Name, gslbutils.ObjectAdd, svcMeta.Hostname, svcMeta.Tenant, c.workqueue)
		},
		DeleteFunc: func(obj interface{}) {
			svc, ok := obj.(*corev1.Service)
			if !ok {
				gslbutils.Debugf("object %v is not of type Service", svc)
				return
			}
			if !isSvcTypeLB(svc) {
				return
			}
			fetchedObj, present := acceptedLBSvcStore.GetClusterNSObjectByName(c.name,
				svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
			DeleteFromLBSvcStore(acceptedLBSvcStore, svc, c.name)
			DeleteFromLBSvcStore(rejectedLBSvcStore, svc, c.name)

			// For services, where the status field was deleted, won't contain the hostname in that case
			hostName := ""
			svcMeta, ok := k8sobjects.GetSvcMeta(svc, c.name)
			if ok {
				hostName = svcMeta.Hostname
			}
			if present {
				fetchedSvc := fetchedObj.(k8sobjects.SvcMeta)
				publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, svc.ObjectMeta.Namespace,
					svc.ObjectMeta.Name, gslbutils.ObjectDelete, hostName, fetchedSvc.Tenant, c.workqueue)
			}
			return
		},
		UpdateFunc: func(old, curr interface{}) {
			oldSvc := old.(*corev1.Service)
			svc := curr.(*corev1.Service)
			if oldSvc.ResourceVersion != svc.ResourceVersion {
				svcMeta, ok := k8sobjects.GetSvcMeta(svc, c.name)
				if !ok || !isSvcTypeLB(svc) || !filter.ApplyFilter(filter.FilterArgs{
					Obj:     svcMeta,
					Cluster: c.name,
				}) {
					// See if the svc was already accepted, if yes, need to delete the key
					fetchedObj, ok := acceptedLBSvcStore.GetClusterNSObjectByName(c.name,
						oldSvc.ObjectMeta.Namespace, oldSvc.ObjectMeta.Name)
					if !ok {
						// Nothing to be done, just add to the rejected svc store
						AddOrUpdateLBSvcStore(rejectedLBSvcStore, svc, c.name)
						return
					}
					// Else, move this svc from accepted to rejected store, and add
					// a DELETE key for this svc to the queue
					AddOrUpdateLBSvcStore(rejectedLBSvcStore, svc, c.name)
					DeleteFromLBSvcStore(acceptedLBSvcStore, svc, c.name)

					fetchedSvc := fetchedObj.(k8sobjects.SvcMeta)
					// Add a DELETE key for this svc
					publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, fetchedSvc.Namespace,
						fetchedSvc.Name, gslbutils.ObjectDelete, fetchedSvc.Hostname, fetchedSvc.Tenant, c.workqueue)
					return
				}
				if !gslbutils.CheckTenant(svc.Namespace, c.name, svcMeta.Tenant) {
					return
				}
				if fetchedObj, ok := acceptedLBSvcStore.GetClusterNSObjectByName(c.name, oldSvc.ObjectMeta.Namespace, oldSvc.ObjectMeta.Name); ok {
					fetchedSvc := fetchedObj.(k8sobjects.SvcMeta)
					// check if tenant has changed for service
					if fetchedSvc.Tenant != svcMeta.Tenant {
						oper := gslbutils.ObjectDelete
						publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, fetchedSvc.Namespace, fetchedSvc.Name,
							oper, fetchedSvc.Hostname, fetchedSvc.Tenant, c.workqueue)
					}
				}

				AddOrUpdateLBSvcStore(acceptedLBSvcStore, svc, c.name)
				// If the svc was already part of rejected store, we need to remove
				// this svc from the rejected store.
				rejectedLBSvcStore.DeleteClusterNSObj(c.name, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
				// Add the key for this svc to the queue.
				publishKeyToGraphLayer(numWorkers, gslbutils.SvcType, c.name, svc.ObjectMeta.Namespace,
					svc.ObjectMeta.Name, gslbutils.ObjectUpdate, svcMeta.Hostname, svcMeta.Tenant, c.workqueue)
			}
		},
	}
	return svcEventHandler
}

func filterAndAddIngressMeta(ingressHostMetaObjs []k8sobjects.IngressHostMeta, c *GSLBMemberController,
	acceptedIngStore, rejectedIngStore *store.ClusterStore, numWorkers uint32, fullsync bool, namespaceTenant string) {
	for _, ihm := range ingressHostMetaObjs {
		if ihm.IPAddr == "" || ihm.Hostname == "" {
			gslbutils.Debugf("cluster: %s, ns: %s, ingress: %s, msg: %s\n",
				c.name, ihm.Namespace, ihm.IngName,
				"rejected ADD ingress because IP address/Hostname not found in status field")
			continue
		}
		if namespaceTenant != "" && ihm.Tenant != namespaceTenant {
			gslbutils.Debugf("cluster: %s, ns: %s, ingress: %s, msg: %s\n",
				c.name, ihm.Namespace, ihm.IngName, "rejected ADD ingress because ingress tenant is different from namespace")
			continue
		}
		if namespaceTenant == "" {
			ihm.Tenant = gslbutils.GetTenant()
			gslbutils.Debugf("cluster: %s, ns: %s, ingress: %s, tenant:%s, namespaceTenant %s ",
				c.name, ihm.Namespace, ihm.IngName, ihm.Tenant, namespaceTenant)
		}
		if !filter.ApplyFilter(filter.FilterArgs{
			Obj:     ihm,
			Cluster: c.name,
		}) {
			AddOrUpdateIngressStore(rejectedIngStore, ihm, c.name)
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s, ing: %v\n", c.name, ihm.Namespace,
				ihm.ObjName, "rejected ADD ingress key because it couldn't pass through the filter", ihm)
			continue
		}
		AddOrUpdateIngressStore(acceptedIngStore, ihm, c.name)
		if !fullsync {
			publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name,
				ihm.Namespace, ihm.ObjName, gslbutils.ObjectAdd, ihm.Hostname, ihm.Tenant, c.workqueue)
		}
	}
}

func deleteIngressMeta(ingressHostMetaObjs []k8sobjects.IngressHostMeta, c *GSLBMemberController, acceptedIngStore,
	rejectedIngStore *store.ClusterStore, numWorkers uint32) {
	for _, ihm := range ingressHostMetaObjs {
		fetchedObj, isAccepted := acceptedIngStore.GetClusterNSObjectByName(c.name, ihm.Namespace,
			ihm.ObjName)
		DeleteFromIngressStore(acceptedIngStore, ihm, c.name)
		DeleteFromIngressStore(rejectedIngStore, ihm, c.name)

		// Only if the ihm object was part of the accepted list previously, we will send a delete key
		// otherwise we will assume that the object was already deleted
		if isAccepted {
			fetchedIngHost := fetchedObj.(k8sobjects.IngressHostMeta)
			publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name,
				ihm.Namespace, ihm.ObjName, gslbutils.ObjectDelete, ihm.Hostname, fetchedIngHost.Tenant, c.workqueue)
		}
	}
}

func filterAndUpdateIngressMeta(oldIngMetaObjs, newIngMetaObjs []k8sobjects.IngressHostMeta, c *GSLBMemberController,
	acceptedIngStore, rejectedIngStore *store.ClusterStore, numWorkers uint32, namespaceTenant string) {

	for _, ihm := range oldIngMetaObjs {
		// Check whether this exists in the new ingressHost list, if not, we need
		// to delete this ingressHost object
		newIhm, found := ihm.IngressHostInList(newIngMetaObjs)
		if !found {
			// ingressHost doesn't exist anymore, delete this ingressHost object
			fetchedObj, isAccepted := acceptedIngStore.GetClusterNSObjectByName(c.name, ihm.Namespace,
				ihm.ObjName)
			DeleteFromIngressStore(acceptedIngStore, ihm, c.name)
			DeleteFromIngressStore(rejectedIngStore, ihm, c.name)
			// If part of accepted store, only then publish the delete key
			if isAccepted {
				fetchedIngHost := fetchedObj.(k8sobjects.IngressHostMeta)
				publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name,
					ihm.Namespace, ihm.ObjName, gslbutils.ObjectDelete, ihm.Hostname, fetchedIngHost.Tenant, c.workqueue)
			}
			continue
		}
		// ingressHost exists, check if that got updated
		if ihm.GetIngressHostCksum() == newIhm.GetIngressHostCksum() {
			// no changes, just continue
			continue
		}
		if namespaceTenant != "" && namespaceTenant != newIhm.Tenant {
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, namespaceTenant: %s, ingressTenant: %s,msg: %s\n",
				c.name, ihm.Namespace, ihm.IngName, namespaceTenant, ihm.Tenant, "rejected update ingress because ingress tenant is different from namespace")
			continue
		}
		if namespaceTenant == "" {
			newIhm.Tenant = gslbutils.GetTenant()
			gslbutils.Debugf("cluster: %s, ns: %s, ingress: %s, tenant:%s, namespaceTenant %s ",
				c.name, newIhm.Namespace, newIhm.IngName, newIhm.Tenant, namespaceTenant)
		}
		// there are changes, need to send an update key, but first apply the filter
		if !filter.ApplyFilter(filter.FilterArgs{
			Obj:     newIhm,
			Cluster: c.name,
		}) {
			// See if the ingressHost was already accepted, if yes, need to delete the key
			fetchedObj, ok := acceptedIngStore.GetClusterNSObjectByName(c.name,
				ihm.Namespace, ihm.ObjName)
			if !ok {
				// Nothing to be done, just add to the rejected ingress store
				AddOrUpdateIngressStore(rejectedIngStore, newIhm, c.name)
				continue
			}
			// Else, delete this ingressHost from accepted list and add the newIhm to the
			// rejected store, and add a delete key for this ingressHost to the queue
			AddOrUpdateIngressStore(rejectedIngStore, newIhm, c.name)
			DeleteFromIngressStore(acceptedIngStore, newIhm, c.name)

			fetchedIngHost := fetchedObj.(k8sobjects.IngressHostMeta)
			// Add a DELETE key for this ingHost
			publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, fetchedIngHost.Cluster,
				fetchedIngHost.Namespace, fetchedIngHost.ObjName, gslbutils.ObjectDelete,
				fetchedIngHost.Hostname, fetchedIngHost.Tenant, c.workqueue)
			continue
		}

		// check if the object existed in the acceptedIngStore
		oper := gslbutils.ObjectAdd
		if fetchedObj, ok := acceptedIngStore.GetClusterNSObjectByName(c.name, newIhm.Namespace, newIhm.ObjName); ok {
			fetchedIngHost := fetchedObj.(k8sobjects.IngressHostMeta)
			// check if tenant has changed for ingressHost
			if fetchedIngHost.Tenant != newIhm.Tenant {
				oper = gslbutils.ObjectDelete
				publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name, fetchedIngHost.Namespace, fetchedIngHost.ObjName,
					oper, fetchedIngHost.Hostname, fetchedIngHost.Tenant, c.workqueue)
				oper = gslbutils.ObjectAdd
			} else {
				oper = gslbutils.ObjectUpdate
			}
		}
		// ingHost passed through the filter, need to send an update key
		// if the ingHost was already part of rejected store, we need to move this ingHost
		// from the rejected to accepted store
		AddOrUpdateIngressStore(acceptedIngStore, newIhm, c.name)
		rejectedIngStore.DeleteClusterNSObj(c.name, ihm.Namespace, ihm.GetIngressHostMetaKey())
		// Add the key for this ingHost to the queue
		publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name, newIhm.Namespace, newIhm.ObjName,
			oper, newIhm.Hostname, newIhm.Tenant, c.workqueue)
		continue
	}
	// Check if there are any new ingHost objects, if yes, we have to add those
	for _, ihm := range newIngMetaObjs {
		_, found := ihm.IngressHostInList(oldIngMetaObjs)
		if found {
			continue
		}
		if namespaceTenant != "" && ihm.Tenant != namespaceTenant {
			gslbutils.Debugf("cluster: %s, ns: %s, ingress: %s, msg: %s\n",
				c.name, ihm.Namespace, ihm.IngName, "rejected ADD ingress because tenant mismatch")
			continue
		}
		if namespaceTenant == "" {
			ihm.Tenant = gslbutils.GetTenant()
			gslbutils.Debugf("cluster: %s, ns: %s, ingress: %s, tenant:%s, namespaceTenant %s ",
				c.name, ihm.Namespace, ihm.IngName, ihm.Tenant, namespaceTenant)
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
		if !filter.ApplyFilter(filter.FilterArgs{
			Obj:     ihm,
			Cluster: c.name,
		}) {
			AddOrUpdateIngressStore(rejectedIngStore, ihm, c.name)
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s\n", c.name, ihm.Namespace,
				ihm.ObjName, "rejected ADD ingress key because it couldn't pass through the filter")
			continue
		}
		AddOrUpdateIngressStore(acceptedIngStore, ihm, c.name)
		publishKeyToGraphLayer(numWorkers, gslbutils.IngressType, c.name,
			ihm.Namespace, ihm.ObjName, gslbutils.ObjectAdd, ihm.Hostname, ihm.Tenant, c.workqueue)
		continue
	}
}

func AddIngressEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {
	acceptedIngStore := store.GetAcceptedIngressStore()
	rejectedIngStore := store.GetRejectedIngressStore()

	gslbutils.Logf("Adding Ingress handler")
	ingressEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ingr, ok := obj.(*networkingv1.Ingress)
			if !ok {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to networking/v1 ingress")
				return
			}
			// Don't add this ingr if there's no status field present or no IP is allocated in this
			// status field
			ingressHostMetaObjs := k8sobjects.GetIngressHostMeta(ingr, c.name)
			namespaceTenant := gslbutils.GetTenantInNamespaceAnnotation(ingr.Namespace, c.name)
			filterAndAddIngressMeta(ingressHostMetaObjs, c, acceptedIngStore, rejectedIngStore, numWorkers, false, namespaceTenant)
		},
		DeleteFunc: func(obj interface{}) {
			ingr, ok := obj.(*networkingv1.Ingress)
			if !ok {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to networking/v1 ingress")
				return
			}
			// Delete from all ingress stores
			ingressHostMetaObjs := k8sobjects.GetIngressHostMeta(ingr, c.name)
			deleteIngressMeta(ingressHostMetaObjs, c, acceptedIngStore, rejectedIngStore, numWorkers)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldIngr, okOld := old.(*networkingv1.Ingress)
			ingr, okNew := curr.(*networkingv1.Ingress)
			if !okOld || !okNew {
				gslbutils.Errf("Unable to convert obj type interface to networking/v1 ingress")
				return
			}
			if oldIngr.ResourceVersion != ingr.ResourceVersion {
				oldIngMetaObjs := k8sobjects.GetIngressHostMeta(oldIngr, c.name)
				newIngMetaObjs := k8sobjects.GetIngressHostMeta(ingr, c.name)
				namespaceTenant := gslbutils.GetTenantInNamespaceAnnotation(ingr.Namespace, c.name)
				filterAndUpdateIngressMeta(oldIngMetaObjs, newIngMetaObjs, c, acceptedIngStore, rejectedIngStore,
					numWorkers, namespaceTenant)
			}
		},
	}
	return ingressEventHandler
}

func AddRouteEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {
	acceptedRouteStore := store.GetAcceptedRouteStore()
	rejectedRouteStore := store.GetRejectedRouteStore()
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
			if !gslbutils.CheckTenant(route.Namespace, c.name, routeMeta.Tenant) {
				return
			}
			if !filter.ApplyFilter(filter.FilterArgs{
				Cluster: c.name,
				Obj:     routeMeta,
			}) {
				AddOrUpdateRouteStore(rejectedRouteStore, route, c.name)
				gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: %s\n", c.name,
					route.ObjectMeta.Namespace, route.ObjectMeta.Name, "rejected ADD route key because it couldn't pass through filter")
				return
			}
			AddOrUpdateRouteStore(acceptedRouteStore, route, c.name)
			publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, route.ObjectMeta.Namespace,
				route.ObjectMeta.Name, gslbutils.ObjectAdd, routeMeta.Hostname, routeMeta.Tenant, c.workqueue)
		},
		DeleteFunc: func(obj interface{}) {
			route, ok := obj.(*routev1.Route)
			if !ok {
				gslbutils.Debugf("object %v type is not Route", route)
				return
			}
			// Delete from all route stores
			fetchedObj, ok := acceptedRouteStore.GetClusterNSObjectByName(c.name,
				route.ObjectMeta.Namespace, route.ObjectMeta.Name)
			DeleteFromRouteStore(acceptedRouteStore, route, c.name)
			DeleteFromRouteStore(rejectedRouteStore, route, c.name)
			routeMeta := k8sobjects.GetRouteMeta(route, c.name)
			if ok {
				fetchedRoute := fetchedObj.(k8sobjects.RouteMeta)
				publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, route.ObjectMeta.Namespace,
					route.ObjectMeta.Name, gslbutils.ObjectDelete, routeMeta.Hostname, fetchedRoute.Tenant, c.workqueue)
			}
		},
		UpdateFunc: func(old, curr interface{}) {
			oldRoute := old.(*routev1.Route)
			route := curr.(*routev1.Route)
			if oldRoute.ResourceVersion != route.ResourceVersion {
				routeMeta := k8sobjects.GetRouteMeta(route, c.name)
				if !gslbutils.CheckTenant(route.Namespace, c.name, routeMeta.Tenant) {
					return
				}
				if _, ok := gslbutils.RouteGetIPAddr(route); !ok || !filter.ApplyFilter(filter.FilterArgs{
					Cluster: c.name,
					Obj:     routeMeta,
				}) {
					// See if the route was already accepted, if yes, need to delete the key
					fetchedObj, ok := acceptedRouteStore.GetClusterNSObjectByName(c.name,
						oldRoute.ObjectMeta.Namespace, oldRoute.ObjectMeta.Name)
					if !ok {
						// Nothing to be done, just add to the rejected route store
						AddOrUpdateRouteStore(rejectedRouteStore, route, c.name)
						return
					}
					// Else, delete this route from accepted store and add to rejected store, and add
					// a key for this route to the queue
					AddOrUpdateRouteStore(rejectedRouteStore, route, c.name)
					DeleteFromRouteStore(acceptedRouteStore, route, c.name)

					fetchedRoute := fetchedObj.(k8sobjects.RouteMeta)
					// Add a DELETE key for this route
					publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, fetchedRoute.Namespace,
						fetchedRoute.Name, gslbutils.ObjectDelete, fetchedRoute.Hostname, fetchedRoute.Tenant, c.workqueue)
					return
				}
				op := gslbutils.ObjectUpdate
				if fetchedObj, ok := acceptedRouteStore.GetClusterNSObjectByName(c.name, route.GetObjectMeta().GetNamespace(),
					route.GetObjectMeta().GetName()); ok {
					fetchedRoute := fetchedObj.(k8sobjects.RouteMeta)
					// check if tenant has changed for route
					if fetchedRoute.Tenant != routeMeta.Tenant {
						oper := gslbutils.ObjectDelete
						publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, fetchedRoute.Namespace, fetchedRoute.Name,
							oper, fetchedRoute.Hostname, fetchedRoute.Tenant, c.workqueue)
						op = gslbutils.ObjectAdd
					}
				} else {
					op = gslbutils.ObjectAdd
				}
				AddOrUpdateRouteStore(acceptedRouteStore, route, c.name)
				// If the route was already part of rejected store, we need to remove this
				// route from the rejected store.
				rejectedRouteStore.DeleteClusterNSObj(c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
				// Add the key for this route to the queue.
				publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, route.ObjectMeta.Namespace,
					route.ObjectMeta.Name, op, routeMeta.Hostname, routeMeta.Tenant, c.workqueue)
			}
		},
	}
	return routeEventHandler
}

func publishKeyToGraphLayer(numWorkers uint32, objType, cname, namespace, name, op, hostname, tenant string, wq []workqueue.RateLimitingInterface) {
	key := gslbutils.MultiClusterKey(op, objType, cname, namespace, name, tenant)
	bkt := containerutils.Bkt(namespace, numWorkers)
	wq[bkt].AddRateLimited(key)
	gslbutils.Logf("cluster: %s, ns: %s, objType: %s, op: %s, objName: %s, msg: added %s key ",
		cname, namespace, objType, op, name, key)
}

func AddNamespaceEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {
	acceptedNSStore := store.GetAcceptedNSStore()
	rejectedNSStore := store.GetRejectedNSStore()
	namespaceTenantStore := store.GetNamespaceToTenantStore()

	gslbutils.Logf("Adding Namespace handler")
	ingressEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ns, ok := obj.(*corev1.Namespace)
			if !ok {
				gslbutils.Debugf("unable to convert obj %v type interface to namespace", obj)
				return
			}
			nsMeta := k8sobjects.GetNSMeta(ns, c.name)
			tenant, _ := ns.Annotations[gslbutils.TenantAnnotation]
			namespaceTenantStore.AddOrUpdate(c.name, ns.Name, tenant)
			if !filter.ApplyFilter(filter.FilterArgs{
				Obj:     nsMeta,
				Cluster: c.name,
			}) {
				AddOrUpdateNSStore(rejectedNSStore, ns, c.name)
				gslbutils.Logf("cluster: %s, ns: %s, msg: %s\n", c.name, nsMeta.Name,
					"ns didn't pass through the filter, adding to rejected list")
				return
			}
			WriteChangedObjsToQueue(c.workqueue, numWorkers, false, []string{})
			AddOrUpdateNSStore(acceptedNSStore, ns, c.name)
		},
		DeleteFunc: func(obj interface{}) {
			ns, ok := obj.(*corev1.Namespace)
			if !ok {
				gslbutils.Debugf("unable to convert obj %v type interface to Namespace", obj)
				return
			}
			nsMeta := k8sobjects.GetNSMeta(ns, c.name)
			if !nsMeta.DeleteFromFilter() {
				gslbutils.Debugf("no namespace exists in the filter, nothing to change")
			}
			// ns deleted from the filter, delete all existing objects from all stores for this namespace
			DeleteNamespacedObjsFromAllStores(c.workqueue, numWorkers, nsMeta)
			DeleteFromNSStore(acceptedNSStore, ns, c.name)
			DeleteFromNSStore(rejectedNSStore, ns, c.name)
			namespaceTenantStore.DeleteNSObj(c.name, ns.Name)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldNS, okOld := old.(*corev1.Namespace)
			ns, okNew := curr.(*corev1.Namespace)
			if !okOld || !okNew {
				gslbutils.Debugf("unable to convert obj %v type interface to Namespace", curr)
				return
			}
			if oldNS.ResourceVersion != ns.ResourceVersion {
				tenant, _ := ns.Annotations[gslbutils.TenantAnnotation]
				gslbutils.Debugf("cluster: %s, namespace: %s, tenant: %s", c.name, ns.Name, tenant)
				namespaceTenantStore.AddOrUpdate(c.name, ns.Name, tenant)
				applyGSLBHostruleOnTenantchange(numWorkers, c, oldNS, ns)

				oldNSMeta := k8sobjects.GetNSMeta(oldNS, c.name)
				newNSMeta := k8sobjects.GetNSMeta(ns, c.name)
				if !newNSMeta.UpdateFilter(oldNSMeta) {
					// no changes, nothing to be dome
					gslbutils.Debugf("ns didn't change, nothing to be done")
					// change the namespace label if updated only in the rejection list, for all other
					// cases, it will be updated
					AddOrUpdateNSStore(rejectedNSStore, ns, c.name)
					return
				}
				// filter changed, re-apply
				gslbutils.Logf("namespace: %s, msg: namespace changed in filter, will re-apply", ns.Name)
				WriteChangedObjsToQueue(c.workqueue, numWorkers, false, []string{})

				// determine if the new namespace is accepted or rejected
				if newNSMeta.ApplyFilter() {
					MoveNSObjs([]string{c.name + "/" + ns.Name}, rejectedNSStore, acceptedNSStore)
					AddOrUpdateNSStore(acceptedNSStore, ns, c.name)
				} else {
					MoveNSObjs([]string{c.name + "/" + ns.Name}, acceptedNSStore, rejectedNSStore)
					AddOrUpdateNSStore(rejectedNSStore, ns, c.name)
				}
			}
		},
	}
	return ingressEventHandler
}

func applyGSLBHostruleOnTenantchange(numWorkers uint32, c *GSLBMemberController, oldNS, ns *corev1.Namespace) {
	oldTenant, oldExists := oldNS.Annotations[gslbutils.TenantAnnotation]
	if !oldExists {
		oldTenant = gslbutils.GetTenant()
	}
	newTenant, newExists := ns.Annotations[gslbutils.TenantAnnotation]
	if !newExists {
		newTenant = gslbutils.GetTenant()
	}
	if oldTenant != newTenant {
		gslbHostRules, err := gslbutils.GetAMKOCRDInformer().GslbHostruleInformer.Lister().GSLBHostRules(ns.Name).List(labels.Set(nil).AsSelector())
		if err != nil {
			gslbutils.Errf("Unable to list GslbHostRules :%s", err.Error())
		} else {
			for _, gslbHostRule := range gslbHostRules {
				err = ValidateGSLBHostRule(gslbHostRule, false)
				if err != nil {
					gslbutils.Errf("Error in accepting GSLB Host Rule %s : %s", gslbHostRule.ObjectMeta.Name, err.Error())
					// check if previous GSLB host rule was accepted, if yes, we need to add a delete key if this new object is rejected
					if gslbHostRule.Status.Status == GslbHostRuleAccepted {
						updateGSLBHR(gslbHostRule, err.Error(), GslbHostRuleRejected)
						gslbutils.Errf("Error in accepting GSLB Host Rule %s : %s", gslbHostRule.ObjectMeta.Name, err.Error())
						deleteObjsForGSHostRule(gslbHostRule, c.workqueue, numWorkers, oldTenant)
					}
				} else {
					if gslbHostRule.Status.Status != GslbHostRuleRejected {
						//remove the gslbhostrule association with  model with old tenant
						gsHostRuleList := gslbutils.GetGSHostRulesList()
						gsHostRuleList.DeleteGSHostRulesForFQDN(gslbHostRule.Spec.Fqdn)
						gslbutils.Logf("ns: %s, gslbHostRule: %s, gsFqdn: %s, msg: GSLB Host Rule deleted for fqdn",
							gslbHostRule.Namespace, gslbHostRule.Name, gslbHostRule.Spec.Fqdn)
						// push the delete key for this fqdn
						key := gslbutils.GSFQDNKey(gslbutils.ObjectDelete, gslbutils.GSFQDNType, gslbHostRule.Spec.Fqdn, gslbHostRule.Namespace, oldTenant)
						gslbutils.Logf("ns: %s, gsFqdn: %s, key: %s, msg: pushed DELETE key",
							gslbHostRule.ObjectMeta.Namespace, gslbHostRule.Spec.Fqdn, key)
						nodes.OperateOnGSLBHostRule(key)
					}
					//add key for processing of gslbhostrule with new tenant

					gsHostRulesList := gslbutils.GetGSHostRulesList()
					gsFqdnHostRules := gsHostRulesList.GetGSHostRulesForFQDN(gslbHostRule.Spec.Fqdn)
					if gsFqdnHostRules == nil {
						// no GSLBHostRule exists for this FQDN, add a new one
						gsHostRulesList.BuildAndSetGSHostRulesForFQDN(gslbHostRule)
					} else {
						// there's an existing GSLBHostRule for this FQDN, reject this
						updateGSLBHR(gslbHostRule, "there's an existing GSLBHostRule for the same FQDN", GslbHostRuleRejected)
						continue
					}
					gslbutils.Logf("ns: %s, gslbhostrule: %s, msg: %s", gslbHostRule.ObjectMeta.Namespace, gslbHostRule.ObjectMeta.Name,
						"GSLBHostRule object added")
					// push the gsFqdn key to graph layer
					bkt := utils.Bkt(gslbHostRule.Spec.Fqdn, numWorkers)
					key := gslbutils.GSFQDNKey(gslbutils.ObjectAdd, gslbutils.GSFQDNType, gslbHostRule.Spec.Fqdn, gslbHostRule.Namespace, newTenant)
					c.workqueue[bkt].AddRateLimited(key)
					gslbutils.Logf("ns: %s, gsFqdn: %s, key: %s, msg: pushed ADD key",
						gslbHostRule.ObjectMeta.Namespace, gslbHostRule.Spec.Fqdn, key)
				}
			}
		}
	}
}

func ReApplyObjectsOnHostRule(hr *akov1beta1.HostRule, add bool, cname, lfqdn, gfqdn string, numWorkers uint32,
	k8swq []workqueue.RateLimitingInterface) {

	// primaryFqdn -> this is the fqdn chosen as the GSName
	primaryFqdn := lfqdn
	if gslbutils.GetCustomFqdnMode() {
		primaryFqdn = gfqdn
	}
	var key string
	objs := []string{gdpalphav2.RouteObj, gdpalphav2.IngressObj, gdpalphav2.LBSvcObj, gslbutils.MCIType}
	for _, o := range objs {
		objKey, acceptedStore, rejectedStore, err := GetObjTypeStores(o)
		if err != nil {
			gslbutils.Errf("objtype error: %s", err.Error())
			continue
		}
		if add {
			var acceptedList []string
			if gslbutils.GetCustomFqdnMode() && rejectedStore != nil {
				// If customFQDN is true - all are objects are in rejectedStore as all are
				//     rejected if no Hostrule for them is found (i.e., no global to local mapping)
				acceptedList, _ = filterstore.GetAllFilteredObjectsForClusterFqdn(filter.ApplyFqdnMapFilter, cname, primaryFqdn, rejectedStore)
				MoveObjs(acceptedList, rejectedStore, acceptedStore, objKey)
			} else if !gslbutils.GetCustomFqdnMode() && acceptedStore != nil {
				// is customFQDN is false - objects are accepted and in accepted store,
				//     rejected store has objects rejected based on filter
				acceptedList, _ = filterstore.GetAllFilteredObjectsForClusterFqdn(filter.ApplyFqdnMapFilter, cname, primaryFqdn, acceptedStore)
			}
			if len(acceptedList) != 0 {
				gslbutils.Logf("ObjList: %v, msg: %s", acceptedList, "object list will be added")
				for _, objName := range acceptedList {
					cname, ns, sname, err := splitName(o, objName)
					fetchedObj, ok := acceptedStore.GetClusterNSObjectByName(cname,
						ns, sname)
					if !ok {
						continue
					}
					metaObj := fetchedObj.(k8sobjects.MetaObject)
					tenant := metaObj.GetTenant()
					if err != nil {
						gslbutils.Errf("objName: %s, msg: processing error, %s", objName, err)
						continue
					}
					bkt := utils.Bkt(ns, numWorkers)

					if o == gdpalphav2.IngressObj {
						ingName := gslbutils.GetIngressNameFromSname(sname)
						key = gslbutils.MultiClusterKeyForHostRule(gslbutils.ObjectAdd, objKey, cname, ns, ingName, lfqdn, gfqdn, tenant)
					} else {
						key = gslbutils.MultiClusterKeyForHostRule(gslbutils.ObjectAdd, objKey, cname, ns, sname, lfqdn, gfqdn, tenant)
					}
					k8swq[bkt].AddRateLimited(key)
					gslbutils.Logf("cluster: %s, ns: %s, objtype:%s, name: %s, key: %s, msg: added ADD obj key",
						cname, ns, o, sname, key)
				}
			}
		}
		if !add && acceptedStore.ClusterObjectMap != nil {
			acceptedList, rejectedList := filterstore.GetAllFilteredObjectsForClusterFqdn(filter.ApplyFqdnMapFilter, cname, primaryFqdn, acceptedStore)
			if len(rejectedList) != 0 {
				filteredRejectedList, err := filterObjListBasedOnFqdn(acceptedStore, rejectedList, hr.Spec.VirtualHost.Fqdn, o)
				if err != nil {
					gslbutils.Errf("cluster: %s, localFqdn: %s, msg: error in filtering the rejected list",
						cname, hr.Spec.VirtualHost.Fqdn)
				}
				gslbutils.Logf("ObjList: %v, msg: %s", filteredRejectedList, "obj list will be deleted")
				MoveObjs(filteredRejectedList, acceptedStore, rejectedStore, objKey)
				for _, objName := range filteredRejectedList {
					cname, ns, sname, err := splitName(o, objName)
					fetchedObj, ok := rejectedStore.GetClusterNSObjectByName(cname,
						ns, sname)
					if !ok {
						continue
					}
					metaObj := fetchedObj.(k8sobjects.MetaObject)
					tenant := metaObj.GetTenant()
					if err != nil {
						gslbutils.Errf("cluster: %s, msg: couldn't process object, objtype: %s, name: %s, error, %s",
							cname, o, objName, err)
						continue
					}

					bkt := utils.Bkt(ns, numWorkers)
					if o == gdpalphav2.IngressObj {
						ingName := gslbutils.GetIngressNameFromSname(sname)
						key = gslbutils.MultiClusterKeyForHostRule(gslbutils.ObjectDelete, objKey, cname, ns, ingName, lfqdn, gfqdn, tenant)
					} else {
						key = gslbutils.MultiClusterKeyForHostRule(gslbutils.ObjectDelete, objKey, cname, ns, sname, lfqdn, gfqdn, tenant)
					}
					k8swq[bkt].AddRateLimited(key)
					gslbutils.Logf("cluster: %s, ns: %s, objType:%s, name: %s, key: %s, msg: added DELETE obj key",
						cname, ns, o, sname, key)
				}
			}

			if len(acceptedList) != 0 {
				// send the objects in the accepted list for an update to layer 2. This is so that, even
				// though the previous logics capture the ADD/DELETE events for the objects because of
				// a hostrule change, they don't take care of the UPDATE events.
				filteredAcceptedList, err := filterObjListBasedOnFqdn(acceptedStore, acceptedList, hr.Spec.VirtualHost.Fqdn, o)
				if err != nil {
					gslbutils.Errf("cluster: %s, localFqdn: %s, msg: error in filtering the accepted list",
						cname, hr.Spec.VirtualHost.Fqdn)
				}
				gslbutils.Logf("cluster: %s, ObjList: %v, msg: %s", cname, filteredAcceptedList, "obj list will be updated")
				for _, objName := range filteredAcceptedList {
					cname, ns, sname, err := splitName(o, objName)
					fetchedObj, ok := acceptedStore.GetClusterNSObjectByName(cname,
						ns, sname)
					if !ok {
						continue
					}
					metaObj := fetchedObj.(k8sobjects.MetaObject)
					tenant := metaObj.GetTenant()
					if err != nil {
						gslbutils.Errf("cluster: %s, msg: couldn't process object, objtype: %s, name: %s, error, %s",
							cname, o, objName, err)
						continue
					}

					bkt := utils.Bkt(ns, numWorkers)
					if o == gdpalphav2.IngressObj {
						ingName := gslbutils.GetIngressNameFromSname(sname)
						key = gslbutils.MultiClusterKeyForHostRule(gslbutils.ObjectUpdate, objKey, cname, ns, ingName, lfqdn, gfqdn, tenant)
					} else {
						key = gslbutils.MultiClusterKeyForHostRule(gslbutils.ObjectUpdate, objKey, cname, ns, sname, lfqdn, gfqdn, tenant)
					}
					k8swq[bkt].AddRateLimited(key)
					gslbutils.Logf("cluster: %s, ns: %s, objType:%s, name: %s, key: %s, msg: added UPDATE obj key",
						cname, ns, o, sname, key)
				}
			}
		}
	}
}

func filterObjListBasedOnFqdn(cstore *store.ClusterStore, objList []string, fqdn string,
	objType string) ([]string, error) {
	result := []string{}
	for _, obj := range objList {
		cname, ns, sname, err := splitName(objType, obj)
		if err != nil {
			return result, fmt.Errorf("error in splitting name in cluster store %v", err)
		}
		objIntf, ok := cstore.GetClusterNSObjectByName(cname, ns, sname)
		if !ok {
			continue
		}
		metaObj := objIntf.(k8sobjects.MetaObject)
		if metaObj.GetHostname() != fqdn {
			continue
		}
		result = append(result, obj)
	}
	return result, nil
}

func HandleHostRuleAliasChange(fqdn, cname string, oldAliasList, newAliasList []string) {
	gsDomainNameMap := gslbutils.GetDomainNameMap()
	// The aliases that are removed need to be deleted from domain names
	gsDomainNameMap.DeleteGSToDomainNameMapping(fqdn, cname,
		gslbutils.SetDifference(oldAliasList, newAliasList))

	// The aliases that are added need to be added to the domain names
	gsDomainNameMap.AddUpdateGSToDomainNameMapping(fqdn, cname,
		gslbutils.SetDifference(newAliasList, oldAliasList))
}

func AddHostRule(numWorkers uint32, hrStore *store.ClusterStore, hr *akov1beta1.HostRule, c *GSLBMemberController) {
	gsDomainNameMap := gslbutils.GetDomainNameMap()
	lFqdn := hr.Spec.VirtualHost.Fqdn
	AddOrUpdateHostRuleStore(hrStore, hr, c.name)

	if gslbutils.GetCustomFqdnMode() {
		gFqdn := hr.Spec.VirtualHost.Gslb.Fqdn
		fqdnMap := gslbutils.GetFqdnMap()
		fqdnMap.AddUpdateToFqdnMapping(gFqdn, lFqdn, c.name)
		if hr.Spec.VirtualHost.Gslb.IncludeAliases {
			// when includeAliases flag in HostRule is set to true
			// We create a GSLB Service with name equal to global fqdn
			// This GSLB Service will have domain names as a list of -
			// all the fqdns mentioned in the Aliases part of the HostRule CRD
			gsDomainNameMap.AddUpdateGSToDomainNameMapping(gFqdn, c.name, hr.Spec.VirtualHost.Aliases)
		}
		ReApplyObjectsOnHostRule(hr, true, c.name, lFqdn, gFqdn, numWorkers, c.workqueue)
	} else {
		// customFqdnMode is false
		// we use the local Fqdn for the gsName
		// domain names - all the fqdns mentioned in the Aliases part of the HostRule CRD
		gsDomainNameMap.AddUpdateGSToDomainNameMapping(lFqdn, c.name, hr.Spec.VirtualHost.Aliases)
		ReApplyObjectsOnHostRule(hr, true, c.name, lFqdn, lFqdn, numWorkers, c.workqueue)
	}
}

func AddHostRuleEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {
	hrStore := store.GetHostRuleStore()

	gslbutils.Logf("cluster: %s, msg: adding handlers for host rule objects", c.name)
	hrEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			hr, ok := obj.(*akov1beta1.HostRule)
			if !ok {
				gslbutils.Debugf("cluster: %s, msg: unable to convert obj %v type interface to HostRule", c.name, obj)
				return
			}
			if !isHostRuleAcceptable(hr) {
				gslbutils.Debugf("cluster: %s, namespace: %s, hostRule: %s, gsFqdn: %s, status: %s, msg: host rule object not in acceptable state",
					c.name, hr.Namespace, hr.Name, hr.Spec.VirtualHost.Gslb.Fqdn, hr.Status.Status)
				return
			}
			AddHostRule(numWorkers, hrStore, hr, c)
		},
		DeleteFunc: func(obj interface{}) {
			hr, ok := obj.(*akov1beta1.HostRule)
			if !ok {
				gslbutils.Debugf("cluster: %s, msg: unable to convert obj %v type interface to HostRule", c.name, obj)
				return
			}
			if !isHostRuleAcceptable(hr) {
				gslbutils.Debugf("cluster: %s, namespace: %s, hostRule: %s, gsFqdn: %s, status: %s, msg: host rule object not in acceptable state",
					c.name, hr.Namespace, hr.Name, hr.Spec.VirtualHost.Gslb.Fqdn, hr.Status.Status)
				return
			}
			// write a delete event to graph layer
			gsDomainNameMap := gslbutils.GetDomainNameMap()
			lFqdn := hr.Spec.VirtualHost.Fqdn
			DeleteFromHostRuleStore(hrStore, hr, c.name)

			if gslbutils.GetCustomFqdnMode() {
				gFqdn := hr.Spec.VirtualHost.Gslb.Fqdn
				fqdnMap := gslbutils.GetFqdnMap()

				fqdnMap.DeleteFromFqdnMapping(gFqdn, lFqdn, c.name)
				gsDomainNameMap.DeleteGSToDomainNameMapping(gFqdn, c.name, hr.Spec.VirtualHost.Aliases)
				ReApplyObjectsOnHostRule(hr, false, c.name, lFqdn, gFqdn, numWorkers, c.workqueue)
			} else {
				gsDomainNameMap.DeleteGSToDomainNameMapping(lFqdn, c.name, hr.Spec.VirtualHost.Aliases)
				ReApplyObjectsOnHostRule(hr, false, c.name, lFqdn, lFqdn, numWorkers, c.workqueue)

			}
		},
		UpdateFunc: func(old, curr interface{}) {
			oldHr, ok := old.(*akov1beta1.HostRule)
			if !ok {
				gslbutils.Debugf("cluster: %s, msg: unable to convert obj %v type interface to HostRule", c.name, old)
				return
			}
			newHr, ok := curr.(*akov1beta1.HostRule)
			if !ok {
				gslbutils.Debugf("cluster: %s, msg: unable to convert obj %v type interface to HostRule", c.name, curr)
				return
			}
			if oldHr.ResourceVersion == newHr.ResourceVersion {
				// no updates to object
				return
			}
			oldHrAccepted := isHostRuleAcceptable(oldHr)
			newHrAccepted := isHostRuleAcceptable(newHr)
			oldGFqdn := oldHr.Spec.VirtualHost.Gslb.Fqdn
			oldLFqdn := oldHr.Spec.VirtualHost.Fqdn
			newGFqdn := newHr.Spec.VirtualHost.Gslb.Fqdn
			newLFqdn := newHr.Spec.VirtualHost.Fqdn
			fqdnMap := gslbutils.GetFqdnMap()
			gsDomainNameMap := gslbutils.GetDomainNameMap()

			if (oldHrAccepted == newHrAccepted) && newHrAccepted {
				// check if an update is required?
				if !isHostRuleUpdated(oldHr, newHr) {
					// no updates to the hostrule, so return
					return
				}
				aliasesChanged := false
				if !gslbutils.SetEqual(oldHr.Spec.VirtualHost.Aliases, newHr.Spec.VirtualHost.Aliases) {
					aliasesChanged = true
				}
				if gslbutils.GetCustomFqdnMode() {
					gFqdnChanged := false
					if oldGFqdn != newGFqdn {
						gFqdnChanged = true
					}
					// the update can be of 3 types
					if aliasesChanged && !gFqdnChanged && newHr.Spec.VirtualHost.Gslb.IncludeAliases {
						// case 1: Only the aliases have changed and includeAliases = true
						HandleHostRuleAliasChange(newGFqdn, c.name, oldHr.Spec.VirtualHost.Aliases, newHr.Spec.VirtualHost.Aliases)
					} else if gFqdnChanged && !aliasesChanged || aliasesChanged && gFqdnChanged {
						// This handles 2 cases
						// case 2: Only the gFqdn has changed
						// and
						// case 3: Both aliases and gFqdn have changed
						fqdnMap.DeleteFromFqdnMapping(oldGFqdn, oldLFqdn, c.name)
						fqdnMap.AddUpdateToFqdnMapping(newGFqdn, newLFqdn, c.name)
						gsDomainNameMap.DeleteGSToDomainNameMapping(oldGFqdn, c.name, oldHr.Spec.VirtualHost.Aliases)
						if newHr.Spec.VirtualHost.Gslb.IncludeAliases {
							gsDomainNameMap.AddUpdateGSToDomainNameMapping(newGFqdn, c.name, newHr.Spec.VirtualHost.Aliases)
						}
					} else if oldHr.Spec.VirtualHost.Gslb.IncludeAliases != newHr.Spec.VirtualHost.Gslb.IncludeAliases {
						// The includeAliases flag has been flipped
						if newHr.Spec.VirtualHost.Gslb.IncludeAliases {
							gsDomainNameMap.AddUpdateGSToDomainNameMapping(newGFqdn, c.name, newHr.Spec.VirtualHost.Aliases)
						} else {
							gsDomainNameMap.DeleteGSToDomainNameMapping(oldGFqdn, c.name, oldHr.Spec.VirtualHost.Aliases)
						}
					}
					DeleteFromHostRuleStore(hrStore, oldHr, c.name)
					AddOrUpdateHostRuleStore(hrStore, newHr, c.name)

					ReApplyObjectsOnHostRule(oldHr, false, c.name, oldLFqdn, oldGFqdn, numWorkers, c.workqueue)
					ReApplyObjectsOnHostRule(newHr, true, c.name, newLFqdn, newGFqdn, numWorkers, c.workqueue)
				} else {
					// Aliases have changed or tls fields have changed
					if aliasesChanged {
						HandleHostRuleAliasChange(newLFqdn, c.name, oldHr.Spec.VirtualHost.Aliases, newHr.Spec.VirtualHost.Aliases)
					}

					DeleteFromHostRuleStore(hrStore, oldHr, c.name)
					AddOrUpdateHostRuleStore(hrStore, newHr, c.name)

					ReApplyObjectsOnHostRule(oldHr, false, c.name, oldLFqdn, oldLFqdn, numWorkers, c.workqueue)
					ReApplyObjectsOnHostRule(newHr, true, c.name, newLFqdn, newLFqdn, numWorkers, c.workqueue)
				}
			} else if oldHrAccepted && !newHrAccepted {
				// delete the old gs fqdn
				DeleteFromHostRuleStore(hrStore, oldHr, c.name)
				fqdnMap.DeleteFromFqdnMapping(oldGFqdn, oldLFqdn, c.name)
				if gslbutils.GetCustomFqdnMode() {
					gsDomainNameMap.DeleteGSToDomainNameMapping(oldGFqdn, c.name, oldHr.Spec.VirtualHost.Aliases)
					ReApplyObjectsOnHostRule(oldHr, false, c.name, oldLFqdn, oldGFqdn, numWorkers, c.workqueue)

				} else {
					gsDomainNameMap.DeleteGSToDomainNameMapping(oldLFqdn, c.name, oldHr.Spec.VirtualHost.Aliases)
					ReApplyObjectsOnHostRule(oldHr, false, c.name, oldLFqdn, oldLFqdn, numWorkers, c.workqueue)
				}
			} else if !oldHrAccepted && newHrAccepted {
				// add the new gs fqdn
				AddHostRule(numWorkers, hrStore, newHr, c)
			}
		},
	}
	return hrEventHandler
}

func filterAndAddMultiClusterIngressMeta(ingressHostMetaObjs []k8sobjects.MultiClusterIngressHostMeta, c *GSLBMemberController,
	acceptedIngStore, rejectedIngStore *store.ClusterStore, numWorkers uint32, fullsync bool, namespaceTenant string) {
	for _, ihm := range ingressHostMetaObjs {
		if ihm.IPAddr == "" || ihm.Hostname == "" {
			gslbutils.Debugf("cluster: %s, ns: %s, ingress: %s, msg: %s\n",
				c.name, ihm.Namespace, ihm.IngName,
				"rejected ADD ingress because IP address/Hostname not found in status field")
			continue
		}
		if namespaceTenant != "" && ihm.Tenant != namespaceTenant {
			gslbutils.Debugf("cluster: %s, ns: %s, ingress: %s, msg: %s\n",
				c.name, ihm.Namespace, ihm.IngName, "rejected ADD ingress because tenant mismatch")
			continue
		}
		if !filter.ApplyFilter(filter.FilterArgs{
			Obj:     ihm,
			Cluster: c.name,
		}) {
			AddOrUpdateMultiClusterIngressStore(rejectedIngStore, ihm, c.name)
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s, ing: %v\n", c.name, ihm.Namespace,
				ihm.ObjName, "rejected ADD ingress key because it couldn't pass through the filter", ihm)
			continue
		}
		AddOrUpdateMultiClusterIngressStore(acceptedIngStore, ihm, c.name)
		if !fullsync {
			publishKeyToGraphLayer(numWorkers, gslbutils.MCIType, c.name,
				ihm.Namespace, ihm.ObjName, gslbutils.ObjectAdd, ihm.Hostname, ihm.Tenant, c.workqueue)
		}
	}
}

func filterAndUpdateMultiClusterIngressMeta(oldIngMetaObjs, newIngMetaObjs []k8sobjects.MultiClusterIngressHostMeta, c *GSLBMemberController,
	acceptedStore, rejectedStore *store.ClusterStore, numWorkers uint32, namespaceTenant string) {

	for _, mcihm := range oldIngMetaObjs {
		// Check whether this exists in the new ingressHost list, if not, we need
		// to delete this ingressHost object
		newMCIhm, found := mcihm.IngressHostInList(newIngMetaObjs)
		if !found {
			// ingressHost doesn't exist anymore, delete this ingressHost object
			fetchedObj, isAccepted := acceptedStore.GetClusterNSObjectByName(c.name, mcihm.Namespace,
				mcihm.ObjName)
			DeleteFromMultiClusterIngressStore(acceptedStore, mcihm, c.name)
			DeleteFromMultiClusterIngressStore(rejectedStore, mcihm, c.name)
			// If part of accepted store, only then publish the delete key
			if isAccepted {
				fetchedMcIng := fetchedObj.(k8sobjects.MultiClusterIngressHostMeta)
				publishKeyToGraphLayer(numWorkers, gslbutils.MCIType, c.name,
					mcihm.Namespace, mcihm.ObjName, gslbutils.ObjectDelete, mcihm.Hostname, fetchedMcIng.Tenant, c.workqueue)
			}
			continue
		}
		// ingressHost exists, check if that got updated
		if mcihm.GetIngressHostCksum() == newMCIhm.GetIngressHostCksum() {
			// no changes, just continue
			continue
		}
		if namespaceTenant == "" {
			newMCIhm.Tenant = gslbutils.GetTenant()
		}
		if namespaceTenant != "" && namespaceTenant != newMCIhm.Tenant {
			gslbutils.Debugf("cluster: %s, ns: %s, mcingress: %s, namespaceTenant: %s, ingressTenant: %s, msg: %s\n",
				c.name, newMCIhm.Namespace, newMCIhm.IngName, namespaceTenant, newMCIhm.Tenant, "rejected update mcingress because tenant mismatch")
			continue
		}
		// there are changes, need to send an update key, but first apply the filter
		if !filter.ApplyFilter(filter.FilterArgs{
			Obj:     newMCIhm,
			Cluster: c.name,
		}) {
			// See if the ingressHost was already accepted, if yes, need to delete the key
			fetchedObj, ok := acceptedStore.GetClusterNSObjectByName(c.name,
				mcihm.Namespace, mcihm.ObjName)
			if !ok {
				// Nothing to be done, just add to the rejected ingress store
				AddOrUpdateMultiClusterIngressStore(rejectedStore, newMCIhm, c.name)
				continue
			}
			// Else, delete this ingressHost from accepted list and add the newIhm to the
			// rejected store, and add a delete key for this ingressHost to the queue
			AddOrUpdateMultiClusterIngressStore(rejectedStore, newMCIhm, c.name)
			DeleteFromMultiClusterIngressStore(acceptedStore, newMCIhm, c.name)

			fetchedIngHost := fetchedObj.(k8sobjects.MultiClusterIngressHostMeta)
			// Add a DELETE key for this ingHost
			publishKeyToGraphLayer(numWorkers, gslbutils.MCIType, fetchedIngHost.Cluster,
				fetchedIngHost.Namespace, fetchedIngHost.ObjName, gslbutils.ObjectDelete,
				fetchedIngHost.Hostname, fetchedIngHost.Tenant, c.workqueue)
			continue
		}
		// check if the object existed in the acceptedIngStore
		oper := gslbutils.ObjectAdd
		if fetchedObj, ok := acceptedStore.GetClusterNSObjectByName(c.name, newMCIhm.Namespace, newMCIhm.ObjName); ok {
			fetchedIngHost := fetchedObj.(k8sobjects.MultiClusterIngressHostMeta)
			// check if tenant has changed for service
			if fetchedIngHost.Tenant != newMCIhm.Tenant {
				oper := gslbutils.ObjectDelete
				publishKeyToGraphLayer(numWorkers, gslbutils.RouteType, c.name, fetchedIngHost.Namespace, fetchedIngHost.ObjName,
					oper, fetchedIngHost.Hostname, fetchedIngHost.Tenant, c.workqueue)
			}
			oper = gslbutils.ObjectUpdate
		}
		// ingHost passed through the filter, need to send an update key
		// if the ingHost was already part of rejected store, we need to move this ingHost
		// from the rejected to accepted store
		AddOrUpdateMultiClusterIngressStore(acceptedStore, newMCIhm, c.name)
		rejectedStore.DeleteClusterNSObj(c.name, mcihm.Namespace, mcihm.GetIngressHostMetaKey())
		// Add the key for this ingHost to the queue
		publishKeyToGraphLayer(numWorkers, gslbutils.MCIType, c.name, newMCIhm.Namespace, newMCIhm.ObjName,
			oper, newMCIhm.Hostname, newMCIhm.Tenant, c.workqueue)
		continue
	}
	// Check if there are any new ingHost objects, if yes, we have to add those
	for _, mcihm := range newIngMetaObjs {
		_, found := mcihm.IngressHostInList(oldIngMetaObjs)
		if found {
			continue
		}
		// only the new ones will be considered, because the old ones
		// have been taken care of already
		// Add this ingressHost object
		if mcihm.IPAddr == "" || mcihm.Hostname == "" {
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s",
				c.name, mcihm.Namespace, mcihm.ObjName,
				"rejected ADD ingress because IP address/Hostname not found in status field")
			continue
		}
		if namespaceTenant != "" && namespaceTenant != mcihm.Tenant {
			gslbutils.Debugf("cluster: %s, ns: %s, mcingress: %s, msg: %s\n",
				c.name, mcihm.Namespace, mcihm.IngName, "rejected update mcingress because tenant mismatch")
			continue
		}
		if namespaceTenant == "" {
			mcihm.Tenant = gslbutils.GetTenant()
		}
		if !filter.ApplyFilter(filter.FilterArgs{
			Obj:     mcihm,
			Cluster: c.name,
		}) {
			AddOrUpdateMultiClusterIngressStore(rejectedStore, mcihm, c.name)
			gslbutils.Logf("cluster: %s, ns: %s, ingress: %s, msg: %s\n", c.name, mcihm.Namespace,
				mcihm.ObjName, "rejected ADD ingress key because it couldn't pass through the filter")
			continue
		}
		AddOrUpdateMultiClusterIngressStore(acceptedStore, mcihm, c.name)
		publishKeyToGraphLayer(numWorkers, gslbutils.MCIType, c.name,
			mcihm.Namespace, mcihm.ObjName, gslbutils.ObjectAdd, mcihm.Hostname, mcihm.Tenant, c.workqueue)
		continue
	}
}

func deleteMultiClusterIngressMeta(ingressHostMetaObjs []k8sobjects.MultiClusterIngressHostMeta, c *GSLBMemberController, acceptedStore,
	rejectedStore *store.ClusterStore, numWorkers uint32) {
	for _, mcihm := range ingressHostMetaObjs {
		fetchedObj, isAccepted := acceptedStore.GetClusterNSObjectByName(c.name, mcihm.Namespace,
			mcihm.ObjName)
		DeleteFromMultiClusterIngressStore(acceptedStore, mcihm, c.name)
		DeleteFromMultiClusterIngressStore(rejectedStore, mcihm, c.name)

		// Only if the ihm object was part of the accepted list previously, we will send a delete key
		// otherwise we will assume that the object was already deleted
		if isAccepted {
			fetchedMultiIngHost := fetchedObj.(k8sobjects.MultiClusterIngressHostMeta)
			publishKeyToGraphLayer(numWorkers, gslbutils.MCIType, c.name,
				mcihm.Namespace, mcihm.ObjName, gslbutils.ObjectDelete, mcihm.Hostname, fetchedMultiIngHost.Tenant, c.workqueue)
		}
	}
}

func AddMultiClusterIngressEventHandler(numWorkers uint32, c *GSLBMemberController) cache.ResourceEventHandler {

	acceptedStore := store.GetAcceptedMultiClusterIngressStore()
	rejectedStore := store.GetRejectedMultiClusterIngressStore()

	gslbutils.Logf("Adding Multi-cluster Ingress handler")
	mciEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mciObj, ok := obj.(*akov1alpha1.MultiClusterIngress)
			if !ok {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to multi-cluster ingress")
				return
			}
			// Don't add this ingr if there's no status field present or no IP is allocated in this
			// status field
			ingressHostMetaObjs := k8sobjects.GetHostMetaForMultiClusterIngress(mciObj, c.name)
			namespaceTenant := gslbutils.GetTenantInNamespaceAnnotation(mciObj.Namespace, c.name)
			filterAndAddMultiClusterIngressMeta(ingressHostMetaObjs, c, acceptedStore, rejectedStore, numWorkers, false, namespaceTenant)
		},
		DeleteFunc: func(obj interface{}) {
			mciObj, ok := obj.(*akov1alpha1.MultiClusterIngress)
			if !ok {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to multi-cluster ingress")
				return
			}
			// Delete from all ingress stores
			ingressHostMetaObjs := k8sobjects.GetHostMetaForMultiClusterIngress(mciObj, c.name)
			deleteMultiClusterIngressMeta(ingressHostMetaObjs, c, acceptedStore, rejectedStore, numWorkers)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldMCIObj, okOld := old.(*akov1alpha1.MultiClusterIngress)
			mciObj, okNew := curr.(*akov1alpha1.MultiClusterIngress)
			if !okOld || !okNew {
				gslbutils.Errf("Unable to convert obj type interface to multi-cluster ingress")
				return
			}
			if oldMCIObj.ResourceVersion != mciObj.ResourceVersion {
				oldIngMetaObjs := k8sobjects.GetHostMetaForMultiClusterIngress(oldMCIObj, c.name)
				newIngMetaObjs := k8sobjects.GetHostMetaForMultiClusterIngress(mciObj, c.name)
				namespaceTenant := gslbutils.GetTenantInNamespaceAnnotation(mciObj.Namespace, c.name)
				filterAndUpdateMultiClusterIngressMeta(oldIngMetaObjs, newIngMetaObjs, c, acceptedStore, rejectedStore,
					numWorkers, namespaceTenant)
			}
		},
	}
	return mciEventHandler
}
