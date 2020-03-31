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

// Construct in memory database that populates updates from all the kubernetes/
// openshift clusters.
// The format is: cluster: [namespace:[object_name: obj]]

package gslbutils

import (
	"sync"

	"github.com/avinetworks/container-lib/utils"
)

type ClusterStore struct {
	ClusterObjectMap map[string]*ObjectStore
	ClusterLock      sync.RWMutex
}

// Filterfn is a type of a function used to filter out objects.
type Filterfn func(obj interface{}, cname string) bool

var acceptedOnce sync.Once

// GetAcceptedRouteStore initializes and returns a new accepted route store.
func GetAcceptedRouteStore() *ClusterStore {
	acceptedOnce.Do(func() {
		AcceptedRouteStore = NewClusterStore()
	})
	return AcceptedRouteStore
}

var rejectedOnce sync.Once

// GetRejectedRouteStore initializes and returns a new accepted route store.
func GetRejectedRouteStore() *ClusterStore {
	rejectedOnce.Do(func() {
		RejectedRouteStore = NewClusterStore()
	})
	return RejectedRouteStore
}

var acceptedSvcOnce sync.Once

// GetAcceptedLBSvcStore initializes and returns a new accepted route store.
func GetAcceptedLBSvcStore() *ClusterStore {
	acceptedSvcOnce.Do(func() {
		AcceptedLBSvcStore = NewClusterStore()
	})
	return AcceptedLBSvcStore
}

var rejectedSvcOnce sync.Once

// GetRejectedLBSvcStore initializes and returns a new accepted route store.
func GetRejectedLBSvcStore() *ClusterStore {
	rejectedSvcOnce.Do(func() {
		RejectedLBSvcStore = NewClusterStore()
	})
	return RejectedLBSvcStore
}

var acceptedIngOnce sync.Once

// GetAcceptedIngressStore initializes and returns a new accepted ingress store.
func GetAcceptedIngressStore() *ClusterStore {
	acceptedIngOnce.Do(func() {
		AcceptedIngressStore = NewClusterStore()
	})
	return AcceptedIngressStore
}

var rejectedIngOnce sync.Once

// GetRejectedIngressStore initializes and returns a new accepted ingress store.
func GetRejectedIngressStore() *ClusterStore {
	rejectedIngOnce.Do(func() {
		RejectedIngressStore = NewClusterStore()
	})
	return RejectedIngressStore
}

// NewClusterStore initializes and returns a new cluster store.
func NewClusterStore() *ClusterStore {
	clusterStore := &ClusterStore{}
	clusterStore.ClusterObjectMap = make(map[string]*ObjectStore)
	return clusterStore
}

// GetClusterStore fetches the the cluster object map if it exists, if not,
// initializes a new one and returns that.
func (clusterStore *ClusterStore) GetClusterStore(cname string) *ObjectStore {
	clusterStore.ClusterLock.Lock()
	defer clusterStore.ClusterLock.Unlock()
	if val, ok := clusterStore.ClusterObjectMap[cname]; ok {
		return val
	}
	// This cluster is not initialized, let's initialize it
	clusterObjStore := NewObjectStore()
	// Update the store
	clusterStore.ClusterObjectMap[cname] = clusterObjStore
	return clusterObjStore
}

// DeleteClusterStore deletes the key for a cluster which means it also deletes
// the entire cluster related objects. Use with care.
func (clusterStore *ClusterStore) DeleteClusterStore(cname string) bool {
	clusterStore.ClusterLock.Lock()
	defer clusterStore.ClusterLock.Unlock()
	if _, ok := clusterStore.ClusterObjectMap[cname]; ok {
		delete(clusterStore.ClusterObjectMap, cname)
		return true
	}
	utils.AviLog.Warning.Printf("Cluster: %s not found, nothing to delete", cname)
	return false
}

// GetAllClusters returns the list of all clusters in clusterStore.
func (clusterStore *ClusterStore) GetAllClusters() []string {
	// Take a read lock on the cluster store and return the list of clusters
	clusterStore.ClusterLock.RLock()
	defer clusterStore.ClusterLock.RUnlock()
	var allClusters []string
	for cname := range clusterStore.ClusterObjectMap {
		allClusters = append(allClusters, cname)
	}
	return allClusters
}

