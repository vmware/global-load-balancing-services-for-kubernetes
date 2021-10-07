/*
 * Copyright 2019-2020 VMware, Inc.
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

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"

	gdpv1alpha2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"

	"github.com/davecgh/go-spew/spew"
	"github.com/vmware/alb-sdk/go/clients"
	"github.com/vmware/alb-sdk/go/models"
	"github.com/vmware/alb-sdk/go/session"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/apiserver"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	apimodels "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/api/models"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

var (
	aviCache       *AviCache
	objCacheOnce   sync.Once
	aviHmCache     *AviHmCache
	hmObjCacheOnce sync.Once
	aviSpCache     *AviSpCache
	spObjCacheOnce sync.Once
)

type AviHmObj struct {
	Tenant           string
	Name             string
	Port             int32
	UUID             string
	Type             string
	CloudConfigCksum uint32
	Description      string
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

func (h *AviHmCache) AviHmGetAllKeys() []interface{} {
	var hmKeys []interface{}

	h.cacheLock.RLock()
	defer h.cacheLock.RUnlock()

	for k := range h.Cache {
		hmKeys = append(hmKeys, k)
	}

	return hmKeys
}

func (h *AviHmCache) AviHmCacheGetHmsForGS(tenant, gsName string) []interface{} {
	var hmObjs []interface{}
	hmObjs = make([]interface{}, 0)
	h.cacheLock.RLock()
	defer h.cacheLock.RUnlock()
	for k, v := range h.Cache {
		hmKey, ok := k.(TenantName)
		if !ok {
			gslbutils.Errf("tenant: %s, gsName: %s, error in parsing the hmkey", tenant, gsName)
			continue
		}
		if hmKey.Tenant != tenant {
			continue
		}
		hmObj := v.(*AviHmObj)
		if strings.Contains(hmObj.Description, gsName) {
			hmObjs = append(hmObjs, v)
		} else if strings.Contains(hmKey.Name, gsName) {
			// if hmname follows the old non encoded naming convention, hmname will contain the gsname
			gslbutils.Warnf("tenant: %s, gsName: %s, hmName: %s, gsname not present in hm description, will check in hmname",
				tenant, gsName, hmKey.Name)
			hmObjs = append(hmObjs, v)
		}
	}
	return hmObjs
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

func (h *AviHmCache) AviHmObjCachePopulate(client *clients.AviClient, hmname ...string) error {
	var nextPageURI string
	uri := "/api/healthmonitor?page_size=100"

	// parse all pages with Health monitors till we hit the last page
	for {
		if len(hmname) == 1 {
			uri = "/api/healthmonitor?name=" + hmname[0]
		} else if nextPageURI != "" {
			uri = nextPageURI
		}
		result, err := gslbutils.GetUriFromAvi(uri+"&is_federated=true", client, false)
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

			if hm.Name == nil || hm.UUID == nil {
				gslbutils.Warnf("incomplete health monitor data unmarshalled %s", utils.Stringify(hm))
				continue
			}

			k := TenantName{Tenant: utils.ADMIN_NS, Name: *hm.Name}
			var monitorPort int32
			if hm.MonitorPort != nil {
				monitorPort = *hm.MonitorPort
			}
			description := ""
			if hm.Description != nil {
				description = *hm.Description
			}
			cksum := gslbutils.GetGSLBHmChecksum(*hm.Type, monitorPort, []string{description})
			hmCacheObj := AviHmObj{
				Name:             *hm.Name,
				Tenant:           utils.ADMIN_NS,
				UUID:             *hm.UUID,
				Port:             monitorPort,
				CloudConfigCksum: cksum,
				Description:      description,
			}
			h.AviHmCacheAdd(k, &hmCacheObj)
			gslbutils.Debugf("processed health monitor %s", *hm.Name)
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

type AviSpCache struct {
	cacheLock sync.RWMutex
	Cache     map[interface{}]interface{}
	UUIDCache map[string]interface{}
}

func GetAviSpCache() *AviSpCache {
	spObjCacheOnce.Do(func() {
		aviSpCache = &AviSpCache{}
		aviSpCache.Cache = make(map[interface{}]interface{})
		aviSpCache.UUIDCache = make(map[string]interface{})
	})
	return aviSpCache
}

func (s *AviSpCache) AviSpCacheAdd(k interface{}, val interface{}) {
	s.cacheLock.Lock()
	defer s.cacheLock.Unlock()
	s.Cache[k] = val
}

func (s *AviSpCache) AviSpCacheAddByUUID(uuid string, val interface{}) {
	s.cacheLock.Lock()
	defer s.cacheLock.Unlock()
	s.UUIDCache[uuid] = val
}

func (s *AviSpCache) AviSpCacheGet(k interface{}) (interface{}, bool) {
	s.cacheLock.RLock()
	defer s.cacheLock.RUnlock()
	val, ok := s.Cache[k]
	return val, ok
}

func (s *AviSpCache) AviSpCacheGetByUUID(uuid string) (interface{}, bool) {
	s.cacheLock.RLock()
	defer s.cacheLock.RUnlock()
	val, ok := s.UUIDCache[uuid]
	return val, ok
}

func (s *AviSpCache) AviSitePersistenceCachePopulate(client *clients.AviClient) error {
	var nextPageURI string
	baseURI := "/api/applicationpersistenceprofile"
	uri := baseURI + "?page_size=100"

	// parse all pages with Health monitors till we hit the last page
	for {
		if nextPageURI != "" {
			uri = nextPageURI
		}
		result, err := gslbutils.GetUriFromAvi(uri+"&is_federated=true", client, false)
		if err != nil {
			return fmt.Errorf("object: AviSitePersistenceCache, msg: SitePersistence get URI %s returned error: %v",
				uri, err)
		}

		gslbutils.Logf("fetched %d Site Persistence profiles", result.Count)
		elems := make([]json.RawMessage, result.Count)
		err = json.Unmarshal(result.Results, &elems)
		if err != nil {
			return errors.New("failed to unmarshal site persistence profile ref, err: " + err.Error())
		}

		processedObjs := 0
		for i := 0; i < len(elems); i++ {
			sp := models.ApplicationPersistenceProfile{}
			err := json.Unmarshal(elems[i], &sp)
			if err != nil {
				gslbutils.Warnf("failed to unmarshal site persistence element, err: %s", err.Error())
				continue
			}

			if sp.Name == nil || sp.UUID == nil {
				gslbutils.Warnf("incomplete site persistence ref unmarshalled %s", utils.Stringify(sp))
				continue
			}

			k := TenantName{Tenant: utils.ADMIN_NS, Name: *sp.Name}
			s.AviSpCacheAdd(k, &sp)
			s.AviSpCacheAddByUUID(*sp.UUID, &sp)
			gslbutils.Debugf("processed site persistence %s, UUID: %s", *sp.Name, *sp.UUID)
			processedObjs++
		}
		gslbutils.Logf("processed %d Site Persistence profiles", processedObjs)

		nextPageURI = ""
		if result.Next != "" {
			nextURI := strings.Split(result.Next, baseURI)
			if len(nextURI) > 1 {
				nextPageURI = baseURI + nextURI[1]
				gslbutils.Logf("object: AviCache, msg: next field in response, will continue fetching")
				continue
			}
			gslbutils.Warnf("error in getting the nextURI, can't proceed further, next URI %s", result.Next)
		}
		break
	}
	return nil
}

type GSMember struct {
	IPAddr     string
	Weight     int32
	Priority   int32
	VsUUID     string
	Controller string
}

type AviGSCache struct {
	Name             string
	Tenant           string
	Uuid             string
	Members          []GSMember
	K8sObjects       []string
	HealthMonitor    []string
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
		result, err := gslbutils.GetUriFromAvi(uri+"&created_by="+gslbutils.AmkoUser, client, false)
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

func parseGSObject(c *AviCache, gsObj models.GslbService, gsname []string) {
	var name, uuid string
	if gsObj.Name == nil || gsObj.UUID == nil {
		gslbutils.Warnf("name: %v, uuid: %v, name/uuid field not set for GSLBService, ignoring", gsObj.Name, gsObj.UUID)
		return
	}
	name = *gsObj.Name
	uuid = *gsObj.UUID

	// find the health monitor for this object
	cksum, gsMembers, memberObjs, hms, err := GetDetailsFromAviGSLBFormatted(gsObj)
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
		HealthMonitor:    hms,
		CloudConfigCksum: cksum,
	}
	c.AviCacheAdd(k, &gsCacheObj)
	gslbutils.Debugf(spew.Sprintf("cacheKey: %v, value: %v, msg: added GS to the cache", k,
		utils.Stringify(gsCacheObj)))

}

func parseDescription(description string) ([]string, error) {
	// description field should be like:
	// LBSvc/cluster-x/namespace-x/svc-x,Ingress/cluster-y/namespace-y/ingress-y/hostname,...,ThirdPartySite
	objList := strings.Split(description, ",")
	if len(objList) == 0 {
		return []string{}, errors.New("description field has no k8s/openshift objects")
	}
	for _, obj := range objList {
		seg := strings.Split(obj, "/")
		switch seg[0] {
		case gdpv1alpha2.IngressObj:
			if len(seg) != 5 {
				return []string{}, errors.New("description field has malformed ingress: " + description)
			}
		case gdpv1alpha2.LBSvcObj:
			if len(seg) != 4 {
				return []string{}, errors.New("description field has malformed LB service: " + description)
			}
		case gdpv1alpha2.RouteObj:
			if len(seg) != 4 {
				return []string{}, errors.New("description field has malformed route: " + description)
			}
		case gslbutils.ThirdPartyMemberType:
			if len(seg) != 1 {
				return []string{}, fmt.Errorf("description field has malformed third party member: %s", description)
			}
		default:
			return []string{}, errors.New("description has unrecognised objects: " + description)
		}
	}
	return objList, nil
}

func ParsePoolAlgorithmSettingsFromPool(gsPool models.GslbPool) *gslbalphav1.PoolAlgorithmSettings {
	return ParsePoolAlgorithmSettings(gsPool.Algorithm, gsPool.FallbackAlgorithm, gsPool.ConsistentHashMask)
}

func ParsePoolAlgorithmSettings(algorithm *string, fallbackAlgorithm *string, consistentHashMask *int32) *gslbalphav1.PoolAlgorithmSettings {
	if algorithm == nil {
		return nil
	}
	pa := gslbalphav1.PoolAlgorithmSettings{LBAlgorithm: *algorithm}
	if fallbackAlgorithm != nil {
		gfa := gslbalphav1.GeoFallback{
			LBAlgorithm: *fallbackAlgorithm,
		}
		if consistentHashMask != nil {
			hashMask := int(*consistentHashMask)
			gfa.HashMask = &hashMask
		}
		pa.FallbackAlgorithm = &gfa
	} else if consistentHashMask != nil {
		hashMask := int(*consistentHashMask)
		pa.HashMask = &hashMask
	}
	return &pa
}

// Parse the algorithm, fallback algorithm and consistent hash mask from the GS Group.
func ParsePoolAlgorithmSettingsFromPoolRaw(group map[string]interface{}) *gslbalphav1.PoolAlgorithmSettings {
	var algorithm, fallbackAlgorithm *string
	var consistentHashMask *int32

	a, ok := group["algorithm"].(string)
	if !ok {
		gslbutils.Warnf("couldn't parse algorithm: %v", group)
		return nil
	}
	algorithm = &a
	f, ok := group["fallback_algorithm"].(string)
	if !ok {
		gslbutils.Debugf("couldn't parse fallback_algorithm: %v", group)
	} else {
		fallbackAlgorithm = &f
	}

	hashMaskF, ok := group["consistent_hash_mask"].(float64)
	if ok {
		hashMaskI := int32(hashMaskF)
		consistentHashMask = &hashMaskI
	} else {
		gslbutils.Logf("couldn't parse hash mask: %v", group["consistent_hash_mask"])
	}

	return ParsePoolAlgorithmSettings(algorithm, fallbackAlgorithm, consistentHashMask)
}

func GetDetailsFromAviGSLBFormatted(gsObj models.GslbService) (uint32, []GSMember, []string, []string, error) {
	var serverList, domainList, memberObjs []string
	var hms []string
	var gsMembers []GSMember
	var persistenceProfileRef string
	var persistenceProfileRefPtr *string
	var sitePersistenceRequired bool
	var ttl *int

	domainNames := gsObj.DomainNames
	if len(domainNames) == 0 {
		return 0, nil, memberObjs, nil, errors.New("domain names absent in gslb service")
	}
	// make a copy of the domain names list
	for _, domain := range domainNames {
		domainList = append(domainList, domain)
	}

	groups := gsObj.Groups
	if len(groups) == 0 {
		return 0, nil, memberObjs, nil, errors.New("groups absent in gslb service")
	}

	if gsObj.Description == nil || *gsObj.Description == "" {
		return 0, nil, memberObjs, nil, errors.New("description absent in gslb service")
	}

	hmRefs := gsObj.HealthMonitorRefs
	for _, hmRef := range hmRefs {
		hmRefSplit := strings.Split(hmRef, "/api/healthmonitor/")
		if len(hmRefSplit) != 2 {
			return 0, nil, memberObjs, nil, errors.New("health monitor name is absent in health monitor ref: " + hmRefs[0])
		}
		hmUUID := hmRefSplit[1]
		hmCache := GetAviHmCache()
		hmObjIntf, found := hmCache.AviHmCacheGetByUUID(hmUUID)
		if !found {
			gslbutils.Debugf("gsName: %s, msg: health monitor object is absent in the controller for GS", *gsObj.Name)
			continue
		}
		hmObj, ok := hmObjIntf.(*AviHmObj)
		if !ok {
			gslbutils.Debugf("gsName: %s, msg: health monitor cache object can't be parsed", *gsObj.Name)
			continue
		}
		hm := hmObj.Name
		hms = append(hms, hm)
	}

	sitePersistenceRequired = *gsObj.SitePersistenceEnabled
	if sitePersistenceRequired && gsObj.ApplicationPersistenceProfileRef != nil {
		// find out the name of the profile
		refSplit := strings.Split(*gsObj.ApplicationPersistenceProfileRef, "/applicationpersistenceprofile/")
		if len(refSplit) == 2 {
			spCache := GetAviSpCache()
			sp, present := spCache.AviSpCacheGetByUUID(refSplit[1])
			if present {
				spObj, ok := sp.(*models.ApplicationPersistenceProfile)
				if ok {
					persistenceProfileRef = *spObj.Name
					persistenceProfileRefPtr = &persistenceProfileRef
				} else {
					gslbutils.Warnf("gsName: %s, fetchedRef: %s, msg: stored site persistence not in right format",
						*gsObj.Name, *gsObj.ApplicationPersistenceProfileRef)
				}
			} else {
				gslbutils.Warnf("gsName: %s, fetchedRef: %s, uuid: %s, msg: site persistence not present in cache by UUID",
					*gsObj.Name, *gsObj.ApplicationPersistenceProfileRef, refSplit[1])
			}
		} else {
			gslbutils.Warnf("gsName: %s, fetchedRef: %s, msg: wrong format for site persistence ref", *gsObj.Name,
				*gsObj.ApplicationPersistenceProfileRef)
		}
	}
	if gsObj.TTL != nil {
		ttlVal := int(*gsObj.TTL)
		ttl = &ttlVal
	}

	var poolAlgorithmSettings *gslbalphav1.PoolAlgorithmSettings
	for _, val := range groups {
		group := *val
		if group.Priority == nil {
			gslbutils.Errf("no priority set for group in GslbService")
			continue
		}
		priority := *group.Priority
		members := group.Members
		if len(members) == 0 {
			gslbutils.Warnf("no members in gslb pool: %v", group)
			continue
		}
		poolAlgorithmSettings = ParsePoolAlgorithmSettingsFromPool(group)
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
			gsMember := GSMember{
				IPAddr: ipAddr,
				Weight: weight,
			}
			// Compute which server to add for this member (for checksum calculation)
			var server string
			if member.VsUUID != nil {
				gsMember.VsUUID = *member.VsUUID
				server = *member.VsUUID
			}
			if member.ClusterUUID != nil {
				gsMember.Controller = *member.ClusterUUID
				server += "-" + *member.ClusterUUID
			}
			if server == "" {
				server = ipAddr
			}
			serverList = append(serverList, server+"-"+strconv.Itoa(int(weight))+"-"+strconv.Itoa(int(priority)))
			gsMembers = append(gsMembers, gsMember)
		}
	}
	memberObjs, err := parseDescription(*gsObj.Description)
	if err != nil {
		gslbutils.Errf("object: GSLBService, msg: error while parsing description field: %s", err)
	}
	// calculate the checksum
	checksum := gslbutils.GetGSLBServiceChecksum(serverList, domainList, memberObjs, hms,
		persistenceProfileRefPtr, ttl, poolAlgorithmSettings)
	return checksum, gsMembers, memberObjs, hms, nil
}

// As name is encoded, retreiving information about the Hm becomes difficult
// Thats why we fetch Hm description for further processing
func GetHmDescriptionFromName(hmName string) string {
	aviRestClientPool := SharedAviClients()
	if len(aviRestClientPool.AviClient) < 1 {
		return ""
	}
	uri := "/api/healthmonitor?name=" + hmName
	result, err := gslbutils.GetUriFromAvi(uri, aviRestClientPool.AviClient[0], false)
	if err != nil {
		gslbutils.Errf("error getting hm data, err : %v", err)
		return ""
	}

	elems := make([]json.RawMessage, result.Count)
	err = json.Unmarshal(result.Results, &elems)
	if err != nil {
		gslbutils.Errf("error unmarshalling hm data, err : %v", err)
		return ""
	}
	hmDescription := ""
	for i := 0; i < len(elems); i++ {
		hm := models.HealthMonitor{}
		err = json.Unmarshal(elems[i], &hm)
		if err != nil {
			continue
		}
		if hm.Name == nil || hm.UUID == nil {
			continue
		}

		if hm.Description != nil {
			hmDescription = *hm.Description
			break
		}
	}
	return hmDescription
}

func GetGSNameFromHmName(hmName string) (string, error) {
	hmNameSplit := strings.Split(hmName, "--")
	if len(hmNameSplit) == 4 {
		return hmNameSplit[2], nil
	} else if len(hmNameSplit) == 2 {
		if strings.Contains(hmNameSplit[1], ".") {
			// this makes sure that the field extracted is actually a gsName
			// and to discard the encoded hmname being returned as gsName, eg: amko--66f4133eeae3c76cf1c20c8cde0c6fa3c162ab8b
			return hmNameSplit[1], nil
		}
	}
	return "", errors.New("error in parsing gs name, unexpected format")
}

// this function returns - gsname, gen, error
// gen = 1 if the HM name is encoded and the gsname is fetched from its description
// gen = 2 if the HM name follows the old non encoded naming convention and gsname is fetched from hmname
// gen = 0 for error
func GetGSFromHmName(hmName string) (string, int8, error) {
	hmDesc := GetHmDescriptionFromName(hmName)
	hmDescriptionSplit := strings.Split(hmDesc, "gsname: ")
	if len(hmDescriptionSplit) == 2 {
		gsNameField := strings.Split(hmDescriptionSplit[1], ",")
		gsName := strings.Trim(gsNameField[0], " ")
		return gsName, 1, nil
	}
	gslbutils.Warnf("hmName: %s, msg: %s",
		hmName, "hm description does not contain gsname, checking if hm name has gsname in it(according to old naming convention)")
	gsName, err := GetGSNameFromHmName(hmName)
	if err == nil {
		return gsName, 2, nil
	}
	return "", 0, fmt.Errorf("hmName: %s, hmDescription: %s, msg: hm is malformed, %v", hmName, hmDesc, err)
}

func GetDetailsFromAviGSLB(gslbSvcMap map[string]interface{}) (uint32, []GSMember, []string, []string, error) {
	var serverList, domainList, memberObjs []string
	var hms []string
	var gsMembers []GSMember
	var ttl *int

	domainNames, ok := gslbSvcMap["domain_names"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, hms, errors.New("domain names absent in gslb service")
	}
	for _, domain := range domainNames {
		domainList = append(domainList, domain.(string))
	}
	groups, ok := gslbSvcMap["groups"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, hms, errors.New("groups absent in gslb service")
	}

	description, ok := gslbSvcMap["description"].(string)
	if !ok {
		return 0, nil, memberObjs, hms, errors.New("description absent in gslb service")
	}

	hmRefs, ok := gslbSvcMap["health_monitor_refs"].([]interface{})
	if ok {
		for _, hmRefIntf := range hmRefs {
			hmRef := hmRefIntf.(string)
			hmRefSplit := strings.Split(hmRef, "#")
			if len(hmRefSplit) != 2 {
				errStr := fmt.Sprintf("health monitor name is absent in health monitor ref: %v", hmRefSplit[0])
				return 0, nil, memberObjs, hms, errors.New(errStr)
			}
			hm := hmRefSplit[1]
			hms = append(hms, hm)
		}
	} else {
		gslbutils.Debugf("gslbsvcmap: %v, health_monitor_refs absent in gslb service", gslbSvcMap)
	}

	sitePersistenceEnabled, ok := gslbSvcMap["site_persistence_enabled"].(bool)
	if !ok {
		return 0, nil, memberObjs, hms, errors.New("site_persistence_enabled absent in gslb service")
	}

	var persistenceProfileRef string
	var persistenceProfileRefPtr *string
	if sitePersistenceEnabled == true {
		var ok bool
		persistenceProfileRef, ok = gslbSvcMap["application_persistence_profile_ref"].(string)
		if !ok {
			return 0, nil, memberObjs, hms,
				errors.New("application_persistence_profile_ref absent in gslb service")
		}
		persistenceProfileRefPtr = &persistenceProfileRef
	}

	ttlVal, ok := gslbSvcMap["ttl"]
	if ok {
		parsedValF, ok := ttlVal.(float64)
		if ok {
			parsedValI := int(parsedValF)
			ttl = &parsedValI
		} else {
			gslbutils.Errf("couldn't parse the ttl value: %v", ttlVal)
		}
	}

	var poolAlgorithmSettings *gslbalphav1.PoolAlgorithmSettings

	for _, val := range groups {
		group, ok := val.(map[string]interface{})
		if !ok {
			gslbutils.Warnf("couldn't parse group: %v", val)
			continue
		}
		priorityF, ok := group["priority"].(float64)
		if !ok {
			gslbutils.Warnf("couldn't parse the priority, won't proceed")
			continue
		}
		priority := int32(priorityF)
		poolAlgorithmSettings = ParsePoolAlgorithmSettingsFromPoolRaw(group)
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
			vsUUID, ok := member["vs_uuid"].(string)
			if !ok {
				gslbutils.Warnf("couldn't parse the vs uuid, assigning \"\": %v", member)
				vsUUID = ""
			}
			controllerUUID, ok := member["cluster_uuid"].(string)
			if !ok {
				gslbutils.Warnf("couldn't parse the controller cluster uuid, assigning \"\": %v", member)
				controllerUUID = ""
			}
			var server string
			if vsUUID != "" {
				server = vsUUID + "-" + controllerUUID
			} else {
				server = ipAddr
			}
			serverList = append(serverList, server+"-"+strconv.Itoa(int(weightI))+"-"+strconv.Itoa(int(priority)))
			gsMember := GSMember{
				IPAddr:     ipAddr,
				Weight:     weightI,
				Controller: controllerUUID,
				VsUUID:     vsUUID,
			}
			gsMembers = append(gsMembers, gsMember)
		}
	}
	memberObjs, err := parseDescription(description)
	if err != nil {
		gslbutils.Errf("object: GSLBService, msg: error while parsing description field: %s", err)
	}
	// calculate the checksum
	checksum := gslbutils.GetGSLBServiceChecksum(serverList, domainList, memberObjs, hms,
		persistenceProfileRefPtr, ttl, poolAlgorithmSettings)
	return checksum, gsMembers, memberObjs, hms, nil
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

func PopulateSPCache() *AviSpCache {
	aviRestClientPool := SharedAviClients()
	aviSpCache := GetAviSpCache()
	if len(aviRestClientPool.AviClient) > 0 {
		aviSpCache.AviSitePersistenceCachePopulate(aviRestClientPool.AviClient[0])
	}
	return aviSpCache
}

func VerifyVersion() error {
	gslbutils.Logf("verifying the controller version")
	version := gslbutils.GetAviConfig().Version

	aviRestClientPool := SharedAviClients()
	if len(aviRestClientPool.AviClient) < 1 {
		gslbutils.Errf("no avi clients initialized, returning")
		apiserver.GetAmkoAPIServer().ShutDown()
		return errors.New("no avi clients initialized")
	}

	if !gslbutils.InTestMode() {
		apimodels.RestStatus.UpdateAviApiRestStatus(utils.AVIAPI_CONNECTED, nil)
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
	_, err := gslbutils.GetUriFromAvi(uri, aviClient, false)
	if err != nil {
		gslbutils.Errf("error: get URI %s returned error: %s", uri, err)
		return err
	}

	return nil
}
