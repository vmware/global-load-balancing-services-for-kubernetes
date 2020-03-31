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

	gdpalphav1 "amko/pkg/apis/avilb/v1alpha1"
	gslbcs "amko/pkg/client/clientset/versioned"
	gdpscheme "amko/pkg/client/clientset/versioned/scheme"
	gslbinformers "amko/pkg/client/informers/externalversions"
	gdplisters "amko/pkg/client/listers/avilb/v1alpha1"

	"github.com/avinetworks/container-lib/utils"
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
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

// MoveObjs moves the objects in "objList" from "fromStore" to "toStore".
// TODO: call this function via an interface, so we can remove dependency
//       on objType.
func MoveObjs(objList []string, fromStore *gslbutils.ClusterStore, toStore *gslbutils.ClusterStore, objType string) {
	var cname, ns, objName string
	var err error
	for _, multiClusterObjName := range objList {
		if objType == gslbutils.IngressType {
			var hostName string
			cname, ns, objName, hostName, err = gslbutils.SplitMultiClusterIngHostName(multiClusterObjName)
			if err != nil {
				gslbutils.Errf("objType: %s, object: %s, msg: processing error, %s", objType,
					objName, err)
				continue
			}
			objName += "/" + hostName
		} else {
			// for routes and services
			// objName consists of cluster name, namespace and the route/service name
			cname, ns, objName, err = gslbutils.SplitMultiClusterObjectName(multiClusterObjName)
			if err != nil {
				gslbutils.Errf("objType: %s, object: %s, msg: processing error, %s", objType,
					objName, err)
				continue
			}
		}
		obj, ok := fromStore.DeleteClusterNSObj(cname, ns, objName)
		if ok {
			// Object was found, add this to the "toStore"
			toStore.AddOrUpdate(obj, cname, ns, objName)
		}
	}
}

func splitName(objType, objName string) (string, string, string, error) {
	var cname, ns, sname, hostname string
	var err error
	if objType == gdpalphav1.IngressObj {
		cname, ns, sname, hostname, err = gslbutils.SplitMultiClusterIngHostName(objName)
		sname += "/" + hostname
	} else {
		cname, ns, sname, err = gslbutils.SplitMultiClusterObjectName(objName)
	}
	return cname, ns, sname, err
}

