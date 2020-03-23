package rest

import (
	"errors"

	"amko/gslb/gslbutils"

	"github.com/avinetworks/container-lib/utils"
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
