/*
 * Copyright 2021 VMware, Inc.
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

package svc_utils

import (
	"fmt"
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	v1 "k8s.io/api/core/v1"
)

var acceptedServiceTypes []string = []string{
	"NodePort",
}

func AcceptedServiceTypes() []string {
	return acceptedServiceTypes
}

func IsServiceOfAcceptedType(svcObj *v1.Service) bool {
	for _, t := range acceptedServiceTypes {
		if t == string(svcObj.Spec.Type) {
			return true
		}
	}
	return false
}

// ClustersetServiceFilter is a global filter for which key is the cluster name and the value
// is a ClusterServiceFilter. This is initialized during bootup.
var clustersetServiceFilter map[string]*NSServiceFilter
var clustersetServiceFilterSync sync.Once

func GetClustersetServiceFilter() map[string]*NSServiceFilter {
	clustersetServiceFilterSync.Do(func() {
		clustersetServiceFilter = make(map[string]*NSServiceFilter)
	})
	return clustersetServiceFilter
}

func InitClustersetServiceFilter(clusters []string) (map[string]*NSServiceFilter, error) {
	csf := GetClustersetServiceFilter()
	for _, c := range clusters {
		csf[c] = InitNSServiceFilter()
	}
	return csf, nil
}

func AddObjToClustersetServiceFilter(cname, ns, obj string) error {
	csf := GetClustersetServiceFilter()
	nsvcf, clusterPresent := csf[cname]
	if !clusterPresent {
		return fmt.Errorf("cluster %s not present in filter", cname)
	}
	nsvcf.AddToNSServiceFilter(ns, obj)
	return nil
}

func DeleteObjFromClustersetServiceFilter(cname, ns, obj string) error {
	csf := GetClustersetServiceFilter()
	nsvcf, clusterPresent := csf[cname]
	if !clusterPresent {
		return fmt.Errorf("cluster %s not present in filter", cname)
	}
	nsvcf.DeleteFromNSServiceFilter(ns, obj)
	return nil
}

func IsObjectInClustersetFilter(cname, ns, obj string) bool {
	nsSvcFilter, present := clustersetServiceFilter[cname]
	if present {
		return nsSvcFilter.IsObjectPresent(ns, obj)
	}
	return false
}

type NSServiceFilter struct {
	nsToObj map[string]*ServiceSet
	lock    sync.RWMutex
}

func InitNSServiceFilter() *NSServiceFilter {
	return &NSServiceFilter{
		nsToObj: map[string]*ServiceSet{},
	}
}

func (nsvcf *NSServiceFilter) AddToNSServiceFilter(ns, obj string) {
	nsvcf.lock.Lock()
	defer nsvcf.lock.Unlock()

	svcSet, nsPresent := nsvcf.nsToObj[ns]
	if nsPresent {
		svcSet.AddToServiceSet(obj)
		return
	}
	nsvcf.nsToObj[ns] = InitServiceSet(obj)
}

func (nsvcf *NSServiceFilter) DeleteFromNSServiceFilter(ns, obj string) {
	nsvcf.lock.Lock()
	defer nsvcf.lock.Unlock()

	svcSet, nsPresent := nsvcf.nsToObj[ns]
	if !nsPresent {
		return
	}
	objRemaining := svcSet.DeleteFromServiceSet(obj)
	if objRemaining == 0 {
		// remove the namespace key from the filter as there are no other objects
		// remaining for that namespace
		delete(nsvcf.nsToObj, ns)
	}
}

func (nsvcf *NSServiceFilter) IsObjectPresent(ns, obj string) bool {
	nsvcf.lock.RLock()
	defer nsvcf.lock.RUnlock()

	svcSet, nsPresent := nsvcf.nsToObj[ns]
	if nsPresent {
		return svcSet.IsSvcPresent(obj)
	}
	return false
}

type ServiceSet struct {
	svcSet map[string]interface{}
	lock   sync.RWMutex
}

func InitServiceSet(svcName string) *ServiceSet {
	svcSet := make(map[string]interface{})
	var i interface{}
	svcSet[svcName] = i

	return &ServiceSet{
		svcSet: svcSet,
	}
}

func (ss *ServiceSet) AddToServiceSet(svc string) {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	var i interface{}
	ss.svcSet[svc] = i
	gslbutils.Logf("svcs: %v", ss.svcSet)
}

func (ss *ServiceSet) DeleteFromServiceSet(svc string) int {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	delete(ss.svcSet, svc)
	return len(ss.svcSet)
}

func (ss *ServiceSet) IsSvcPresent(svc string) bool {
	ss.lock.RLock()
	defer ss.lock.RUnlock()

	_, svcPresent := ss.svcSet[svc]
	return svcPresent
}
