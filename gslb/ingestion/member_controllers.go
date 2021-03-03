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

package ingestion

import (
	"fmt"
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"

	routev1 "github.com/openshift/api/route/v1"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// GSLBMemberController is actually kubernetes cluster which is added to an AVI controller
// here which is added to an AVI controller
type GSLBMemberController struct {
	name            string
	worker_id       uint32
	worker_id_mutex sync.Mutex
	informers       *containerutils.Informers
	workqueue       []workqueue.RateLimitingInterface
	syncType        int
}

// GetAviController sets config for an AviController
func GetGSLBMemberController(clusterName string, informersInstance *containerutils.Informers) GSLBMemberController {
	return GSLBMemberController{
		name:      clusterName,
		worker_id: (uint32(1) << containerutils.NumWorkersIngestion) - 1,
		informers: informersInstance,
	}
}

func (ctrl GSLBMemberController) GetName() string {
	return ctrl.name
}

// AddOrUpdateRouteStore traverses through the cluster store for cluster name cname,
// and then to ns store for the route's namespace and then adds/updates the route obj
// in the object map store.
func AddOrUpdateRouteStore(clusterRouteStore *gslbutils.ClusterStore,
	route *routev1.Route, cname string) {
	routeMeta := k8sobjects.GetRouteMeta(route, cname)
	gslbutils.Debugf("route meta object: %v", routeMeta)
	clusterRouteStore.AddOrUpdate(routeMeta, cname, route.ObjectMeta.Namespace, route.ObjectMeta.Name)
}

// DeleteFromRouteStore traverses through the cluster store for cluster name cname,
// and then ns store for the route's namespace and then deletes the route key from
// the object map store.
func DeleteFromRouteStore(clusterRouteStore *gslbutils.ClusterStore,
	route *routev1.Route, cname string) bool {
	if clusterRouteStore == nil {
		// Store is empty, so, noop
		return false
	}
	ns := route.ObjectMeta.Namespace
	routeName := route.ObjectMeta.Name
	_, present := clusterRouteStore.DeleteClusterNSObj(cname, ns, routeName)
	return present
}

// AddOrUpdateIngressStore traverses through the cluster store for cluster name cname,
// and then to ns store for the ingressHost's namespace and then adds/updates the ingressHost
// obj in the object map store.
func AddOrUpdateIngressStore(clusterRouteStore *gslbutils.ClusterStore,
	ingHost k8sobjects.IngressHostMeta, cname string) {
	clusterRouteStore.AddOrUpdate(ingHost, cname, ingHost.Namespace, ingHost.ObjName)
}

// DeleteFromIngressStore traverses through the cluster store for cluster name cname,
// and then ns store for the ingHost's namespace and then deletes the ingHost key from
// the object map store.
func DeleteFromIngressStore(clusterIngStore *gslbutils.ClusterStore,
	ingHost k8sobjects.IngressHostMeta, cname string) bool {
	if clusterIngStore == nil {
		// Store is empty, so, noop
		return false
	}
	_, present := clusterIngStore.DeleteClusterNSObj(ingHost.Cluster, ingHost.Namespace, ingHost.ObjName)
	return present
}

// SetupEventHandlers sets up event handlers for the controllers of the member clusters.
// They define the ingress/route event handlers and start the informers as well.
func (c *GSLBMemberController) SetupEventHandlers(k8sinfo K8SInformers) {
	cs := k8sinfo.Cs
	gslbutils.Logf("k8scontroller: %s, msg: %s", c.name, "creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(containerutils.AviLog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})

	k8sQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	c.workqueue = k8sQueue.Workqueue
	numWorkers := k8sQueue.NumWorkers

	if c.informers.IngressInformer != nil {
		ingressEventHandler := AddIngressEventHandler(numWorkers, c)
		c.informers.IngressInformer.Informer().AddEventHandler(ingressEventHandler)
	}
	if c.informers.RouteInformer != nil {
		routeEventHandler := AddRouteEventHandler(numWorkers, c)
		c.informers.RouteInformer.Informer().AddEventHandler(routeEventHandler)
	}

	if c.informers.ServiceInformer != nil {
		lbsvcEventHandler := AddLBSvcEventHandler(numWorkers, c)
		c.informers.ServiceInformer.Informer().AddEventHandler(lbsvcEventHandler)
	}

	if c.informers.NSInformer != nil {
		nsEventHandler := AddNamespaceEventHandler(numWorkers, c)
		c.informers.NSInformer.Informer().AddEventHandler(nsEventHandler)
	}
}

func isSvcTypeLB(svc *corev1.Service) bool {
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		return true
	}
	return false
}

// AddOrUpdateLBSvcStore traverses through the cluster store for cluster name cname,
// and then to ns store for the service's namespace and then adds/updates the service obj
// in the object map store.
func AddOrUpdateLBSvcStore(clusterSvcStore *gslbutils.ClusterStore,
	svc *corev1.Service, cname string) {
	svcMeta, _ := k8sobjects.GetSvcMeta(svc, cname)
	gslbutils.Debugf("updating service store: %s", svc.ObjectMeta.Name)
	clusterSvcStore.AddOrUpdate(svcMeta, cname, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
}

// DeleteFromLBSvcStore traverses through the cluster store for cluster name cname,
// and then ns store for the service's namespace and then deletes the service key from
// the object map store.
func DeleteFromLBSvcStore(clusterSvcStore *gslbutils.ClusterStore,
	svc *corev1.Service, cname string) {
	if clusterSvcStore == nil {
		// Store is empty, so, noop
		return
	}
	clusterSvcStore.DeleteClusterNSObj(cname, svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
}

func (c *GSLBMemberController) Start(stopCh <-chan struct{}) {
	var cacheSyncParam []cache.InformerSynced

	if c.informers.IngressInformer != nil {
		gslbutils.Logf("cluster: %s, msg: %s", c.name, "starting Ingress informer")
		go c.informers.IngressInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.IngressInformer.Informer().HasSynced)
	}

	if c.informers.RouteInformer != nil {
		gslbutils.Logf("cluster: %s, msg: %s", c.name, "starting route informer")
		go c.informers.RouteInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.RouteInformer.Informer().HasSynced)
	}

	if c.informers.ServiceInformer != nil {
		gslbutils.Logf("cluster: %s, msg: %s", c.name, "starting service informer")
		go c.informers.ServiceInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.ServiceInformer.Informer().HasSynced)
	}

	if c.informers.NSInformer != nil {
		gslbutils.Logf("cluster: %s, msg: %s", c.name, "starting namespace informer")
		go c.informers.NSInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.NSInformer.Informer().HasSynced)
	}

	if !cache.WaitForCacheSync(stopCh, cacheSyncParam...) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
	} else {
		gslbutils.Logf("cluster: %s, msg: %s", c.name, "caches synced")
	}
}

func (c *GSLBMemberController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()

	gslbutils.Logf("cluster: %s, msg: %s", c.name, "started the kubernetes controller")
	<-stopCh
	gslbutils.Logf("cluster: %s, msg: %s", c.name, "shutting down the kubernetes controller")
	return nil
}
