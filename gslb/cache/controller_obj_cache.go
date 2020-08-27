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
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/avinetworks/amko/gslb/gslbutils"

	gdpv1alpha1 "github.com/avinetworks/amko/internal/apis/amko/v1alpha1"

	"github.com/avinetworks/sdk/go/clients"
	"github.com/avinetworks/sdk/go/models"
	"github.com/avinetworks/sdk/go/session"
	"github.com/davecgh/go-spew/spew"
	apimodels "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/api/models"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

var (
	aviCache       *AviCache
	objCacheOnce   sync.Once
	aviHmCache     *AviHmCache
	hmObjCacheOnce sync.Once
)

type AviHmObj struct {
	Tenant           string
	Name             string
	Port             int32
	UUID             string
	Type             string
	CloudConfigCksum uint32
}

type AviHmCache struct {
	cacheLock sync.RWMutex
	Cache     map[interface{}]interface{}
	UUIDCache map[string]interface{}
}

func GetAviHmCache() *AviHmCache {
	hmObjCacheOnce.Do(func() {
		aviHmCache = &AviHmCache{}
		aviHmCache.Cache = make(map[interface{}]interface{})
		aviHmCache.UUIDCache = make(map[string]interface{})
	})
	return aviHmCache
}

func (h *AviHmCache) AviHmCacheAdd(k interface{}, val *AviHmObj) {
	h.cacheLock.Lock()
	defer h.cacheLock.Unlock()
	h.Cache[k] = val
	h.UUIDCache[val.UUID] = val
}

func (h *AviHmCache) AviHmCacheGet(k interface{}) (interface{}, bool) {
	h.cacheLock.RLock()
	defer h.cacheLock.RUnlock()
	val, ok := h.Cache[k]
	return val, ok
}

func (h *AviHmCache) AviHmCacheGetByUUID(k string) (interface{}, bool) {
	h.cacheLock.RLock()
	defer h.cacheLock.RUnlock()
	val, ok := h.UUIDCache[k]
	return val, ok
}

func (h *AviHmCache) AviHmCacheDelete(k interface{}) {
	h.cacheLock.Lock()
	defer h.cacheLock.Unlock()

	delete(h.Cache, k)
}

func (h *AviHmCache) AviHmCachePopulate(client *clients.AviClient,
	version string) {
	SetTenantAndVersion(client, version)

	// Populate the GS cache
	h.AviHmObjCachePopulate(client)
}

func (h *AviHmCache) AviHmObjCachePopulate(client *clients.AviClient) error {
	var nextPageURI string
	uri := "/api/healthmonitor?page_size=100"

	// parse all pages with Health monitors till we hit the last page
	for {
		if nextPageURI != "" {
			uri = nextPageURI
		}
		result, err := AviGetCollectionRaw(client, uri+"&is_federated=true")
		if err != nil {
			return errors.New("object: AviCache, msg: HealthMonitor get URI " + uri + " returned error: " + err.Error())
		}

		gslbutils.Logf("fetched %d Health Monitors", result.Count)
		elems := make([]json.RawMessage, result.Count)
		err = json.Unmarshal(result.Results, &elems)
		if err != nil {
			return errors.New("failed to unmarshal health monitor data, err: " + err.Error())
		}

		processedObjs := 0
		for i := 0; i < len(elems); i++ {
			hm := models.HealthMonitor{}
			err := json.Unmarshal(elems[i], &hm)
			if err != nil {
				gslbutils.Warnf("failed to unmarshal health monitor element, err: %s", err.Error())
				continue
			}

			if hm.MonitorPort == nil {
				// not a GSLB health monitor or isn't a TCP/UDP/HTTP/HTTPS health monitor
				continue
			}

			if hm.Name == nil || hm.UUID == nil {
				gslbutils.Warnf("incomplete health monitor data unmarshalled %s", utils.Stringify(hm))
				continue
			}

			k := TenantName{Tenant: utils.ADMIN_NS, Name: *hm.Name}
			cksum := gslbutils.GetGSLBHmChecksum(*hm.Name, *hm.Type, *hm.MonitorPort)
			hmCacheObj := AviHmObj{
				Name:             *hm.Name,
				Tenant:           utils.ADMIN_NS,
				UUID:             *hm.UUID,
				Port:             *hm.MonitorPort,
				CloudConfigCksum: cksum,
			}
			h.AviHmCacheAdd(k, &hmCacheObj)
			processedObjs++
		}
		gslbutils.Logf("processed %d Health monitor objects", processedObjs)

		nextPageURI = ""
		if result.Next != "" {
			nextURI := strings.Split(result.Next, "/api/healthmonitor")
			if len(nextURI) > 1 {
				nextPageURI = "/api/healthmonitor" + nextURI[1]
				gslbutils.Logf("object: AviCache, msg: next field in response, will continue fetching")
				continue
			}
			gslbutils.Warnf("error in getting the nextURI, can't proceed further, next URI %s", result.Next)
			break
		}
		break
	}
	return nil
}

type GSMember struct {
	IPAddr string
	Weight int32
}

