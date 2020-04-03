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
	"sync"

	"amko/gslb/gslbutils"

	"github.com/avinetworks/container-lib/utils"
)

var AviClientInstance *utils.AviRestClientPool

var clientOnce sync.Once

// SharedAviClients initializes a pool of connections to the avi controller
func SharedAviClients() *utils.AviRestClientPool {
	var err error

	ctrlCfg := gslbutils.GetAviConfig()
	if ctrlCfg.Username == "" || ctrlCfg.Password == "" || ctrlCfg.IPAddr == "" {
		utils.AviLog.Error.Panic("AVI Controller information is missing, update them in kubernetes secret or via environment variable.")
	}
	AviClientInstance, err = utils.NewAviRestClientPool(utils.NumWorkersGraph, ctrlCfg.IPAddr, ctrlCfg.Username, ctrlCfg.Password)
	if err != nil {
		utils.AviLog.Error.Printf("AVI Controller Initialization failed, %s", err)
	}
	return AviClientInstance
}
