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
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"

	"amko/gslb/gslbutils"

	gdpv1alpha1 "amko/pkg/apis/amko/v1alpha1"

	"github.com/avinetworks/container-lib/utils"
	"github.com/avinetworks/sdk/go/clients"
	"github.com/avinetworks/sdk/go/models"
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

func (c *AviCache) AviCacheGetAllKeys() []TenantName {
	var gses []TenantName

	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()

	for k := range c.Cache {
		gses = append(gses, k.(TenantName))
	}
	return gses
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

func (c *AviCache) AviObjGSCachePopulate(client *clients.AviClient, gsname ...string) {
	var nextPageURI string
	uri := "/api/gslbservice?page_size=100"

	// Parse all the pages with GSLB services till we hit the last page
	for {
		if len(gsname) == 1 {
			uri = "/api/gslbservice?name=" + gsname[0]
		} else if nextPageURI != "" {
			uri = nextPageURI
		}
		result, err := AviGetCollectionRaw(client, uri)
		if err != nil {
			gslbutils.Warnf("object: AviCache, msg: GS get URI %s returned error: %s", uri, err)
			return
		}

		gslbutils.Logf("fetched %d GSLB services", result.Count)
		elems := make([]json.RawMessage, result.Count)
		err = json.Unmarshal(result.Results, &elems)
		if err != nil {
			gslbutils.Warnf("failed to unmarshal gslb service data, err: %s", err.Error())
			return
		}

		processedObjs := 0
		for i := 0; i < len(elems); i++ {
			gs := models.GslbService{}
			err = json.Unmarshal(elems[i], &gs)
			if err != nil {
				gslbutils.Warnf("failed to unmarshal gs element, err: %s", err.Error())
				continue
			}

			if gs.Name == nil || gs.UUID == nil {
				gslbutils.Warnf("incomplete gs data unmarshalled %s", utils.Stringify(gs))
				continue
			}

			parseGSObject(c, gs, gsname)
			processedObjs++
		}
		gslbutils.Logf("processed %d GSLB services", processedObjs)

		nextPageURI = ""
		if result.Next != "" {
			nextURI := strings.Split(result.Next, "/api/gslbservice")
			if len(nextURI) > 1 {
				nextPageURI = "/api/gslbservice" + nextURI[1]
				gslbutils.Logf("object: AviCache, msg: next field in response, will continue fetching")
				continue
			}
			gslbutils.Warnf("error in getting the nextURI, can't proceed further, next URI %s", result.Next)
			break
		}
		break
	}
}

func AviGetCollectionRaw(client *clients.AviClient, uri string) (session.AviCollectionResult, error) {
	result, err := client.AviSession.GetCollectionRaw(uri)
	if err != nil {
		return session.AviCollectionResult{}, err
	}
	return result, nil
}

func parseGSObject(c *AviCache, gsObj models.GslbService, gsname []string) {
	var name, uuid string
	if gsObj.Name == nil || gsObj.UUID == nil {
		gslbutils.Warnf("name: %v, uuid: %v, name/uuid field not set for GSLBService, ignoring", gsObj.Name, gsObj.UUID)
		return
	}
	name = *gsObj.Name
	uuid = *gsObj.UUID

	createdBy := gsObj.CreatedBy
	if createdBy == nil {
		gslbutils.Warnf("createdBy: %v, msg: created_by not present for GSLBService, ignoring", createdBy)
		return
	}
	if *createdBy == "" {
		gslbutils.Warnf("createdBy: %v, msg: created_by empty for GSLBService, ignoring", createdBy)
		// if we want to get avi gs object for a spefic gs name,
		// then don't skip even if created_by field is not present.
		// This is used while retrying after a failure
		if len(gsname) == 0 {
			return
		}
	}
	if *createdBy != gslbutils.AmkoUser {
		gslbutils.Warnf("createdBy: %v, msg: GS not created by amko, ignoring", createdBy, gslbutils.AmkoUser)
		// if we want to get avi gs object for a spefic gs name,
		// then don't skip even if created_by field is wrong.
		// This is used while retrying after a failure
		if len(gsname) == 0 {
			return
		}
	}

	cksum, gsMembers, memberObjs, err := GetDetailsFromAviGSLBFormatted(gsObj)
	if err != nil {
		gslbutils.Errf("resp: %v, msg: error occurred while parsing the response: %s", gsObj, err)
		// if we want to get avi gs object for a spefic gs name,
		// then don't skip even if not all expected fields are present.
		// This is used while retrying after a failure
		if len(gsname) == 0 {
			return
		}
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
	gslbutils.Debugf(spew.Sprintf("cacheKey: %v, value: %v, msg: added GS to the cache", k,
		utils.Stringify(gsCacheObj)))

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
				return []string{}, errors.New("description field has malformed ingress: " + description)
			}
		case gdpv1alpha1.LBSvcObj:
			if len(seg) != 4 {
				return []string{}, errors.New("description field has malformed LB service: " + description)
			}
		case gdpv1alpha1.RouteObj:
			if len(seg) != 4 {
				return []string{}, errors.New("description field has malformed route: " + description)
			}
		default:
			return []string{}, errors.New("description has unrecognised objects: " + description)
		}
	}
	return objList, nil
}

