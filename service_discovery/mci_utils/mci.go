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

package mciutils

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/golang/glog"
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	mciapi "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/apis/ako/v1alpha1"
	mcics "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	mcischeme "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned/scheme"
	mciinformers "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions"
	mcilisters "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/listers/ako/v1alpha1"
	containerutils "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	k8sutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/k8s_utils"
	svcutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/svc_utils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
)

func ValidateMCIObj(mciObj *mciapi.MultiClusterIngress, clusterList []string) error {
	// hostname can't be empty
	if mciObj.Spec.Hostname == "" {
		return fmt.Errorf("spec.hostname can't be empty")
	}

	cl := make(map[string]interface{})
	for _, cname := range clusterList {
		cl[cname] = struct{}{}
	}

	// for each config:
	//    - cluster context can't be empty
	//    - cluster context must be present in clusterset
	//    - at least one service must be there in the backend config
	//    - service name can't be empty
	for _, config := range mciObj.Spec.Config {
		if config.ClusterContext == "" {
			return fmt.Errorf("spec.config.clusterContext can't be empty")
		}
		_, clusterContextPresent := cl[config.ClusterContext]
		if !clusterContextPresent {
			return fmt.Errorf("cluster context %s is invalid and not part of clusterset", config.ClusterContext)
		}
		s := config.Service
		if s.Name == "" {
			return fmt.Errorf("service name can't be empty")
		}
		if s.Namespace == "" {
			return fmt.Errorf("service namespace can't be empty")
		}
	}

	return nil
}

type MCIServiceElement struct {
	cluster   string
	namespace string
	name      string
	port      int32
}

func InitServiceElement(cname, namespace, name string, port int32) *MCIServiceElement {
	return &MCIServiceElement{
		cluster:   cname,
		namespace: namespace,
		name:      name,
		port:      port,
	}
}

func (se *MCIServiceElement) Cluster() string {
	return se.cluster
}

func (se *MCIServiceElement) Namespace() string {
	return se.namespace
}

func (se *MCIServiceElement) Name() string {
	return se.name
}

func (se *MCIServiceElement) Port() int32 {
	return se.port
}

func GetServiceList(mciObj *mciapi.MultiClusterIngress) ([]*MCIServiceElement, error) {
	if mciObj == nil {
		return nil, fmt.Errorf("error in getting service list as MCI object is nil")
	}

	mciSvcList := []*MCIServiceElement{}
	for _, c := range mciObj.Spec.Config {
		mciSvcList = append(mciSvcList, InitServiceElement(c.ClusterContext,
			c.Service.Namespace, c.Service.Name, int32(c.Service.Port)))
	}
	return mciSvcList, nil
}

func GetServiceListStr(mciObj *mciapi.MultiClusterIngress) []string {
	svcList := []string{}
	for _, c := range mciObj.Spec.Config {
		svcList = append(svcList, c.ClusterContext+"/"+c.Service.Namespace+"/"+c.Service.Name+"/"+strconv.Itoa(c.Service.Port))
	}
	return svcList
}

func GetMCIServicesChecksum(mciObj *mciapi.MultiClusterIngress) uint32 {
	svcList := GetServiceListStr(mciObj)
	sort.Strings(svcList)
	return containerutils.Hash(containerutils.Stringify(svcList))
}

func GetMCIsDiff(oldMCI, newMCI *mciapi.MultiClusterIngress) ([]*MCIServiceElement, []*MCIServiceElement, error) {
	svcToAdd := []*MCIServiceElement{}
	svcToDelete := []*MCIServiceElement{}

	oldSvcs, err := GetServiceList(oldMCI)
	if err != nil {
		return svcToAdd, svcToDelete, fmt.Errorf("error in getting svc list from old MCI: %v", err)
	}
	newSvcs, err := GetServiceList(newMCI)
	if err != nil {
		return svcToAdd, svcToDelete, fmt.Errorf("error in getting svc list from new MCI: %v", err)
	}

	// generate a map of both old and new services
	oldSvcsMap := make(map[string]interface{})
	for _, s := range oldSvcs {
		oldSvcsMap[s.Cluster()+"/"+s.Namespace()+"/"+s.Name()] = struct{}{}
	}
	newSvcsMap := make(map[string]interface{})
	for _, s := range newSvcs {
		newSvcsMap[s.Cluster()+"/"+s.Namespace()+"/"+s.Name()] = struct{}{}
	}

	// find out the services that need to be deleted
	for _, c := range oldMCI.Spec.Config {
		_, present := newSvcsMap[c.ClusterContext+c.Service.Namespace+c.Service.Name]
		if !present {
			svcToDelete = append(svcToDelete, InitServiceElement(c.ClusterContext,
				c.Service.Namespace, c.Service.Name, int32(c.Service.Port)))
		}
	}
	for _, c := range newMCI.Spec.Config {
		_, present := oldSvcsMap[c.ClusterContext+c.Service.Namespace+c.Service.Name]
		if !present {
			svcToAdd = append(svcToAdd, InitServiceElement(c.ClusterContext,
				c.Service.Namespace, c.Service.Name, int32(c.Service.Port)))
		}
	}

	return svcToAdd, svcToDelete, nil
}

