package cache

import (
	"sync"
)

var (
	aviCache     *AviCache
	objCacheOnce sync.Once
)

type GSMember struct {
	IPAddr string
}

type AviGSCache struct {
	Name             string
	Tenant           string
	Uuid             string
	Members          []GSMember
	CloudConfigCksum uint32
}

type AviCache struct {
	cacheLock sync.RWMutex
	cache     map[interface{}]interface{}
}

func NewAviCache() *AviCache {
	objCacheOnce.Do(func() {
		aviCache = &AviCache{}
		aviCache.cache = make(map[interface{}]interface{})
	})
	return aviCache
}

func (c *AviCache) AviCacheGet(k interface{}) (interface{}, bool) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	val, ok := c.cache[k]
	return val, ok
}

func (c *AviCache) AviCacheGetByUuid(uuid string) (interface{}, bool) {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()
	for key, value := range c.cache {
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
	c.cache[k] = val
}

func (c *AviCache) AviCacheDelete(k interface{}) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	delete(c.cache, k)
}

type TenantName struct {
	Tenant string
	Name   string
}
