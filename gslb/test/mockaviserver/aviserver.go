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

package mockaviserver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/avinetworks/amko/gslb/gslbutils"
)

type InjectFault func(w http.ResponseWriter, r *http.Request)

var (
	AviMockAPIServer       *httptest.Server
	initServer             sync.Once
	CustomServerMiddleware InjectFault
)

const (
	RandomUUID = "random-uuid"
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

func buildHealthMonitorRef(hmRefs []interface{}) []interface{} {
	rHmRef := hmRefs[0].(string)
	rHmSplit := strings.Split(rHmRef, "name=")
	rHmName := rHmSplit[1]
	return []interface{}{"https://10.79.111.29/api/healthmonitor/healthmonitor-dfe63e98-2e8c-41c7-9390-6992ed71106f#" + rHmName}
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
		//gslbutils.Logf("resp: %v", resp["url"])
		objects := strings.Split(strings.Trim(url, "/"), "/")
		rData, aviObject := resp, objects[1]
		rName := rData["name"].(string)
		if aviObject == "gslbservice" {
			rHmRefs := rData["health_monitor_refs"].([]interface{})
			objURL := fmt.Sprintf("https://localhost/api/%s/%s-%s-%s#%s", aviObject, aviObject, rName, RandomUUID, rName)
			rData["url"] = objURL
			rData["uuid"] = fmt.Sprintf("%s-%s-%s", aviObject, rName, RandomUUID)
			rData["health_monitor_refs"] = buildHealthMonitorRef(rHmRefs)
			finalResponse, _ = json.Marshal(rData)
			w.WriteHeader(http.StatusOK)
			w.Write(finalResponse)
			return
		} else if aviObject == "healthmonitor" {
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
		json.Unmarshal(data, &resp)
		resp["uuid"] = strings.Split(strings.Trim(url, "/"), "/")[2]
		resp["health_monitor_refs"] = []interface{}{"https://10.79.111.29/api/healthmonitor/healthmonitor-dfe63e98-2e8c-41c7-9390-6992ed71106f#System-GSLB-TCP"}
		finalResponse, _ = json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		w.Write(finalResponse)
	case "GET":
		objects := strings.Split(strings.Trim(url, "/"), "/")
		if len(objects) > 1 && objects[1] != "gslbservice" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "resource not found"}`))
		}
		FeedMockData(w, r)
	case "DELETE":
		w.WriteHeader(http.StatusNoContent)
		w.Write(finalResponse)
	default:
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Bad request"}`))
	}
}

func FeedMockData(w http.ResponseWriter, r *http.Request) {
	mockFilePath := "../avimockobjects/gslbservice_mock.json"
	url := r.URL.EscapedPath()
	object := strings.Split(strings.Trim(url, "/"), "/")
	if len(object) > 1 && r.Method == "GET" {
		data, _ := ioutil.ReadFile(mockFilePath)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}
