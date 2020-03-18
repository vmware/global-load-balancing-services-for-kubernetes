package cache

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/avinetworks/sdk/go/clients"
	"github.com/avinetworks/sdk/go/session"
	"github.com/davecgh/go-spew/spew"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
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
	Routes           []string
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
		cksum, gsMembers, memberRoutes, err := GetDetailsFromAviGSLB(gs)
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
			Routes:           memberRoutes,
			CloudConfigCksum: cksum,
		}
		c.AviCacheAdd(k, &gsCacheObj)
		gslbutils.Logf(spew.Sprintf("cacheKey: %v, value: %v, msg: added GS to the cache", k,
			utils.Stringify(gsCacheObj)))
	}
}

func parseDescription(description string) ([]string, error) {
	// description field should be like:
	// cluster-x/namespace-x/route-x,cluster-y/namespace-y/route-y
	routeList := strings.Split(description, ",")
	if len(routeList) == 0 {
		return []string{}, errors.New("description field has no routes")
	}
	for _, route := range routeList {
		routeSeg := strings.Split(route, "/")
		if len(routeSeg) != 3 {
			return []string{}, errors.New("description field has malformed route")
		}
	}
	return routeList, nil
}

func GetDetailsFromAviGSLB(gslbSvcMap map[string]interface{}) (uint32, []GSMember, []string, error) {
	var ipList []string
	var domainList []string
	var gsMembers []GSMember
	var routeMembers []string

	domainNames, ok := gslbSvcMap["domain_names"].([]interface{})
	if !ok {
		return 0, nil, routeMembers, errors.New("domain names absent in gslb service")
	}
	for _, domain := range domainNames {
		domainList = append(domainList, domain.(string))
	}
	groups, ok := gslbSvcMap["groups"].([]interface{})
	if !ok {
		return 0, nil, routeMembers, errors.New("groups absent in gslb service")
	}

	description, ok := gslbSvcMap["description"].(string)
	if !ok {
		return 0, nil, routeMembers, errors.New("description absent in gslb service")
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
	routeMembers, err := parseDescription(description)
	if err != nil {
		gslbutils.Errf("object: GSLBService, msg: error while parsing description field: %s", err)
	}
	// calculate the checksum
	checksum := gslbutils.GetGSLBServiceChecksum(ipList, domainList, routeMembers)
	return checksum, gsMembers, routeMembers, nil
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