// GetAllFilteredClusterNSObjects gets the list of all accepted and rejected
// objects which pass the filter function applyFilter.
func (clusterStore *ClusterStore) GetAllFilteredClusterNSObjects(applyFilter Filterfn) ([]string, []string) {
	var acceptedList, rejectedList []string
	clusterStore.ClusterLock.RLock()
	defer clusterStore.ClusterLock.RUnlock()
	for cname, clusterMap := range clusterStore.ClusterObjectMap {
		nsObjListAcc, nsObjListRej := clusterMap.GetAllFilteredNSObjects(applyFilter, cname)
		for _, nsObj := range nsObjListAcc {
			// Prefix the cluster name to the ns+obj name
			acceptedList = append(acceptedList, cname+"/"+nsObj)
		}
		for _, nsObj := range nsObjListRej {
			rejectedList = append(rejectedList, cname+"/"+nsObj)
		}
	}
	return acceptedList, rejectedList
}

// AddOrUpdate fetches the right cluster store and then updates the object inside the
// namespace store inside the cluster store.
func (clusterStore *ClusterStore) AddOrUpdate(obj interface{}, cname, ns, objName string) {
	clusterStoreMap := clusterStore.GetClusterStore(cname)
	// Updating an object inside the cluster store map requires a read lock.
	clusterStore.ClusterLock.RLock()
	defer clusterStore.ClusterLock.RUnlock()
	clusterStoreMap.AddOrUpdate(ns, objName, obj)
}

// DeleteClusterNSObj deletes the object from the object map in namespace store
// in the cluster store. It also checks if the cluster is empty and not required
// anymore and removes it.
func (clusterStore *ClusterStore) DeleteClusterNSObj(cname, ns, objName string) (interface{}, bool) {
	clusterStoreMap := clusterStore.GetClusterStore(cname)
	// Before trying out anything, we have to take a read lock on this
	clusterStore.ClusterLock.RLock()
	// Can't use defer here, since we need to unlock the read lock later on, and then
	// take a write lock in DeleteClusterStore.
	obj, ok := clusterStoreMap.DeleteNSObj(ns, objName)
	nsList := clusterStoreMap.GetAllNamespaces()
	clusterStore.ClusterLock.RUnlock()
	if len(nsList) == 0 {
		// No more namespaces present, just remove the cluster.
		clusterStore.DeleteClusterStore(cname)
	}
	return obj, ok
}

// GetClusterNSObjectByName returns the object objName by looking into the ns Object map.
func (clusterStore *ClusterStore) GetClusterNSObjectByName(cname, ns, objName string) (interface{}, bool) {
	clusterStoreMap := clusterStore.GetClusterStore(cname)
	clusterStore.ClusterLock.RLock()
	defer clusterStore.ClusterLock.RUnlock()
	obj, ok := clusterStoreMap.GetNSObjectByName(ns, objName)
	return obj, ok
}

// ObjectStore consists of a map of string and ObjectMapStore and a lock.
type ObjectStore struct {
	NSObjectMap map[string]*ObjectMapStore
	NSLock      sync.RWMutex
}

// NewObjectStore initilizes a new ObjectStore and returns the address for it.
func NewObjectStore() *ObjectStore {
	objectStore := &ObjectStore{}
	objectStore.NSObjectMap = make(map[string]*ObjectMapStore)
	return objectStore
}

// GetNSStore returns a specific store for the required nsName namespace.
func (store *ObjectStore) GetNSStore(nsName string) *ObjectMapStore {
	store.NSLock.Lock()
	defer store.NSLock.Unlock()
	val, ok := store.NSObjectMap[nsName]
	if ok {
		return val
	} else {
		// This namespace is not initialized, let's initialze it
		nsObjStore := NewObjectMapStore()
		// Update the store.
		store.NSObjectMap[nsName] = nsObjStore
		return nsObjStore
	}
}

// DeleteNSStore deletes the object map store for the namespace nsName.
func (store *ObjectStore) DeleteNSStore(nsName string) bool {
	// Deletes the key for a namespace. Wipes off the entire NS. So use with care.
	store.NSLock.Lock()
	defer store.NSLock.Unlock()
	_, ok := store.NSObjectMap[nsName]
	if ok {
		delete(store.NSObjectMap, nsName)
		return true
	}
	utils.AviLog.Warning.Printf("Namespace: %s not found, nothing to delete returning false", nsName)
	return false

}

// GetAllNamespaces returns the list of all namespaces in the object store.
func (store *ObjectStore) GetAllNamespaces() []string {
	// Take a read lock on the store and write lock on NS object
	store.NSLock.RLock()
	defer store.NSLock.RUnlock()
	var allNamespaces []string
	for ns, _ := range store.NSObjectMap {
		allNamespaces = append(allNamespaces, ns)
	}
	return allNamespaces

}

// AddOrUpdate fetches the right NS Store and then updates the object map store.
func (store *ObjectStore) AddOrUpdate(ns, objName string, obj interface{}) {
	// fetch the minimal version of this route
	nsStore := store.GetNSStore(ns)
	// Updating an object inside the object map requires a read lock on the ns store.
	store.NSLock.RLock()
	store.NSLock.RUnlock()
	nsStore.AddOrUpdate(objName, obj)
}

