/*
 * Copyright 2019-2020 VMware, Inc.
 * All Rights Reserved.
 d
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
	"os"
	"sync"

	"github.com/vmware/alb-sdk/go/models"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"

	"github.com/vmware/alb-sdk/go/clients"
	"github.com/vmware/alb-sdk/go/session"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

var aviClientInstanceMap sync.Map

// SharedAviClients initializes a pool of connections to the avi controller
func SharedAviClients(tenant string) *utils.AviRestClientPool {
	aviClientInstance, ok := aviClientInstanceMap.Load(tenant)
	if ok {
		return aviClientInstance.(*utils.AviRestClientPool)
	}
	var err error
	ctrlCfg := gslbutils.GetAviConfig()
	if ctrlCfg.Username == "" || ctrlCfg.Password == "" || ctrlCfg.IPAddr == "" || ctrlCfg.Version == "" {
		utils.AviLog.Fatal("AVI Controller information is missing, update them in kubernetes secret or via environment variable.")
	}
	os.Setenv("CTRL_VERSION", ctrlCfg.Version)
	userHeaders := utils.SharedCtrlProp().GetCtrlUserHeader()
	userHeaders[gslbutils.XAviUserAgentHeader] = "AMKO"
	apiScheme := utils.SharedCtrlProp().GetCtrlAPIScheme()

	aviRestClientPool, _, err := utils.NewAviRestClientPool(gslbutils.NumRestWorkers, ctrlCfg.IPAddr, ctrlCfg.Username, ctrlCfg.Password, "", ctrlCfg.Version, "", tenant, apiScheme, userHeaders)
	if err != nil {
		utils.AviLog.Errorf("AVI Controller Initialization failed, %s", err)
		return nil
	}
	// set the tenant and controller version in avisession obj
	for _, client := range aviRestClientPool.AviClient {
		SetVersion := session.SetVersion(ctrlCfg.Version)
		SetVersion(client.AviSession)
	}
	aviClientInstanceMap.Store(tenant, aviRestClientPool)
	return aviRestClientPool
}

func IsAviSiteLeader() (bool, error) {
	aviRestClientPool := SharedAviClients("admin")
	if len(aviRestClientPool.AviClient) < 1 {
		gslbutils.Errf("no avi clients initialized, returning")
		return false, errors.New("no avi clients initialized")
	}

	aviClient := aviRestClientPool.AviClient[0]
	clusterUuid, err := GetClusterUuid(aviClient)
	if err != nil {
		gslbutils.Errf("error in finding controller cluster uuid: %s", err.Error())
		return false, err
	}

	gslbLeaderUuid, err := GetGslbLeaderUuid(aviClient)
	if err != nil {
		gslbutils.Errf("error in finding the GSLB leader's uuid: %s", err.Error())
		return false, errors.New("error in finding the GSLB leader's uuid")
	}
	if clusterUuid == gslbLeaderUuid {
		return true, nil
	}
	return false, nil
}

func getAviObjectByUuid(uri string, intf *interface{}, client *clients.AviClient) error {
	var err error
	for i := 0; i < 3; i++ {
		err = client.AviSession.Get(uri, &intf)
		if err == nil {
			return nil
		}
		aviError, ok := err.(session.AviError)
		if !ok {
			gslbutils.Errf("error in parsing the web api error to avi error: %v, will retry %d", err, i)
			continue
		}
		if aviError.HttpStatusCode != 401 {
			gslbutils.Errf("uri: %s, won't retru for status code other than 401", uri)
			return fmt.Errorf("%s", *aviError.Message)
		}
		gslbutils.Errf("uri: %s, aviErr: %s, will retry for %d", uri, *aviError.Message, i)
	}
	return err
}

func GetClusterUuid(client *clients.AviClient) (string, error) {
	var clusterIntf interface{}

	uri := "/api/cluster"

	err := getAviObjectByUuid(uri, &clusterIntf, client)
	if err != nil {
		gslbutils.Logf("object: ControllerCluster, msg: Cluster get URI %s returned error %s", uri, err.Error())
		return "", err
	}

	if clusterIntf == nil {
		gslbutils.Logf("object: ControllerCluster, msg: Cluster get URI %s returned %v type %T",
			uri, clusterIntf, clusterIntf)
		return "", errors.New("unexpected response for get cluster")
	}
	gslbutils.Debugf("object: ControllerCluster, msg: Cluster get URI %s returned a cluster", uri)

	cluster, ok := clusterIntf.(map[string]interface{})
	if !ok {
		gslbutils.Warnf("resp: %v, msg: response can't be parsed to map[string]interface", clusterIntf)
		return "", errors.New("response can't be parsed to map[string]interface")
	}
	name, ok := cluster["name"].(string)
	if !ok {
		gslbutils.Warnf("resp: %v, msg: name not present in response", clusterIntf)
		return "", errors.New("name not present in the cluster response")
	}
	clusterUUID, ok := cluster["uuid"].(string)
	if !ok {
		gslbutils.Warnf("resp: %v, msg: uuid not present in response", clusterIntf)
		return "", errors.New("uuid not present in the cluster response")
	}

	gslbutils.Logf("object: ControllerCluster, name: %s, uuid: %s, msg: fetched uuid for cluster", name, clusterUUID)
	return clusterUUID, nil
}

func GetGslbLeaderUuid(client *clients.AviClient) (string, error) {
	var resp interface{}

	uri := "/api/gslb"
	err := getAviObjectByUuid(uri, &resp, client)
	if err != nil {
		gslbutils.Logf("object: GslbConfig, msg: gslb get URI %s returned error %s", uri, err.Error())
		return "", err
	}

	restResp, ok := resp.(map[string]interface{})
	if !ok {
		gslbutils.Logf("object: GslbConfig, msg: gslb get URI %s returned %v type %T",
			uri, resp, restResp)
		return "", errors.New("unexpected response for get gslb")
	}
	gslbutils.Debugf("object: GslbConfig, msg: gslb get URI %s returned %v count", uri, restResp["count"])
	results, ok := restResp["results"].([]interface{})

	if !ok {
		gslbutils.Logf("object: GslbConfig, msg: results not of type []interface{} instead of type %T",
			restResp["results"])
		return "", errors.New("results not of type []interface{}")
	}

	if len(results) == 0 {
		gslbutils.Logf("object: GslbConfig, msg: results length is zero, probably controller not a part of gslb config")
		return "", errors.New("no results for uri " + uri)
	}
	// results[0] contains the GSLB information
	gslbIntf := results[0]
	gslbConfig := gslbIntf.(map[string]interface{})
	leaderUUID, ok := gslbConfig["leader_cluster_uuid"].(string)
	if !ok {
		gslbutils.Warnf("resp: %v, msg: leader_cluster_uuid not present in response", gslbIntf)
		return "", errors.New("gslb_leader_uuid not present in gslb response")
	}

	gslbutils.Logf("object: GslbConfig, leader_cluster_uuid: %s, msg: fetched leader_cluster_uuid for gslb",
		leaderUUID)
	return leaderUUID, nil
}

func GetHMFromName(name string, gdp bool, tenant string) (*models.HealthMonitor, error) {
	aviClient := SharedAviClients(tenant).AviClient[0]
	uri := "api/healthmonitor?name=" + name

	result, err := gslbutils.GetUriFromAvi(uri, aviClient, gdp)
	if err != nil {
		gslbutils.Errf("Error in getting uri %s from Avi: %v", uri, err)
		return nil, err
	}
	if result.Count == 0 {
		gslbutils.Errf("Health Monitor %s does not exist", name)
		return nil, fmt.Errorf("health Monitor %s does not exist", name)
	}
	gslbutils.Logf("health monitor %s fetched from controller", name)
	elems := make([]json.RawMessage, result.Count)
	err = json.Unmarshal(result.Results, &elems)
	if err != nil {
		gslbutils.Errf("failed to unmarshal health monitor data for ref %s: %v", name, err)
		return nil, err
	}
	hm := models.HealthMonitor{}
	err = json.Unmarshal(elems[0], &hm)
	if err != nil {
		gslbutils.Errf("failed to unmarshal the first health monitor element: %v", err)
		return nil, err
	}
	return &hm, nil
}
