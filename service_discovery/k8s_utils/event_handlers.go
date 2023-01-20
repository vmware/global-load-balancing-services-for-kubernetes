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

package k8sutils

import (
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	svcutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/svc_utils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
)

func SvcEventHandlers(numWorkers uint32, c *K8sClusterConfig) cache.ResourceEventHandler {
	gslbutils.Logf("cluster: %s, msg: initializing service event handlers", c.Name())
	svcEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*corev1.Service).DeepCopy()
			if !svcutils.IsServiceOfAcceptedType(svc) {
				return
			}
			gslbutils.Logf("cluster: %s, namespace: %s, svc: %s, msg: added service", c.Name(),
				svc.GetNamespace(), svc.GetName())

			if svcutils.IsObjectInClustersetFilter(c.Name(), svc.GetNamespace(), svc.GetName()) {
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: added service, present in filter, will be pushed to layer 2",
					c.Name(), svc.GetNamespace(), svc.GetName())
				key := utils.GetKey(utils.SvcObjType, c.Name(), svc.GetNamespace(), svc.GetName())
				bkt := containerutils.Bkt(c.Name(), numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: pushed service key to ingestion queue",
					c.Name(), svc.GetNamespace(), svc.GetName())
			}
		},
		DeleteFunc: func(obj interface{}) {
			svc := obj.(*corev1.Service).DeepCopy()
			if !svcutils.IsServiceOfAcceptedType(svc) {
				return
			}
			gslbutils.Logf("cluster: %s, namespace: %s, svc: %s, msg: deleted service", c.Name(),
				svc.GetNamespace(), svc.GetName())

			if svcutils.IsObjectInClustersetFilter(c.Name(), svc.GetNamespace(), svc.GetName()) {
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: service deleted, present in filter, will be pushed to layer 2",
					c.Name(), svc.GetNamespace(), svc.GetName())
				key := utils.GetKey(utils.SvcObjType, c.Name(), svc.GetNamespace(), svc.GetName())
				bkt := containerutils.Bkt(c.Name(), numWorkers)
				c.workqueue[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: pushed service key to ingestion queue",
					c.Name(), svc.GetNamespace(), svc.GetName())
			}
		},
		UpdateFunc: func(old, curr interface{}) {
			oldSvc := old.(*corev1.Service).DeepCopy()
			svc := curr.(*corev1.Service).DeepCopy()
			gslbutils.Logf("cluster: %s, namespace: %s, svc: %s, msg: service updated", c.Name(),
				svc.GetNamespace(), svc.GetName())

			if oldSvc.GetResourceVersion() == svc.GetResourceVersion() {
				return
			}
			if svcutils.IsServiceOfAcceptedType(oldSvc) || svcutils.IsServiceOfAcceptedType(svc) {
				if svcutils.IsObjectInClustersetFilter(c.Name(), oldSvc.GetNamespace(), oldSvc.GetName()) {
					gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: service updated, present in filter, key will be published",
						c.Name(), svc.GetNamespace(), svc.GetName())
					key := utils.GetKey(utils.SvcObjType, c.Name(), svc.GetNamespace(), svc.GetName())
					bkt := containerutils.Bkt(c.Name(), numWorkers)
					c.workqueue[bkt].AddRateLimited(key)
					gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: pushed service key to ingestion queue",
						c.Name(), svc.GetNamespace(), svc.GetName())
				}
			}
		},
	}
	return svcEventHandler
}

func NodeEventHandlers(numWorkers uint32, c *K8sClusterConfig) cache.ResourceEventHandler {
	gslbutils.Logf("cluster: %s, msg: initializing node event handlers", c.Name())

	nodeEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node := obj.(*corev1.Node).DeepCopy()
			gslbutils.Logf("cluster: %s, node: %s, msg: node added, will be published to layer 2",
				c.Name(), node.GetName())
			key := utils.GetKey(utils.NodeObjType, c.Name(), node.GetName())
			bkt := containerutils.Bkt(c.Name(), numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			gslbutils.Logf("cluster: %s, node: %s, msg: pushed node key to ingestion queue",
				c.Name(), node.GetNamespace(), node.GetName())
		},
		DeleteFunc: func(obj interface{}) {
			node := obj.(*corev1.Node).DeepCopy()
			gslbutils.Logf("cluster: %s, node: %s, msg: node deleted, will be published to layer 2",
				c.Name(), node.GetName())
			key := utils.GetKey(utils.NodeObjType, c.Name(), node.GetName())
			bkt := containerutils.Bkt(c.Name(), numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			gslbutils.Logf("cluster: %s, node: %s, msg: pushed node key to ingestion queue",
				c.Name(), node.GetNamespace(), node.GetName())
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldNode := oldObj.(*corev1.Node).DeepCopy()
			newNode := newObj.(*corev1.Node).DeepCopy()
			if oldNode.GetResourceVersion() == newNode.GetResourceVersion() {
				return
			}
			gslbutils.Logf("cluster: %s, node: %s, msg: node updated, will be published to layer 2",
				c.Name(), oldNode.GetName())
			key := utils.GetKey(utils.NodeObjType, c.Name(), newNode.GetName())
			bkt := containerutils.Bkt(c.Name(), numWorkers)
			c.workqueue[bkt].AddRateLimited(key)
			gslbutils.Logf("cluster: %s, node: %s, msg: pushed node key to ingestion queue", c.Name(),
				newNode.GetName())
		},
	}
	return nodeEventHandler
}
