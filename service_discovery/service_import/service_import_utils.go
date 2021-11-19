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

package serviceimport

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	siapi "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	amkov1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	amkoInformers "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions/amko/v1alpha1"
	k8sutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/k8s_utils"
	svcutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/svc_utils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type NamespacedName struct {
	Namespace string
	Name      string
}

type ServiceImportCache struct {
	serviceImportCache map[NamespacedName]*siapi.ServiceImport
	lock               sync.RWMutex
}

func (sic *ServiceImportCache) AddUpdateCache(si *siapi.ServiceImport) {
	sic.lock.Lock()
	defer sic.lock.Unlock()

	sic.serviceImportCache[NamespacedName{
		Namespace: si.Spec.Namespace,
		Name:      si.Spec.Service,
	}] = si
}

func (sic *ServiceImportCache) LookupServiceImport(ns, name string) *siapi.ServiceImport {
	sic.lock.RLock()
	defer sic.lock.RUnlock()
	if v, ok := sic.serviceImportCache[NamespacedName{
		Namespace: ns,
		Name:      name,
	}]; ok {
		return v
	}
	return nil
}

func (sic *ServiceImportCache) DeleteServiceImport(ns, name string) {
	sic.lock.Lock()
	defer sic.lock.Unlock()

	delete(sic.serviceImportCache, NamespacedName{
		Namespace: ns,
		Name:      name,
	})
}

type ClusterServiceImportCache struct {
	clusterServiceImportMap map[string]*ServiceImportCache
	lock                    sync.RWMutex
}

func InitClusterServiceImportCache(clusterList []string) *ClusterServiceImportCache {
	csic := &ClusterServiceImportCache{
		clusterServiceImportMap: make(map[string]*ServiceImportCache),
	}
	for _, c := range clusterList {
		csic.AddCluster(c)
	}
	return csic
}

func (csic *ClusterServiceImportCache) AddCluster(cname string) {
	csic.lock.Lock()
	defer csic.lock.Unlock()

	csic.clusterServiceImportMap[cname] = &ServiceImportCache{
		serviceImportCache: map[NamespacedName]*siapi.ServiceImport{},
	}
}

func (csic *ClusterServiceImportCache) GetServiceImportCache(cname string) *ServiceImportCache {
	csic.lock.RLock()
	defer csic.lock.RUnlock()

	if v, ok := csic.clusterServiceImportMap[cname]; ok {
		return v
	}
	return nil
}

func (csic *ClusterServiceImportCache) AddUpdateServiceImport(si *siapi.ServiceImport) {
	// this function uses GetServiceImportCache twice to prevent locking down the entire
	// cluster service import cache to add a service for one cluster. First, it sees if
	// there's an entry for a cluster. If yes, it adds the service to that cluster's cache.
	// If there's no entry for a cluster, it first adds a cluster (involves taking a write
	// lock), and then adds/updates the cache (involves taking a read lock).
	if sic := csic.GetServiceImportCache(si.Spec.Cluster); sic != nil {
		sic.AddUpdateCache(si)
	}
	csic.AddCluster(si.Spec.Cluster)
	csic.GetServiceImportCache(si.Spec.Cluster).AddUpdateCache(si)
}

func (csic *ClusterServiceImportCache) DeleteServiceImport(cname, ns, name string) {
	if sic := csic.GetServiceImportCache(cname); sic != nil {
		csic.lock.RLock()
		defer csic.lock.RUnlock()
		sic.DeleteServiceImport(ns, name)
		return
	}
}

func (csic *ClusterServiceImportCache) GetServiceImport(cname, ns, name string) *siapi.ServiceImport {
	if sic := csic.GetServiceImportCache(cname); sic != nil {
		return sic.LookupServiceImport(ns, name)
	}
	return nil
}

type ServiceImportHandler struct {
	clusterServiceImportCache *ClusterServiceImportCache
	amkoClientset             *amkov1.Clientset
	serviceImportController   *ServiceImportController
}

var serviceImportHandler *ServiceImportHandler
var sihOnce sync.Once

func InitServiceImportHandler(amkoClientset *amkov1.Clientset, clusterList []string,
	siCtrl *ServiceImportController) {
	sihOnce.Do(func() {
		serviceImportHandler = &ServiceImportHandler{
			clusterServiceImportCache: InitClusterServiceImportCache(clusterList),
			amkoClientset:             amkoClientset,
			serviceImportController:   siCtrl,
		}
	})
}

func GetServiceImportHandler() *ServiceImportHandler {
	return serviceImportHandler
}

