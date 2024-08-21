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

	"google.golang.org/protobuf/proto"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
)

// getSitePersistence returns the applicable site persistence for a GS object. Three conditions:
//  1. GSLBHostRule contains site persistence, but it is disabled:
//     Regardless of what's in the GDP object, we disable Site Persistence on this GS object.
//  2. GSLBHostRule contains site persistence, it is enabled and a profile ref is given:
//     We enable Site Persistence on the GS object and set the provided ref as the persistence ref.
//  3. GSLBHostRule doesn't contain Site Persistence, we inherit the Site Persistence properties from
//     the Global filter (GDP object).
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

func getPKIProfile(gsRuleExists bool, gsRule *gslbutils.GSHostRules, gf *gslbutils.GlobalFilter) *string {
	if gsRuleExists && gsRule.SitePersistence != nil {
		if gsRule.SitePersistence.Enabled {
			if gsRule.SitePersistence.PKIProfileRef != nil {
				ref := gsRule.SitePersistence.PKIProfileRef
				return ref
			}
		} else {
			return nil
		}
	}
	return gf.GetPKIProfile()
}

// getGslbPoolAlgorithm returns the applicable algorithn settings for a GS object. Two conditions:
// 1. If the GSLBHostRule has the pool algorithm settings defined, we return that.
// 2. If no settings defined in the GSLBHostRule (i.e., value is nil), we return the GDP object's settings.
func getGslbPoolAlgorithm(gsRuleExists bool, gsRule *gslbutils.GSHostRules, gf *gslbutils.GlobalFilter) *gslbalphav1.PoolAlgorithmSettings {
	if gsRuleExists && gsRule.GslbPoolAlgorithm != nil {
		return gsRule.GslbPoolAlgorithm
	}
	return gf.GetGslbPoolAlgorithm()
}

