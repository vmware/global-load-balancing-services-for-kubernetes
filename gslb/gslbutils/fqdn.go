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

package gslbutils

import (
	"fmt"
	"sync"
)

type LocalFqdn struct {
	Cluster string
	Fqdn    string
}
type globalToLocalFqdn struct {
	globalToLocalMap      map[string][]LocalFqdn
	localToGlobalFqdnList *LocalToGlobalFqdn

	lock sync.RWMutex
}

var globalToLocalFqdnList *globalToLocalFqdn
var globalToLocalFqdnOnce sync.Once

func GetFqdnMap() *globalToLocalFqdn {
	globalToLocalFqdnOnce.Do(func() {
		globalToLocalFqdnList = &globalToLocalFqdn{
			localToGlobalFqdnList: &LocalToGlobalFqdn{
				make(map[string]string),
			},
			globalToLocalMap: make(map[string][]LocalFqdn),
		}
	})
	return globalToLocalFqdnList
}

func (glFqdn *globalToLocalFqdn) AddUpdateToFqdnMapping(gFqdn, lFqdn, cname string) {
	glFqdn.lock.Lock()
	defer glFqdn.lock.Unlock()

	if _, ok := glFqdn.globalToLocalMap[gFqdn]; !ok {
		glFqdn.globalToLocalMap[gFqdn] = []LocalFqdn{
			{Cluster: cname, Fqdn: lFqdn},
		}
		glFqdn.localToGlobalFqdnList.AddUpdateFqdnMapping(gFqdn, lFqdn, cname)
		return
	}
	glFqdn.globalToLocalMap[gFqdn] = append(glFqdn.globalToLocalMap[gFqdn],
		LocalFqdn{
			Cluster: cname,
			Fqdn:    lFqdn,
		})
	glFqdn.localToGlobalFqdnList.AddUpdateFqdnMapping(gFqdn, lFqdn, cname)
}

func (glFqdn *globalToLocalFqdn) getLocalFqdnIdx(gFqdn, lFqdn, cname string) (int, error) {
	glFqdn.lock.RLock()
	glFqdn.lock.RUnlock()

	lFqdnList, ok := glFqdn.globalToLocalMap[gFqdn]
	if !ok {
		return 0, fmt.Errorf("no entries for global fqdn %s", gFqdn)
	}
	for idx, lFqdnObj := range lFqdnList {
		if lFqdnObj.Cluster == cname && lFqdnObj.Fqdn == lFqdn {
			return idx, nil
		}
	}
	return 0, fmt.Errorf("local fqdn %s for cluster %s not found for global fqdn %s", lFqdn, cname,
		gFqdn)
}

func (glFqdn *globalToLocalFqdn) deleteFromFqdnMappingIdx(gFqdn string, idx int) {
	glFqdn.lock.Lock()
	defer glFqdn.lock.Unlock()
	localFqdn := glFqdn.globalToLocalMap[gFqdn][idx]
	glFqdn.globalToLocalMap[gFqdn] = append(glFqdn.globalToLocalMap[gFqdn][:idx], glFqdn.globalToLocalMap[gFqdn][idx+1:]...)
	if len(glFqdn.globalToLocalMap[gFqdn]) == 0 {
		// delete the key
		delete(glFqdn.globalToLocalMap, gFqdn)
	}
	glFqdn.localToGlobalFqdnList.DeleteFqdn(localFqdn.Cluster, localFqdn.Fqdn)
}

func (glFqdn *globalToLocalFqdn) DeleteFromFqdnMapping(gFqdn, lFqdn, cname string) {
	idx, err := glFqdn.getLocalFqdnIdx(gFqdn, lFqdn, cname)
	if err != nil {
		Warnf("gFqdn: %s, cluster: %s, lFqdn: %s, msg: error in deleting fqdn mapping: %v",
			gFqdn, cname, lFqdn, err)
		return
	}
	glFqdn.deleteFromFqdnMappingIdx(gFqdn, idx)
}

func (glFqdn *globalToLocalFqdn) GetLocalFqdnsForGlobalFqdn(gFqdn string) ([]LocalFqdn, error) {
	glFqdn.lock.RLock()
	defer glFqdn.lock.RUnlock()

	fqdnList, ok := glFqdn.globalToLocalMap[gFqdn]
	if !ok {
		return []LocalFqdn{}, fmt.Errorf("no local fqdns for gFqdn %s", gFqdn)
	}
	return fqdnList, nil
}

func (glFqdn *globalToLocalFqdn) GetGlobalFqdnForLocalFqdn(cname, lFqdn string) (string, error) {
	glFqdn.lock.RLock()
	defer glFqdn.lock.RUnlock()

	gFqdn, err := glFqdn.localToGlobalFqdnList.GetGlobalFqdnFor(cname, lFqdn)
	if err != nil {
		return "", fmt.Errorf("error in fetching global fqdn: %v", err)
	}
	return gFqdn, nil
}

type LocalToGlobalFqdn struct {
	localToGlobalMap map[string]string
}

func (lgFqdn *LocalToGlobalFqdn) AddUpdateFqdnMapping(gsFqdn, lFqdn, cname string) {
	key := cname + "/" + lFqdn
	lgFqdn.localToGlobalMap[key] = gsFqdn
}

func (lgFqdn *LocalToGlobalFqdn) DeleteFqdn(cname string, lFqdn string) {
	key := cname + "/" + lFqdn
	delete(lgFqdn.localToGlobalMap, key)
}

func (lgFqdn *LocalToGlobalFqdn) GetGlobalFqdnFor(cname string, lFqdn string) (string, error) {
	key := cname + "/" + lFqdn
	gFqdn, ok := lgFqdn.localToGlobalMap[key]
	if !ok {
		return "", fmt.Errorf("no gFqdn for lFqdn %s", lFqdn)
	}
	return gFqdn, nil
}