func DiffMCIServicesAndUpdateFilter(oldMCI, newMCI *mciapi.MultiClusterIngress) ([]*MCIServiceElement, []*MCIServiceElement, error) {
	svcToAdd, svcToDelete, err := GetMCIsDiff(oldMCI, newMCI)
	if err != nil {
		return svcToAdd, svcToDelete, fmt.Errorf("error in getting diff: %v", err)
	}
	for _, s := range svcToDelete {
		if err := svcutils.DeleteObjFromClustersetServiceFilter(s.Cluster(), s.Namespace(),
			s.Name(), s.Port()); err != nil {
			gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: error in deleting service from filter: %v",
				s.Cluster(), s.Namespace(), s.Name(), err)
			continue
		}
	}
	for _, s := range svcToAdd {
		if err := svcutils.AddObjToClustersetServiceFilter(s.Cluster(), s.Namespace(),
			s.Name(), s.Port()); err != nil {
			gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: error in adding service to filter: %v",
				s.Cluster(), s.Namespace(), s.Name(), err)
			continue
		}
	}

	return svcToAdd, svcToDelete, nil
}

func AddMCISvcListToFilter(mci *mciapi.MultiClusterIngress) error {
	svcList, err := GetServiceList(mci)
	if err != nil {
		return fmt.Errorf("error in getting service list from MCI object: %v", err)
	}
	for _, s := range svcList {
		if err := svcutils.AddObjToClustersetServiceFilter(s.Cluster(), s.Namespace(),
			s.Name(), s.Port()); err != nil {
			gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: error in adding service to filter: %v",
				s.Cluster(), s.Namespace(), s.Name(), err)
			continue
		}
		// TODO: push the service key to layer 2
	}
	return nil
}

func DeleteMCISvcListFromFilter(mci *mciapi.MultiClusterIngress) error {
	svcList, err := GetServiceList(mci)
	if err != nil {
		return fmt.Errorf("error in getting service list from MCI object: %v", err)
	}
	for _, s := range svcList {
		if err := svcutils.DeleteObjFromClustersetServiceFilter(s.Cluster(), s.Namespace(),
			s.Name(), s.Port()); err != nil {
			gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: error in deleting service from filter",
				s.Cluster(), s.Namespace(), s.Name())
			continue
		}
		// TODO: push the service key to layer 2
	}
	return nil
}