func setGSLBPropertiesForGS(gsFqdn string, gsGraph *AviGSObjectGraph, newObj bool, tls bool) {
	gf := gslbutils.GetGlobalFilter()
	// check if a GSLB Host Rule has been defined for this fqdn (gsName)
	gsHostRuleList := gslbutils.GetGSHostRulesList()
	var gsRule gslbutils.GSHostRules
	var gsRuleExists bool

	gsGraph.DomainNames = DeriveGSLBServiceDomainNames(gsFqdn)
	if ghRulesForFqdn := gsHostRuleList.GetGSHostRulesForFQDN(gsFqdn); ghRulesForFqdn != nil {
		ghRulesForFqdn.DeepCopyInto(&gsRule)
		gsRuleExists = true
	}

	if gsRuleExists && gsRule.TTL != nil {
		gsGraph.TTL = gsRule.TTL
	} else {
		gsGraph.TTL = gf.GetTTL()
	}

	gsGraph.HmTemplate = nil
	gsGraph.HmRefs = nil
	gsGraph.ControlPlaneHmOnly = false
	if gsRuleExists && gsRule.ControlPlaneHmOnly != nil {
		gsGraph.ControlPlaneHmOnly = *gsRule.ControlPlaneHmOnly
	} else if gf.GetControlPlaneHmOnlyFlag() != nil {
		gsGraph.ControlPlaneHmOnly = *gf.GetControlPlaneHmOnlyFlag()
	}
	if !gsGraph.ControlPlaneHmOnly {
		gslbutils.Logf(utils.Stringify(gsGraph.ControlPlaneHmOnly))
		if gsRuleExists && gsRule.HmRefs != nil && len(gsRule.HmRefs) != 0 {
			gsGraph.HmRefs = make([]string, len(gsRule.HmRefs))
			copy(gsGraph.HmRefs, gsRule.HmRefs)
			// set the previous path based health monitor(s) to empty
			gsGraph.Hm = HealthMonitor{}
		} else if gsRuleExists && gsRule.HmTemplate != nil {
			gsGraph.HmTemplate = proto.String(*gsRule.HmTemplate)
		} else if gfHmRefs := gf.GetAviHmRefs(); len(gfHmRefs) != 0 {
			gsGraph.HmRefs = gfHmRefs
			gsGraph.Hm = HealthMonitor{}
		} else if hmTemplate := gf.GetAviHmTemplate(); hmTemplate != nil {
			gsGraph.HmTemplate = proto.String(*hmTemplate)
		}
	} else {
		gsGraph.Hm = HealthMonitor{}
	}

	if tls {
		gsGraph.SitePersistenceRef = getSitePersistence(gsRuleExists, &gsRule, gf)
		gsGraph.PkiProfileRef = getPKIProfile(gsRuleExists, &gsRule, gf)
	}

	pa := getGslbPoolAlgorithm(gsRuleExists, &gsRule, gf)
	if pa == nil {
		defaultAlgo := gslbalphav1.PoolAlgorithmSettings{LBAlgorithm: gslbalphav1.PoolAlgorithmRoundRobin}
		gsGraph.GslbPoolAlgorithm = &defaultAlgo
	} else {
		gsGraph.GslbPoolAlgorithm = pa
	}

	if gsRuleExists && gsRule.ThirdPartyMembers != nil && len(gsRule.ThirdPartyMembers) != 0 {
		if newObj {
			for _, tpm := range gsRule.ThirdPartyMembers {
				memberObj := AviGSK8sObj{
					ObjType:     gslbutils.ThirdPartyMemberType,
					IPAddr:      tpm.VIP,
					Name:        tpm.Site,
					PublicIP:    tpm.PublicIP,
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
	weightMap := make(map[string]uint32)
	priorityMap := make(map[string]uint32)
	publicIPMap := make(map[string]string)
	for _, publicIP := range gsRule.PublicIP {
		publicIPMap[publicIP.Cluster] = publicIP.IP
	}
	for _, clusterWeight := range gsRule.TrafficSplit {
		weightMap[clusterWeight.Cluster] = uint32(clusterWeight.Weight)
		priorityMap[clusterWeight.Cluster] = uint32(clusterWeight.Priority)
	}

	for idx, member := range gsGraph.MemberObjs {
		if member.ObjType == gslbutils.ThirdPartyMemberType {
			gsGraph.MemberObjs[idx].Weight = getThirdPartyMemberWeight(weightMap, member.Name)
			gsGraph.MemberObjs[idx].Priority = getThirdPartyMemberPriority(priorityMap, member.Name)
		} else {
			gsGraph.MemberObjs[idx].Weight = getK8sMemberWeight(weightMap, member.Cluster, member.Namespace)
			gsGraph.MemberObjs[idx].Priority = getK8sMemberPriority(priorityMap, member.Cluster, member.Namespace)
			gsGraph.MemberObjs[idx].PublicIP = getMemberPublicIP(publicIPMap, member.Cluster)
		}
	}

	if gsRuleExists && gsRule.GslbDownResponse != nil {
		gsGraph.GslbDownResponse = gsRule.GslbDownResponse.DeepCopy()
	} else {
		gsGraph.GslbDownResponse = gf.GetDownResponse()
	}
}

func getMemberPublicIP(publicIPMap map[string]string, site string) string {
	if ip, ok := publicIPMap[site]; ok {
		return ip
	}
	return ""
}

func getThirdPartyMemberWeight(weightMap map[string]uint32, site string) uint32 {
	if weight, ok := weightMap[site]; ok {
		return weight
	}
	return 1
}

func getThirdPartyMemberPriority(priorityMap map[string]uint32, site string) uint32 {
	if priority, ok := priorityMap[site]; ok {
		return priority
	}
	return 1
}

func getK8sMemberWeight(ghrWeightMap map[string]uint32, cname, ns string) uint32 {
	if weight, ok := ghrWeightMap[cname]; ok {
		return weight
	}
	return GetObjTrafficRatio(ns, cname)
}

func getK8sMemberPriority(ghrPriorityMap map[string]uint32, cname, ns string) uint32 {
	if priority, ok := ghrPriorityMap[cname]; ok {
		return priority
	}
	return GetObjTrafficPriority(ns, cname)
}

func updateThirdPartyMembers(gsGraph *AviGSObjectGraph, thirdPartyMembers []v1alpha1.ThirdPartyMember) {

	gslbutils.Logf("gs members before update: %v", gsGraph.MemberObjs)
	existingMembers := make(map[string]struct{})
	newMembers := make(map[string]bool)

	for _, tpm := range thirdPartyMembers {
		newMembers[tpm.Site+"/"+tpm.VIP+"/"+tpm.PublicIP] = false
	}
	// find any existing member which is supposed to be deleted
	for idx, member := range gsGraph.MemberObjs {
		if member.ObjType != gslbutils.ThirdPartyMemberType {
			continue
		}
		siteIP := member.Name + "/" + member.IPAddr + "/" + member.PublicIP
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
		site, IP, PubIP := siteIP[0], siteIP[1], siteIP[2]
		memberObj := AviGSK8sObj{
			ObjType:     gslbutils.ThirdPartyMemberType,
			IPAddr:      IP,
			Name:        site,
			SyncVIPOnly: true,
			PublicIP:    PubIP,
		}
		gsGraph.MemberObjs = append(gsGraph.MemberObjs, memberObj)
	}
	gslbutils.Logf("gslb members: %v", gsGraph.MemberObjs)
}

func PresentInHealthMonitorPathList(key PathHealthMonitorDetails, pathHmList []PathHealthMonitorDetails) bool {
	for _, pathHM := range pathHmList {
		if pathHM.Path == key.Path && pathHM.IngressProtocol == key.IngressProtocol {
			return true
		}
	}
	return false
}

func GetDescriptionForPathHMName(hmName string, gsMeta *AviGSObjectGraph) string {
	for _, pathnames := range gsMeta.Hm.PathHM {
		if pathnames.Name == hmName {
			return pathnames.GetPathHMDescription(gsMeta.Name, gsMeta.HmTemplate)
		}
	}
	gslbutils.Warnf("hmName: %s, msg: cannot find description for health monitor", hmName)
	return ""
}

func GetPathFromHmDescription(hmName, hmDescription string) string {
	hmDescriptionSplit := strings.Split(hmDescription, ": ")
	if len(hmDescriptionSplit) != 5 &&
		len(hmDescriptionSplit) != 6 {
		gslbutils.Warnf("hmName: %s, msg: hm description - \"%s\" is malformed, expected a path based hm", hmName, hmDescription)
		return ""
	}
	hmPathField := strings.Split(hmDescriptionSplit[3], ",")
	hmPath := strings.Trim(hmPathField[0], " ")
	return hmPath
}

func GetTemplateFromHmDescription(hmName, hmDescription string) *string {
	hmDescriptionSplit := strings.Split(hmDescription, ": ")
	if len(hmDescriptionSplit) != 6 {
		gslbutils.Debugf("hmName: %s, msg: hm description - \"%s\" is malformed, hm is not created from template", hmName, hmDescription)
		return nil
	}
	hmTemplateField := strings.Split(hmDescriptionSplit[5], ": ")
	hmTemplate := strings.Trim(hmTemplateField[0], " ")
	return &hmTemplate
}

func GetPathHmNameList(hm HealthMonitor) []string {
	var hmNameList []string
	for _, path := range hm.PathHM {
		hmNameList = append(hmNameList, path.Name)
	}
	return hmNameList
}
