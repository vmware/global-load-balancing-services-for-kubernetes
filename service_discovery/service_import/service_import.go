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
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	k8sutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/k8s_utils"
	svcutils "github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/svc_utils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/service_discovery/utils"
)

func HandleMCIObject(ns, name string) {
	// for any MCI object received, fetch all the services
}

func HandleClusterObject(cname string) {
	// TODO: for a new cluster added, fetch all services and nodes and create
	// service imports for all of them. For a cluster delete event, delete all
	// service imports belonging to that cluster.
}

func HandleServiceObject(cname, ns, name string, args ...*v1.Service) {
	var svc *v1.Service
	var err error
	siHandler := GetServiceImportHandler()

	if len(args) > 1 {
		gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: can't handle service, more than 1 extra args provided: %v",
			cname, ns, name, args)
		return
	}
	if len(args) == 1 {
		// svc is provided by the caller
		svc = args[0]
	} else {
		svc, err = k8sutils.GetSvcInfoFromSharedClusters(cname, ns, name)
		if err != nil {
			if k8sutils.IsErrorTypeNotFound(err) {
				if err := siHandler.DeleteService(cname, ns, name); err != nil {
					gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: cluster service deleted, but error in deleting service import: %v",
						cname, ns, name, err)
					return
				}
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: cluster service deleted, service import object is deleted successfully",
					cname, ns, name)
				return
			}
			gslbutils.Errf("cluster: %s, ns: %s, svc: %s, msg: error in getting service: %v",
				cname, ns, name, err)
		}
	}

	gslbutils.Logf("cluster: %s, ns: %s, svc: %s, msg: service added, will update endpoint",
		cname, ns, name)
	// check if service is of accepted type
	if !svcutils.IsServiceOfAcceptedType(svc) {
		_, err := siHandler.GetService(cname, ns, name)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: error in service import lookup: %v",
					cname, ns, name, err)
				return
			} else {
				gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: service import doesn't exist and service is of unaccepted type, nothing to do",
					cname, ns, name)
			}
		}
		gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: service import found for unaccepted service type, will be deleted",
			cname, ns, name, svc.Spec.Type)
		if err := siHandler.DeleteService(cname, ns, name); err != nil {
			gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: error in deleting service import: %v",
				cname, ns, name, err)
			return
		}
	}

	// svcPorts contains the ports present in the filter for this service
	svcPorts, err := BuildPortListForService(cname, ns, name, svc)
	if err != nil {
		gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: error in building service endpoints: %v", err,
			cname, ns, name)
		return
	}
	if len(svcPorts) == 0 {
		// endpoint list is empty, delete the service import object
		gslbutils.Errf("cluster: %s. ns: %s, name: %s, msg: endpoint list is empty for this service, service import object will be deleted",
			cname, ns, name)
		if err := siHandler.DeleteService(cname, ns, name); err != nil {
			gslbutils.Errf("cluster: %s. ns: %s, name: %s, msg: service import object couldn't be deleted: %v",
				cname, ns, name, err)
		}
		gslbutils.Logf("cluster: %s, ns: %s, name: %s, msg: service import object deleted successfully",
			cname, ns, name)
		return
	}
	si := BuildServiceImportFromService(cname, ns, name, svcPorts)
	err = siHandler.AddUpdateService(si)
	if err != nil {
		gslbutils.Errf("cluster: %s, ns: %s, name: %s, msg: error in adding/updating service: %v", cname,
			ns, name, err)
	}
}

func AddUpdateAllServiceImportsForCluster(cname string) {
	siHandler := GetServiceImportHandler()
	siObjs, err := siHandler.GetAllServiceImportsForCluster(cname)
	if err != nil {
		gslbutils.Errf("cluster: %s, msg: error in getting all the service imports: %v",
			cname, err)
	}
	for _, siObj := range siObjs {
		HandleServiceObject(siObj.Spec.Cluster, siObj.Spec.Namespace, siObj.Spec.Service)
	}
	gslbutils.Logf("cluster: %s, msg: all service import objects updated", cname)
}

func HandleNodeObject(cname, nodeName string, args ...string) {
	var nodeIP string
	var err error
	cnc := k8sutils.GetClusterNodeCache()
	if len(args) > 1 {
		gslbutils.Errf("error in handling node object, more than 1 extra parameters: %v", args)
		return
	}
	if len(args) == 1 {
		// arg0 contains the node IP
		nodeIP = args[0]
	} else {
		nodeIP, err = k8sutils.GetNodeInfoFromSharedClusters(cname, nodeName)
		if err != nil {
			if k8sutils.IsErrorTypeNotFound(err) {
				// TODO: this is a node deletion event, delete this node's entry from all
				// endpoints and return
				cnc.DeleteNode(cname, nodeName)
				// update all services for this cluster
				AddUpdateAllServiceImportsForCluster(cname)
				return
			}
			gslbutils.Errf("cluster: %s, node: %s, msg: error in getting node details: %v",
				cname, nodeName, err)
			return
		}
	}
	// a new node was added/updated, update this node's entry to all endpoints
	gslbutils.Logf("cluster: %s, node: %s, ip: %s, msg: node added, will update endpoints",
		cname, nodeName, nodeIP)
	cnc.AddNode(cname, nodeName, nodeIP)
	AddUpdateAllServiceImportsForCluster(cname)
}

func ProcessIngestionKey(key string) error {
	keySplit := strings.Split(key, "/")
	switch keySplit[0] {
	case utils.MCIObjType:
		// key: MCIType/namespace/mci
		if len(keySplit) != 3 {
			return fmt.Errorf("invalid key length, expected: %d, got: %d", 3, len(keySplit))
		}
		HandleMCIObject(keySplit[1], keySplit[2])
	case utils.ClusterObjType:
		// key: ClusterType/cluster
		if len(keySplit) != 2 {
			return fmt.Errorf("invalid key length, expected: %d, got: %d", 2, len(keySplit))
		}
		HandleClusterObject(keySplit[1])
	case utils.SvcObjType:
		// key: SvcType/cluster/namespace/svc
		if len(keySplit) != 4 {
			return fmt.Errorf("invalid key length, expected: %d, got: %d", 4, len(keySplit))
		}
		HandleServiceObject(keySplit[1], keySplit[2], keySplit[3])
	case utils.NodeObjType:
		// key: NodeType/cluster/node
		if len(keySplit) != 3 {
			return fmt.Errorf("invalid key length, expected: %d, got: %d", 3, len(keySplit))
		}
		HandleNodeObject(keySplit[1], keySplit[2])
	default:
		return fmt.Errorf("invalid object name %s in key", keySplit[0])
	}
	return nil
}

func SyncFromIngestionLayer(key interface{}, wg *sync.WaitGroup) error {
	keyStr, ok := key.(string)
	if !ok {
		gslbutils.Errf("key: %v, msg: key is not of string type", key)
		return fmt.Errorf("key %v is not of type string type", key)
	}
	if err := utils.ValidateKey(keyStr); err != nil {
		gslbutils.Errf("key: %s, msg: error in ingestion key validation: %v", key, err)
		return err
	}

	// process key
	if err := ProcessIngestionKey(keyStr); err != nil {
		gslbutils.Errf("key: %v, msg: error in ingestion key processing: %v", key, err)
		return err
	}
	return nil
}
