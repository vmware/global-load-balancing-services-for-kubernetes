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

package mockaviserver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"

	"github.com/vmware/alb-sdk/go/models"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
)

type InjectFault func(w http.ResponseWriter, r *http.Request) bool
type InjectFaultWithData func(data []byte, w http.ResponseWriter) bool

var (
	AviMockAPIServer       *httptest.Server
	initServer             sync.Once
	CustomServerMiddleware InjectFault
	PostGSMiddleware       InjectFaultWithData
	PostHMMiddleware       InjectFaultWithData
	PutMiddleware          InjectFaultWithData
	GetMiddleware          InjectFault
	DeleteMiddleware       InjectFault
)

const (
	RandomUUID              = "random-uuid"
	InvalidObjectNameSuffix = "does-not-exist"
)

func AddMiddleware(exec InjectFault) {
	CustomServerMiddleware = exec
}

func ResetMiddleware() {
	CustomServerMiddleware = nil
}

func NewAviMockAPIServer() {
	initServer.Do(func() {
		AviMockAPIServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			url := r.URL.EscapedPath()
			gslbutils.Logf("[fakeAPI]: %s %s", r.Method, url)

			if CustomServerMiddleware != nil {
				CustomServerMiddleware(w, r)
				return
			}

			DefaultServerMiddleware(w, r)
		}))
	})
}

func GetMockServerURL() string {
	return strings.Split(AviMockAPIServer.URL, "https://")[1]
}

func BuildHealthMonitorRef(hmRefs []interface{}) []interface{} {
	rHmRef := hmRefs[0].(string)
	rHmSplit := strings.Split(rHmRef, "name=")
	rHmName := rHmSplit[1]
	return []interface{}{"https://10.10.10.10/api/healthmonitor/healthmonitor-dfe63e98-2e8c-41c7-9390-6992ed71106f#" + rHmName}
}

func DefaultServerMiddleware(w http.ResponseWriter, r *http.Request) {
	url := r.URL.EscapedPath()
	var resp map[string]interface{}
	var finalResponse []byte

	// Handle login
	if strings.Contains(url, "login") {
		// Used for /login
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": "true"}`))
		return
	}

	switch r.Method {
	case "POST":
		data, _ := ioutil.ReadAll(r.Body)
		json.Unmarshal(data, &resp)
		objects := strings.Split(strings.Trim(url, "/"), "/")
		rData, aviObject := resp, objects[1]
		rName := rData["name"].(string)
		if aviObject == "gslbservice" {
			if PostGSMiddleware != nil && PostGSMiddleware(data, w) {
				return
			}
			rHmRefs := rData["health_monitor_refs"].([]interface{})
			objURL := fmt.Sprintf("https://localhost/api/%s/%s-%s-%s#%s", aviObject, aviObject, rName, RandomUUID, rName)
			rData["url"] = objURL
			rData["uuid"] = fmt.Sprintf("%s-%s-%s", aviObject, rName, RandomUUID)
			rData["health_monitor_refs"] = BuildHealthMonitorRef(rHmRefs)
			finalResponse, _ = json.Marshal(rData)
			w.WriteHeader(http.StatusOK)
			w.Write(finalResponse)
			return
		} else if aviObject == "healthmonitor" {
			if PostHMMiddleware != nil && PostHMMiddleware(data, w) {
				return
			}
			objURL := fmt.Sprintf("https://localhost/api/%s/%s-%s-%s#%s", aviObject, aviObject, rName, RandomUUID, rName)
			rData["url"] = objURL
			rData["uuid"] = fmt.Sprintf("%s-%s-%s", aviObject, rName, RandomUUID)
			finalResponse, _ = json.Marshal(rData)
			w.WriteHeader(http.StatusOK)
			w.Write(finalResponse)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "resource not found"}`))
	case "PUT":
		data, _ := ioutil.ReadAll(r.Body)
		if PutMiddleware != nil && PutMiddleware(data, w) {
			return
		}
		json.Unmarshal(data, &resp)
		resp["uuid"] = strings.Split(strings.Trim(url, "/"), "/")[2]
		if resp["health_monitor_refs"] == nil {
			resp["health_monitor_refs"] = []interface{}{"https://10.10.10.10/api/healthmonitor/healthmonitor-dfe63e98-2e8c-41c7-9390-6992ed71106f#System-GSLB-TCP"}
		} else {
			respHm := fmt.Sprintf("%v", resp["health_monitor_refs"])
			respHmList := strings.Split(respHm, " ")
			hmrefs := []string{}
			for _, hm := range respHmList {
				hmName := strings.Trim(hm, " ")
				hmName = strings.Trim(hmName, "]")
				hmName = strings.Split(hmName, "name=")[1]
				hmRef := fmt.Sprintf("https://10.10.10.10/api/healthmonitor/healthmonitor-%s-%s#%s", hmName, RandomUUID, hmName)
				hmrefs = append(hmrefs, hmRef)
			}
			resp["health_monitor_refs"] = hmrefs
		}
		finalResponse, _ = json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		w.Write(finalResponse)
	case "GET":
		if GetMiddleware != nil && GetMiddleware(w, r) {
			return
		}
		objects := strings.Split(strings.Trim(url, "/"), "/")
		gslbutils.Logf("objects: %v", objects)
		SendResponseForObjects(objects, w, r)
	case "DELETE":
		if DeleteMiddleware != nil && DeleteMiddleware(w, r) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
		w.Write(finalResponse)
	default:
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Bad request"}`))
	}
}