// If we encounter a new service from a cluster, we need to check the corresponding service import
// object at a couple of places: the service import cache and the informer cache.
//
// If the service import object exists in the service import cache but not in the informer cache,
// we need to first update the service import cache and then create a new service import object
// in the cluster.
// If the service import object doesn't exist in the service import cache, but exists in the
// informer cache, we need to add this object to the service import cache and check if an update
// is required for the corresponding object in the informer cache.
// If the service import object exists at both places, we need to check if an endpoint update
// is required in the service import cache. If yes, update the endpoints in the service import
// cache and then in the cluster's service import object.
// If the service import doesn't exist in either place, create a new one in both places.
func (sih *ServiceImportHandler) AddUpdateService(obj *siapi.ServiceImport) error {
	oldObj, err := sih.serviceImportController.GetServiceImportObjectFromInformerCache(obj.Spec.Cluster,
		obj.Spec.Namespace, obj.Spec.Service)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// object not found, will create one
			err := sih.serviceImportController.CreateServiceImportObj(obj)
			if err != nil {
				return err
			}
			// add this object to the cache
			sih.clusterServiceImportCache.AddUpdateServiceImport(obj)
			gslbutils.Logf("ns: %s, name: %s, msg: service import object created successfully", obj.GetNamespace(),
				obj.GetName())
			return nil
		}
		return fmt.Errorf("error in getting object from informer cache: %v", err)
	}
	// this will be an update/delete call
	oldCksum := GetServiceImportChecksum(oldObj)
	newCksum := GetServiceImportChecksum(obj)
	if oldCksum != newCksum {
		if err := sih.serviceImportController.UpdateServiceImportObj(obj); err != nil {
			return fmt.Errorf("error in updating object in service import informer cache: %v", err)
		}
		sih.clusterServiceImportCache.AddUpdateServiceImport(obj)
		gslbutils.Logf("ns: %s, name: %s, msg: service import object updated successfully", obj.GetNamespace(),
			obj.GetName())
	}
	return nil
}

func (sih *ServiceImportHandler) GetServiceFromCache(cname, ns, name string) *siapi.ServiceImport {
	return sih.clusterServiceImportCache.GetServiceImport(cname, ns, name)
}

func (sih *ServiceImportHandler) DeleteService(cname, ns, name string) error {
	// fetch the corresponding service import object from the informer cache
	obj, err := sih.serviceImportController.GetServiceImportObjectFromInformerCache(cname, ns, name)
	if err != nil {
		return fmt.Errorf("error in getting object from informer cache: %v", err)
	}
	if err = sih.serviceImportController.DeleteServiceImportObject(obj.GetNamespace(), obj.GetName()); err != nil {
		return fmt.Errorf("error in deleting service import object: %v", err)
	}
	return nil
}

func (sih *ServiceImportHandler) GetService(cname, ns, name string) (*siapi.ServiceImport, error) {
	obj, err := sih.serviceImportController.GetServiceImportObjectFromInformerCache(cname, ns, name)
	if err != nil {
		return nil, fmt.Errorf("error in getting object from informer cache: %v", err)
	}
	return obj, err
}

func (sih *ServiceImportHandler) GetAllServiceImportsForCluster(cname string) ([]*siapi.ServiceImport, error) {
	objs, err := sih.serviceImportController.GetServiceImportsFromClusterIndexer(cname)
	if err != nil {
		return nil, fmt.Errorf("error in getting service import objects from cluster indexer: %v", err)
	}
	return objs, nil
}

func GetServiceImportChecksum(si *siapi.ServiceImport) uint32 {
	result := containerutils.Hash(si.Spec.Cluster) +
		containerutils.Hash(si.Spec.Namespace) +
		containerutils.Hash(si.Spec.Service)
	epList := []string{}
	for _, sp := range si.Spec.SvcPorts {
		for _, ep := range sp.Endpoints {
			epList = append(epList, strconv.Itoa(int(sp.Port))+"-"+ep.IP+"-"+strconv.Itoa(int(ep.Port)))
		}
	}
	sort.Strings(epList)
	result += containerutils.Hash(containerutils.Stringify(epList))
	return result
}

func BuildServiceImportFromService(cname, ns, name string, svcPorts []siapi.BackendPort) *siapi.ServiceImport {

	// generate a name, namespace should be avi-system for now
	si := siapi.ServiceImport{
		ObjectMeta: v1.ObjectMeta{
			Namespace: utils.AviSystemNS,
			Name:      GetNameForServiceImport(cname, ns, name),
		},
		Spec: siapi.ServiceImportSpec{
			Cluster:   cname,
			Namespace: ns,
			Service:   name,
			SvcPorts:  svcPorts,
		},
	}
	return &si
}

func GetNameForServiceImport(cname, ns, name string) string {
	return cname + "--" + ns + "--" + name
}

func BuildPortListForService(cname, ns, svcName string, svc *corev1.Service) ([]siapi.BackendPort, error) {
	svcPorts := []siapi.BackendPort{}
	nodePorts := map[int32]int32{}
	for _, p := range svc.Spec.Ports {
		if svcutils.IsSvcPortInClustersetFilter(cname, svc.GetNamespace(), svc.GetName(), p.Port) {
			gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: service with port present", cname,
				svc.GetNamespace(), svc.GetName())
			nodePorts[p.Port] = p.NodePort
			svcPorts = append(svcPorts, siapi.BackendPort{
				Port:      p.Port,
				Endpoints: []siapi.IPPort{},
			})
		}
	}

	cnc := k8sutils.GetClusterNodeCache()
	nodeIPs, err := cnc.GetNodeList(cname)
	if err != nil {
		return nil, fmt.Errorf("error in getting node list: %v", err)
	}

	for idx, sp := range svcPorts {
		for _, node := range nodeIPs {
			svcPorts[idx].Endpoints = append(svcPorts[idx].Endpoints, siapi.IPPort{
				IP:   node,
				Port: nodePorts[sp.Port],
			})
		}
	}
	return svcPorts, nil
}

func AddIndexer(siInformer amkoInformers.ServiceImportInformer) {
	siInformer.Informer().AddIndexers(cache.Indexers{})
}

func GenerateSIInformerCacheKey(cname, ns, name string) string {
	return cname + "/" + ns + "/" + name
}
