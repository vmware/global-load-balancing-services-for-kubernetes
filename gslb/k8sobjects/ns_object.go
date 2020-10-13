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

package k8sobjects

import (
	"github.com/avinetworks/amko/gslb/gslbutils"
	gdpv1alpha1 "github.com/avinetworks/amko/internal/apis/amko/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

// GetNSMeta returns a trimmed down version of a route
func GetNSMeta(ns *corev1.Namespace, cname string) NSMeta {
	metaObj := NSMeta{
		Name:    ns.ObjectMeta.Name,
		Cluster: cname,
	}
	metaObj.Labels = make(map[string]string)
	for key, value := range ns.GetLabels() {
		metaObj.Labels[key] = value
	}
	return metaObj
}

// NSMeta is the metadata for a ns. It is the minimal information
// that we maintain for each namespace, accepted or rejected.
type NSMeta struct {
	Cluster string
	Name    string
	Labels  map[string]string
}

func (nsObj NSMeta) GetType() string {
	return gdpv1alpha1.NSObj
}

func (nsObj NSMeta) GetName() string {
	return nsObj.Name
}

func (nsObj NSMeta) GetCluster() string {
	return nsObj.Cluster
}

func (ns NSMeta) ApplyFilter() bool {
	gf := gslbutils.GetGlobalFilter()
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	if !gslbutils.PresentInList(ns.Cluster, gf.ApplicableClusters) {
		gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace rejected because cluster was not selected",
			ns.Cluster, ns.Name)
		return false
	}
	nsFilter := gf.NSFilter
	if nsFilter != nil {
		nsFilter.Lock.Lock()
		defer nsFilter.Lock.Unlock()
		lblMatch := false
		for k, v := range ns.Labels {
			if k == nsFilter.Key && v == nsFilter.Value {
				lblMatch = true
			}
		}
		if !lblMatch {
			gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace rejected because it was not selected via label",
				ns.Cluster, ns.Name)
			return false
		}
		nsList, ok := nsFilter.SelectedNS[ns.Cluster]
		if !ok {
			if len(nsFilter.SelectedNS) == 0 {
				gf.NSFilter.SelectedNS = make(map[string][]string)
			}
			gf.NSFilter.SelectedNS[ns.Cluster] = []string{ns.Name}
			gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace added to filter",
				ns.Cluster, ns.Name)
			return true
		}
		// cluster already exists, check for namespace
		if !gslbutils.PresentInList(ns.Name, nsList) {
			gf.NSFilter.SelectedNS[ns.Cluster] = append(gf.NSFilter.SelectedNS[ns.Cluster], ns.Name)
			gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace added to filter",
				ns.Cluster, ns.Name)
			return true
		}
		gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace already exists in filter, nothing to update",
			ns.Cluster, ns.Name)
		return true
	}
	gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: no namespace filter present, returning false",
		ns.Cluster, ns.Name)

	return false
}

func (ns NSMeta) DeleteFromFilter() bool {
	gf := gslbutils.GetGlobalFilter()
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	nsFilter := gf.NSFilter
	// nsFilter nil indicates GDP object doesn't contain the namespaceSelector field, don't do anything
	if nsFilter != nil {
		nsFilter.Lock.Lock()
		defer nsFilter.Lock.Unlock()
		nsList, ok := nsFilter.SelectedNS[ns.Cluster]
		if !ok {
			// cluster not found, nothing to be done
			gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace not part of filter, nothing to be done",
				ns.Cluster, ns.Name)
			return false
		}
		idx, ok := gslbutils.GetKeyIdx(nsList, ns.Name)
		if !ok {
			// namespace doesn't exist, nothing to be done
			gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace not part of filter, nothing to be done",
				ns.Cluster, ns.Name)
			return false
		}
		// Delete the index
		nsFilter.SelectedNS[ns.Cluster] = append(nsList[:idx], nsList[idx+1:]...)
		gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace part of filter, deleted",
			ns.Cluster, ns.Name)

		// Check if this was the last namespace, if yes, remove that cluster from the map
		if len(nsFilter.SelectedNS[ns.Cluster]) == 0 {
			delete(nsFilter.SelectedNS, ns.Cluster)
			gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: last namespace for cluster, deleted cluster from filter",
				ns.Cluster, ns.Name)
		}

		// update everything
		gf.NSFilter = nsFilter
		return true
	}

	return false
}

// UpdateFilter returns true if there was a change in the filter
func (ns NSMeta) UpdateFilter(old NSMeta) bool {
	oldApplied := old.ApplyFilter()
	newApplied := ns.ApplyFilter()

	if oldApplied == newApplied {
		gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: no changes", ns.Cluster, ns.Name)
		return false
	}
	if oldApplied == true && newApplied == false {
		gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace changed, deleting the new namespace from filter",
			ns.Cluster, ns.Name)
		// delete the ns from the filter
		return ns.DeleteFromFilter()
	}

	// oldApplied == false, newApplied == true, namespace already added as part of ApplyFilter
	gslbutils.Logf("objType: Namespace, cluster: %s, name: %s, msg: namespace changed, added the namespace to filter")
	return true
}

func RemoveAllSelectedNamespaces() {
	gf := gslbutils.GetGlobalFilter()
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	nsFilter := gf.NSFilter
	// nsFilter nil indicates GDP object doesn't contain the namespaceSelector field, don't do anything
	if nsFilter != nil {
		nsFilter.Lock.Lock()
		defer nsFilter.Lock.Unlock()
		gf.NSFilter.SelectedNS = make(map[string][]string)
	}
}
