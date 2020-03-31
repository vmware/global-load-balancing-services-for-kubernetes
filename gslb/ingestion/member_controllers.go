package ingestion

import (
	"fmt"
	"sync"

	filter "amko/gslb/gdp_filter"
	"amko/gslb/k8sobjects"

	"amko/gslb/gslbutils"

	containerutils "github.com/avinetworks/container-lib/utils"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/runtime"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// GSLBMemberController is actually kubernetes cluster which is added to an AVI controller
// here which is added to an AVI controller
type GSLBMemberController struct {
	name            string
	worker_id       uint32
	worker_id_mutex sync.Mutex
	informers       *containerutils.Informers
	workqueue       []workqueue.RateLimitingInterface
}

// GetAviController sets config for an AviController
func GetGSLBMemberController(clusterName string, informersInstance *containerutils.Informers) GSLBMemberController {
	return GSLBMemberController{
		name:      clusterName,
		worker_id: (uint32(1) << containerutils.NumWorkersIngestion) - 1,
		informers: informersInstance,
	}
}

func rejectIngress(ingr *extensionv1beta1.Ingress) bool {
	// Return true if the IP address is present in an ingress's status field, else return false
	ingrStatus := ingr.Status
	lb := ingrStatus.LoadBalancer
	for _, lbf := range lb.Ingress {
		ip := lbf.IP
		if ip != "" {
			return false
		}
	}
	return true
}

// AddOrUpdateRouteStore traverses through the cluster store for cluster name cname,
// and then to ns store for the route's namespace and then adds/updates the route obj
// in the object map store.
func AddOrUpdateRouteStore(clusterRouteStore *gslbutils.ClusterStore,
	route *routev1.Route, cname string) {
	routeMeta := k8sobjects.GetRouteMeta(route, cname)
	clusterRouteStore.AddOrUpdate(routeMeta, cname, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
}

// DeleteFromRouteStore traverses through the cluster store for cluster name cname,
// and then ns store for the route's namespace and then deletes the route key from
// the object map store.
func DeleteFromRouteStore(clusterRouteStore *gslbutils.ClusterStore,
	route *routev1.Route, cname string) {
	if clusterRouteStore == nil {
		// Store is empty, so, noop
		return
	}
	ns := route.ObjectMeta.Namespace
	routeName := route.ObjectMeta.Name
	clusterRouteStore.DeleteClusterNSObj(cname, ns, routeName)
}

// SetupEventHandlers sets up event handlers for the controllers of the member clusters.
// They define the ingress/route event handlers and start the informers as well.
func (c *GSLBMemberController) SetupEventHandlers(k8sinfo K8SInformers) {
	cs := k8sinfo.cs
	gslbutils.Logf("k8scontroller: %s, msg: %s", c.name, "creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(containerutils.AviLog.Info.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})

	k8sQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	c.workqueue = k8sQueue.Workqueue
	numWorkers := k8sQueue.NumWorkers
	ingressEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ingr := obj.(*extensionv1beta1.Ingress)
			// Don't add this ingr if there's no status field present or no IP is allocated in this
			// status field
			if rejectIngress(ingr) {
				containerutils.AviLog.Info.Printf("Rejecting ADD Ingress: %v", ingr)
				return
			}
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ingr))
			key := gslbutils.MultiClusterKey(gslbutils.ObjectAdd, "Ingress/", c.name, ingr.ObjectMeta.Namespace,
				ingr.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added ADD Ingress key from the kubernetes controller %s", key)
		},
		DeleteFunc: func(obj interface{}) {
			ingr, ok := obj.(*extensionv1beta1.Ingress)
			if !ok {
				containerutils.AviLog.Error.Printf("object type is not Ingress")
			}
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ingr))
			key := gslbutils.MultiClusterKey(gslbutils.ObjectDelete, "Ingress/", c.name, ingr.ObjectMeta.Namespace,
				ingr.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added DELETE Ingress key from the kubernetes controller %s", key)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldIngr := old.(*extensionv1beta1.Ingress)
			ingr := curr.(*extensionv1beta1.Ingress)
			if oldIngr.ResourceVersion != ingr.ResourceVersion {
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ingr))
				key := gslbutils.MultiClusterKey(gslbutils.ObjectUpdate, "Ingress/", c.name, ingr.ObjectMeta.Namespace, ingr.ObjectMeta.Name)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Info.Printf("UPDATE Ingress key: %s", key)
			}
		},
	}

	acceptedRouteStore := gslbutils.GetAcceptedRouteStore()
	rejectedRouteStore := gslbutils.GetRejectedRouteStore()
	gf := filter.GetGlobalFilter()
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
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
			key := gslbutils.MultiClusterKey(gslbutils.ObjectAdd, "Route/", c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			gslbutils.Logf("cluster: %s, ns: %s, route: %s, key: %s, msg: %s\n", c.name, namespace,
				route.ObjectMeta.Namespace, key, "added ADD route key")
		},
		DeleteFunc: func(obj interface{}) {
			route, ok := obj.(*routev1.Route)
			if !ok {
				containerutils.AviLog.Error.Printf("object type is not route")
				return
			}
			// Delete from all route stores
			DeleteFromRouteStore(acceptedRouteStore, route, c.name)
			DeleteFromRouteStore(rejectedRouteStore, route, c.name)
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
			key := gslbutils.MultiClusterKey(gslbutils.ObjectDelete, "Route/", c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			gslbutils.Logf("cluster: %s, ns: %s, route: %s, key: %s, msg: %s", c.name, namespace,
				route.ObjectMeta.Namespace, key, "added DELETE route key")
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
					MoveObjects([]string{multiClusterRouteName}, acceptedRouteStore, rejectedRouteStore)
					fetchedRoute := fetchedObj.(k8sobjects.RouteMeta)
					// Add a DELETE key for this route
					key := gslbutils.MultiClusterKey(gslbutils.ObjectDelete, "Route/", c.name, fetchedRoute.Namespace,
						fetchedRoute.Name)
					bkt := containerutils.Bkt(fetchedRoute.Namespace, numWorkers)
					c.workqueue[bkt].AddRateLimited(key)
					gslbutils.Logf("cluster: %s, ns: %s, route: %s, key: %s, msg: %s", c.name, fetchedRoute.Namespace,
						fetchedRoute.Name, key, "added DELETE route key")
					return
				}
				AddOrUpdateRouteStore(acceptedRouteStore, route, c.name)
				// If the route was already part of rejected store, we need to remove from
				// this route from the rejected store.
				rejectedRouteStore.DeleteClusterNSObj(c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
				// Add the key for this route to the queue.
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
				key := gslbutils.MultiClusterKey(gslbutils.ObjectUpdate, "Route/", c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, route: %s, key: %s, msg: %s", c.name, namespace,
					route.ObjectMeta.Name, key, "added UPDATE route key")
			}
		},
	}

	if c.informers.ExtV1IngressInformer != nil {
		c.informers.ExtV1IngressInformer.Informer().AddEventHandler(ingressEventHandler)
	}

	if c.informers.RouteInformer != nil {
		c.informers.RouteInformer.Informer().AddEventHandler(routeEventHandler)
	}

	if c.informers.ServiceInformer != nil {
		lbsvcEventHandler := c.addLBSvcEventHandler(numWorkers)
		c.informers.ServiceInformer.Informer().AddEventHandler(lbsvcEventHandler)
	}
}

