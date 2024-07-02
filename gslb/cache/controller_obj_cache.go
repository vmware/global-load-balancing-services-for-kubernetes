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
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"

	gdpv1alpha2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"

	"github.com/davecgh/go-spew/spew"
	"github.com/vmware/alb-sdk/go/clients"
	"github.com/vmware/alb-sdk/go/models"
	"github.com/vmware/alb-sdk/go/session"
	apimodels "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/api/models"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/apiserver"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
)

var (
	aviCache        *AviCache
	objCacheOnce    sync.Once
	aviHmCache      *AviHmCache
	hmObjCacheOnce  sync.Once
	aviSpCache      *AviSpCache
	spObjCacheOnce  sync.Once
	aviPkiCache     *AviPkiCache
	pkiObjCacheOnce sync.Once
)

type CustomHmSettings struct {
	RequestHeader string
	ResponseCode  []string
}

type AviHmObj struct {
	Tenant           string
	Name             string
	Port             int32
	UUID             string
	Type             string
	CloudConfigCksum uint32
	Template         *string
	CustomHmSettings *CustomHmSettings
	Description      string
	CreatedBy        string
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

	matchCreatedBy := gslbutils.AMKOControlConfig().CreatedByField()

	// parse all pages with Health monitors till we hit the last page
	for {
		if len(hmname) == 1 {
			uri = "/api/healthmonitor?name=" + hmname[0]
		} else if nextPageURI != "" {
			uri = nextPageURI
		}
		// first fetch all federated HMs. All federated HMs can be grouped into 3 categories:
		// 1. HMs created by this AMKO instance
		// 2. Custom federated HMs created by the user
		// 3. HMs created by other AMKO instances
		// Category 1 and 2 HMs are the ones that we need to store in the cache. Category 3 HMs
		// must be ignored and not stored in the HM cache.
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

			k := TenantName{Tenant: getTenantFromTenantRef(*hm.TenantRef), Name: *hm.Name}
			var monitorPort int32
			if hm.MonitorPort != nil {
				monitorPort = *hm.MonitorPort
			}
			description := ""
			if hm.Description != nil {
				description = *hm.Description
			}

			var createdBy string
			var createdByDifferentAMKO bool
			for _, m := range hm.Markers {
				if m.Key != nil && *m.Key == gslbutils.CreatedByLabelKey {
					createdBy = m.Values[0]
					// add only those health monitors to the cache whose labels match this
					// AMKO's created by field, ignore all other AMKO's health monitors
					if createdBy != matchCreatedBy {
						createdByDifferentAMKO = true
						break
					}
				}
			}
			if createdByDifferentAMKO {
				continue
			}
			cksum := gslbutils.GetGSLBHmChecksum(*hm.Type, monitorPort, []string{description}, createdBy)
			hmCacheObj := AviHmObj{
				Name:             *hm.Name,
				Tenant:           getTenantFromTenantRef(*hm.TenantRef),
				UUID:             *hm.UUID,
				Port:             monitorPort,
				CloudConfigCksum: cksum,
				Template:         nodes.GetTemplateFromHmDescription(*hm.Name, description),
				Description:      description,
				CreatedBy:        createdBy,
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

type AviPkiCache struct {
	cacheLock sync.RWMutex
	Cache     map[interface{}]interface{}
	UUIDCache map[string]interface{}
}

func GetAviPkiCache() *AviPkiCache {
	pkiObjCacheOnce.Do(func() {
		aviPkiCache = &AviPkiCache{}
		aviPkiCache.Cache = make(map[interface{}]interface{})
		aviPkiCache.UUIDCache = make(map[string]interface{})
	})
	return aviPkiCache
}

func (s *AviPkiCache) AviPkiCacheAdd(k interface{}, val interface{}) {
	s.cacheLock.Lock()
	defer s.cacheLock.Unlock()
	s.Cache[k] = val
}

func (s *AviPkiCache) AviPkiCacheAddByUUID(uuid string, val interface{}) {
	s.cacheLock.Lock()
	defer s.cacheLock.Unlock()
	s.UUIDCache[uuid] = val
}

func (s *AviPkiCache) AviPkiCacheGet(k interface{}) (interface{}, bool) {
	s.cacheLock.RLock()
	defer s.cacheLock.RUnlock()
	val, ok := s.Cache[k]
	return val, ok
}

func (s *AviPkiCache) AviPkiCacheGetByUUID(uuid string) (interface{}, bool) {
	s.cacheLock.RLock()
	defer s.cacheLock.RUnlock()
	val, ok := s.UUIDCache[uuid]
	return val, ok
}

func (s *AviPkiCache) AviPkiCachePopulate(client *clients.AviClient) error {
	var nextPageURI string
	baseURI := "/api/pkiprofile"
	uri := baseURI + "?page_size=100"

	// parse all pages with PKI Profile till we hit the last page
	for {
		if nextPageURI != "" {
			uri = nextPageURI
		}
		result, err := gslbutils.GetUriFromAvi(uri+"&is_federated=true", client, false)
		if err != nil {
			return fmt.Errorf("object: AviPkiProfileCache, msg: Pkiprofile get URI %s returned error: %v",
				uri, err)
		}

		gslbutils.Logf("fetched %d PKI profiles", result.Count)
		elems := make([]json.RawMessage, result.Count)
		err = json.Unmarshal(result.Results, &elems)
		if err != nil {
			return errors.New("failed to unmarshal pki  profile ref, err: " + err.Error())
		}

		processedObjs := 0
		for i := 0; i < len(elems); i++ {
			sp := models.PKIprofile{}
			err := json.Unmarshal(elems[i], &sp)
			if err != nil {
				gslbutils.Warnf("failed to unmarshal pki profile element, err: %s", err.Error())
				continue
			}

			if sp.Name == nil || sp.UUID == nil {
				gslbutils.Warnf("incomplete pki profile ref unmarshalled %s", utils.Stringify(sp))
				continue
			}

			k := TenantName{Tenant: getTenantFromTenantRef(*sp.TenantRef), Name: *sp.Name}
			s.AviPkiCacheAdd(k, &sp)
			s.AviPkiCacheAddByUUID(*sp.UUID, &sp)
			gslbutils.Debugf("processed pki profile %s, UUID: %s", *sp.Name, *sp.UUID)
			processedObjs++
		}
		gslbutils.Logf("processed %d pki profiles", processedObjs)

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

			k := TenantName{Tenant: getTenantFromTenantRef(*sp.TenantRef), Name: *sp.Name}
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
	Weight     uint32
	Priority   uint32
	VsUUID     string
	Controller string
	PublicIP   string
}

type AviGSCache struct {
	Name             string
	Tenant           string
	Uuid             string
	Members          []GSMember
	K8sObjects       []string
	HealthMonitor    []string
	GSDownResponse   *gslbalphav1.DownResponse
	CloudConfigCksum uint32
	CreatedBy        string
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
	createdBy := gslbutils.AmkoUser
	createdByChanged := false

	// Parse all the pages with GSLB services till we hit the last page
	// First fetch all GSs with created_by=gslbutils.AmkoUser, if no GSs were found,
	// then fetch with created_by=gslbutils.AMKOControlConfig().CreatedByField().
	// This is to ensure that we are backward compatible and update all previously
	// existing GSs with the new created_by field.
	for {
		if len(gsname) == 1 {
			uri = "/api/gslbservice?name=" + gsname[0]
		} else if nextPageURI != "" {
			uri = nextPageURI
		}
		result, err := gslbutils.GetUriFromAvi(uri+"&created_by="+createdBy, client, false)
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

		if len(elems) == 0 && !createdByChanged {
			createdBy = gslbutils.AMKOControlConfig().CreatedByField()
			createdByChanged = true
			continue
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
	cksum, gsMembers, memberObjs, hms, gsDownResponse, createdBy, err := GetDetailsFromAviGSLBFormatted(gsObj)
	if err != nil {
		gslbutils.Errf("resp: %v, msg: error occurred while parsing the response: %s", gsObj, err)
		// if we want to get avi gs object for a spefic gs name,
		// then don't skip even if not all expected fields are present.
		// This is used while retrying after a failure
		if len(gsname) == 0 {
			return
		}
	}
	k := TenantName{Tenant: getTenantFromTenantRef(*gsObj.TenantRef), Name: name}
	gsCacheObj := AviGSCache{
		Name:             name,
		Tenant:           getTenantFromTenantRef(*gsObj.TenantRef),
		Uuid:             uuid,
		Members:          gsMembers,
		K8sObjects:       memberObjs,
		HealthMonitor:    hms,
		GSDownResponse:   gsDownResponse,
		CloudConfigCksum: cksum,
		CreatedBy:        createdBy,
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

func ParsePoolAlgorithmSettings(algorithm *string, fallbackAlgorithm *string, consistentHashMask *uint32) *gslbalphav1.PoolAlgorithmSettings {
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
	var consistentHashMask *uint32

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

	if a == gslbalphav1.PoolAlgorithmConsistentHash || f == gslbalphav1.PoolAlgorithmConsistentHash {
		hashMaskF, ok := group["consistent_hash_mask"].(float64)
		if ok {
			hashMaskI := uint32(hashMaskF)
			consistentHashMask = &hashMaskI
		} else {
			gslbutils.Warnf("couldn't parse hash mask: %v", group)
		}
	}

	return ParsePoolAlgorithmSettings(algorithm, fallbackAlgorithm, consistentHashMask)
}

func parseDownResponse(gsDownResponse *models.GslbServiceDownResponse) *gslbalphav1.DownResponse {
	if gsDownResponse == nil {
		return nil
	}
	response := &gslbalphav1.DownResponse{}
	response.Type = *gsDownResponse.Type
	if *gsDownResponse.Type == gslbalphav1.GSLBServiceDownResponseFallbackIP {
		if gsDownResponse.FallbackIP != nil {
			response.FallbackIP = *gsDownResponse.FallbackIP.Addr
		}
	}
	return response
}

func parseDownResponseFromRaw(val interface{}) *gslbalphav1.DownResponse {
	response, ok := val.(map[string]interface{})
	if !ok {
		gslbutils.Warnf("couldn't parse down response: %v", val)
		return nil
	}
	responseType, ok := response["type"].(string)
	if !ok {
		gslbutils.Warnf("couldn't parse down response type: %v", val)
		return nil
	}
	downResponse := &gslbalphav1.DownResponse{}
	downResponse.Type = responseType
	if responseType != gslbalphav1.GSLBServiceDownResponseFallbackIP {
		return downResponse
	}

	fallbackIP, ok := response["fallback_ip"].(map[string]interface{})
	if !ok {
		gslbutils.Warnf("couldn't parse fallback address: %v", response)
		return nil
	}
	addr, ok := fallbackIP["addr"].(string)
	if !ok {
		gslbutils.Warnf("couldn't parse fallback IP address: %v", response)
		return nil
	}
	downResponse.FallbackIP = addr
	return downResponse
}

func GetDetailsFromAviGSLBFormatted(gsObj models.GslbService) (uint32, []GSMember, []string, []string, *gslbalphav1.DownResponse, string, error) {
	var serverList, domainList, memberObjs []string
	var hms []string
	var gsMembers []GSMember
	var persistenceProfileRef, createdBy string
	var persistenceProfileRefPtr *string
	var pkiProfileRef *string
	var sitePersistenceRequired bool
	var ttl *uint32
	var gsDownResponse *gslbalphav1.DownResponse

	domainNames := gsObj.DomainNames
	if len(domainNames) == 0 {
		return 0, nil, memberObjs, nil, gsDownResponse, createdBy, errors.New("domain names absent in gslb service")
	}
	// make a copy of the domain names list
	for _, domain := range domainNames {
		domainList = append(domainList, domain)
	}

	groups := gsObj.Groups
	if len(groups) == 0 {
		return 0, nil, memberObjs, nil, gsDownResponse, createdBy, errors.New("groups absent in gslb service")
	}

	if gsObj.Description == nil || *gsObj.Description == "" {
		return 0, nil, memberObjs, nil, gsDownResponse, createdBy, errors.New("description absent in gslb service")
	}

	if gsObj.CreatedBy != nil {
		createdBy = *gsObj.CreatedBy
	}

	hmRefs := gsObj.HealthMonitorRefs
	for _, hmRef := range hmRefs {
		hmRefSplit := strings.Split(hmRef, "/api/healthmonitor/")
		if len(hmRefSplit) != 2 {
			return 0, nil, memberObjs, nil, gsDownResponse, createdBy, errors.New("health monitor name is absent in health monitor ref: " + hmRefs[0])
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

	if sitePersistenceRequired && gsObj.PkiProfileRef != nil {
		// find out the name of the profile
		refSplit := strings.Split(*gsObj.PkiProfileRef, "/pkiprofile/")
		if len(refSplit) == 2 {
			spCache := GetAviPkiCache()
			sp, present := spCache.AviPkiCacheGetByUUID(refSplit[1])
			if present {
				spObj, ok := sp.(*models.PKIprofile)
				if ok {
					pkiProfileRef = spObj.Name
				} else {
					gslbutils.Warnf("gsName: %s, fetchedRef: %s, msg: stored pki profile not in right format",
						*gsObj.Name, *gsObj.PkiProfileRef)
				}
			} else {
				gslbutils.Warnf("gsName: %s, fetchedRef: %s, uuid: %s, msg: pki profile not present in cache by UUID",
					*gsObj.Name, *gsObj.PkiProfileRef, refSplit[1])
			}
		} else {
			gslbutils.Warnf("gsName: %s, fetchedRef: %s, msg: wrong format for pki profile ref", *gsObj.Name,
				*gsObj.PkiProfileRef)
		}
	}
	ttl = gsObj.TTL

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
			gsMember := GSMember{
				IPAddr:   ipAddr,
				Weight:   weight,
				Priority: priority,
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
			if member.PublicIP != nil && member.PublicIP.IP != nil && member.PublicIP.IP.Addr != nil {
				gsMember.PublicIP = *member.PublicIP.IP.Addr
			}
			serverList = append(serverList, server+"-"+strconv.Itoa(int(weight))+"-"+strconv.Itoa(int(priority)))
			gsMembers = append(gsMembers, gsMember)
		}
	}
	memberObjs, err := parseDescription(*gsObj.Description)
	if err != nil {
		gslbutils.Errf("object: GSLBService, msg: error while parsing description field: %s", err)
	}

	gsDownResponse = parseDownResponse(gsObj.DownResponse)

	// calculate the checksum
	checksum := gslbutils.GetGSLBServiceChecksum(serverList, domainList, memberObjs, hms,
		persistenceProfileRefPtr, ttl, poolAlgorithmSettings, gsDownResponse, pkiProfileRef, createdBy)
	return checksum, gsMembers, memberObjs, hms, gsDownResponse, createdBy, nil
}

// As name is encoded, retreiving information about the Hm becomes difficult
// Thats why we fetch Hm description for further processing
func GetHmDescriptionFromName(hmName, tenant string) string {
	aviRestClientPool := SharedAviClients(tenant)
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
func GetGSFromHmName(hmName, tenant string) (string, int8, error) {
	hmDesc := GetHmDescriptionFromName(hmName, tenant)
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

func GetDetailsFromAviGSLB(gslbSvcMap map[string]interface{}) (uint32, []GSMember, []string, []string, *gslbalphav1.DownResponse, string, error) {
	var serverList, domainList, memberObjs []string
	var hms []string
	var gsMembers []GSMember
	var ttl *uint32
	var createdBy string
	var gsDownResponse *gslbalphav1.DownResponse

	domainNames, ok := gslbSvcMap["domain_names"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, hms, gsDownResponse, createdBy, errors.New("domain names absent in gslb service")
	}
	for _, domain := range domainNames {
		domainList = append(domainList, domain.(string))
	}
	groups, ok := gslbSvcMap["groups"].([]interface{})
	if !ok {
		return 0, nil, memberObjs, hms, gsDownResponse, createdBy, errors.New("groups absent in gslb service")
	}

	description, ok := gslbSvcMap["description"].(string)
	if !ok {
		return 0, nil, memberObjs, hms, gsDownResponse, createdBy, errors.New("description absent in gslb service")
	}

	createdBy, ok = gslbSvcMap["created_by"].(string)
	if !ok {
		createdBy = ""
	}

	hmRefs, ok := gslbSvcMap["health_monitor_refs"].([]interface{})
	if ok {
		for _, hmRefIntf := range hmRefs {
			hmRef := hmRefIntf.(string)
			hmRefSplit := strings.Split(hmRef, "#")
			if len(hmRefSplit) != 2 {
				errStr := fmt.Sprintf("health monitor name is absent in health monitor ref: %v", hmRefSplit[0])
				return 0, nil, memberObjs, hms, gsDownResponse, createdBy, errors.New(errStr)
			}
			hm := hmRefSplit[1]
			hms = append(hms, hm)
		}
	} else {
		gslbutils.Debugf("gslbsvcmap: %v, health_monitor_refs absent in gslb service", gslbSvcMap)
	}

	sitePersistenceEnabled, ok := gslbSvcMap["site_persistence_enabled"].(bool)
	if !ok {
		return 0, nil, memberObjs, hms, gsDownResponse, createdBy, errors.New("site_persistence_enabled absent in gslb service")
	}

	var persistenceProfileRef string
	var persistenceProfileRefPtr, pkiProfileRefPtr *string
	if sitePersistenceEnabled == true {
		var ok bool
		persistenceProfileRef, ok = gslbSvcMap["application_persistence_profile_ref"].(string)
		if !ok {
			return 0, nil, memberObjs, hms, gsDownResponse, createdBy,
				errors.New("application_persistence_profile_ref absent in gslb service")
		}
		persistenceProfileRefPtr = &persistenceProfileRef
		pkiProfileRef, ok := gslbSvcMap["pki_profile_ref"].(string)
		if ok {
			pkiProfileRefPtr = &pkiProfileRef
		}
	}

	ttlVal, ok := gslbSvcMap["ttl"]
	if ok {
		parsedValF, ok := ttlVal.(float64)
		if ok {
			parsedValI := uint32(parsedValF)
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
		priority := uint32(priorityF)
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
			weightI := uint32(weight)
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
				Priority:   priority,
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

	if val, ok := gslbSvcMap["down_response"]; ok {
		gsDownResponse = parseDownResponseFromRaw(val)
	}

	// calculate the checksum
	checksum := gslbutils.GetGSLBServiceChecksum(serverList, domainList, memberObjs, hms,
		persistenceProfileRefPtr, ttl, poolAlgorithmSettings, gsDownResponse, pkiProfileRefPtr, createdBy)
	return checksum, gsMembers, memberObjs, hms, gsDownResponse, createdBy, nil
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
	aviRestClientPool := SharedAviClients("*")
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
	aviRestClientPool := SharedAviClients("*")
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
	aviRestClientPool := SharedAviClients("*")
	aviSpCache := GetAviSpCache()
	if len(aviRestClientPool.AviClient) > 0 {
		aviSpCache.AviSitePersistenceCachePopulate(aviRestClientPool.AviClient[0])
	}
	return aviSpCache
}

func PopulatePkiCache() *AviPkiCache {
	aviRestClientPool := SharedAviClients("*")
	aviSpCache := GetAviPkiCache()
	if len(aviRestClientPool.AviClient) > 0 {
		aviSpCache.AviPkiCachePopulate(aviRestClientPool.AviClient[0])
	}
	return aviSpCache
}

func VerifyVersion() error {
	gslbutils.Logf("verifying the controller version")
	version := gslbutils.GetAviConfig().Version

	aviRestClientPool := SharedAviClients("*")
	if len(aviRestClientPool.AviClient) < 1 {
		gslbutils.Errf("no avi clients initialized, returning")
		gslbutils.AMKOControlConfig().PodEventf(corev1.EventTypeWarning, gslbutils.AMKOShutdown, "No Avi clients initialized.")
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

func getTenantFromTenantRef(tenantRef string) string {
	arr := strings.Split(tenantRef, "#")
	if len(arr) == 2 {
		return arr[1]
	}
	if len(arr) == 1 {
		arr = strings.Split(tenantRef, "/")
		return arr[len(arr)-1]
	}
	return tenantRef
}
