package ingestion

import (
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	filter "gitlab.eng.vmware.com/orion/mcc/gslb/gdp_filter"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
	gdpalphav1 "gitlab.eng.vmware.com/orion/mcc/pkg/apis/avilb/v1alpha1"
	gslbcs "gitlab.eng.vmware.com/orion/mcc/pkg/client/clientset/versioned"
	gdpscheme "gitlab.eng.vmware.com/orion/mcc/pkg/client/clientset/versioned/scheme"
	gslbinformers "gitlab.eng.vmware.com/orion/mcc/pkg/client/informers/externalversions"
	gdplisters "gitlab.eng.vmware.com/orion/mcc/pkg/client/listers/avilb/v1alpha1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// GDPAddDelfn is a type of function which handles an add or a delete of a GDP
// object
type GDPAddDelfn func(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32)

// GDPUpdfn is a function type which handles an update of a GDP object.
type GDPUpdfn func(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32)

// GDPController defines the members required to hold an instance of a controller
// handling GDP events.
type GDPController struct {
	kubeclientset kubernetes.Interface
	gdpclientset  gslbcs.Interface
	gdpLister     gdplisters.GlobalDeploymentPolicyLister
	gdpSynced     cache.InformerSynced
	workqueue     workqueue.RateLimitingInterface
	recorder      record.EventRecorder
}

func (gdpController *GDPController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	gslbutils.Logf("object: GDPController, msg: %s", "starting the workers")
	<-stopCh
	gslbutils.Logf("object: GDPController, msg: %s", "shutting down the workers")
	return nil
}

// MoveRoutes moves the route objects in "routeList" from "fromStore" to
// "toStore".
func MoveRoutes(routeList []string, fromStore *gslbutils.ClusterStore, toStore *gslbutils.ClusterStore) {
	for _, routeName := range routeList {
		// routeName consists of cluster name, namespace and the route name
		cname, ns, route, err := gslbutils.SplitMultiClusterRouteName(routeName)
		if err != nil {
			gslbutils.Errf("route: %s, msg: processing error, %s", routeName, err)
			continue
		}
		routeObj, ok := fromStore.DeleteClusterNSObj(cname, ns, route)
		if ok {
			// Object was found, add this to the "toStore"
			toStore.AddOrUpdate(routeObj, cname, ns, route)
		}
	}
}

func writeChangedRoutesToQueue(k8swq []workqueue.RateLimitingInterface, numWorkers uint32, trafficWeightChanged bool) {
	acceptedRouteStore := gslbutils.AcceptedRouteStore
	rejectedRouteStore := gslbutils.RejectedRouteStore
	gf := filter.GetGlobalFilter()
	if acceptedRouteStore != nil {
		// If we have routes in the accepted store, each one has to be passed through
		// the filter again. If any route fails to pass through the filter, we need to
		// add DELETE keys for them.
		acceptedList, rejectedList := acceptedRouteStore.GetAllFilteredClusterNSObjects(gf.ApplyFilter)
		if len(rejectedList) != 0 {
			gslbutils.Logf("routeList: %v, msg: %s", rejectedList, "route list will be deleted")
			// Since, these routes are now rejected, they have to be moved to
			// the rejected list.
			MoveRoutes(rejectedList, acceptedRouteStore, rejectedRouteStore)
			for _, routeName := range rejectedList {
				cname, ns, rname, err := gslbutils.SplitMultiClusterRouteName(routeName)
				if err != nil {
					gslbutils.Errf("route: %s, msg: processing error, %s", routeName, err)
					continue
				}

				bkt := utils.Bkt(ns, numWorkers)
				key := gslbutils.MultiClusterKey(gslbutils.ObjectDelete, "Route/", cname, ns, rname)
				k8swq[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, route: %s, key: %s, msg: %s\n", cname, ns, rname,
					key, "added DELETE route key")
			}
		}
		// if the traffic weight changed, then the accepted list has to be sent to the nodes layer
		for _, routeName := range acceptedList {
			cname, ns, rname, err := gslbutils.SplitMultiClusterRouteName(routeName)
			if err != nil {
				gslbutils.Errf("route: %s, msg: processing error, %s", routeName, err)
				continue
			}
			bkt := utils.Bkt(ns, numWorkers)
			key := gslbutils.MultiClusterKey(gslbutils.ObjectUpdate, "Route/", cname, ns, rname)
			k8swq[bkt].AddRateLimited(key)
			gslbutils.Logf("cluster: %s, ns: %s, route: %s, key: %s, msg: %s", cname, ns, rname, key,
				"added ADD route key")
		}
	}

	if rejectedRouteStore != nil {
		// If we have routes in the rejected store, each one has to be passed through
		// the filter again. If any route passes through the filter, we need to add ADD
		// keys for them.
		acceptedList, _ := rejectedRouteStore.GetAllFilteredClusterNSObjects(gf.ApplyFilter)
		if len(acceptedList) != 0 {
			gslbutils.Logf("routeList: %v, msg: %s", acceptedList, "route list will be added")
			MoveRoutes(acceptedList, rejectedRouteStore, acceptedRouteStore)
			for _, routeName := range acceptedList {
				cname, ns, rname, err := gslbutils.SplitMultiClusterRouteName(routeName)
				if err != nil {
					gslbutils.Errf("route: %s, msg: processing error, %s", routeName, err)
					continue
				}
				bkt := utils.Bkt(ns, numWorkers)
				key := gslbutils.MultiClusterKey(gslbutils.ObjectAdd, "Route/", cname, ns, rname)
				k8swq[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, route: %s, key: %s, msg: %s", cname, ns, rname, key,
					"added ADD route key")
			}
		}
	}
}

// AddGDPObj creates a new GlobalFilter if not present on the first GDP object. Subsequent ADD calls add
// on to the existing GlobalFilter. For each namespace, there can only be one filter. So, if a filter
// already exists for a namespace, a user needs to edit that and not add a new one. This rule is taken
// care of in the admission controller. All in all, a namespace can have only one GDP object and hence,
// only one filter object.
func AddGDPObj(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	gdp := obj.(*gdpalphav1.GlobalDeploymentPolicy)
	gslbutils.Logf("ns: %s, gdp: %s, msg: %s", gdp.ObjectMeta.Namespace, gdp.ObjectMeta.Name,
		"GDP object added")
	gf := filter.GetGlobalFilter()
	if gf == nil {
		// Create a new GlobalFilter
		gslbutils.Logf("creating a new filter")
		gf = filter.GetNewGlobalFilter(obj)
		filter.Gfi = gf
		writeChangedRoutesToQueue(k8swq, numWorkers, false)
		return
	}

	// Else, add on to the existing GlobalFilter.
	gf.AddToGlobalFilter(gdp)
	writeChangedRoutesToQueue(k8swq, numWorkers, false)
}

// UpdateGDPObj updates the global and the namespace filters if a the GDP object
// was really changed. The update of a GDP object also requires re-evaluation of
// all the previously accepted and rejected objects. Hence, those are re-evaluated
// and added or deleted based on whether or not, they pass the new fitler objects.
// TODO: Optimize the filter process a bit more based on how the filters are processed.
func UpdateGDPObj(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	oldGdp := old.(*gdpalphav1.GlobalDeploymentPolicy)
	newGdp := new.(*gdpalphav1.GlobalDeploymentPolicy)
	if oldGdp.ObjectMeta.ResourceVersion == newGdp.ObjectMeta.ResourceVersion {
		return
	}
	gf := filter.GetGlobalFilter()
	// utils.AviLog.Info.Printf("old: %v, new: %v", oldGdp, newGdp)
	if gf == nil {
		// global filter not initialized, return
		gslbutils.Errf("object: GlobalFilter, msg: global filter not initialized, can't update")
		return
	}
	if gdpChanged, trafficWeightChanged := gf.UpdateGlobalFilter(oldGdp, newGdp); gdpChanged {
		gslbutils.Logf("GDP object changed, will go through the routes again")
		writeChangedRoutesToQueue(k8swq, numWorkers, trafficWeightChanged)
	}
}

// DeleteGDPObj requires to delete the filters that were previously created. If a GDP
// object is deleted, the previously accepted and rejected objects need to pass through
// this filter again to find out which filter is applicable, the global one or the
// local one.
func DeleteGDPObj(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	gdp := obj.(*gdpalphav1.GlobalDeploymentPolicy)
	gslbutils.Logf("ns: %s, gdp: %s, msg: %s", gdp.ObjectMeta.Namespace, gdp.ObjectMeta.Name,
		"deleted GDP object")
	gf := filter.GetGlobalFilter()
	if gf == nil {
		gslbutils.Errf("object: GlobalFilter, msg: global filter not initialized, can't delete")
		return
	}
	gf.DeleteFromGlobalFilter(gdp)
	// Need to re-evaluate the routes again according to the deleted filter
	writeChangedRoutesToQueue(k8swq, numWorkers, false)
}

// InitializeGDPController handles initialization of a controller which handles
// GDP object events. Also, starts the required informers for this.
func InitializeGDPController(kubeclientset kubernetes.Interface,
	gdpclientset gslbcs.Interface,
	gslbInformerFactory gslbinformers.SharedInformerFactory,
	AddGDPFunc GDPAddDelfn, UpdateGDPFunc GDPUpdfn,
	DeleteGDPFunc GDPAddDelfn) *GDPController {

	gdpInformer := gslbInformerFactory.Avilb().V1alpha1().GlobalDeploymentPolicies()
	gdpscheme.AddToScheme(scheme.Scheme)
	gslbutils.Logf("object: GDPController, msg: %s", "creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(utils.AviLog.Info.Printf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	k8sQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	k8sWorkqueue := k8sQueue.Workqueue
	numWorkers := k8sQueue.NumWorkers

	//recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "gdp-controller"})
	gdpController := &GDPController{
		kubeclientset: kubeclientset,
		gdpclientset:  gdpclientset,
		gdpLister:     gdpInformer.Lister(),
		gdpSynced:     gdpInformer.Informer().HasSynced,
		// workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "gdps"),
		//recorder:      recorder,
	}
	gslbutils.Logf("object: GDPController, msg: %s", "setting up event handlers")
	// Event handlers for GDP change
	gdpInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			AddGDPFunc(obj, k8sWorkqueue, numWorkers)
		},
		UpdateFunc: func(old, new interface{}) {
			UpdateGDPFunc(old, new, k8sWorkqueue, numWorkers)
		},
		DeleteFunc: func(obj interface{}) {
			DeleteGDPFunc(obj, k8sWorkqueue, numWorkers)
		},
	})

	return gdpController
}
