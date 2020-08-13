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

package rest

import (
	"errors"

	"github.com/avinetworks/amko/gslb/gslbutils"

	"github.com/avinetworks/ako/pkg/utils"
)

func RestRespArrToObjByType(operation *utils.RestOp, objType string, key string) (map[string]interface{}, error) {
	var respElem map[string]interface{}

	if operation.Method == utils.RestPost {
		resp, ok := operation.Response.(map[string]interface{})

		if !ok {
			gslbutils.Logf("key: %s, msg: response has unknown type %T", key, operation.Response)
			return nil, errors.New("malformed response")
		}

		aviURL, ok := resp["url"].(string)
		if !ok {
			gslbutils.Logf("key: %s, resp: %v, msg: url not present in response", key, resp)
			return nil, errors.New("url not present in response")
		}

		aviObjType, err := utils.AviUrlToObjType(aviURL)
		if err == nil && aviObjType == objType {
			respElem = resp
		}
	} else {
		// PUT calls are specific for the resource
		respElem = operation.Response.(map[string]interface{})
	}
	return respElem, nil
}
