package gslb

import (
	"fmt"
	"sync"

	routev1 "github.com/openshift/api/route/v1"
	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/runtime"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

var (
	// Cluster Routes store for all the route objects.
	acceptedRouteStore *gslbutils.ClusterStore
	rejectedRouteStore *gslbutils.ClusterStore
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

func rejectRoute(route *routev1.Route) bool {
	// Return true if the IP address is present in an route's status field, else return false
	routeStatus := route.Status
	for _, ingr := range routeStatus.Ingress {
		conditions := ingr.Conditions
		for _, condition := range conditions {
			// TODO: Check if the message field contains an IP address
			if condition.Message != "" {
				return false
			}
		}
	}
	return true
}

func initializeClusterRouteStore() *gslbutils.ClusterStore {
	return gslbutils.NewClusterStore()
}

// AddOrUpdateRouteStore traverses through the cluster store for cluster name cname,
// and then to ns store for the route's namespace and then adds/updates the route obj
// in the object map store.
func AddOrUpdateRouteStore(clusterRouteStore *gslbutils.ClusterStore,
	route *routev1.Route, cname string) {
	clusterRouteStore.AddOrUpdate(route, cname, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
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
	containerutils.AviLog.Info.Printf("Creating event broadcaster for %v", c.name)
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
			key := gslbutils.MultiClusterKey("Ingress/", c.name, ingr.ObjectMeta.Namespace, ingr.ObjectMeta.Name)
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
			key := gslbutils.MultiClusterKey("Ingress/", c.name, ingr.ObjectMeta.Namespace, ingr.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added DELETE Ingress key from the kubernetes controller %s", key)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldIngr := old.(*extensionv1beta1.Ingress)
			ingr := curr.(*extensionv1beta1.Ingress)
			if oldIngr.ResourceVersion != ingr.ResourceVersion {
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ingr))
				key := gslbutils.MultiClusterKey("Ingress/", c.name, ingr.ObjectMeta.Namespace, ingr.ObjectMeta.Name)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Info.Printf("UPDATE Ingress key: %s", key)
			}
		},
	}

	if acceptedRouteStore == nil {
		containerutils.AviLog.Info.Print("Initializing accepted route store")
		acceptedRouteStore = initializeClusterRouteStore()
	}
	if rejectedRouteStore == nil {
		containerutils.AviLog.Info.Print("Initializing rejected route store")
		rejectedRouteStore = initializeClusterRouteStore()
	}
	routeEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			route := obj.(*routev1.Route)
			// Don't add this route if there's no status field present or no IP is allocated in this
			// status field
			// TODO: See if we can change rejectRoute to Graph layer.
			if rejectRoute(route) {
				containerutils.AviLog.Info.Printf("Rejecting ADD route: %s, cluster: %s",
					route.ObjectMeta.Name, c.name)
				return
			}
			if gf == nil || !gf.ApplyFilter(route, c.name) {
				containerutils.AviLog.Info.Printf("Rejecting ADD route: %s, cluster: %s",
					route.ObjectMeta.Name, c.name)
				AddOrUpdateRouteStore(rejectedRouteStore, route, c.name)
				return
			}
			containerutils.AviLog.Info.Printf("route %s being added", route.ObjectMeta.Name)
			AddOrUpdateRouteStore(acceptedRouteStore, route, c.name)
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
			key := gslbutils.MultiClusterKey("Route/", c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added ADD Route key from the controller: %s, cluster: %s",
				key, c.name)
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
			key := gslbutils.MultiClusterKey("Route/", c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added DELETE Route key from the kubernetes controller %s", key)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldRoute := old.(*routev1.Route)
			route := curr.(*routev1.Route)
			if oldRoute.ResourceVersion != route.ResourceVersion {
				if gf == nil || !gf.ApplyFilter(route, c.name) {
					AddOrUpdateRouteStore(rejectedRouteStore, route, c.name)
					// See if the route was already accepted, if yes, need to delete the key
					fetchedObj, ok := acceptedRouteStore.GetClusterNSObjectByName(c.name,
						oldRoute.ObjectMeta.Namespace, oldRoute.ObjectMeta.Name)
					if ok {
						multiClusterRouteName := c.name + "/" + route.ObjectMeta.Namespace + "/" + route.ObjectMeta.Name
						MoveRoutes([]string{multiClusterRouteName}, acceptedRouteStore, rejectedRouteStore)
					}
					fetchedRoute := fetchedObj.(*routev1.Route)
					// Add a DELETE key for this route
					key := gslbutils.MultiClusterKey("Route/", c.name, fetchedRoute.ObjectMeta.Namespace,
						fetchedRoute.ObjectMeta.Name)
					bkt := containerutils.Bkt(fetchedRoute.ObjectMeta.Namespace, numWorkers)
					c.workqueue[bkt].AddRateLimited(key)
					containerutils.AviLog.Info.Printf("Added DELETE Route key: %s", key)
					return
				}
				AddOrUpdateRouteStore(acceptedRouteStore, route, c.name)
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
				key := gslbutils.MultiClusterKey("Route/", c.name, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Info.Printf("UPDATE Route key: %s", key)
			}
		},
	}

	if c.informers.IngressInformer != nil {
		c.informers.IngressInformer.Informer().AddEventHandler(ingressEventHandler)
	}

	if c.informers.RouteInformer != nil {
		c.informers.RouteInformer.Informer().AddEventHandler(routeEventHandler)
	}
}

func (c *GSLBMemberController) Start(stopCh <-chan struct{}) {
	var cacheSyncParam []cache.InformerSynced
	if c.informers.IngressInformer != nil {
		go c.informers.IngressInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.IngressInformer.Informer().HasSynced)
	}

	if c.informers.RouteInformer != nil {
		containerutils.AviLog.Info.Printf("starting route informer for cluster %s\n",
			c.name)
		go c.informers.RouteInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.RouteInformer.Informer().HasSynced)
	}

	if !cache.WaitForCacheSync(stopCh, cacheSyncParam...) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
	} else {
		containerutils.AviLog.Info.Print("Caches synced")
	}
}

func (c *GSLBMemberController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()

	containerutils.AviLog.Info.Printf("Started the Kubernetes Controller %s", c.name)
	<-stopCh
	containerutils.AviLog.Info.Printf("Shutting down the Kubernetes Controller: %s", c.name)
	return nil
}
