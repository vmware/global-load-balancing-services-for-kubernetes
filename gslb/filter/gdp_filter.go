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

package filter

import (
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"
)

type FilterArgs struct {
	Obj     interface{}
	Cluster string
	GFqdn   string
}

// ApplyFilter applies the local namespace filter first to an object, if the namespace
// filter is not present or if the object is rejected by the namespace filter, apply
// the cluster filter if present. Default action is to reject the object.
func ApplyFilter(fargs FilterArgs) bool {
	gf := gslbutils.GetGlobalFilter()
	if gf == nil {
		gslbutils.Errf("cname: %s, msg: global filter doesn't exist, returning false", fargs.Cluster)
		return false
	}
	metaobj, ok := fargs.Obj.(k8sobjects.FilterableObject)
	if !ok {
		gslbutils.Warnf("cname: %s, msg: not a meta object, returning", fargs.Cluster)
		return false
	}

	// First see, if there's a namespace filter set for this object's namespace, if not, apply
	// the global filter.
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	if gf.AppFilter == nil && gf.NSFilter == nil {
		return false
	}
	return metaobj.ApplyFilter()
}

func ApplyFqdnMapFilter(fargs FilterArgs) bool {
	ok := ApplyFilter(fargs)
	if ok {
		// the object passed through the global filter, check if it passes the fqdn (hostrule) filter
		metaObj, ok := fargs.Obj.(k8sobjects.MetaObject)
		if !ok {
			gslbutils.Warnf("cname: %s, msg: not a k8s meta object, returning", fargs.Cluster)
			return false
		}
		cname := metaObj.GetCluster()
		lfqdn := metaObj.GetHostname()

		if gslbutils.GetCustomFqdnMode() {
			fqdnMap := gslbutils.GetFqdnMap()

			lfqdnList, err := fqdnMap.GetLocalFqdnsForGlobalFqdn(fargs.GFqdn)
			if err != nil {
				gslbutils.Warnf("cname: %s, msg: error in getting local fqdn list from map for global fqdn %s",
					fargs.Cluster, fargs.GFqdn)
				return false
			}
			for _, f := range lfqdnList {
				if f.Cluster == cname && f.Fqdn == lfqdn {
					return true
				}
			}
		} else {
			if fargs.GFqdn == lfqdn {
				return true
			}
		}
	}
	return false
}