func (c *GSLBMemberController) addLBSvcEventHandler(numWorkers uint32) cache.ResourceEventHandler {
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
				gslbutils.Logf("cluster: %s, ns:%s, svc %s, type not lb", c.name, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
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
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(svc))
			key := gslbutils.MultiClusterKey(gslbutils.ObjectAdd, "LBSvc/", c.name, svc.ObjectMeta.Namespace,
				svc.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added ADD LBSvc key from the kubernetes controller %s", key)
		},
		DeleteFunc: func(obj interface{}) {
			svc, ok := obj.(*corev1.Service)
			if !ok {
				containerutils.AviLog.Error.Printf("object type is not Svc")
				return
			}
			if !isSvcTypeLB(svc) {
				return
			}
			DeleteFromLBSvcStore(acceptedLBSvcStore, svc, c.name)
			DeleteFromLBSvcStore(rejectedLBSvcStore, svc, c.name)
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(svc))
			key := gslbutils.MultiClusterKey(gslbutils.ObjectDelete, "LBSvc/", c.name, svc.ObjectMeta.Namespace,
				svc.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added DELETE LBSvc key from the kubernetes controller %s", key)
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
					MoveObjects([]string{multiClusterSvcName}, acceptedLBSvcStore, rejectedLBSvcStore)
					fetchedSvc := fetchedObj.(k8sobjects.SvcMeta)
					// Add a DELETE key for this svc
					key := gslbutils.MultiClusterKey(gslbutils.ObjectDelete, "LBSvc/", c.name, fetchedSvc.Namespace,
						fetchedSvc.Name)
					bkt := containerutils.Bkt(fetchedSvc.Namespace, numWorkers)
					c.workqueue[bkt].AddRateLimited(key)
					gslbutils.Logf("cluster: %s, ns: %s, svc: %s, key: %s, msg: %s", c.name, fetchedSvc.Namespace,
						fetchedSvc.Name, key, "added DELETE svc key")
					return
				}
				AddOrUpdateLBSvcStore(acceptedLBSvcStore, svc, c.name)
				// If the svc was already part of rejected store, we need to remove from
				// this svc from the rejected store.
				rejectedLBSvcStore.DeleteClusterNSObj(c.name, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
				// Add the key for this svc to the queue.

				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(svc))
				key := gslbutils.MultiClusterKey(gslbutils.ObjectUpdate, "LBSvc/", c.name, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Info.Printf("UPDATE LBSvc key: %s", key)
			}
		},
	}
	return svcEventHandler
}

