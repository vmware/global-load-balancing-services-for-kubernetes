package gslb

import (
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
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

var (
	// Need to keep this global since, it will be used across multiple layers and multiple handlers
	gf *GlobalFilter
)

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
	utils.AviLog.Info.Print("Starting the workers for GDP controller...")
	<-stopCh
	utils.AviLog.Info.Print("Shutting down the workers for GDP controller...")
	return nil
}

// MoveRoutes moves the route objects in "routeList" from "fromStore" to
// "toStore".
func MoveRoutes(routeList []string, fromStore *gslbutils.ClusterStore, toStore *gslbutils.ClusterStore) {
	for _, routeName := range routeList {
		// routeName consists of cluster name, namespace and the route name
		cname, ns, route, err := gslbutils.SplitMultiClusterRouteName(routeName)
		if err != nil {
			utils.AviLog.Error.Printf("Error processing the route name %s: %s", routeName, err.Error())
			continue
		}
		routeObj, ok := fromStore.DeleteClusterNSObj(cname, ns, route)
		if ok {
			// Object was found, add this to the "toStore"
			toStore.AddOrUpdate(routeObj, cname, ns, route)
		}
	}
}

func writeChangedRoutesToQueue(k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	if acceptedRouteStore != nil {
		// If we have routes in the accepted store, each one has to be passed through
		// the filter again. If any route fails to pass through the filter, we need to
		// add DELETE keys for them.
		_, rejectedList := acceptedRouteStore.GetAllFilteredClusterNSObjects(gf.ApplyFilter)
		if len(rejectedList) != 0 {
			utils.AviLog.Info.Printf("These routes will be deleted: %v\n", rejectedList)
			// Since, these routes are now rejected, they have to be moved to
			// the rejected list.
			MoveRoutes(rejectedList, acceptedRouteStore, rejectedRouteStore)
			for _, routeName := range rejectedList {
				_, ns, _, err := gslbutils.SplitMultiClusterRouteName(routeName)
				if err != nil {
					utils.AviLog.Error.Printf("Error processing the route %s: %s", routeName, err.Error())
					continue
				}
				bkt := utils.Bkt(ns, numWorkers)
				k8swq[bkt].AddRateLimited(routeName)
				utils.AviLog.Info.Printf("Added DELETE Route key from the cache %s", routeName)
			}
		}
	}
	if rejectedRouteStore != nil {
		// If we have routes in the rejected store, each one has to be passed through
		// the filter again. If any route passes through the filter, we need to add ADD
		// keys for them.
		acceptedList, _ := rejectedRouteStore.GetAllFilteredClusterNSObjects(gf.ApplyFilter)
		if len(acceptedList) != 0 {
			utils.AviLog.Info.Printf("These routes will be added: %v\n", acceptedList)
			MoveRoutes(acceptedList, rejectedRouteStore, acceptedRouteStore)
			for _, routeName := range acceptedList {
				_, ns, _, err := gslbutils.SplitMultiClusterRouteName(routeName)
				if err != nil {
					utils.AviLog.Error.Printf("Error processing the route %s: %s", routeName, err.Error())
					continue
				}
				bkt := utils.Bkt(ns, numWorkers)
				k8swq[bkt].AddRateLimited(routeName)
				utils.AviLog.Info.Printf("Added ADD Route key from the cache %s", routeName)
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
	utils.AviLog.Info.Printf("gdp object %s in namespace %s added\n",
		gdp.ObjectMeta.Name, gdp.ObjectMeta.Namespace)
	if gf == nil {
		// Create a new GlobalFilter
		gf = GetNewGlobalFilter(obj)
		writeChangedRoutesToQueue(k8swq, numWorkers)
		return
	}

	// Else, add on to the existing GlobalFilter.
	gf.AddToGlobalFilter(gdp)
	writeChangedRoutesToQueue(k8swq, numWorkers)
}

// UpdateGDPObj updates the global and the namespace filters if a the GDP object
// was really changed. The update of a GDP object also requires re-evaluation of
// all the previously accepted and rejected objects. Hence, those are re-evaluated
// and added or deleted based on whether or not, they pass the new fitler objects.
// TODO: Optimize the filter process a bit more based on how the filters are processed.
func UpdateGDPObj(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	oldGdp := old.(*gdpalphav1.GlobalDeploymentPolicy)
	newGdp := new.(*gdpalphav1.GlobalDeploymentPolicy)
	// utils.AviLog.Info.Printf("old: %v, new: %v", oldGdp, newGdp)
	if gf == nil {
		// global filter not initialized, return
		utils.AviLog.Error.Print("Can't update the global filter if its not initialized... returning")
		return
	}
	if changed := gf.UpdateGlobalFilter(oldGdp, newGdp); changed {
		writeChangedRoutesToQueue(k8swq, numWorkers)
	}
}

// DeleteGDPObj requires to delete the filters that were previously created. If a GDP
// object is deleted, the previously accepted and rejected objects need to pass through
// this filter again to find out which filter is applicable, the global one or the
// local one.
func DeleteGDPObj(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	gdp := obj.(*gdpalphav1.GlobalDeploymentPolicy)
	utils.AviLog.Info.Printf("deleted GDP Object: %v", gdp)
	gf.DeleteFromGlobalFilter(gdp)
	// Need to re-evaluate the routes again according to the deleted filter
	writeChangedRoutesToQueue(k8swq, numWorkers)
}

// InitializeGDPController handles initialization of a controller which handles
// GDP object events. Also, starts the required informers for this.
func InitializeGDPController(kubeclientset *kubernetes.Clientset,
	gdpclientset gslbcs.Interface,
	gslbInformerFactory gslbinformers.SharedInformerFactory,
	AddGDPFunc GDPAddDelfn, UpdateGDPFunc GDPUpdfn,
	DeleteGDPFunc GDPAddDelfn) {

	gdpInformer := gslbInformerFactory.Avilb().V1alpha1().GlobalDeploymentPolicies()
	gdpscheme.AddToScheme(scheme.Scheme)
	utils.AviLog.Info.Print("Creating event broadcaster for GDP controller")
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
	utils.AviLog.Info.Printf("Setting up event handlers for GDP controller")
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

	// Start the informer for the GDP controller
	go gdpInformer.Informer().Run(stopCh)

	if err := gdpController.Run(stopCh); err != nil {
		utils.AviLog.Error.Fatalf("Error running controller: %s\n", err.Error())
	}
}
