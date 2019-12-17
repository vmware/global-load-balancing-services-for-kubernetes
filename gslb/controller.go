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

// AviController is actually kubernetes cluster which is added to an AVI controller
// here which is added to an AVI controller
type AviController struct {
	name            string
	worker_id       uint32
	worker_id_mutex sync.Mutex
	informers       *containerutils.Informers
	workqueue       []workqueue.RateLimitingInterface
}

// GetAviController sets config for an AviController
func GetAviController(clusterName string, informersInstance *containerutils.Informers) AviController {
	return AviController{
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

func (c *AviController) SetupEventHandlers(k8sinfo K8SInformers) {
	cs := k8sinfo.cs
	containerutils.AviLog.Info.Printf("Creating event broadcaster for %v", c.name)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(containerutils.AviLog.Info.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})

	containerutils.AviLog.Info.Printf("c.informers: %v", c.informers)
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
			key := gslbutils.MultiClusterKey("Ingress/", c.name, ingr)
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
			key := gslbutils.MultiClusterKey("Ingress/", c.name, ingr)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added DELETE Ingress key from the kubernetes controller %s", key)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldIngr := old.(*extensionv1beta1.Ingress)
			ingr := curr.(*extensionv1beta1.Ingress)
			if oldIngr.ResourceVersion != ingr.ResourceVersion {
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ingr))
				key := gslbutils.MultiClusterKey("Ingress/", c.name, ingr)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Info.Printf("UPDATE Ingress key: %s", key)
			}
		},
	}

	routeEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			route := obj.(*routev1.Route)
			// Don't add this route if there's no status field present or no IP is allocated in this
			// status field
			if rejectRoute(route) {
				containerutils.AviLog.Info.Printf("Rejecting ADD route: %v", route)
				return
			}
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
			key := gslbutils.MultiClusterKey("Route/", c.name, route)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added ADD Route key from the controller: %v", route)
		},
		DeleteFunc: func(obj interface{}) {
			route, ok := obj.(*routev1.Route)
			if !ok {
				containerutils.AviLog.Error.Printf("object type is not route")
			}
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
			key := gslbutils.MultiClusterKey("Route/", c.name, route)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Info.Printf("Added DELETE Route key from the kubernetes controller %s", key)
		},
		UpdateFunc: func(old, curr interface{}) {
			oldRoute := old.(*routev1.Route)
			route := curr.(*routev1.Route)
			if oldRoute.ResourceVersion != route.ResourceVersion {
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
				key := gslbutils.MultiClusterKey("Route/", c.name, route)
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
		containerutils.AviLog.Info.Printf("adding route informer...")
		c.informers.RouteInformer.Informer().AddEventHandler(routeEventHandler)
	}
}

func (c *AviController) Start(stopCh <-chan struct{}) {
	var cacheSyncParam []cache.InformerSynced
	containerutils.AviLog.Info.Printf("informers: %v", c.informers)
	if c.informers.IngressInformer != nil {
		go c.informers.IngressInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.IngressInformer.Informer().HasSynced)
	}

	if c.informers.RouteInformer != nil {
		containerutils.AviLog.Info.Print("starting the route informer")
		go c.informers.RouteInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.RouteInformer.Informer().HasSynced)
	}

	if !cache.WaitForCacheSync(stopCh, cacheSyncParam...) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
	} else {
		containerutils.AviLog.Info.Print("Caches synced")
	}
}

func (c *AviController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()

	containerutils.AviLog.Info.Print("Started the Kubernetes Controller")
	<-stopCh
	containerutils.AviLog.Info.Print("Shutting down the Kubernetes Controller")
	return nil
}
