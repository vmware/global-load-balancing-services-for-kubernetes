/***************************************************************************
 *
 * AVI CONFIDENTIAL
 * __________________
 *
 * [2013] - [2019] Avi Networks Incorporated
 * All Rights Reserved.
 *
 * NOTICE: All information contained herein is, and remains the property
 * of Avi Networks Incorporated and its suppliers, if any. The intellectual
 * and technical concepts contained herein are proprietary to Avi Networks
 * Incorporated, and its suppliers and are covered by U.S. and Foreign
 * Patents, patents in process, and are protected by trade secret or
 * copyright law, and other laws. Dissemination of this information or
 * reproduction of this material is strictly forbidden unless prior written
 * permission is obtained from Avi Networks Incorporated.
 */

/*
Kubernets/Openshift Controller for HA cloud.
This file has been taken from AVi servicemesh repo with modifications to support for ingress and route.
https://github.com/avinetworks/servicemesh/blob/master/amc/pkg/k8s/controller.go
*/
package hacloud

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/avinetworks/amko/hacloud/hautils"

	containerutils "github.com/avinetworks/ako/pkg/utils"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

var controllerInstances []*AviController
var ctrlonce sync.Once

type AviController struct {
	name            string
	worker_id       uint32
	worker_id_mutex sync.Mutex
	informers       *containerutils.Informers
	workqueue       []workqueue.RateLimitingInterface
}

func GetAviController(clusterName string, informersInstance *containerutils.Informers) AviController {
	controller := AviController{
		name:      clusterName,
		worker_id: (uint32(1) << containerutils.NumWorkersIngestion) - 1,
		informers: informersInstance,
	}
	return controller
}