type AviGSCache struct {
	Name              string
	Tenant            string
	Uuid              string
	Members           []GSMember
	K8sObjects        []string
	HealthMonitorName string
	CloudConfigCksum  uint32
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
		result, err := AviGetCollectionRaw(client, uri+"&created_by="+gslbutils.AmkoUser)
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

	// find the health monitor for this object
	cksum, gsMembers, memberObjs, hm, err := GetDetailsFromAviGSLBFormatted(gsObj)
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
		Name:              name,
		Tenant:            utils.ADMIN_NS,
		Uuid:              uuid,
		Members:           gsMembers,
		K8sObjects:        memberObjs,
		HealthMonitorName: hm,
		CloudConfigCksum:  cksum,
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

func GetDetailsFromAviGSLBFormatted(gsObj models.GslbService) (uint32, []GSMember, []string, string, error) {
	var ipList []string
	var domainList []string
	var gsMembers []GSMember
	var memberObjs []string
	var hm string

	domainNames := gsObj.DomainNames
	if len(domainNames) == 0 {
		return 0, nil, memberObjs, hm, errors.New("domain names absent in gslb service")
	}
	// make a copy of the domain names list
	for _, domain := range domainNames {
		domainList = append(domainList, domain)
	}

	groups := gsObj.Groups
	if len(groups) == 0 {
		return 0, nil, memberObjs, hm, errors.New("groups absent in gslb service")
	}

	description := *gsObj.Description
	if description == "" {
		return 0, nil, memberObjs, hm, errors.New("description absent in gslb service")
	}

	hmRefs := gsObj.HealthMonitorRefs
	if len(hmRefs) == 0 {
		return 0, nil, memberObjs, hm, errors.New("health monitor refs absent in gslb service")
	}
	hmRefSplit := strings.Split(hmRefs[0], "/api/healthmonitor/")
	if len(hmRefSplit) != 2 {
		return 0, nil, memberObjs, hm, errors.New("health monitor name is absent in health monitor ref: " + hmRefs[0])
	}
	hmUUID := hmRefSplit[1]
	hmCache := GetAviHmCache()
	hmObjIntf, found := hmCache.AviHmCacheGetByUUID(hmUUID)
	if !found {
		return 0, nil, memberObjs, hm, errors.New("health monitor object is absent in the controller for GS")
	}
	hmObj, ok := hmObjIntf.(*AviHmObj)
	if !ok {
		return 0, nil, memberObjs, hm, errors.New("health monitor cache object can't be parsed")
	}
	hm = hmObj.Name

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
	checksum := gslbutils.GetGSLBServiceChecksum(ipList, domainList, memberObjs, hm)
	return checksum, gsMembers, memberObjs, hm, nil
}

func GetDetailsFromAviGSLB(gslbSvcMap map[string]interface{}) (uint32, []GSMember, []string, string, error) {
	var ipList []string
	var domainList []string
	var gsMembers []GSMember
	var memberObjs []string
	var hm string

	domainNames, ok := gslbSvcMap["domain_names"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, hm, errors.New("domain names absent in gslb service")
	}
	for _, domain := range domainNames {
		domainList = append(domainList, domain.(string))
	}
	groups, ok := gslbSvcMap["groups"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, hm, errors.New("groups absent in gslb service")
	}

	description, ok := gslbSvcMap["description"].(string)
	if !ok {
		return 0, nil, memberObjs, hm, errors.New("description absent in gslb service")
	}

	hmRefs, ok := gslbSvcMap["health_monitor_refs"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, hm, errors.New("health_monitor_refs absent in gslb service")
	}
	hmRef := hmRefs[0].(string)
	hmRefSplit := strings.Split(hmRef, "#")
	if len(hmRefSplit) != 2 {
		errStr := fmt.Sprintf("health monitor name is absent in health monitor ref: %v", hmRefSplit[0])
		return 0, nil, memberObjs, hm, errors.New(errStr)
	}
	hm = hmRefSplit[1]

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
	checksum := gslbutils.GetGSLBServiceChecksum(ipList, domainList, memberObjs, hm)
	return checksum, gsMembers, memberObjs, hm, nil
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

func PopulateGSCache(createSharedCache bool) *AviCache {
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

func PopulateHMCache(createSharedCache bool) *AviHmCache {
	aviRestClientPool := SharedAviClients()
	var aviHmCache *AviHmCache
	if createSharedCache {
		aviHmCache = GetAviHmCache()
	} else {
		aviHmCache = &AviHmCache{}
		aviHmCache.Cache = make(map[interface{}]interface{})
		aviHmCache.UUIDCache = make(map[string]interface{})
	}
	if len(aviRestClientPool.AviClient) > 0 {
		aviHmCache.AviHmCachePopulate(aviRestClientPool.AviClient[0],
			gslbutils.GetAviConfig().Version)
	}
	return aviHmCache
}

func VerifyVersion() error {
	gslbutils.Logf("verifying the controller version")
	version := gslbutils.GetAviConfig().Version

	aviRestClientPool := SharedAviClients()
	if len(aviRestClientPool.AviClient) < 1 {
		gslbutils.Errf("no avi clients initialized, returning")
		gslbutils.GetAmkoAPIServer().ShutDown()
		return errors.New("no avi clients initialized")
	}

	apimodels.RestStatus.UpdateAviApiRestStatus(utils.AVIAPI_CONNECTED, nil)
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
