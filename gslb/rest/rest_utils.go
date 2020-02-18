package rest

import (
	"errors"
	"sort"

	"gitlab.eng.vmware.com/orion/container-lib/utils"
	avicache "gitlab.eng.vmware.com/orion/mcc/gslb/cache"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
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

func GetDetailsFromAviGSLB(gslbSvcMap map[string]interface{}) (uint32, []avicache.GSMember, error) {
	var ipList, weightList []string
	var domainList []string
	var gsMembers []avicache.GSMember

	domainNames, ok := gslbSvcMap["domain_names"].([]interface{})
	if !ok {
		return 0, nil, errors.New("domain names absent in gslb service")
	}
	for _, domain := range domainNames {
		domainList = append(domainList, domain.(string))
	}
	groups, ok := gslbSvcMap["groups"].([]interface{})
	if !ok {
		return 0, nil, errors.New("groups absent in gslb service")
	}
	for _, val := range groups {
		group, ok := val.(map[string]interface{})
		if !ok {
			gslbutils.Warnf("couldn't parse group: %v", val)
			continue
		}
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
			weight, ok := member["ratio"].(int32)
			ipList = append(ipList, ipAddr)
			weightList = append(weightList, string(weight))
			gsMember := avicache.GSMember{
				IPAddr: ipAddr,
				Weight: weight,
			}
			gsMembers = append(gsMembers, gsMember)
		}
	}
	// Calculate the checksum
	sort.Strings(ipList)
	sort.Strings(weightList)
	sort.Strings(domainList)
	checksum := utils.Hash(utils.Stringify(ipList)) + utils.Hash(utils.Stringify((domainList))) +
		utils.Hash(utils.Stringify(weightList))
	return checksum, gsMembers, nil
}
