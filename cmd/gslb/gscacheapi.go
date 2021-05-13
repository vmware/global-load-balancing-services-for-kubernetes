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

package main

import (
	"net/http"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/apiserver"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/api"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/api/models"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

type GSCacheAPI struct{}

func (g GSCacheAPI) InitModel() {}

func (g GSCacheAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/gscache",
		Method:  "GET",
		Handler: GSCacheHandler,
	}
	return []models.OperationMap{get}
}

func GSCacheHandler(w http.ResponseWriter, r *http.Request) {
	aviCache := cache.GetAviCache()

	names, ok := r.URL.Query()["name"]
	if ok {
		name := names[0]
		key := cache.TenantName{Tenant: utils.ADMIN_NS, Name: name}
		obj, present := aviCache.AviCacheGet(key)
		if !present {
			apiserver.WriteErrorToResponse(w)
			return
		}
		apiserver.WriteToResponse(w, obj)
		return
	}
	keys := aviCache.AviCacheGetAllKeys()
	objs := []interface{}{}
	for _, k := range keys {
		obj, _ := aviCache.AviCacheGet(k)
		gsObj := obj.(*cache.AviGSCache)
		objs = append(objs, gsObj)
	}
	apiserver.WriteToResponse(w, objs)
}

type HmCacheAPI struct{}

func (h HmCacheAPI) InitModel() {}

func (h HmCacheAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/hmcache",
		Method:  "GET",
		Handler: HmCacheHandler,
	}
	return []models.OperationMap{get}
}

func HmCacheHandler(w http.ResponseWriter, r *http.Request) {
	aviHmCache := cache.GetAviHmCache()

	names, ok := r.URL.Query()["name"]
	if ok {
		name := names[0]
		obj, present := aviHmCache.AviHmCacheGet(cache.TenantName{Tenant: utils.ADMIN_NS, Name: name})
		if !present {
			apiserver.WriteErrorToResponse(w)
			return
		}
		apiserver.WriteToResponse(w, obj)
		return
	}
	keys := aviHmCache.AviHmGetAllKeys()
	objs := []interface{}{}
	for _, k := range keys {
		obj, _ := aviHmCache.AviHmCacheGet(k)
		objs = append(objs, obj)
	}
	apiserver.WriteToResponse(w, objs)
}

func InitAmkoAPIServer() {
	modelList := []models.ApiModel{
		apiserver.AcceptedIngressAPI{},
		apiserver.RejectedIngressAPI{},
		apiserver.AcceptedLBSvcAPI{},
		apiserver.RejectedLBSvcAPI{},
		apiserver.AcceptedRouteAPI{},
		apiserver.RejectedRouteAPI{},
		apiserver.FilterAPI{},
		apiserver.GslbHostRuleAPI{},
		apiserver.GSGraphAPI{},
		GSCacheAPI{},
		HmCacheAPI{},
	}
	amkoAPIServer := api.NewServer("8080", modelList)
	amkoAPIServer.InitApi()

	apiserver.SetAmkoAPIServer(amkoAPIServer)
}
