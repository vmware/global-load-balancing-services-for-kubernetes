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
	// mciapi "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	"context"
	"encoding/json"
	"fmt"

	"github.com/golang/glog"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	sics "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha2/clientset/versioned/scheme"

	siapi "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	sischeme "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned/scheme"
	siinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions"
	silisters "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/listers/amko/v1alpha1"
	k8sutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/k8s_utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

type ServiceImportController struct {
	kubeClientset kubernetes.Interface
	siClientset   sics.Interface
	siLister      silisters.ServiceImportLister
	siSynced      cache.InformerSynced
	workqueue     workqueue.RateLimitingInterface
	recorder      record.EventRecorder
	Informer      cache.SharedIndexInformer
}

var siController *ServiceImportController

func InitializeServiceImportController(kubeClient *kubernetes.Clientset, siClient *sics.Clientset,
	siInformerFactory siinformers.SharedInformerFactory) *ServiceImportController {

	siInformer := siInformerFactory.Amko().V1alpha1().ServiceImports()
	// create event broadcaster
	sischeme.AddToScheme(sischeme.Scheme)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "service-import-controller"})

	siController = &ServiceImportController{
		kubeClientset: kubeClient,
		siClientset:   siClient,
		siLister:      siInformer.Lister(),
		siSynced:      siInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "si"),
		recorder:      recorder,
		Informer:      siInformer.Informer(),
	}

	siInformer.Informer().AddIndexers(cache.Indexers{
		k8sutils.ServiceImportFullIndexer: func(obj interface{}) ([]string, error) {
			siObj, ok := obj.(*siapi.ServiceImport)
			if !ok {
				return []string{}, nil
			}
			siObjCopy := siObj.DeepCopy()
			key := GenerateSIInformerCacheKey(siObjCopy.Spec.Cluster, siObjCopy.Spec.Namespace,
				siObjCopy.Spec.Service)
			return []string{key}, nil
		},
	})

	siInformer.Informer().AddIndexers(cache.Indexers{
		k8sutils.ServiceImportClusterIndexer: func(obj interface{}) ([]string, error) {
			siObj, ok := obj.(*siapi.ServiceImport)
			if !ok {
				return []string{}, nil
			}
			siObjCopy := siObj.DeepCopy()
			return []string{siObjCopy.Spec.Cluster}, nil
		},
	})

	gslbutils.Logf("object: ServiceImportController, msg: setting up event handlers")
	return siController
}

func (siController *ServiceImportController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	gslbutils.Logf("object: ServiceImportController, msg: starting the workers")
	<-stopCh
	gslbutils.Logf("object: ServiceImportController, msg: shutting down the workers")
	return nil
}

func (siController *ServiceImportController) GetServiceImportObjectFromInformerCache(cname, ns,
	name string) (*siapi.ServiceImport, error) {

	key := GenerateSIInformerCacheKey(cname, ns, name)
	siObjs, err := siController.Informer.GetIndexer().ByIndex(k8sutils.ServiceImportFullIndexer, key)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch ServiceImport object for key %s", key)
	}

	if len(siObjs) > 1 {
		return nil, fmt.Errorf("multiple objects found for key %s: %v", key, siObjs)
	}
	if len(siObjs) == 0 {
		err := k8serrors.NewNotFound(siapi.Resource("serviceimports"), name)
		return nil, err
	}
	if siObj, ok := siObjs[0].(*siapi.ServiceImport); ok {
		return siObj, nil
	}
	return nil, fmt.Errorf("object is not of type ServiceImport %T", siObjs[0])
}

func (siController *ServiceImportController) GetServiceImportsFromClusterIndexer(cname string) ([]*siapi.ServiceImport, error) {
	objs, err := siController.Informer.GetIndexer().ByIndex(k8sutils.ServiceImportClusterIndexer, cname)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch ServiceImport objects for cluster %s: %v", cname, err)
	}
	siObjs := []*siapi.ServiceImport{}
	for _, o := range objs {
		if si, ok := o.(*siapi.ServiceImport); ok {
			siObjs = append(siObjs, si.DeepCopy())
			continue
		}
		gslbutils.Errf("error in parsing object to service import, type: %T, obj: %v", o, o)
	}
	return siObjs, nil
}

func (siController *ServiceImportController) UpdateServiceImportObj(obj *siapi.ServiceImport) error {
	spec := map[string]interface{}{}
	spec["cluster"] = obj.Spec.Cluster
	spec["namespace"] = obj.Spec.Namespace
	spec["service"] = obj.Spec.Service
	spec["svcPorts"] = obj.Spec.SvcPorts

	patchPayload, err := json.Marshal(map[string]map[string]interface{}{
		"spec": spec,
	})
	if err != nil {
		return fmt.Errorf("error in marshalling spec for service import: %v", err)
	}

	if _, err := siController.siClientset.AmkoV1alpha1().ServiceImports(obj.GetNamespace()).Patch(context.TODO(),
		obj.GetName(), types.MergePatchType, patchPayload, v1.PatchOptions{}); err != nil {
		return fmt.Errorf("error in patching service import object: %v", err)
	}
	return nil
}

func (siController *ServiceImportController) CreateServiceImportObj(obj *siapi.ServiceImport) error {
	if _, err := siController.siClientset.AmkoV1alpha1().ServiceImports(obj.GetNamespace()).Create(context.TODO(),
		obj, v1.CreateOptions{}); err != nil {
		return fmt.Errorf("error in creating service import object: %v", err)
	}
	return nil
}

func (siController *ServiceImportController) DeleteServiceImportObject(ns, name string) error {
	if err := siController.siClientset.AmkoV1alpha1().ServiceImports(ns).Delete(context.TODO(),
		name, v1.DeleteOptions{}); err != nil {
		return fmt.Errorf("error in deleting service import objet: %v", err)
	}
	return nil
}

func ServiceImportEventHandlers(numWorkers uint32) cache.ResourceEventHandler {
	gslbutils.Logf("initializing service import event handlers")

	siEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
		},
		DeleteFunc: func(obj interface{}) {
		},
	}
	return siEventHandler
}