func MCIEventHandlers(numWorkers uint32, clusterList []string) cache.ResourceEventHandler {
	gslbutils.Logf("initializing mci event handlers")

	mciEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mci := obj.(*mciapi.MultiClusterIngress)
			if err := ValidateMCIObj(mci, clusterList); err != nil {
				gslbutils.Errf("ns: %s, name: %s, msg: error in validating MCI object: %v",
					mci.GetNamespace(), mci.GetName(), err)
				return
			}
			if err := AddMCISvcListToFilter(mci); err != nil {
				gslbutils.Errf("ns: %s, name: %s, msg: error in adding service list to filter: %v",
					mci.GetNamespace(), mci.GetName(), err)
			}
			svcList, err := GetServiceList(mci.DeepCopy())
			if err != nil {
				gslbutils.Errf("ns: %s, name: %s, msg: couldn't get service list from MCI object: %v",
					mci.GetNamespace(), mci.GetName(), err)
				return
			}
			for _, s := range svcList {
				key := utils.GetKey(utils.SvcObjType, s.Cluster(), s.Namespace(), s.Name())
				wq := k8sutils.GetWorkqueueForCluster(s.Cluster())
				bkt := containerutils.Bkt(s.Cluster(), numWorkers)
				wq[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: pushed service key to ingestion queue",
					s.Cluster(), s.Namespace(), s.Name())
			}
		},

		DeleteFunc: func(obj interface{}) {
			mci := obj.(*mciapi.MultiClusterIngress)
			if !mci.Status.Status.Accepted {
				gslbutils.Logf("ns: %s, name: %s, msg: MCI object got deleted, was in rejected state, nothing to do",
					mci.GetNamespace(), mci.GetName())
				return
			}
			// MCI object got deleted, will remove the service list form the filter
			if err := DeleteMCISvcListFromFilter(mci); err != nil {
				gslbutils.Logf("ns: %s, name: %s, msg: couldn't delete service list in the MCI object from the filter, err: %v",
					mci.GetNamespace(), mci.GetName(), err)
			}
			svcList, err := GetServiceList(mci)
			if err != nil {
				gslbutils.Errf("ns: %s, name: %s, msg: couldn't get service list from MCI object: %v",
					mci.GetNamespace(), mci.GetName(), err)
				return
			}
			for _, s := range svcList {
				key := utils.GetKey(utils.SvcObjType, s.Cluster(), s.Namespace(), s.Name())
				wq := k8sutils.GetWorkqueueForCluster(s.Cluster())
				bkt := containerutils.Bkt(s.Cluster(), numWorkers)
				wq[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: pushed service key to ingestion queue",
					s.Cluster(), s.Namespace(), s.Name())
			}
		},

		UpdateFunc: func(oldObj, newObj interface{}) {
			oldMCI := oldObj.(*mciapi.MultiClusterIngress)
			newMCI := newObj.(*mciapi.MultiClusterIngress)
			if oldMCI.GetResourceVersion() == newMCI.GetResourceVersion() {
				return
			}
			if GetMCIServicesChecksum(oldMCI) == GetMCIServicesChecksum(newMCI) {
				return
			}
			// find out the diff between the services: resultant services should be
			// added/removed from the filter
			svcToAdd, svcToDel, err := DiffMCIServicesAndUpdateFilter(oldMCI, newMCI)
			if err != nil {
				gslbutils.Errf("ns: %s, name: %s, msg: error in finding diff between old and new MCIs: %v",
					newMCI.GetNamespace(), newMCI.GetName(), err)
			}
			for _, s := range svcToAdd {
				key := utils.GetKey(utils.SvcObjType, s.Cluster(), s.Namespace(), s.Name())
				wq := k8sutils.GetWorkqueueForCluster(s.Cluster())
				bkt := containerutils.Bkt(s.Cluster(), numWorkers)
				wq[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: pushed service key to ingestion queue",
					s.Cluster(), s.Namespace(), s.Name())
			}
			for _, s := range svcToDel {
				key := utils.GetKey(utils.SvcObjType, s.Cluster(), s.Namespace(), s.Name())
				wq := k8sutils.GetWorkqueueForCluster(s.Cluster())
				bkt := containerutils.Bkt(s.Cluster(), numWorkers)
				wq[bkt].AddRateLimited(key)
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: pushed service key to ingestion queue",
					s.Cluster(), s.Namespace(), s.Name())
			}
		},
	}
	return mciEventHandler
}

type MCIController struct {
	kubeClientset kubernetes.Interface
	mciClientset  mcics.Interface
	mciLister     mcilisters.MultiClusterIngressLister
	mciSynced     cache.InformerSynced
	workqueue     workqueue.RateLimitingInterface //nolint:staticcheck
	recorder      record.EventRecorder
}

func (mciController *MCIController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	gslbutils.Logf("object: MCIController, msg: %s", "starting the workers")
	<-stopCh
	gslbutils.Logf("object: MCIController, msg: %s", "shutting down the workers")
	return nil
}

func InitializeMCIController(kubeClient *kubernetes.Clientset, mciClient *mcics.Clientset, mciInformerFactory mciinformers.SharedInformerFactory,
	clusterList []string) *MCIController {

	mciInformer := mciInformerFactory.Ako().V1alpha1().MultiClusterIngresses()
	// create event broadcaster
	mcischeme.AddToScheme(mcischeme.Scheme)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "mci-controller"})

	mciController := &MCIController{
		kubeClientset: kubeClient,
		mciClientset:  mciClient,
		mciLister:     mciInformer.Lister(),
		mciSynced:     mciInformer.Informer().HasSynced,
		workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), //nolint:staticcheck
			"mci"),
		recorder: recorder,
	}
	gslbutils.Logf("object: MCIController, msg: setting up event handlers")
	mciInformer.Informer().AddEventHandler(MCIEventHandlers(2, clusterList))
	return mciController
}