func SendResponseForObjects(objects []string, w http.ResponseWriter, r *http.Request) {
	switch objects[1] {
	case "gslbservice":
		if len(objects) > 1 {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "resource not found"}`))
			return
		}
		FeedMockGSData(w, r)
	case "cloud":
		FeedMockCloudData(w, r)
	case "cluster":
		FeedMockClusterData(w, r)
	case "gslb":
		FeedMockGslbData(w, r)
	case "healthmonitor":
		FeedMockHMData(w, r)
	case "applicationpersistenceprofile":
		FeedMockPersistenceData(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "resource not found"}`))
	}

}

func GetMockFilePath(mockFileName string) string {
	mockDir := os.Getenv("MOCK_DATA_DIR")
	if mockDir != "" {
		return mockDir + mockFileName
	}

	return "../avimockobjects/" + mockFileName
}

func FeedMockGSData(w http.ResponseWriter, r *http.Request) {
	mockFilePath := GetMockFilePath("gslbservice_mock.json")
	url := r.URL.EscapedPath()
	object := strings.Split(strings.Trim(url, "/"), "/")
	if len(object) > 1 && r.Method == "GET" {
		data, err := ioutil.ReadFile(mockFilePath)
		if err != nil {
			gslbutils.Errf("Error opening mock file %s", mockFilePath)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

func FeedMockCloudData(w http.ResponseWriter, r *http.Request) {
	mockFilePath := GetMockFilePath("cloud_mock.json")
	url := r.URL.EscapedPath()
	object := strings.Split(strings.Trim(url, "/"), "/")
	if len(object) > 1 && r.Method == "GET" {
		data, err := ioutil.ReadFile(mockFilePath)
		if err != nil {
			gslbutils.Errf("can't read file: %v", err)
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

func FeedMockClusterData(w http.ResponseWriter, r *http.Request) {
	mockFilePath := GetMockFilePath("cluster_mock.json")
	url := r.URL.EscapedPath()
	object := strings.Split(strings.Trim(url, "/"), "/")
	if len(object) > 1 && r.Method == "GET" {
		data, err := ioutil.ReadFile(mockFilePath)
		if err != nil {
			gslbutils.Errf("error in reading file: %v", err)
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

func FeedMockGslbData(w http.ResponseWriter, r *http.Request) {
	mockFilePath := GetMockFilePath("gslb_mock.json")
	url := r.URL.EscapedPath()
	object := strings.Split(strings.Trim(url, "/"), "/")
	if len(object) > 1 && r.Method == "GET" {
		data, err := ioutil.ReadFile(mockFilePath)
		if err != nil {
			gslbutils.Errf("error in reading file: %v", err)
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

func FeedMockHMData(w http.ResponseWriter, r *http.Request) {
	mockFilePath := GetMockFilePath("hm_mock.json")
	url := r.URL.String()
	object := strings.Split(strings.Trim(url, "/"), "/")
	if len(object) > 1 && r.Method == "GET" {
		splitData := strings.Split(url, "?name=")
		type MockHMData struct {
			Count   int                    `json:"count"`
			Results []models.HealthMonitor `json:"results"`
		}
		// Handling a invalid case - No Health Monitor of given name(suffix = InvalidObjectNameSuffix) exists. Sending an empty response
		if len(splitData) == 2 && strings.HasSuffix(splitData[1], InvalidObjectNameSuffix) {
			responseData := MockHMData{
				Count: 0,
			}
			data, err := json.Marshal(responseData)
			if err != nil {
				gslbutils.Errf("error in marshalling health monitor data: %v", err)
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		}
		data, err := ioutil.ReadFile(mockFilePath)
		if err != nil {
			gslbutils.Errf("error in reading file: %v", err)
			w.WriteHeader(404)
			return
		}
		mockHmData := MockHMData{
			Results: []models.HealthMonitor{},
		}
		err = json.Unmarshal([]byte(data), &mockHmData)
		if err != nil {
			gslbutils.Errf("error in unmarshalling health monitor data: %v", err)
			w.WriteHeader(404)
		}
		if len(splitData) == 2 {
			// we need a specific hm data
			for _, hm := range mockHmData.Results {
				if *hm.Name == splitData[1] {
					responseData := MockHMData{
						Count:   1,
						Results: []models.HealthMonitor{hm},
					}
					data, err = json.Marshal(responseData)
					if err != nil {
						gslbutils.Errf("error in marshalling health monitor data: %v", err)
						w.WriteHeader(404)
						return
					}
					w.WriteHeader(http.StatusOK)
					w.Write(data)
					return
				}
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

func FeedMockPersistenceData(w http.ResponseWriter, r *http.Request) {
	mockFilePath := GetMockFilePath("ap_mock.json")
	url := r.URL.String()
	object := strings.Split(strings.Trim(url, "/"), "/")
	if len(object) > 1 && r.Method == "GET" {
		splitData := strings.Split(url, "?name=")
		type MockAPData struct {
			Count   int                                    `json:"count"`
			Results []models.ApplicationPersistenceProfile `json:"results"`
		}
		// Handling a invalid case - No Persistence Profile of give name(suffix = InvalidObjectNameSuffix) exists. Sending an empty response
		if len(splitData) == 2 && strings.HasSuffix(splitData[1], InvalidObjectNameSuffix) {
			responseData := MockAPData{
				Count: 0,
			}
			data, err := json.Marshal(responseData)
			if err != nil {
				gslbutils.Errf("error in marshalling persistence profile data: %v", err)
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		}
		data, err := ioutil.ReadFile(mockFilePath)
		if err != nil {
			gslbutils.Errf("error in reading file: %v", err)
			w.WriteHeader(404)
			return
		}
		mockHmData := MockAPData{
			Results: []models.ApplicationPersistenceProfile{},
		}
		err = json.Unmarshal([]byte(data), &mockHmData)
		if err != nil {
			gslbutils.Errf("error in unmarshalling persistence profile data: %v", err)
			w.WriteHeader(404)
		}
		if len(splitData) == 2 {
			// we need a specific persistence profile data
			for _, ap := range mockHmData.Results {
				if *ap.Name == splitData[1] {
					responseData := MockAPData{
						Count:   1,
						Results: []models.ApplicationPersistenceProfile{ap},
					}
					data, err = json.Marshal(responseData)
					if err != nil {
						gslbutils.Errf("error in marshalling persistence profile data: %v", err)
						w.WriteHeader(404)
						return
					}
					w.WriteHeader(http.StatusOK)
					w.Write(data)
					return
				}
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}
