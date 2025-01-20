/*
 * Copyright 2025-2026 VMware, Inc.
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

package filterstore

import (
	filter "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/filter"
	store "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/store"
)

// Filterfn is a type of a function used to filter out objects.
type Filterfn func(filter.FilterArgs) bool

// GetAllFilteredClusterNSObjects gets the list of all accepted and rejected
// objects which pass the filter function applyFilter.
func GetAllFilteredClusterNSObjects(applyFilter Filterfn, clusterStore *store.ClusterStore) ([]string, []string) {
	var acceptedList, rejectedList []string
	clusterStore.ClusterLock.RLock()
	defer clusterStore.ClusterLock.RUnlock()
	for cname, clusterMap := range clusterStore.ClusterObjectMap {
		nsObjListAcc, nsObjListRej := GetAllFilteredNSObjects(applyFilter, cname, clusterMap)
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

// GetAllFilteredClusterNSObjectsForCluster gets the list of all accepted and rejected
// objects which pass the filter function applyFilter for a specified cluster.
func GetAllFilteredObjectsForClusterFqdn(applyFilter Filterfn, cluster string,
	gfqdn string, clusterStore *store.ClusterStore) ([]string, []string) {
	var acceptedList, rejectedList []string
	clusterStore.ClusterLock.RLock()
	defer clusterStore.ClusterLock.RUnlock()
	for cname, clusterMap := range clusterStore.ClusterObjectMap {
		if cname != cluster {
			continue
		}
		nsObjListAcc, nsObjListRej := GetAllFilteredNSObjectsForFqdn(applyFilter, cname, gfqdn, clusterMap)
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

func GetAllFilteredNamespaces(applyFilter Filterfn, store *store.ObjectStore) ([]string, []string) {
	store.NSLock.RLock()
	defer store.NSLock.RUnlock()
	var acceptedList, rejectedList []string

	for cluster, clusterNSMap := range store.NSObjectMap {
		nsListAcc, nsListRej := GetAllFilteredObjects(applyFilter, cluster, clusterNSMap)
		for _, ns := range nsListAcc {
			// Prefix a cluster name to the list of objects
			acceptedList = append(acceptedList, cluster+"/"+ns)
		}
		for _, ns := range nsListRej {
			// Prefix a cluster name to the list of objects
			rejectedList = append(rejectedList, cluster+"/"+ns)
		}
	}
	return acceptedList, rejectedList
}

// GetAllFilteredNSObjects fetches all the objects from Object Map Store and prefixes
// the namespace to it.
func GetAllFilteredNSObjects(applyFilter Filterfn,
	cname string, store *store.ObjectStore) ([]string, []string) {
	store.NSLock.RLock()
	defer store.NSLock.RUnlock()
	var acceptedList, rejectedList []string
	for ns, nsObjMap := range store.NSObjectMap {
		objListAcc, objListRej := GetAllFilteredObjects(applyFilter, cname, nsObjMap)
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

func GetAllFilteredNSObjectsForFqdn(applyFilter Filterfn,
	cname string, fqdn string, store *store.ObjectStore) ([]string, []string) {
	store.NSLock.RLock()
	defer store.NSLock.RUnlock()
	var acceptedList, rejectedList []string
	for ns, nsObjMap := range store.NSObjectMap {
		objListAcc, objListRej := GetAllFilteredObjectsForFqdn(applyFilter, cname, fqdn, nsObjMap)
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

// GetAll(FilteredObjects returns a list of all the objects which pass the filter function "applyFilter".
func GetAllFilteredObjects(applyFilter Filterfn, cname string, o *store.ObjectMapStore) ([]string, []string) {
	o.ObjLock.RLock()
	defer o.ObjLock.RUnlock()
	var acceptedList, rejectedList []string
	for objName, obj := range o.ObjectMap {
		if applyFilter(filter.FilterArgs{
			Obj:     obj,
			Cluster: cname,
		}) {
			acceptedList = append(acceptedList, objName)
		} else {
			rejectedList = append(rejectedList, objName)
		}
	}
	return acceptedList, rejectedList
}

func GetAllFilteredObjectsForFqdn(applyFilter Filterfn, cname string, fqdn string, o *store.ObjectMapStore) ([]string, []string) {
	o.ObjLock.RLock()
	defer o.ObjLock.RUnlock()
	var acceptedList, rejectedList []string
	for objName, obj := range o.ObjectMap {
		if applyFilter(filter.FilterArgs{
			Obj:     obj,
			Cluster: cname,
			GFqdn:   fqdn,
		}) {
			acceptedList = append(acceptedList, objName)
		} else {
			rejectedList = append(rejectedList, objName)
		}
	}
	return acceptedList, rejectedList
}
