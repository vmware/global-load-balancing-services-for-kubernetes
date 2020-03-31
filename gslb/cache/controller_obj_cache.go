/*
* [2013] - [2020] Avi Networks Incorporated
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

package cache

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"amko/gslb/gslbutils"

	gdpv1alpha1 "amko/pkg/apis/avilb/v1alpha1"

	"github.com/avinetworks/container-lib/utils"
	"github.com/avinetworks/sdk/go/clients"
	"github.com/avinetworks/sdk/go/session"
	"github.com/davecgh/go-spew/spew"
)

var (
	aviCache     *AviCache
	objCacheOnce sync.Once
)

type GSMember struct {
	IPAddr string
	Weight int32
}

type AviGSCache struct {
	Name             string
	Tenant           string
	Uuid             string
	Members          []GSMember
	K8sObjects       []string
	CloudConfigCksum uint32
}

type AviCache struct {
	cacheLock sync.RWMutex
	Cache     map[interface{}]interface{}
}

func GetAviCache() *AviCache {
	objCacheOnce.Do(func() {
		aviCache = &AviCache{}
		aviCache.Cache = make(map[interface{}]interface{})
	})
	return aviCache
}

func (c *AviCache) AviCacheGet(k interface{}) (interface{}, bool) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	val, ok := c.Cache[k]
	return val, ok
}

func (c *AviCache) AviCacheGetByUuid(uuid string) (interface{}, bool) {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()
	for key, value := range c.Cache {
		switch value.(type) {
		case *AviGSCache:
			if value.(*AviGSCache).Uuid == uuid {
				return key, true
			}
		}
	}
	return nil, false
}

func (c *AviCache) AviCacheAdd(k interface{}, val interface{}) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	c.Cache[k] = val
}

func (c *AviCache) AviCacheDelete(k interface{}) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	delete(c.Cache, k)
}

func (c *AviCache) AviObjGSCachePopulate(client *clients.AviClient) {
	var restResponse interface{}
	uri := "/api/gslbservice"
	err := client.AviSession.Get(uri, &restResponse)
	if err != nil {
		gslbutils.Logf("object: AviCache, msg: GS get URI %s returned error: %s", uri, err)
		return
	}
	resp, ok := restResponse.(map[string]interface{})
	if !ok {
		gslbutils.Logf("object: AviCache, msg: GS get URI %s returned %v type %T",
			uri, restResponse, restResponse)
		return
	}
	gslbutils.Logf("object: AviCache, msg: GS get URI %s returned %v GSes", uri, resp["count"])
	results, ok := resp["results"].([]interface{})
	if !ok {
		gslbutils.Logf("object: AviCache, msg: results not of type []interface{} instead of type %T",
			resp["results"])
		return
	}
	for _, gsIntf := range results {
		gs := gsIntf.(map[string]interface{})
		name, ok := gs["name"].(string)
		if !ok {
			gslbutils.Warnf("resp: %v, msg: name not present in response", gsIntf)
			continue
		}
		uuid, ok := gs["uuid"].(string)
		if !ok {
			gslbutils.Warnf("resp: %v, msg: uuid not present in response", gsIntf)
			continue
		}
		createdBy, ok := gs["created_by"].(string)
		if !ok {
			gslbutils.Warnf("resp: %v, msg: created_by not present in response", gsIntf)
			continue
		}
		if createdBy != "mcc-gslb" {
			gslbutils.Warnf("resp: %v, msg: created_by contains %s instead of mcc-gslb", gsIntf, createdBy)
			continue
		}
		cksum, gsMembers, memberObjs, err := GetDetailsFromAviGSLB(gs)
		if err != nil {
			gslbutils.Errf("resp: %v, msg: error occurred while parsing the response: %s", err)
			continue
		}
		k := TenantName{Tenant: utils.ADMIN_NS, Name: name}
		gsCacheObj := AviGSCache{
			Name:             name,
			Tenant:           utils.ADMIN_NS,
			Uuid:             uuid,
			Members:          gsMembers,
			K8sObjects:       memberObjs,
			CloudConfigCksum: cksum,
		}
		c.AviCacheAdd(k, &gsCacheObj)
		gslbutils.Logf(spew.Sprintf("cacheKey: %v, value: %v, msg: added GS to the cache", k,
			utils.Stringify(gsCacheObj)))
	}
}

func parseDescription(description string) ([]string, error) {
	// description field should be like:
	// LBSvc/cluster-x/namespace-x/svc-x,Ingress/cluster-y/namespace-y/ingress-y/hostname,...
	objList := strings.Split(description, ",")
	if len(objList) == 0 {
		return []string{}, errors.New("description field has no k8s/openshift objects")
	}
	for _, obj := range objList {
		seg := strings.Split(obj, "/")
		switch seg[0] {
		case gdpv1alpha1.IngressObj:
			if len(seg) != 5 {
				return []string{}, errors.New("description field has malformed ingress")
			}
		case gdpv1alpha1.LBSvcObj:
			if len(seg) != 4 {
				return []string{}, errors.New("description field has malformed LB service")
			}
		case gdpv1alpha1.RouteObj:
			if len(seg) != 4 {
				return []string{}, errors.New("description field has malformed route")
			}
		default:
			return []string{}, errors.New("description has unrecognised objects")
		}
	}
	return objList, nil
}

func GetDetailsFromAviGSLB(gslbSvcMap map[string]interface{}) (uint32, []GSMember, []string, error) {
	var ipList []string
	var domainList []string
	var gsMembers []GSMember
	var memberObjs []string

	domainNames, ok := gslbSvcMap["domain_names"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, errors.New("domain names absent in gslb service")
	}
	for _, domain := range domainNames {
		domainList = append(domainList, domain.(string))
	}
	groups, ok := gslbSvcMap["groups"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, errors.New("groups absent in gslb service")
	}

	description, ok := gslbSvcMap["description"].(string)
	if !ok {
		return 0, nil, memberObjs, errors.New("description absent in gslb service")
	}

	for _, val := range groups {
		group, ok := val.(map[string]interface{})
		if !ok {
			gslbutils.Warnf("couldn't parse group: %v", val)
			continue
		}
		members, ok := group["members"].([]interface{})
		if !ok {
			gslbutils.Warnf("couldn't parse group members: %v", group)
			continue
		}
		for _, memberVal := range members {
			member, ok := memberVal.(map[string]interface{})
			if !ok {
				gslbutils.Warnf("couldn't parse member: %v", memberVal)
				continue
			}
			ip, ok := member["ip"].(map[string]interface{})
			if !ok {
				gslbutils.Warnf("couldn't parse IP: %v", member)
				continue
			}
			ipAddr, ok := ip["addr"].(string)
			if !ok {
				gslbutils.Warnf("couldn't parse addr: %v", member)
				continue
			}
			weight, ok := member["ratio"].(float64)
			if !ok {
				gslbutils.Warnf("couldn't parse the weight, assigning 0: %v", member)
				weight = 0
			}
			weightI := int32(weight)
			ipList = append(ipList, ipAddr+"-"+strconv.Itoa(int(weightI)))
			gsMember := GSMember{
				IPAddr: ipAddr,
				Weight: weightI,
			}
			gsMembers = append(gsMembers, gsMember)
		}
	}
	memberObjs, err := parseDescription(description)
	if err != nil {
		gslbutils.Errf("object: GSLBService, msg: error while parsing description field: %s", err)
	}
	// calculate the checksum
	checksum := gslbutils.GetGSLBServiceChecksum(ipList, domainList, memberObjs)
	return checksum, gsMembers, memberObjs, nil
}

func (c *AviCache) AviObjCachePopulate(client *clients.AviClient,
	version string) {
	SetTenant := session.SetTenant("*")
	SetTenant(client.AviSession)
	SetVersion := session.SetVersion(version)
	SetVersion(client.AviSession)

	// Populate the VS cache
	c.AviObjGSCachePopulate(client)
}

type TenantName struct {
	Tenant string
	Name   string
}

func PopulateCache(createSharedCache bool) *AviCache {
	aviRestClientPool := SharedAviClients()
	var aviObjCache *AviCache
	if createSharedCache {
		aviObjCache = GetAviCache()
	} else {
		aviObjCache = &AviCache{}
		aviObjCache.Cache = make(map[interface{}]interface{})
	}

	// Randomly pickup a client
	if len(aviRestClientPool.AviClient) > 0 {
		aviObjCache.AviObjCachePopulate(aviRestClientPool.AviClient[0],
			gslbutils.GetAviConfig().Version)
	}
	return aviObjCache
}