// GetAllFilteredNSObjects fetches all the objects from Object Map Store and prefixes
// the namespace to it.
func (store *ObjectStore) GetAllFilteredNSObjects(applyFilter Filterfn,
	cname string) ([]string, []string) {
	store.NSLock.RLock()
	defer store.NSLock.RUnlock()
	var acceptedList, rejectedList []string
	for ns, nsObjMap := range store.NSObjectMap {
		objListAcc, objListRej := nsObjMap.GetAllFilteredObjects(applyFilter, cname)
		for _, obj := range objListAcc {
			// Prefixes a namespace to the list of objects
			acceptedList = append(acceptedList, ns+"/"+obj)
		}
		for _, obj := range objListRej {
			// Prefix a namespace to the list of the objects
			rejectedList = append(rejectedList, ns+"/"+obj)
		}
	}
	return acceptedList, rejectedList
}

// DeleteNSObj deletes the obj from the object map store. Checks if that was the last
// element in this namespace, if yes, it also removes the namespace.
func (store *ObjectStore) DeleteNSObj(ns, objName string) (interface{}, bool) {
	nsStore := store.GetNSStore(ns)
	// Not using defer for unlock here, as there's a different lock i.e., a write
	// lock which will be taken inside DeleteNSStore.
	store.NSLock.RLock()
	obj, ok := nsStore.Delete(objName)
	objList := nsStore.GetAllObjectNames()
	store.NSLock.RUnlock()

	if len(objList) == 0 {
		store.DeleteNSStore(ns)
	}
	return obj, ok
}

// GetNSObjectByName gets the object with name objName in the ns store keyed on ns namespace.
// Returns the object and true if found.
func (store *ObjectStore) GetNSObjectByName(ns, objName string) (interface{}, bool) {
	nsStore := store.GetNSStore(ns)
	store.NSLock.RLock()
	defer store.NSLock.RUnlock()
	ok, obj := nsStore.Get(objName)
	return obj, ok
}

// ObjectMapStore contains an ObjectMap and a lock.
type ObjectMapStore struct {
	ObjectMap map[string]interface{}
	ObjLock   sync.RWMutex
}

// NewObjectMapStore initializes and returns a new ObjectMapStore.
func NewObjectMapStore() *ObjectMapStore {
	nsObjStore := &ObjectMapStore{}
	nsObjStore.ObjectMap = make(map[string]interface{})
	return nsObjStore
}

// AddOrUpdate adds or updates the object objName in object map store.
func (o *ObjectMapStore) AddOrUpdate(objName string, obj interface{}) {
	o.ObjLock.Lock()
	defer o.ObjLock.Unlock()
	o.ObjectMap[objName] = obj
}

// Delete deletes the key and the value from the map store and returns that object
// along with whether that element existed or not.
func (o *ObjectMapStore) Delete(objName string) (interface{}, bool) {
	o.ObjLock.Lock()
	defer o.ObjLock.Unlock()
	obj, ok := o.ObjectMap[objName]
	if ok {
		delete(o.ObjectMap, objName)
		return obj, true
	}
	utils.AviLog.Warning.Printf("Object Not found in store. Nothing to delete: %s ", objName)
	return nil, false
}

// Get returns the object with name "objName" in the object map store.
func (o *ObjectMapStore) Get(objName string) (bool, interface{}) {
	o.ObjLock.RLock()
	defer o.ObjLock.RUnlock()
	val, ok := o.ObjectMap[objName]
	if ok {
		return true, val
	}
	utils.AviLog.Warning.Printf("Object Not found in store:  %s ", objName)
	return false, nil

}

// GetAllObjectNames returns the object map of all the objects in ObjectMapStore.
func (o *ObjectMapStore) GetAllObjectNames() map[string]interface{} {
	o.ObjLock.RLock()
	defer o.ObjLock.RUnlock()
	// TODO (sudswas): Pass a copy instead of the reference
	return o.ObjectMap

}

// GetAllFilteredObjects returns a list of all the objects which pass the filter function "applyFilter".
func (o *ObjectMapStore) GetAllFilteredObjects(applyFilter Filterfn, cname string) ([]string, []string) {
	o.ObjLock.RLock()
	defer o.ObjLock.RUnlock()
	var acceptedList, rejectedList []string
	for objName, obj := range o.ObjectMap {
		if applyFilter(obj, cname) {
			acceptedList = append(acceptedList, objName)
		} else {
			rejectedList = append(rejectedList, objName)
		}
	}
	return acceptedList, rejectedList
}