func isSvcTypeLB(svc *corev1.Service) bool {
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		return true
	}
	return false
}

// AddOrUpdateLBSvcStore traverses through the cluster store for cluster name cname,
// and then to ns store for the service's namespace and then adds/updates the service obj
// in the object map store.
func AddOrUpdateLBSvcStore(clusterSvcStore *gslbutils.ClusterStore,
	svc *corev1.Service, cname string) {
	svcMeta, _ := k8sobjects.GetSvcMeta(svc, cname)
	gslbutils.Logf("updating service store: %s", svc.ObjectMeta.Name)
	clusterSvcStore.AddOrUpdate(svcMeta, cname, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
}

// DeleteFromLBSvcStore traverses through the cluster store for cluster name cname,
// and then ns store for the service's namespace and then deletes the service key from
// the object map store.
func DeleteFromLBSvcStore(clusterSvcStore *gslbutils.ClusterStore,
	svc *corev1.Service, cname string) {
	if clusterSvcStore == nil {
		// Store is empty, so, noop
		return
	}
	clusterSvcStore.DeleteClusterNSObj(cname, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
}

func (c *GSLBMemberController) Start(stopCh <-chan struct{}) {
	var cacheSyncParam []cache.InformerSynced
	if c.informers.ExtV1IngressInformer != nil {
		go c.informers.ExtV1IngressInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.ExtV1IngressInformer.Informer().HasSynced)
	}

	if c.informers.RouteInformer != nil {
		gslbutils.Logf("cluster: %s, msg: %s", c.name, "starting route informer")
		go c.informers.RouteInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.RouteInformer.Informer().HasSynced)
	}

	if c.informers.ServiceInformer != nil {
		gslbutils.Logf("cluster: %s, msg: %s", c.name, "starting route informer")
		go c.informers.ServiceInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.ServiceInformer.Informer().HasSynced)
	}

	if !cache.WaitForCacheSync(stopCh, cacheSyncParam...) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
	} else {
		gslbutils.Logf("cluster: %s, msg: %s", c.name, "caches synced")
	}
}

func (c *GSLBMemberController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()

	gslbutils.Logf("cluster: %s, msg: %s", c.name, "started the kubernetes controller")
	<-stopCh
	gslbutils.Logf("cluster: %s, msg: %s", c.name, "shutting down the kubernetes controller")
	return nil
}
