/*
 * Copyright 2020-2021 VMware, Inc.
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
	"sort"
	"strconv"
	"sync"

	gslbhralphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

type GSHostRules struct {
	GSFqdn            string
	HmRefs            []string
	SitePersistence   *gslbhralphav1.SitePersistence
	TTL               *int
	TrafficSplit      []gslbhralphav1.TrafficSplitElem
	ThirdPartyMembers []gslbhralphav1.ThirdPartyMember
	Checksum          uint32
	Lock              sync.RWMutex
}

func (in *GSHostRules) DeepCopyInto(out *GSHostRules) {
	in.Lock.RLock()
	defer in.Lock.RUnlock()
	*out = *in

	if in.TTL != nil {
		in, out := &in.TTL, &out.TTL
		*out = new(int)
		**out = **in
	}
	if in.SitePersistence != nil {
		in, out := &in.SitePersistence, &out.SitePersistence
		*out = new(gslbhralphav1.SitePersistence)
		**out = **in
	}
	if in.ThirdPartyMembers != nil {
		in, out := &in.ThirdPartyMembers, &out.ThirdPartyMembers
		*out = make([]gslbhralphav1.ThirdPartyMember, len(*in))
		copy(*out, *in)
	}
	if in.HmRefs != nil {
		in, out := &in.HmRefs, &out.HmRefs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.TrafficSplit != nil {
		in, out := &in.TrafficSplit, &out.TrafficSplit
		*out = make([]gslbhralphav1.TrafficSplitElem, len((*in)))
		copy(*out, *in)
	}
}

func (ghr *GSHostRules) CalculateAndSetChecksum() {
	ghr.Lock.Lock()
	defer ghr.Lock.Unlock()

	var cksum uint32
	var sitePersistence string
	var ttl int
	if ghr.SitePersistence != nil {
		cksum += utils.Hash(utils.Stringify(ghr.SitePersistence.Enabled)) +
			utils.Hash(utils.Stringify(ghr.SitePersistence.ProfileRef))
	}
	if ghr.TTL != nil {
		ttl = *ghr.TTL
	}

	clusterWeights := []string{}
	for _, c := range ghr.TrafficSplit {
		weight := strconv.Itoa(int(c.Weight))
		clusterWeights = append(clusterWeights, c.Cluster+weight)
	}
	sort.Strings(clusterWeights)
	thirdPartyMembers := []string{}
	for _, tp := range ghr.ThirdPartyMembers {
		thirdPartyMembers = append(thirdPartyMembers, tp.Site+tp.VIP)
	}
	sort.Strings(thirdPartyMembers)

	cksum += utils.Hash(utils.Stringify(ghr.HmRefs)) +
		utils.Hash(sitePersistence) +
		utils.Hash(utils.Stringify(ttl)) +
		utils.Hash(utils.Stringify(clusterWeights)) +
		utils.Hash(utils.Stringify(thirdPartyMembers))

	ghr.Checksum = cksum
}

func (ghr *GSHostRules) GetChecksum() uint32 {
	ghr.Lock.RLock()
	defer ghr.Lock.RUnlock()

	return ghr.Checksum
}

// GetGSHostRuleForGSLBHr parses a GSLB HostRule object and returns a GSHostRules struct
func GetGSHostRuleForGSLBHR(gslbhr *gslbhralphav1.GSLBHostRule) *GSHostRules {
	gslbhrSpec := gslbhr.Spec.DeepCopy()
	gsHostRules := GSHostRules{
		GSFqdn: gslbhrSpec.Fqdn,
	}
	if gslbhrSpec.SitePersistence != nil {
		gsHostRules.SitePersistence = &gslbhralphav1.SitePersistence{
			Enabled:    gslbhrSpec.SitePersistence.Enabled,
			ProfileRef: gslbhr.Spec.SitePersistence.ProfileRef,
		}
	}
	if gslbhrSpec.TTL != nil {
		ttl := *gslbhrSpec.TTL
		gsHostRules.TTL = &ttl
	}
	gsHostRules.ThirdPartyMembers = make([]gslbhralphav1.ThirdPartyMember, len(gslbhrSpec.ThirdPartyMembers))
	copy(gsHostRules.ThirdPartyMembers, gslbhrSpec.ThirdPartyMembers)
	gsHostRules.TrafficSplit = make([]gslbhralphav1.TrafficSplitElem, len(gslbhrSpec.TrafficSplit))
	copy(gsHostRules.TrafficSplit, gslbhrSpec.TrafficSplit)
	gsHostRules.HmRefs = make([]string, len(gslbhrSpec.HealthMonitorRefs))
	copy(gsHostRules.HmRefs, gslbhrSpec.HealthMonitorRefs)
	gsHostRules.CalculateAndSetChecksum()
	return &gsHostRules
}

type GSFqdnHostRules struct {
	GSHostRuleList map[string]*GSHostRules
	GlobalLock     sync.RWMutex
}

var gsFqdnHostRules *GSFqdnHostRules
var ghrSyncOnce sync.Once

func GetGSHostRulesList() *GSFqdnHostRules {
	ghrSyncOnce.Do(func() {
		hostRules := make(map[string]*GSHostRules)
		gsFqdnHostRules = &GSFqdnHostRules{GSHostRuleList: hostRules}
	})
	return gsFqdnHostRules
}

func (ghrules *GSFqdnHostRules) GetGSHostRulesForFQDN(gsFqdn string) *GSHostRules {
	ghrules.GlobalLock.RLock()
	defer ghrules.GlobalLock.RUnlock()

	if rules, ok := ghrules.GSHostRuleList[gsFqdn]; ok {
		return rules
	}
	return nil
}

func (ghrules *GSFqdnHostRules) BuildAndSetGSHostRulesForFQDN(gslbhr *gslbhralphav1.GSLBHostRule) {
	newObj := GetGSHostRuleForGSLBHR(gslbhr)
	ghrules.GlobalLock.Lock()
	defer ghrules.GlobalLock.Unlock()

	ghrules.GSHostRuleList[gslbhr.Spec.Fqdn] = newObj
}

func (ghrules *GSFqdnHostRules) SetGSHostRulesForFQDN(ghr *GSHostRules) {
	ghrules.GlobalLock.Lock()
	defer ghrules.GlobalLock.Unlock()

	ghrules.GSHostRuleList[ghr.GSFqdn] = ghr
}

func (ghrules *GSFqdnHostRules) DeleteGSHostRulesForFQDN(fqdn string) {
	ghrules.GlobalLock.Lock()
	defer ghrules.GlobalLock.Unlock()

	delete(ghrules.GSHostRuleList, fqdn)
}