func GetDetailsFromAviGSLBFormatted(gsObj models.GslbService) (uint32, []GSMember, []string, error) {
	var ipList []string
	var domainList []string
	var gsMembers []GSMember
	var memberObjs []string

	domainNames := gsObj.DomainNames
	if len(domainNames) == 0 {
		return 0, nil, memberObjs, errors.New("domain names absent in gslb service")
	}
	// make a copy of the domain names list
	for _, domain := range domainNames {
		domainList = append(domainList, domain)
	}

	groups := gsObj.Groups
	if len(groups) == 0 {
		return 0, nil, memberObjs, errors.New("groups absent in gslb service")
	}

	description := *gsObj.Description
	if description == "" {
		return 0, nil, memberObjs, errors.New("description absent in gslb service")
	}

	for _, val := range groups {
		group := *val
		members := group.Members
		if len(members) == 0 {
			gslbutils.Warnf("no members in gslb pool: %v", group)
			continue
		}
		for _, memberVal := range members {
			member := *memberVal
			ipAddr := *member.IP.Addr
			if ipAddr == "" {
				gslbutils.Warnf("couldn't get member addr: %v", member)
				continue
			}
			weight := *member.Ratio
			if weight < 0 {
				gslbutils.Warnf("invalid weight present, assigning 0: %v", member)
				weight = 0
			}
			ipList = append(ipList, ipAddr+"-"+strconv.Itoa(int(weight)))
			gsMember := GSMember{
				IPAddr: ipAddr,
				Weight: weight,
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
	SetTenantAndVersion(client, version)

	// Populate the GS cache
	c.AviObjGSCachePopulate(client)
}

func SetTenantAndVersion(client *clients.AviClient, version string) {
	SetTenant := session.SetTenant("*")
	SetTenant(client.AviSession)
	SetVersion := session.SetVersion(version)
	SetVersion(client.AviSession)
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

func VerifyVersion() error {
	gslbutils.Logf("verifying the controller version")
	version := gslbutils.GetAviConfig().Version

	aviRestClientPool := SharedAviClients()
	if len(aviRestClientPool.AviClient) < 1 {
		gslbutils.Errf("no avi clients initialized, returning")
		return errors.New("no avi clients initialized")
	}

	aviClient := aviRestClientPool.AviClient[0]

	if version == "" {
		gslbutils.Warnf("no controller version provided by user")
		ver, err := aviClient.AviSession.GetControllerVersion()
		if err != nil {
			gslbutils.Warnf("unable to fetch the version of the controller, error: %s", err.Error())
			return err
		}
		gslbutils.Warnf("taking default version of the controller as: %s", ver)
		version = ver
	}

	SetTenantAndVersion(aviClient, version)

	uri := "/api/cloud"

	// we don't actually need the cloud object, rather we want to see if the version is fine or not
	_, err := AviGetCollectionRaw(aviClient, uri)
	if err != nil {
		gslbutils.Errf("error: get URI %s returned error: %s", uri, err)
		return err
	}

	return nil
}
