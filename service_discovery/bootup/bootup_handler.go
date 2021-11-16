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

package bootup

import (
	"context"
	"fmt"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	mcics "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/clusterset"
	k8sutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/k8s_utils"
	mciutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/mci_utils"
	serviceimport "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/service_import"
	svcutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/svc_utils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func BootupSync(clusterConfigs []*k8sutils.K8sClusterConfig, mcics *mcics.Clientset) error {
	clusterList := clusterset.GetClusterList(clusterConfigs)
	// initialize the service filter
	svcutils.InitClustersetServiceFilter(clusterList)

	// fetch all the MCI objects, validate each object and add them to filter
	mciObjs, err := mcics.AmkoV1alpha1().MultiClusterIngresses(utils.AviSystemNS).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error in fetching MCI list: %v", err)
	}

	if len(mciObjs.Items) == 0 {
		gslbutils.Logf("no MCI objects present in the cluster, returning")
		return nil
	}

	// the following loop should build the clusterset service filter. This filter will then
	// be used by the services from the member clusters.
	for _, mci := range mciObjs.Items {
		err := mciutils.ValidateMCIObj(&mci, clusterList)
		if err != nil {
			gslbutils.Errf("ns: %s, name: %s, msg: validation error for MCI object: %v",
				mci.GetNamespace(), mci.GetName(), err)
			continue
		}

		// TODO: update the MCI object
		svcList, err := mciutils.GetServiceList(&mci)
		if err != nil {
			gslbutils.Errf("ns: %s, name: %s, msg: error in getting service list: %v",
				mci.GetNamespace(), mci.GetName(), err)
			continue
		}

		for _, s := range svcList {
			if err := svcutils.AddObjToClustersetServiceFilter(s.Cluster(), s.Namespace(),
				s.Name(), s.Port()); err != nil {
				gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: error in adding service to filter: %v")
				continue
			}
		}
	}

	// fetch all the services from the member clusters
	for _, cc := range clusterConfigs {
		// fetch all the member cluster nodes, push them to layer 2
		nodeList, err := cc.GetNodes()
		if err != nil {
			gslbutils.Errf("error in fetching nodes for cluster %s: %v", cc.Name(), err)
			continue
		}
		for _, node := range nodeList.Items {
			nodeIP, err := k8sutils.GetNodeIP(node.Status)
			if err != nil {
				gslbutils.Errf("cluster: %s, nodeName: %s, msg: error in fetching node IP", cc.Name(), node.GetName())
				continue
			}
			gslbutils.Logf("cluster: %s, nodeName: %s, msg: fetched node", cc.Name(),
				node.GetName())
			serviceimport.HandleNodeObject(cc.Name(), node.GetName(), nodeIP)
			gslbutils.Logf("cluster: %s, nodeName: %s, msg: processed node", cc.Name(), node.GetName())
		}

		svcs, err := cc.ClientSet().CoreV1().Services("").List(context.TODO(), v1.ListOptions{})
		if err != nil {
			gslbutils.Errf("error in fetching services for cluster %s: %v", cc.Name(), err)
			continue
		}
		for _, svc := range svcs.Items {
			// for each service, see if it is of the accepted type
			if !svcutils.IsServiceOfAcceptedType(&svc) {
				continue
			}
			if !svcutils.IsObjectInClustersetFilter(cc.Name(), svc.GetNamespace(), svc.GetName()) {
				continue
			}
			// service must be accepted, pass it on layer 2
			gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: service present in filter, will be processed",
				cc.Name(), svc.GetNamespace(), svc.GetName())
			serviceimport.HandleServiceObject(cc.Name(), svc.GetNamespace(), svc.GetName(), svc.DeepCopy())
			gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: service processed", cc.Name(),
				svc.GetNamespace(), svc.GetName())
		}
	}

	// fetch all the nodes from the member clusters
	return nil
}
