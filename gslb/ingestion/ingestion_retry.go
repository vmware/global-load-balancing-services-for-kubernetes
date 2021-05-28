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

package ingestion

import (
	"context"
	"fmt"
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	gslbhralphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type retryUpdateValues struct {
	old *gslbhralphav1.GSLBHostRule
	new *gslbhralphav1.GSLBHostRule
}

type retryUpdateCache struct {
	cache map[string]retryUpdateValues
	lock  sync.RWMutex
}

var retryUpdateMap *retryUpdateCache
var retryUpdateOnce sync.Once

func getRetryUpdateCache() *retryUpdateCache {
	retryUpdateOnce.Do(func() {
		retryUpdateMap = &retryUpdateCache{
			cache: map[string]retryUpdateValues{},
		}
	})
	return retryUpdateMap
}

func (c *retryUpdateCache) writeValueFor(ns, name string, old, new *gslbhralphav1.GSLBHostRule) {
	c.lock.Lock()
	defer c.lock.Unlock()

	key := ns + "/" + name
	val, ok := c.cache[ns+"/"+name]
	if !ok {
		// value doesn't exist, just write and return
		c.cache[key] = retryUpdateValues{
			old: old,
			new: new,
		}
		return
	}
	// value exists, just update the new value
	c.cache[key] = retryUpdateValues{
		old: val.old,
		new: new,
	}
}

func (c *retryUpdateCache) readAndDeleteKeyFor(ns, name string) (retryUpdateValues, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	val, ok := c.cache[ns+"/"+name]
	if !ok {
		return retryUpdateValues{}, fmt.Errorf("value not present for namespace %s and name %s", ns, name)
	}
	delete(c.cache, ns+"/"+name)
	return val, nil
}

type retryAddCache struct {
	cache map[string]*gslbhralphav1.GSLBHostRule
	lock  sync.RWMutex
}

var retryAddMap *retryAddCache
var retryAddOnce sync.Once

func getRetryAddCache() *retryAddCache {
	retryAddOnce.Do(func() {
		retryAddMap = &retryAddCache{
			cache: map[string]*gslbhralphav1.GSLBHostRule{},
		}
	})
	return retryAddMap
}

func (c *retryAddCache) writeValueFor(ns, name string, obj *gslbhralphav1.GSLBHostRule) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.cache[ns+"/"+name] = obj
}

func (c *retryAddCache) readAndDeleteKeyFor(ns, name string) (*gslbhralphav1.GSLBHostRule, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	val, ok := c.cache[ns+"/"+name]
	if !ok {
		return nil, fmt.Errorf("value not present for namespace %s and name %s", ns, name)
	}
	delete(c.cache, ns+"/"+name)
	return val, nil
}

func publishKeyToIngestionRetry(op, obj, namespace, name string) {
	key := op + "/" + obj + "/" + namespace + "/" + name
	rq := utils.SharedWorkQueue().GetQueueByName(gslbutils.IngestionRetryQueue)
	rq.Workqueue[0].AddRateLimited(key)
	gslbutils.Logf("key: %s, msg: published key to ingestion retry queue", key)
}

func updateIngestionRetryAddCache(obj *gslbhralphav1.GSLBHostRule) {
	if obj == nil {
		return
	}
	gslbhr := obj.DeepCopy()
	addCache := getRetryAddCache()
	addCache.writeValueFor(gslbhr.Namespace, gslbhr.Name, gslbhr)
}

func updateIngestionRetryUpdateCache(oldObj, newObj *gslbhralphav1.GSLBHostRule) {
	if oldObj == nil || newObj == nil {
		return
	}
	old, new := oldObj.DeepCopy(), newObj.DeepCopy()
	updateCache := getRetryUpdateCache()
	updateCache.writeValueFor(new.Namespace, new.Name, old, new)
}

func getGslbHostRule(ns, name string) (*gslbhralphav1.GSLBHostRule, error) {
	obj, err := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBHostRules(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func IngestionRetryAddUpdate(key string, wg *sync.WaitGroup) error {
	gslbutils.Logf("key: %s, msg: processing key in ingestion retry", key)
	op, objType, ns, name, err := gslbutils.ExtractIngestionRetryQueueKey(key)
	if err != nil {
		gslbutils.Errf("key: %s, msg: error in processing key for ingestion retry: %v", key, err)
		return nil
	}

	k8sQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
	switch objType {
	case gslbutils.GslbHostRuleType:
		ghrObj, err := getGslbHostRule(ns, name)
		if err != nil {
			gslbutils.Errf("key: %s, msg: error in getting GSLBHostRule object: %v", key, err)
			return nil
		}

		switch op {
		case gslbutils.ObjectAdd:
			addCache := getRetryAddCache()
			addObj, err := addCache.readAndDeleteKeyFor(ns, name)
			if err != nil {
				gslbutils.Errf("key: %s, ns: %s, name: %s, msg: object not present in retry add cache",
					key, ns, name)
				return nil
			}
			if ghrObj.ResourceVersion > addObj.ResourceVersion {
				// a new resource version is available, no point in retrying for the old object
				gslbutils.Logf("key: %s, ns: %s, name %s, msg: an object with new resource version available, won't retry",
					key, ns, name)
				return nil
			}
			gslbutils.Logf("key: %s, ns: %s, name: %s, msg: will retry adding object", key, ns, name)
			AddGSLBHostRuleObj(addObj, k8sQueue.Workqueue, k8sQueue.NumWorkers)
			return nil

		case gslbutils.ObjectUpdate:
			updateCache := getRetryUpdateCache()
			updateObjs, err := updateCache.readAndDeleteKeyFor(ns, name)
			if err != nil {
				gslbutils.Errf("key: %s, ns: %s, name: %s, msg: objects not present in the update cache",
					key, ns, name)
				return nil
			}
			UpdateGSLBHostRuleObj(updateObjs.old, updateObjs.new, k8sQueue.Workqueue, k8sQueue.NumWorkers)
			return nil
		}

	default:
		gslbutils.Errf("key: %s, msg: unsupported object in ingestion retry worker", key)
	}
	return nil
}