func writeChangedObjToQueue(objType string, k8swq []workqueue.RateLimitingInterface, numWorkers uint32, trafficWeightChanged bool) {
	var acceptedObjStore *gslbutils.ClusterStore
	var rejectedObjStore *gslbutils.ClusterStore
	var objKey string
	var cname, ns, sname string
	var err error

	if objType == gdpalphav1.RouteObj {
		acceptedObjStore = gslbutils.GetAcceptedRouteStore()
		rejectedObjStore = gslbutils.GetRejectedRouteStore()
		objKey = gslbutils.RouteType
	} else if objType == gdpalphav1.LBSvcObj {
		acceptedObjStore = gslbutils.GetAcceptedLBSvcStore()
		rejectedObjStore = gslbutils.GetRejectedLBSvcStore()
		objKey = gslbutils.SvcType
	} else if objType == gdpalphav1.IngressObj {
		acceptedObjStore = gslbutils.GetAcceptedIngressStore()
		rejectedObjStore = gslbutils.GetRejectedIngressStore()
		objKey = gslbutils.IngressType
	} else {
		gslbutils.Warnf("Unknown Object type: %s", objType)
		return
	}
	gf := filter.GetGlobalFilter()
	if acceptedObjStore != nil {
		// If we have objects in the accepted store, each one has to be passed through
		// the filter again. If any object fails to pass through the filter, we need to
		// add DELETE keys for them.
		acceptedList, rejectedList := acceptedObjStore.GetAllFilteredClusterNSObjects(gf.ApplyFilter)
		if len(rejectedList) != 0 {
			gslbutils.Logf("ObjList: %v, msg: %s", rejectedList, "obj list will be deleted")
			// Since, these objects are now rejected, they have to be moved to
			// the rejected list.
			MoveObjs(rejectedList, acceptedObjStore, rejectedObjStore, objKey)
			for _, objName := range rejectedList {
				cname, ns, sname, err = splitName(objType, objName)
				if err != nil {
					gslbutils.Errf("cluster: %s, msg: couldn't process object, objtype: %s, name: %s, error, %s",
						cname, objType, objName, err)
					continue
				}

				bkt := utils.Bkt(ns, numWorkers)
				key := gslbutils.MultiClusterKey(gslbutils.ObjectDelete, objKey, cname, ns, sname)
				k8swq[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, objType:%s, name: %s, key: %s, msg: added DELETE obj key",
					cname, ns, objType, sname, key)
			}
		}
		// if the traffic weight changed, then the accepted list has to be sent to the nodes layer
		for _, objName := range acceptedList {
			cname, ns, sname, err = splitName(objType, objName)
			if err != nil {
				gslbutils.Errf("msg: couldn't split the key: %s, error, %s", objName, err)
				continue
			}
			bkt := utils.Bkt(ns, numWorkers)
			key := gslbutils.MultiClusterKey(gslbutils.ObjectUpdate, objKey, cname, ns, sname)
			k8swq[bkt].AddRateLimited(key)
			gslbutils.Logf("cluster: %s, ns: %s, objtype: %s, name: %s, key: %s, msg: added key",
				cname, ns, objType, sname, key)
		}
	}

	if rejectedObjStore != nil {
		// If we have objects in the rejected store, each one has to be passed through
		// the filter again. If any object passes through the filter, we need to add ADD
		// keys for them.
		acceptedList, _ := rejectedObjStore.GetAllFilteredClusterNSObjects(gf.ApplyFilter)
		if len(acceptedList) != 0 {
			gslbutils.Logf("ObjList: %v, msg: %s", acceptedList, "object list will be added")
			MoveObjs(acceptedList, rejectedObjStore, acceptedObjStore, objKey)
			for _, objName := range acceptedList {
				cname, ns, sname, err = splitName(objType, objName)
				if err != nil {
					gslbutils.Errf("objName: %s, msg: processing error, %s", objName, err)
					continue
				}
				bkt := utils.Bkt(ns, numWorkers)
				key := gslbutils.MultiClusterKey(gslbutils.ObjectAdd, objKey, cname, ns, sname)
				k8swq[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, objtype:%s, name: %s, key: %s, msg: added ADD obj key",
					cname, ns, objType, sname, key)
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
	gslbutils.Logf("creating a new filter")
	gf.AddToGlobalFilter(gdp)
	writeChangedObjToQueue(gdpalphav1.RouteObj, k8swq, numWorkers, false)
	writeChangedObjToQueue(gdpalphav1.LBSvcObj, k8swq, numWorkers, false)
	writeChangedObjToQueue(gdpalphav1.IngressObj, k8swq, numWorkers, false)
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
	if gf == nil {
		// global filter not initialized, return
		gslbutils.Errf("object: GlobalFilter, msg: global filter not initialized, can't update")
		return
	}
	if gdpChanged, trafficWeightChanged := gf.UpdateGlobalFilter(oldGdp, newGdp); gdpChanged {
		gslbutils.Logf("GDP object changed, will go through the objects again")
		writeChangedObjToQueue(gdpalphav1.RouteObj, k8swq, numWorkers, trafficWeightChanged)
		writeChangedObjToQueue(gdpalphav1.LBSvcObj, k8swq, numWorkers, trafficWeightChanged)
		writeChangedObjToQueue(gdpalphav1.IngressObj, k8swq, numWorkers, trafficWeightChanged)
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
	// Need to re-evaluate the objects again according to the deleted filter
	writeChangedObjToQueue(gdpalphav1.RouteObj, k8swq, numWorkers, false)
	writeChangedObjToQueue(gdpalphav1.LBSvcObj, k8swq, numWorkers, false)
	writeChangedObjToQueue(gdpalphav1.IngressObj, k8swq, numWorkers, false)
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
