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

func AddObjToClustersetServiceFilter(cname, ns, obj string, port int32) error {
	csf := GetClustersetServiceFilter()
	nsvcf, clusterPresent := csf[cname]
	if !clusterPresent {
		return fmt.Errorf("cluster %s not present in filter", cname)
	}
	nsvcf.AddToNSServiceFilter(ns, obj, port)
	return nil
}

func DeleteObjFromClustersetServiceFilter(cname, ns, obj string, port int32) error {
	csf := GetClustersetServiceFilter()
	nsvcf, clusterPresent := csf[cname]
	if !clusterPresent {
		return fmt.Errorf("cluster %s not present in filter", cname)
	}
	nsvcf.DeleteFromNSServiceFilter(ns, obj, port)
	return nil
}

func IsObjectInClustersetFilter(cname, ns, obj string) bool {
	nsSvcFilter, present := clustersetServiceFilter[cname]
	if present {
		return nsSvcFilter.IsObjectPresent(ns, obj)
	}
	return false
}

func IsSvcPortInClustersetFilter(cname, ns, obj string, port int32) bool {
	nsSvcFilter, present := clustersetServiceFilter[cname]
	if present {
		return nsSvcFilter.IsSvcPortPresent(ns, obj, port)
	}
	return false
}

type NSServiceFilter struct {
	nsToObj map[string]*ServiceCache
	lock    sync.RWMutex
}

func InitNSServiceFilter() *NSServiceFilter {
	return &NSServiceFilter{
		nsToObj: map[string]*ServiceCache{},
	}
}

func (nsvcf *NSServiceFilter) AddToNSServiceFilter(ns, svc string, port int32) {
	nsvcf.lock.Lock()
	defer nsvcf.lock.Unlock()

	svcSet, nsPresent := nsvcf.nsToObj[ns]
	if nsPresent {
		svcSet.Add(svc, port)
		return
	}
	nsvcf.nsToObj[ns] = InitServiceSet(svc, port)
}

func (nsvcf *NSServiceFilter) DeleteFromNSServiceFilter(ns, obj string, port int32) {
	nsvcf.lock.Lock()
	defer nsvcf.lock.Unlock()

	svcSet, nsPresent := nsvcf.nsToObj[ns]
	if !nsPresent {
		return
	}
	objRemaining := svcSet.Delete(obj, port)
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

func (nsvcf *NSServiceFilter) IsSvcPortPresent(ns, obj string, port int32) bool {
	nsvcf.lock.RLock()
	defer nsvcf.lock.RUnlock()

	svcSet, nsPresent := nsvcf.nsToObj[ns]
	if nsPresent {
		return svcSet.IsSvcPortPresent(obj, port)
	}
	return false
}

type ServiceCache struct {
	svcSet map[string]*PortCache
	lock   sync.RWMutex
}

func InitServiceSet(svcName string, port int32) *ServiceCache {
	pc := InitPortCache(port)
	svcSet := make(map[string]*PortCache)
	svcSet[svcName] = pc

	return &ServiceCache{
		svcSet: svcSet,
	}
}

func (ss *ServiceCache) Add(svc string, port int32) {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	if pc, svcExists := ss.svcSet[svc]; svcExists {
		pc.Add(port)
		return
	}
	ss.svcSet[svc] = InitPortCache(port)
	gslbutils.Logf("svcs: %v", ss.svcSet)
}

func (ss *ServiceCache) Delete(svc string, port int32) int {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	if pc, svcExists := ss.svcSet[svc]; svcExists {
		leftPorts := pc.Delete(port)
		if leftPorts == 0 {
			// no more ports left, delete the service
			delete(ss.svcSet, svc)
		}
	}
	return len(ss.svcSet)
}

func (ss *ServiceCache) IsSvcPresent(svc string) bool {
	ss.lock.RLock()
	defer ss.lock.RUnlock()

	_, svcPresent := ss.svcSet[svc]
	return svcPresent
}

func (ss *ServiceCache) IsSvcPortPresent(svc string, port int32) bool {
	ss.lock.RLock()
	defer ss.lock.RUnlock()

	if pc, svcExists := ss.svcSet[svc]; svcExists {
		return pc.IsPortPresent(port)
	}
	return false
}

type PortCache struct {
	portSet map[int32]interface{}
}

func InitPortCache(port int32) *PortCache {
	portSet := make(map[int32]interface{})
	portSet[port] = struct{}{}

	return &PortCache{
		portSet: portSet,
	}
}

func (pc *PortCache) Add(port int32) {
	pc.portSet[port] = struct{}{}
}

func (pc *PortCache) Delete(port int32) int {
	delete(pc.portSet, port)
	return len(pc.portSet)
}

func (pc *PortCache) IsPortPresent(port int32) bool {
	_, portPresent := pc.portSet[port]
	return portPresent
}
