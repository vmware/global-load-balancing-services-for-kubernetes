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

package nodes

import (
	"strings"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
)

// getSitePersistence returns the applicable site persistence for a GS object. Three conditions:
// 1. GSLBHostRule contains site persistence, but it is disabled:
//    Regardless of what's in the GDP object, we disable Site Persistence on this GS object.
// 2. GSLBHostRule contains site persistence, it is enabled and a profile ref is given:
//    We enable Site Persistence on the GS object and set the provided ref as the persistence ref.
// 3. GSLBHostRule doesn't contain Site Persistence, we inherit the Site Persistence properties from
//    the Global filter (GDP object).
func getSitePersistence(gsRuleExists bool, gsRule *gslbutils.GSHostRules, gf *gslbutils.GlobalFilter) *string {
	if gsRuleExists && gsRule.SitePersistence != nil {
		if gsRule.SitePersistence.Enabled {
			ref := gsRule.SitePersistence.ProfileRef
			return &ref
		} else {
			return nil
		}
	}
	return gf.GetSitePersistence()
}

func setGSLBPropertiesForGS(gsFqdn string, gsGraph *AviGSObjectGraph, newObj bool, tls bool) {
	gf := gslbutils.GetGlobalFilter()
	// check if a GSLB Host Rule has been defined for this fqdn (gsName)
	gsHostRuleList := gslbutils.GetGSHostRulesList()
	var gsRule gslbutils.GSHostRules
	var gsRuleExists bool

	gsGraph.DomainNames = []string{gsFqdn}
	if ghRulesForFqdn := gsHostRuleList.GetGSHostRulesForFQDN(gsFqdn); ghRulesForFqdn != nil {
		ghRulesForFqdn.DeepCopyInto(&gsRule)
		gsRuleExists = true
	}

	if gsRuleExists && gsRule.TTL != nil {
		gsGraph.TTL = gsRule.TTL
	} else {
		gsGraph.TTL = gf.GetTTL()
	}

	if gsRuleExists && gsRule.HmRefs != nil && len(gsRule.HmRefs) != 0 {
		gsGraph.HmRefs = make([]string, len(gsRule.HmRefs))
		copy(gsGraph.HmRefs, gsRule.HmRefs)
		// set the previous path based health monitor(s) to empty
		gsGraph.Hm = HealthMonitor{}
	} else if gfHmRefs := gf.GetAviHmRefs(); len(gfHmRefs) != 0 {
		gsGraph.HmRefs = gfHmRefs
		gsGraph.Hm = HealthMonitor{}
	} else {
		gsGraph.HmRefs = nil
	}

	if tls {
		gsGraph.SitePersistenceRef = getSitePersistence(gsRuleExists, &gsRule, gf)
	}

	if gsRuleExists && gsRule.ThirdPartyMembers != nil && len(gsRule.ThirdPartyMembers) != 0 {
		if newObj {
			for _, tpm := range gsRule.ThirdPartyMembers {
				memberObj := AviGSK8sObj{
					ObjType:     gslbutils.ThirdPartyMemberType,
					IPAddr:      tpm.VIP,
					Name:        tpm.Site,
					SyncVIPOnly: true,
				}
				// weight of a third party member is decided only by a GSLBHostRule, and not
				// by the GDP object. So, if there's no weight given in the GSLBHostRule, the
				// default weight of 1 is provided.
				gsGraph.MemberObjs = append(gsGraph.MemberObjs, memberObj)
			}
		} else {
			// we have to update the 3rd party members
			updateThirdPartyMembers(gsGraph, gsRule.ThirdPartyMembers)
		}
	}
	weightMap := make(map[string]int32)
	for _, clusterWeight := range gsRule.TrafficSplit {
		weightMap[clusterWeight.Cluster] = int32(clusterWeight.Weight)
	}

	for idx, member := range gsGraph.MemberObjs {
		if member.ObjType == gslbutils.ThirdPartyMemberType {
			gsGraph.MemberObjs[idx].Weight = getThirdPartyMemberWeight(weightMap, member.Name)
		} else {
			gsGraph.MemberObjs[idx].Weight = getK8sMemberWeight(weightMap, member.Cluster, member.Namespace)
		}
	}
}

func getThirdPartyMemberWeight(weightMap map[string]int32, site string) int32 {
	if weight, ok := weightMap[site]; ok {
		return weight
	}
	return 1
}

func getK8sMemberWeight(ghrWeightMap map[string]int32, cname, ns string) int32 {
	if weight, ok := ghrWeightMap[cname]; ok {
		return weight
	}
	return GetObjTrafficRatio(ns, cname)
}

func updateThirdPartyMembers(gsGraph *AviGSObjectGraph, thirdPartyMembers []v1alpha1.ThirdPartyMember) {

	gslbutils.Logf("gs members before update: %v", gsGraph.MemberObjs)
	existingMembers := make(map[string]struct{})
	newMembers := make(map[string]bool)

	for _, tpm := range thirdPartyMembers {
		newMembers[tpm.Site+"/"+tpm.VIP] = false
	}
	// find any existing member which is supposed to be deleted
	for idx, member := range gsGraph.MemberObjs {
		if member.ObjType != gslbutils.ThirdPartyMemberType {
			continue
		}
		siteIP := member.Name + "/" + member.IPAddr
		existingMembers[siteIP] = struct{}{}

		if _, exists := newMembers[siteIP]; !exists {
			// delete this entry
			gsGraph.MemberObjs = append(gsGraph.MemberObjs[:idx], gsGraph.MemberObjs[idx+1:]...)
		} else {
			// true indicates that this new member is already present in the existing members list
			newMembers[siteIP] = true
		}
	}

	// find new members that need to be added
	for k, v := range newMembers {
		if v {
			continue
		}
		siteIP := strings.Split(k, "/")
		site, IP := siteIP[0], siteIP[1]
		memberObj := AviGSK8sObj{
			ObjType:     gslbutils.ThirdPartyMemberType,
			IPAddr:      IP,
			Name:        site,
			SyncVIPOnly: true,
		}
		gsGraph.MemberObjs = append(gsGraph.MemberObjs, memberObj)
	}
	gslbutils.Logf("gslb members: %v", gsGraph.MemberObjs)
}
