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
	"errors"
	"strconv"

	"github.com/avinetworks/amko/gslb/gslbutils"
	"github.com/avinetworks/amko/gslb/k8sobjects"

	filter "github.com/avinetworks/amko/gslb/gdp_filter"

	gdpalphav1 "github.com/avinetworks/amko/internal/apis/amko/v1alpha1"
	gslbcs "github.com/avinetworks/amko/internal/client/clientset/versioned"
	gdpscheme "github.com/avinetworks/amko/internal/client/clientset/versioned/scheme"
	gslbinformers "github.com/avinetworks/amko/internal/client/informers/externalversions"
	gdplisters "github.com/avinetworks/amko/internal/client/listers/amko/v1alpha1"

	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const (
	AlreadyExistsErr = "a GDP object already exists, can't add another"
	GDPSuccess       = "success"
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

func AddOrUpdateNSStore(clusterNSStore *gslbutils.ObjectStore, ns *corev1.Namespace, cname string) {
	nsMeta := k8sobjects.GetNSMeta(ns, cname)
	clusterNSStore.AddOrUpdate(cname, nsMeta.Name, nsMeta)
}

func DeleteFromNSStore(clusterNSStore *gslbutils.ObjectStore, ns *corev1.Namespace, cname string) {
	clusterNSStore.DeleteNSObj(cname, ns.Name)
}

func MoveNSObjs(nsObjs []string, fromStore *gslbutils.ObjectStore, toStore *gslbutils.ObjectStore) {
	var cname, ns string
	var err error
	for _, multiClusterNS := range nsObjs {
		cname, ns, err = gslbutils.SplitMultiClusterNS(multiClusterNS)
		if err != nil {
			gslbutils.Errf("objType: Namespace, object: %s, msg: processing error %s", ns, err.Error())
			continue
		}
		obj, ok := fromStore.DeleteNSObj(cname, ns)
		if ok {
			// Object was found, add this to "toStore"
			toStore.AddOrUpdate(cname, ns, obj)
		}
	}
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

func GetObjTypeStores(objType string) (string, *gslbutils.ClusterStore, *gslbutils.ClusterStore, error) {
	var objKey string
	var acceptedObjStore *gslbutils.ClusterStore
	var rejectedObjStore *gslbutils.ClusterStore

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
		gslbutils.Errf("Unknown Object type: %s", objType)
		return "", nil, nil, errors.New("unknown object type " + objType)
	}
	return objKey, acceptedObjStore, rejectedObjStore, nil
}

func writeChangedObjToQueue(objType string, k8swq []workqueue.RateLimitingInterface, numWorkers uint32, trafficWeightChanged bool) {
	var cname, ns, sname string
	var err error

	objKey, acceptedObjStore, rejectedObjStore, err := GetObjTypeStores(objType)
	if err != nil {
		gslbutils.Errf("objtype error: %s", err.Error())
		return
	}
	if acceptedObjStore != nil {
		// If we have objects in the accepted store, each one has to be passed through
		// the filter again. If any object fails to pass through the filter, we need to
		// add DELETE keys for them.
		acceptedList, rejectedList := acceptedObjStore.GetAllFilteredClusterNSObjects(filter.ApplyFilter)
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
		if trafficWeightChanged {
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
	}

	if rejectedObjStore != nil {
		// If we have objects in the rejected store, each one has to be passed through
		// the filter again. If any object passes through the filter, we need to add ADD
		// keys for them.
		acceptedList, _ := rejectedObjStore.GetAllFilteredClusterNSObjects(filter.ApplyFilter)
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

func validObjectType(objType string) bool {
	if objType == gdpalphav1.IngressObj || objType == gdpalphav1.LBSvcObj || objType == gdpalphav1.RouteObj {
		return true
	}
	return false
}

func validLabel(label map[string]string) error {
	for k, v := range label {
		if k == "" {
			return errors.New("label value is missing for key " + k)
		}
		if v == "" {
			return errors.New("label key is missing for value " + v)
		}
	}
	return nil
}

func GDPSanityChecks(gdp *gdpalphav1.GlobalDeploymentPolicy) error {
	// MatchRules checks
	mr := gdp.Spec.MatchRules
	// no app selector and no namespace selector means, no objects selected
	if len(mr.AppSelector.Label) > 0 {
		if err := validLabel(mr.AppSelector.Label); err != nil {
			return errors.New(err.Error() + " for appSelector")
		}
	}
	if len(mr.NamespaceSelector.Label) > 0 {
		if err := validLabel(mr.NamespaceSelector.Label); err != nil {
			return errors.New(err.Error() + "for namespaceSelector")
		}
	}

	// MatchClusters checks, empty matchClusters are allowed
	for _, cluster := range gdp.Spec.MatchClusters {
		if !gslbutils.IsClusterContextPresent(cluster) {
			return errors.New("cluster context " + cluster + " not present in GSLBConfig")
		}
	}

	// TrafficSplit checks
	for _, tp := range gdp.Spec.TrafficSplit {
		if !gslbutils.IsClusterContextPresent(tp.Cluster) {
			return errors.New("cluster " + tp.Cluster + " in traffic policy not present in GSLBConfig")
		}
		if tp.Weight < 1 || tp.Weight > 20 {
			return errors.New("traffic weight " + strconv.Itoa(int(tp.Weight)) + " must be between 1 and 20")
		}
	}
	return nil
}

func updateGDPStatus(gdp *gdpalphav1.GlobalDeploymentPolicy, msg string) {
	gdp.Status.ErrorStatus = msg

	// Always check this flag before writing the status on the GDP object. The reason is, for unit tests,
	// the fake client doesn't have CRD capability and hence, can't do a runtime create/update of CRDs.
	if !gslbutils.PublishGDPStatus {
		return
	}
	obj, updateErr := gslbutils.GlobalGslbClient.AmkoV1alpha1().GlobalDeploymentPolicies(gdp.GetObjectMeta().GetNamespace()).Update(gdp)
	if updateErr != nil {
		gslbutils.Errf("Error in updating the GDP status object %v: %s", obj, updateErr)
	}
}

func filterExists(f *gslbutils.GlobalFilter) bool {
	// Check if a filter already exists for this namespace
	// Check if AppFilter or NSFilter is set
	if f != nil {
		f.GlobalLock.RLock()
		defer f.GlobalLock.RUnlock()
		if f.AppFilter != nil {
			gslbutils.Debugf("no app filter")
			return true
		}
		if f.NSFilter != nil {
			f.NSFilter.Lock.RLock()
			defer f.NSFilter.Lock.RUnlock()
			if len(f.NSFilter.SelectedNS) > 0 {
				gslbutils.Debugf("no ns filter")
				return true
			}
		}
	}
	// For all other conditions, return false
	return false
}

func WriteChangedObjsToQueue(k8swq []workqueue.RateLimitingInterface, numWorkers uint32, trafficWeightChanged bool) {
	writeChangedObjToQueue(gdpalphav1.RouteObj, k8swq, numWorkers, trafficWeightChanged)
	writeChangedObjToQueue(gdpalphav1.LBSvcObj, k8swq, numWorkers, trafficWeightChanged)
	writeChangedObjToQueue(gdpalphav1.IngressObj, k8swq, numWorkers, trafficWeightChanged)
}

func applyAndUpdateNamespaces() {
	acceptedNSStore := gslbutils.GetAcceptedNSStore()
	rejectedNSStore := gslbutils.GetRejectedNSStore()

	// first move from acceptedStore to rejectedStore
	gslbutils.Logf("applying filter on all rejected namespaces")
	_, rejectedList := acceptedNSStore.GetAllFilteredNamespaces(filter.ApplyFilter)
	if len(rejectedList) != 0 {
		gslbutils.Logf("objList: %v, msg: obj list will be deleted", rejectedList)
		MoveNSObjs(rejectedList, acceptedNSStore, rejectedNSStore)
		// we also need to delete these namespaces from the filter
		for _, objName := range rejectedList {
			cname, ns, err := gslbutils.SplitMultiClusterNS(objName)
			if err != nil {
				gslbutils.Errf("cluster: %s, ns: %s, msg: key processing error", cname, ns)
				continue
			}
			nsMetaObj, ok := rejectedNSStore.GetNSObjectByName(cname, ns)
			if !ok {
				// object doesn't exist, continue
				gslbutils.Warnf("objName: namespace, msg: object doesn't exist in the rejected store, returning")
				continue
			}
			nsMeta := nsMetaObj.(k8sobjects.NSMeta)
			nsMeta.DeleteFromFilter()
		}
	}

	acceptedList, _ := rejectedNSStore.GetAllFilteredNamespaces(filter.ApplyFilter)
	if len(acceptedList) != 0 {
		gslbutils.Logf("objList: %v, msg: obj list will be added", acceptedList)
		MoveNSObjs(acceptedList, rejectedNSStore, acceptedNSStore)
		// no need to add these namespaces to the filter, as they are already added via the ApplyFilter() function
	}
}

func applyAndRejectNamespaces(gf *gslbutils.GlobalFilter, gdp *gdpalphav1.GlobalDeploymentPolicy) {
	acceptedNSStore := gslbutils.GetAcceptedNSStore()
	rejectedNSStore := gslbutils.GetRejectedNSStore()

	// Since, we have just deleted a GDP object, we need to just check the acceptedNSStore
	acceptedList, _ := acceptedNSStore.GetAllFilteredNamespaces(filter.ApplyFilter)
	if len(acceptedList) == 0 {
		gslbutils.Logf("accepted list of namespaces is empty, nothing to be done")
		return
	}
	MoveNSObjs(acceptedList, acceptedNSStore, rejectedNSStore)
	gslbutils.Logf("objList: %v, msg: moved namespaces from accepted to rejected store", acceptedList)
}

func applyAndAcceptNamespaces() {
	acceptedNSStore := gslbutils.GetAcceptedNSStore()
	rejectedNSStore := gslbutils.GetRejectedNSStore()

	// Since, we have just added a fresh GDP object, all the previous namespaces will be in rejected store
	// so, apply and move the objects from rejected store
	acceptedList, rejectedList := rejectedNSStore.GetAllFilteredNamespaces(filter.ApplyFilter)
	if len(rejectedList) == 0 {
		gslbutils.Logf("rejected list of namespaces is empty, nothing to be done")
		return
	}

	MoveNSObjs(acceptedList, rejectedNSStore, acceptedNSStore)
	gslbutils.Logf("objList: %v, msg: moved these namespaces from rejected to accepted store", acceptedList)
}

// AddGDPObj creates a new GlobalFilter if not present on the first GDP object. Subsequent
// adds for GDP objects must fail as only one GDP object is allowed globally.
func AddGDPObj(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	gdp, ok := obj.(*gdpalphav1.GlobalDeploymentPolicy)
	if !ok {
		gslbutils.Errf("object added is not of type GDP")
		return
	}

	// GDPs for all other namespaces are rejected
	if gdp.ObjectMeta.Namespace != gslbutils.AVISystem {
		return
	}

	gf := gslbutils.GetGlobalFilter()
	if filterExists(gf) {
		msg := "a GDP object already exists, can't add another"
		gslbutils.Errf(msg)
		updateGDPStatus(gdp, msg)
		return
	}
	err := GDPSanityChecks(gdp)
	if err != nil {
		gslbutils.Errf("Error in accepting GDP object: %s", err.Error())
		updateGDPStatus(gdp, err.Error())
		return
	}
	updateGDPStatus(gdp, GDPSuccess)

	gslbutils.Logf("ns: %s, gdp: %s, msg: %s", gdp.ObjectMeta.Namespace, gdp.ObjectMeta.Name,
		"GDP object added")

	gslbutils.Logf("creating a new filter")
	gf.AddToFilter(gdp)
	// First apply the filter on the namespaces
	applyAndAcceptNamespaces()
	// for bootup sync, k8swq will be nil, in which case, the movement of objects will be taken
	// care of by the bootupSync function
	if k8swq != nil {
		WriteChangedObjsToQueue(k8swq, numWorkers, false)
	}
	gslbutils.SetGDPObj(gdp.GetObjectMeta().GetName(), gdp.GetObjectMeta().GetNamespace())
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

	// update only the accepted GDP
	if name, ns := gslbutils.GetGDPObj(); name != newGdp.GetObjectMeta().GetName() && ns != newGdp.GetObjectMeta().GetNamespace() {
		gslbutils.Errf("A GDP object already exists, updates will be ignored for other GDP objects")
		return
	}

	err := GDPSanityChecks(newGdp)
	if err != nil {
		gslbutils.Errf("Error in accepting the new GDP object: %s", err.Error())
		updateGDPStatus(newGdp, err.Error())
		return
	}
	updateGDPStatus(newGdp, "success")

	gf := gslbutils.GetGlobalFilter()
	if gf == nil {
		// global filter not initialized, return
		gslbutils.Errf("object: GlobalFilter, msg: global filter not initialized, can't update")
		return
	}
	if gdpChanged, trafficWeightChanged := gf.UpdateGlobalFilter(oldGdp, newGdp); gdpChanged {
		gslbutils.Logf("GDP object changed, will go through the objects again")
		// first apply and update the namespaces in the filter
		applyAndUpdateNamespaces()
		WriteChangedObjsToQueue(k8swq, numWorkers, trafficWeightChanged)
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

	if name, ns := gslbutils.GetGDPObj(); name != gdp.GetObjectMeta().GetName() && ns != gdp.GetObjectMeta().GetNamespace() {
		gslbutils.Errf("won't delete the filter as GDP object deleted wasn't accepted")
		return
	}

	gf := gslbutils.GetGlobalFilter()
	if gf == nil {
		gslbutils.Errf("object: GlobalFilter, msg: global filter not initialized, can't delete")
		return
	}
	applyAndRejectNamespaces(gf, gdp)
	gf.DeleteFromGlobalFilter(gdp)
	// remove all namespaces from filter and re-apply
	k8sobjects.RemoveAllSelectedNamespaces()
	WriteChangedObjsToQueue(k8swq, numWorkers, false)

	gslbutils.SetGDPObj("", "")
}

// InitializeGDPController handles initialization of a controller which handles
// GDP object events. Also, starts the required informers for this.
func InitializeGDPController(kubeclientset kubernetes.Interface,
	gdpclientset gslbcs.Interface,
	gslbInformerFactory gslbinformers.SharedInformerFactory,
	AddGDPFunc GDPAddDelfn, UpdateGDPFunc GDPUpdfn,
	DeleteGDPFunc GDPAddDelfn) *GDPController {

	gdpInformer := gslbInformerFactory.Amko().V1alpha1().GlobalDeploymentPolicies()
	gdpscheme.AddToScheme(scheme.Scheme)
	gslbutils.Logf("object: GDPController, msg: %s", "creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(utils.AviLog.Infof)
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
