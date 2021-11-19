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
	"fmt"
	"sync"
)

type ClusterNodeCache struct {
	clusterNodeSet map[string]*NodeCache
	lock           sync.RWMutex
}

var clusterNodeCache *ClusterNodeCache
var clusterNodeCacheOnce sync.Once

func GetClusterNodeCache() *ClusterNodeCache {
	clusterNodeCacheOnce.Do(func() {
		cns := make(map[string]*NodeCache)
		clusterNodeCache = &ClusterNodeCache{
			clusterNodeSet: cns,
		}
	})
	return clusterNodeCache
}

func (cnc *ClusterNodeCache) GetNodeCache(cname string) *NodeCache {
	cnc.lock.RLock()
	defer cnc.lock.RUnlock()

	if v, clusterPresent := cnc.clusterNodeSet[cname]; clusterPresent {
		return v
	}
	return nil
}

func (cnc *ClusterNodeCache) AddCluster(cname string) {
	cnc.lock.Lock()
	defer cnc.lock.Unlock()

	cnc.clusterNodeSet[cname] = GetNodeCache()
}

func (cnc *ClusterNodeCache) AddNode(cname, node, nodeIP string) {
	if nodeCache := cnc.GetNodeCache(cname); nodeCache != nil {
		nodeCache.Add(node, nodeIP)
		return
	}
	// add a new entry
	cnc.AddCluster(cname)
	cnc.GetNodeCache(cname).Add(node, nodeIP)
}

func (cnc *ClusterNodeCache) DeleteNode(cname, node string) {
	if nodeCache := cnc.GetNodeCache(cname); nodeCache != nil {
		nodeCache.Delete(node)
	}
}

func (cnc *ClusterNodeCache) GetNodeInfo(cname, node string) (string, error) {
	if nodeCache := cnc.GetNodeCache(cname); nodeCache != nil {
		return nodeCache.Get(node)
	}
	return "", fmt.Errorf("cluster entry %s doesn't exist in cache", cname)
}

func (cnc *ClusterNodeCache) GetNodeList(cname string) ([]string, error) {
	if nodeCache := cnc.GetNodeCache(cname); nodeCache != nil {
		return nodeCache.GetAllNodeIPs(), nil
	}
	return nil, fmt.Errorf("no entry for cluster %s", cname)
}

type NodeCache struct {
	nodeSet map[string]string
	lock    sync.RWMutex
}

func GetNodeCache() *NodeCache {
	nodeSet := make(map[string]string)
	nodeCache := &NodeCache{
		nodeSet: nodeSet,
	}
	return nodeCache
}

func (nc *NodeCache) Add(node string, nodeIP string) {
	nc.lock.Lock()
	defer nc.lock.Unlock()

	nc.nodeSet[node] = nodeIP
}

func (nc *NodeCache) Delete(node string) {
	nc.lock.Lock()
	defer nc.lock.Unlock()

	delete(nc.nodeSet, node)
}

func (nc *NodeCache) Get(node string) (string, error) {
	nc.lock.RLock()
	defer nc.lock.RUnlock()

	if nodeIP, nodePresent := nc.nodeSet[node]; nodePresent {
		return nodeIP, nil
	}
	return "", fmt.Errorf("no entry for %s in node cache", node)
}

func (nc *NodeCache) GetAllNodeIPs() []string {
	result := []string{}

	nc.lock.RLock()
	defer nc.lock.RUnlock()
	for _, v := range nc.nodeSet {
		result = append(result, v)
	}
	return result
}
