/*
 * Copyright 2019-2021 VMware, Inc.
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
package apiserver

import (
	"encoding/json"
	"net/http"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/store"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/api/models"
)

type FilterAPI struct{}

func (f FilterAPI) InitModel() {}

func (f FilterAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/filter",
		Method:  "GET",
		Handler: FilterAPIGetHandler,
	}
	return []models.OperationMap{get}
}

func FilterAPIGetHandler(w http.ResponseWriter, r *http.Request) {
	f := gslbutils.GetGlobalFilter()

	gslbutils.Logf("fetching filter data")
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(f.GetCopy())
}

type GslbHostRuleAPI struct{}

func (g GslbHostRuleAPI) InitModel() {}

func (g GslbHostRuleAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/ghrules",
		Method:  "GET",
		Handler: GhRuleAPIHandler,
	}
	return []models.OperationMap{get}
}

func GhRuleAPIHandler(w http.ResponseWriter, r *http.Request) {
	keys := r.URL.Query()
	fqdns, ok := r.URL.Query()["fqdn"]
	if !ok && len(keys) != 0 {
		gslbutils.Logf("unsupported keys: %v", keys)
		return
	}
	ghRuleList := gslbutils.GetGSHostRulesList()
	if len(fqdns) > 0 {
		gsFqdn := fqdns[0]
		rules := ghRuleList.GetGSHostRulesForFQDN(gsFqdn)
		WriteToResponse(w, rules)
		return
	} else {
		rules := ghRuleList.GetAllGSHostRules()
		WriteToResponse(w, rules)
		return
	}
}

func WriteToResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func WriteErrorToResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(`{"error": "Bad Request"}`))
}

type AcceptedIngressAPI struct{}

func (ai AcceptedIngressAPI) InitModel() {}

func (ai AcceptedIngressAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/accepted/ingress",
		Method:  "GET",
		Handler: AcceptedIngressAPIHandler,
	}
	return []models.OperationMap{get}
}

func AcceptedIngressAPIHandler(w http.ResponseWriter, r *http.Request) {
	FetchIngestionObjectsAndRespond(w, r, gdpalphav2.IngressObj, true)
}

type RejectedIngressAPI struct{}

func (ai RejectedIngressAPI) InitModel() {}

func (ri RejectedIngressAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/rejected/ingress",
		Method:  "GET",
		Handler: RejectedIngressAPIHandler,
	}
	return []models.OperationMap{get}
}

func RejectedIngressAPIHandler(w http.ResponseWriter, r *http.Request) {
	FetchIngestionObjectsAndRespond(w, r, gdpalphav2.IngressObj, false)
}

type AcceptedRouteAPI struct{}

func (ar AcceptedRouteAPI) InitModel() {}

func (ar AcceptedRouteAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/accepted/route",
		Method:  "GET",
		Handler: AcceptedRouteAPIHandler,
	}
	return []models.OperationMap{get}
}

func AcceptedRouteAPIHandler(w http.ResponseWriter, r *http.Request) {
	FetchIngestionObjectsAndRespond(w, r, gdpalphav2.RouteObj, true)
}

type RejectedRouteAPI struct{}

func (rr RejectedRouteAPI) InitModel() {}

func (rr RejectedRouteAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/rejected/route",
		Method:  "GET",
		Handler: RejectedRouteAPIHandler,
	}
	return []models.OperationMap{get}
}

func RejectedRouteAPIHandler(w http.ResponseWriter, r *http.Request) {
	FetchIngestionObjectsAndRespond(w, r, gdpalphav2.RouteObj, false)
}

type AcceptedLBSvcAPI struct{}

func (as AcceptedLBSvcAPI) InitModel() {}

func (as AcceptedLBSvcAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/accepted/lbsvc",
		Method:  "GET",
		Handler: AcceptedLBSvcAPIHandler,
	}
	return []models.OperationMap{get}
}

func AcceptedLBSvcAPIHandler(w http.ResponseWriter, r *http.Request) {
	FetchIngestionObjectsAndRespond(w, r, gdpalphav2.LBSvcObj, true)
}

type RejectedLBSvcAPI struct{}

func (rs RejectedLBSvcAPI) InitModel() {}

func (rs RejectedLBSvcAPI) ApiOperationMap() []models.OperationMap {
	get := models.OperationMap{
		Route:   "/api/rejected/lbsvc",
		Method:  "GET",
		Handler: RejectedLBSvcAPIHandler,
	}
	return []models.OperationMap{get}
}

func RejectedLBSvcAPIHandler(w http.ResponseWriter, r *http.Request) {
	FetchIngestionObjectsAndRespond(w, r, gdpalphav2.LBSvcObj, false)
}

func FetchIngestionObjectsAndRespond(w http.ResponseWriter, r *http.Request, objType string, accepted bool) {
	var cluster, ns, name string

	clusters, ok := r.URL.Query()["cluster"]
	if ok {
		cluster = clusters[0]
	}

	nss, ok := r.URL.Query()["ns"]
	if ok {
		ns = nss[0]
	}

	names, ok := r.URL.Query()["name"]
	if ok {
		name = names[0]
	}

	var objList *store.ClusterStore

	if objType == gdpalphav2.RouteObj {
		if accepted {
			objList = store.GetAcceptedRouteStore()
		} else {
			objList = store.GetRejectedRouteStore()
		}
	} else if objType == gdpalphav2.LBSvcObj {
		if accepted {
			objList = store.GetAcceptedLBSvcStore()
		} else {
			objList = store.GetRejectedLBSvcStore()
		}
	} else if objType == gdpalphav2.IngressObj {
		if accepted {
			objList = store.GetAcceptedIngressStore()
		} else {
			objList = store.GetRejectedIngressStore()
		}
	} else {
		gslbutils.Errf("Unknown Object type: %s", objType)
		WriteErrorToResponse(w)
		return
	}

	objects := objList.GetAllClusterNSObjects()
	result := []interface{}{}
	for _, o := range objects {
		cname, namespace, sname, err := splitName(gdpalphav2.IngressObj, o)
		if err != nil {
			gslbutils.Logf("can't split name for object: %s", o)
			continue
		}
		if cluster != "" && cluster != cname {
			continue
		}
		if ns != "" && ns != namespace {
			continue
		}
		if name != "" && name != sname {
			continue
		}
		obj, ok := objList.GetClusterNSObjectByName(cname, namespace, sname)
		if !ok {
			gslbutils.Logf("couldn't find object: %s", o)
			continue
		}
		result = append(result, obj)
	}
	WriteToResponse(w, result)
}

func splitName(objType, objName string) (string, string, string, error) {
	var cname, ns, sname, hostname string
	var err error
	if objType == gdpalphav2.IngressObj {
		cname, ns, sname, hostname, err = gslbutils.SplitMultiClusterIngHostName(objName)
		sname += "/" + hostname
	} else {
		cname, ns, sname, err = gslbutils.SplitMultiClusterObjectName(objName)
	}
	return cname, ns, sname, err
}