func (c *AviController) SetupEventHandlers(k8sinfo K8sinformers) {
	cs := k8sinfo.cs
	containerutils.AviLog.Infof("Creating event broadcaster for %v", c.name)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(containerutils.AviLog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events("")})

	k8sQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	c.workqueue = k8sQueue.Workqueue
	numWorkers := k8sQueue.NumWorkers

	ep_event_handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ep := obj.(*corev1.Endpoints)
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ep))
			key := hautils.MultiClusterKey("Endpoints/", c.name, ep)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Infof("ADD Endpoint key: %s", key)
		},
		DeleteFunc: func(obj interface{}) {
			ep, ok := obj.(*corev1.Endpoints)
			if !ok {
				// endpoints was deleted but its final state is unrecorded.
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					containerutils.AviLog.Errorf("couldn't get object from tombstone %#v", obj)
					return
				}
				ep, ok = tombstone.Obj.(*corev1.Endpoints)
				if !ok {
					containerutils.AviLog.Errorf("Tombstone contained object that is not an Endpoints: %#v", obj)
					return
				}
			}
			ep = obj.(*corev1.Endpoints)
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ep))
			key := hautils.MultiClusterKey("Endpoints/", c.name, ep)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Infof("DELETE Endpoint key: %s", key)
		},
		UpdateFunc: func(old, cur interface{}) {
			oep := old.(*corev1.Endpoints)
			cep := cur.(*corev1.Endpoints)
			if !reflect.DeepEqual(cep.Subsets, oep.Subsets) {
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(cep))
				key := hautils.MultiClusterKey("Endpoints/", c.name, cep)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Infof("UPDATE Endpoint key: %s", key)
			}
		},
	}

	svc_event_handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*corev1.Service)
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(svc))
			key := hautils.MultiClusterKey("Service/", c.name, svc)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Infof("ADD Service key: %s", key)
		},
		DeleteFunc: func(obj interface{}) {
			svc, ok := obj.(*corev1.Service)
			if !ok {
				// Service was deleted but its final state is unrecorded.
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					containerutils.AviLog.Errorf("couldn't get object from tombstone %#v", obj)
					return
				}
				svc, ok = tombstone.Obj.(*corev1.Service)
				if !ok {
					containerutils.AviLog.Errorf("Tombstone contained object that is not an Service: %#v", obj)
					return
				}
			}
			svc = obj.(*corev1.Service)
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(svc))
			key := hautils.MultiClusterKey("Service/", c.name, svc)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Infof("DELETE Service key: %s", key)
		},
		UpdateFunc: func(old, cur interface{}) {
			oldobj := old.(*corev1.Service)
			svc := cur.(*corev1.Service)
			if oldobj.ResourceVersion != svc.ResourceVersion {
				// Only add the key if the resource versions have changed.
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(svc))
				key := hautils.MultiClusterKey("Service/", c.name, svc)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Infof("UPDATE service key: %s", key)
			}
		},
	}

	ingress_event_handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ingr, ok := containerutils.ToNetworkingIngress(obj)
			if !ok {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to networking/v1beta1 ingress")
			}
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ingr))
			key := hautils.MultiClusterKey("Ingress/", c.name, ingr)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Infof("Added ADD Ingress key from the kubernetes controller %s", key)
		},
		DeleteFunc: func(obj interface{}) {
			ingr, ok := containerutils.ToNetworkingIngress(obj)
			if !ok {
				containerutils.AviLog.Errorf("object type is not Ingress")
			}
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ingr))
			key := hautils.MultiClusterKey("Ingress/", c.name, ingr)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Infof("Added DELETE Ingress key from the kubernetes controller %s", key)
		},
		UpdateFunc: func(old, cur interface{}) {
			oldobj, okOld := containerutils.ToNetworkingIngress(old)
			ingr, okNew := containerutils.ToNetworkingIngress(cur)
			if !okOld || !okNew {
				containerutils.AviLog.Errorf("Unable to convert obj type interface to networking/v1beta1 ingress")
			}
			if oldobj.ResourceVersion != ingr.ResourceVersion {
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(ingr))
				key := hautils.MultiClusterKey("Ingress/", c.name, ingr)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Infof("UPDATE ingress key: %s", key)
			}
		},
	}

	route_event_handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			route := obj.(*routev1.Route)
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
			key := hautils.MultiClusterKey("Route/", c.name, route)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Infof("Added ADD Route key from the controller %s", key)
		},
		DeleteFunc: func(obj interface{}) {
			route, ok := obj.(*routev1.Route)
			if !ok {
				containerutils.AviLog.Errorf("object type is not Route")
			}
			namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
			key := hautils.MultiClusterKey("Route/", c.name, route)
			bkt := containerutils.Bkt(namespace, numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			containerutils.AviLog.Infof("Added DELETE Route key from the controller %s", key)
		},
		UpdateFunc: func(old, cur interface{}) {
			oldobj := old.(*routev1.Route)
			route := cur.(*routev1.Route)
			if oldobj.ResourceVersion != route.ResourceVersion {
				namespace, _, _ := cache.SplitMetaNamespaceKey(containerutils.ObjKey(route))
				key := hautils.MultiClusterKey("Route/", c.name, route)
				bkt := containerutils.Bkt(namespace, numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				containerutils.AviLog.Infof("UPDATE Route key: %s", key)
			}
		},
	}

	if c.informers.EpInformer != nil {
		c.informers.EpInformer.Informer().AddEventHandler(ep_event_handler)
	}
	if c.informers.ServiceInformer != nil {
		c.informers.ServiceInformer.Informer().AddEventHandler(svc_event_handler)
	}
	if c.informers.IngressInformer != nil {
		c.informers.IngressInformer.Informer().AddEventHandler(ingress_event_handler)
	}
	if c.informers.RouteInformer != nil {
		c.informers.RouteInformer.Informer().AddEventHandler(route_event_handler)
	}
}

func (c *AviController) Start(stopCh <-chan struct{}) {
	var cacheSyncParam []cache.InformerSynced
	if c.informers.EpInformer != nil {
		go c.informers.ServiceInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.EpInformer.Informer().HasSynced)
	}
	if c.informers.ServiceInformer != nil {
		go c.informers.EpInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.ServiceInformer.Informer().HasSynced)
	}
	if c.informers.IngressInformer != nil {
		go c.informers.IngressInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.IngressInformer.Informer().HasSynced)
	}
	if c.informers.RouteInformer != nil {
		go c.informers.RouteInformer.Informer().Run(stopCh)
		cacheSyncParam = append(cacheSyncParam, c.informers.RouteInformer.Informer().HasSynced)
	}

	if !cache.WaitForCacheSync(stopCh, cacheSyncParam...) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
	} else {
		containerutils.AviLog.Info("Caches synced")
	}
}

// // Run will set up the event handlers for types we are interested in, as well
// // as syncing informer caches and starting workers. It will block until stopCh
// // is closed, at which point it will shutdown the workqueue and wait for
// // workers to finish processing their current work items.
func (c *AviController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()

	containerutils.AviLog.Info("Started the Kubernetes Controller")
	<-stopCh
	containerutils.AviLog.Info("Shutting down the Kubernetes Controller")

	return nil
}
