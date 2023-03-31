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

package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/vmware/alb-sdk/go/models"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"

	avictrl "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"
	gslbhralphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gslbcs "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	gslbhrscheme "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned/scheme"
	gslbinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions"
	gslbHostRuleListers "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/listers/amko/v1alpha1"

	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const (
	GslbHostRuleAccepted = "Accepted"
	GslbHostRuleRejected = "Rejected"
)

type AddDelGSLBHostRulefn func(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32)

type UpdateGSLBHostRulefn func(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32)

type GSLBHostRuleController struct {
	kubeclientset   kubernetes.Interface
	gslbhrclientset gslbcs.Interface
	gslbhrLister    gslbHostRuleListers.GSLBHostRuleLister
	gslbhrSynced    cache.InformerSynced
}

func (gslbHostRuleController *GSLBHostRuleController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	gslbutils.Logf("object: GSLBHostRuleController, msg: %s", "starting the workers")
	<-stopCh
	gslbutils.Logf("object: GSLBHostRuleController, msg: %s", "shutting down the workers")
	return nil
}

func updateGSLBHR(gslbhr *gslbhralphav1.GSLBHostRule, msg string, status string) {
	gslbhr.Status.Error = msg
	gslbhr.Status.Status = status
	obj, updateErr := gslbutils.AMKOControlConfig().GSLBClientset().AmkoV1alpha1().GSLBHostRules(gslbhr.ObjectMeta.Namespace).Update(context.TODO(), gslbhr, metav1.UpdateOptions{})
	if updateErr != nil {
		gslbutils.Errf("Error is updating the GSLBHostRules status object %v : %s", obj, updateErr)
	}
}

func isSitePersistenceRefPresentInCache(spName string) bool {
	aviSpCache := avictrl.GetAviSpCache()

	_, present := aviSpCache.AviSpCacheGet(avictrl.TenantName{Tenant: utils.ADMIN_NS, Name: spName})
	return present
}

func isSitePersistenceProfilePresent(profileName string, gdp bool, fullSync bool) bool {
	if fullSync && isSitePersistenceRefPresentInCache(profileName) {
		gslbutils.Debugf("site persistence ref %s present in site persistence cache", profileName)
		return true
	}
	// Check if the profile mentioned in gslbHostRule are present as application persistence profile on the gslb leader
	aviClient := avictrl.SharedAviClients().AviClient[0]
	uri := "api/applicationpersistenceprofile?name=" + profileName

	// for gdp objects, we need to infinitely retry
	result, err := gslbutils.GetUriFromAvi(uri, aviClient, gdp)
	if err != nil {
		gslbutils.Errf("Error getting uri %s from Avi : %s", uri, err)
		return false
	}
	if result.Count == 0 {
		gslbutils.Errf("Site Persistent Profile %s does not exist", profileName)
		return false
	}

	// if site persistence profile exists and is not present in the internal cache, add it
	elems := make([]json.RawMessage, result.Count)
	err = json.Unmarshal(result.Results, &elems)
	if err != nil {
		gslbutils.Errf("failed to unmarshal site persistence profile ref, err: %v", err.Error())
		return false
	}
	sp := models.ApplicationPersistenceProfile{}
	if err = json.Unmarshal(elems[0], &sp); err != nil {
		gslbutils.Errf("failed to unmarshal site persistence element, err: %v", err.Error())
		return false
	}
	if sp.Name == nil || sp.UUID == nil {
		gslbutils.Errf("incomplete site persistence ref unmarshalled %s", utils.Stringify(sp))
		return false
	}
	k := avictrl.TenantName{Tenant: utils.ADMIN_NS, Name: *sp.Name}
	spCache := avictrl.GetAviSpCache()
	spCache.AviSpCacheAdd(k, &sp)
	spCache.AviSpCacheAddByUUID(*sp.UUID, &sp)
	gslbutils.Debugf("sitePersistence: %s, msg: added site persistence to in memory cache", *sp.Name)
	return true
}

func isFallbackAlgorithmValid(fa *gslbhralphav1.GeoFallback) (bool, error) {
	switch fa.LBAlgorithm {
	case gslbhralphav1.PoolAlgorithmRoundRobin:
		if fa.HashMask != nil {
			return false, fmt.Errorf("hashMask is not allowed with %s as geoFallback algorithm", fa.LBAlgorithm)
		}
		return true, nil
	case gslbhralphav1.PoolAlgorithmConsistentHash:
		if fa.HashMask == nil {
			return false, fmt.Errorf("hashMask is required for %s as the geoFallback algorithm", fa.LBAlgorithm)
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported algorithm %s for geoFallback", fa.LBAlgorithm)
	}
}

// Check if the pool algorithm settings provided, are valid. Verification logic:
//  1. If nothing is specified, we return true, as that's the default case (and, RoundRobin will be
//     selected).
//  2. If "RoundRobin" or "Topology" algos are specfied, no other fields should be set, and we return
//     true.
//  3. If "ConsistentHash" is specified, we check if a valid mask is present, if yes, we return true.
//  4. If "Geo" is specified, there can be three conditions:
//     i) If the fallback algorithm is specified as "ConsistentHash", a valid "mask" must be specified.
//     ii) If the fallback algorithm is specified as "RoundRobin", this is a valid condition.
//     iii) No other algorithm can be added as the fallback algorithm.
func isGslbPoolAlgorithmValid(algoSettings *gslbhralphav1.PoolAlgorithmSettings) (bool, error) {
	if algoSettings == nil {
		return true, nil
	}
	switch algoSettings.LBAlgorithm {
	case gslbhralphav1.PoolAlgorithmRoundRobin, gslbhralphav1.PoolAlgorithmTopology:
		if algoSettings.FallbackAlgorithm != nil {
			return false, fmt.Errorf("geoFallback not allowed for %s", algoSettings.LBAlgorithm)
		}
		if algoSettings.HashMask != nil {
			return false, fmt.Errorf("hashMask not allowed for %s", algoSettings.LBAlgorithm)
		}
		return true, nil
	case gslbhralphav1.PoolAlgorithmConsistentHash:
		if algoSettings.FallbackAlgorithm != nil {
			return false, fmt.Errorf("geoFallback is not allowed with ConsistentHash")
		}
		if algoSettings.HashMask == nil {
			return false, fmt.Errorf("hashMask is required for ConsistentHash")
		}
		return true, nil
	case gslbhralphav1.PoolAlgorithmGeo:
		if algoSettings.FallbackAlgorithm == nil {
			return false, fmt.Errorf("geoFallback has to be set for Geo, available choices: RoundRobin, ConsistentHash")
		}
		if ok, err := isFallbackAlgorithmValid(algoSettings.FallbackAlgorithm); !ok {
			return false, err
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported lbAlgorithm %s", algoSettings.LBAlgorithm)
	}
}

func isHealthMonitorRefPresentInCache(hmName string) bool {
	aviHmCache := avictrl.GetAviHmCache()

	_, present := aviHmCache.AviHmCacheGet(avictrl.TenantName{Tenant: utils.ADMIN_NS, Name: hmName})
	return present
}

// Check if the health monitors mentioned in the GDP/GSLBHostRule objects are present in the
// hm cache for full sync. If not, check with the GSLB Leader node. If found, return true.
// If not, return false.
// For non-full sync cases, the hm ref will be fetched from the GSLB leader and verified. The HM
// cache won't be checked for such cases.
func isHealthMonitorRefValid(refName string, gdp bool, fullSync bool) bool {
	if fullSync && isHealthMonitorRefPresentInCache(refName) {
		gslbutils.Debugf("health monitor %s present in hm cache", refName)
		return true
	}
	hm, err := avictrl.GetHMFromName(refName, gdp)
	if err != nil {
		return false
	}
	if hm.IsFederated != nil && *hm.IsFederated {
		return true
	}
	gslbutils.Errf("health monitor ref %s is not federated, can't add", refName)
	return false
}

func isThirdPartyMemberSitePresent(gslbhr *gslbhralphav1.GSLBHostRule, siteName string) bool {
	// Verify the presence of the third party member sites on the gslb leader
	aviClient := avictrl.SharedAviClients().AviClient[0]
	uri := "api/gslb"
	result, err := gslbutils.GetUriFromAvi(uri, aviClient, false)
	if err != nil {
		gslbutils.Errf("Error in getting uri %s from Avi: %v", uri, err)
		return false
	}
	elems := make([]json.RawMessage, result.Count)
	err = json.Unmarshal(result.Results, &elems)
	if err != nil {
		gslbutils.Errf("Failed to unmarshal GSLB data, err: %v", err)
	}
	for _, elem := range elems {
		gslb := models.Gslb{}
		err = json.Unmarshal(elem, &gslb)
		if err != nil {
			gslbutils.Errf("Failed to unmarshal GSLB data, err: %v", err)
		}
		tpms := gslb.ThirdPartySites
		for _, tpm := range tpms {
			if *tpm.Name == siteName {
				return true
			}
		}
	}
	gslbutils.Errf("Third Party Member Site %s does not exist", siteName)
	return false
}

func isGSLBDownResponseValid(responseType string, fallbackIP string) error {
	switch responseType {
	case gslbhralphav1.GSLBServiceDownResponseFallbackIP:
		if fallbackIP == "" {
			return fmt.Errorf("Fallback IP is required for %s", responseType)
		}
		if net.ParseIP(fallbackIP) == nil {
			return fmt.Errorf("Fallback IP %s is not valid", fallbackIP)
		}
	default:
		if fallbackIP != "" {
			return fmt.Errorf("Fallback IP is not allowed for %s", responseType)
		}
	}
	return nil
}

func ValidateGSLBHostRule(gslbhr *gslbhralphav1.GSLBHostRule, fullSync bool) error {
	gslbhrName := gslbhr.ObjectMeta.Name
	gslbhrSpec := gslbhr.Spec
	var errmsg string
	if gslbhrSpec.Fqdn == "" {
		errmsg = "GSFqdn missing for " + gslbhrName + " GSLBHostRule"
		return fmt.Errorf(errmsg)
	}

	// There are 3 conditions for site persistence:
	// 1. Site persistence is enabled, a ref has to be given: this applies on the respective GSLBService,
	//    provided the ref exists on the controller.
	// 2. Site persistence is disabled, site persistence will be disabled on the GSLBService, regardless of
	//    what the GDP object may contain.
	// 3. Site persistence is not provided, the site persistence from the GDP object will be applied on the
	//    GSLBService.
	sitePersistence := gslbhrSpec.SitePersistence
	if sitePersistence != nil {
		sitePersistenceProfileName := sitePersistence.ProfileRef
		if sitePersistence.Enabled && !isSitePersistenceProfilePresent(sitePersistenceProfileName, false, fullSync) {
			errmsg = "SitePersistence Profile " + sitePersistenceProfileName + " error for " + gslbhrName + " GSLBHostRule"
			return fmt.Errorf(errmsg)
		}
	}

	if ok, err := isGslbPoolAlgorithmValid(gslbhrSpec.PoolAlgorithmSettings); !ok {
		errmsg := "Invalid Pool Algorithm: " + err.Error()
		updateGSLBHR(gslbhr, errmsg, GslbHostRuleRejected)
		return fmt.Errorf(errmsg)
	}

	thirdPartyMembers := gslbhrSpec.ThirdPartyMembers
	for _, tpmember := range thirdPartyMembers {
		if vip := net.ParseIP(tpmember.VIP); vip == nil {
			errmsg := "Invalid VIP for thirdPartyMember site " + tpmember.Site + "," + gslbhrName + " GSLBHostRule (expecting IP address)"
			return fmt.Errorf(errmsg)
		}
		if !isThirdPartyMemberSitePresent(gslbhr, tpmember.Site) {
			errmsg = "ThirdPartyMember site " + tpmember.Site + " does not exist for " + gslbhrName + " GSLBHostRule"
			return fmt.Errorf(errmsg)
		}
	}
	// PublicIP checks
	for _, ip := range gslbhrSpec.PublicIP {
		if !gslbutils.IsClusterContextPresent(ip.Cluster) {
			errmsg := "cluster " + ip.Cluster + " in Public IP  not present in GSLBConfig"
			return fmt.Errorf(errmsg)
		}
		if net.ParseIP(ip.IP) == nil {
			errmsg := "Invalid IP for site " + ip.Cluster + "," + gslbhrName + " GSLBHostRule (expecting IP address)"
			return fmt.Errorf(errmsg)
		}
	}
	// HM template and reference cannot be specified together
	if gslbhrSpec.HealthMonitorTemplate != nil &&
		len(gslbhrSpec.HealthMonitorRefs) != 0 {
		return fmt.Errorf("health monitor reference and template cannot be specified together for %s GSLBHostRule", gslbhrName)
	}

	if gslbhrSpec.HealthMonitorTemplate != nil {
		if err := validateAndAddHmTemplateToCache(*gslbhrSpec.HealthMonitorTemplate, false, fullSync); err != nil {
			return err
		}
	} else {
		healthMonitorRefs := gslbhrSpec.HealthMonitorRefs
		for _, ref := range healthMonitorRefs {
			if !isHealthMonitorRefValid(ref, false, fullSync) {
				errmsg = "Health Monitor Ref " + ref + " error for " + gslbhrName + " GSLBHostRule"
				return fmt.Errorf(errmsg)
			}
		}
	}

	// DownResponse check
	if gslbhrSpec.DownResponse != nil {
		err := isGSLBDownResponseValid(gslbhrSpec.DownResponse.Type, gslbhrSpec.DownResponse.FallbackIP)
		if err != nil {
			return err
		}
	}

	return nil
}

func AddGSLBHostRuleObj(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	gslbhr, ok := obj.(*gslbhralphav1.GSLBHostRule)
	if !ok {
		gslbutils.Errf("object added is not of type GSLB Host Rule")
		return
	}

	// Validate GSLBHostRule fields
	gsFqdn := gslbhr.Spec.Fqdn
	err := ValidateGSLBHostRule(gslbhr, false)
	if err != nil {
		updateGSLBHR(gslbhr, err.Error(), GslbHostRuleRejected)
		gslbutils.Errf("Error in accepting GSLB Host Rule %s : %s", gsFqdn, err.Error())
		return
	}

	gsHostRulesList := gslbutils.GetGSHostRulesList()
	gsFqdnHostRules := gsHostRulesList.GetGSHostRulesForFQDN(gslbhr.Spec.Fqdn)
	if gsFqdnHostRules == nil {
		// no GSLBHostRule exists for this FQDN, add a new one
		gsHostRulesList.BuildAndSetGSHostRulesForFQDN(gslbhr)
	} else {
		// there's an existing GSLBHostRule for this FQDN, reject this
		updateGSLBHR(gslbhr, "there's an existing GSLBHostRule for the same FQDN", GslbHostRuleRejected)
		return
	}
	updateGSLBHR(gslbhr, "", GslbHostRuleAccepted)
	gslbutils.Logf("ns: %s, gslbhostrule: %s, msg: %s", gslbhr.ObjectMeta.Namespace, gslbhr.ObjectMeta.Name,
		"GSLBHostRule object added")
	// push the gsFqdn key to graph layer
	bkt := utils.Bkt(gsFqdn, numWorkers)
	key := gslbutils.GSFQDNKey(gslbutils.ObjectAdd, gslbutils.GSFQDNType, gsFqdn)
	k8swq[bkt].AddRateLimited(key)
	gslbutils.Logf("ns: %s, gsFqdn: %s, key: %s, msg: pushed ADD key",
		gslbhr.ObjectMeta.Namespace, gsFqdn, key)
}

func handleGSLBHostRuleFQDNUpdate(oldGslbhr, newGslbhr *gslbhralphav1.GSLBHostRule, k8swq []workqueue.RateLimitingInterface,
	numWorkers uint32, gsHostRulesList *gslbutils.GSFqdnHostRules) {

	gslbutils.Logf("ns: %s, gslbHostRule: %s, gsFqdn: %s, msg: fqdn changed from %s -> %s",
		newGslbhr.Namespace, newGslbhr.Name, newGslbhr.Spec.Fqdn, oldGslbhr.Spec.Fqdn, newGslbhr.Spec.Fqdn)
	// fqdn has been changed, so we need to delete the older gslb hr mapping
	// however, before deleting the older mapping, need to check if the older GSLB HR
	// was accepted or rejected. If rejected, no need to delete the previous GSLB HR for
	// this FQDN. If accepted, delete the GSLB HR for the old fqdn.
	if oldGslbhr.Status.Status == GslbHostRuleAccepted {
		gslbutils.Logf("ns: %s, gslbHostRule: %s, gsFqdn: %s, msg: deleted entry for GS Host Rules",
			oldGslbhr.Namespace, oldGslbhr.Name, oldGslbhr.Spec.Fqdn)
		gsHostRulesList.DeleteGSHostRulesForFQDN(oldGslbhr.Spec.Fqdn)
		// push the old gsFqdn key to graph layer
		bkt := utils.Bkt(oldGslbhr.Spec.Fqdn, numWorkers)
		key := gslbutils.GSFQDNKey(gslbutils.ObjectDelete, gslbutils.GSFQDNType, oldGslbhr.Spec.Fqdn)
		k8swq[bkt].AddRateLimited(key)
		gslbutils.Logf("ns: %s, gsFqdn: %s, key: %s, msg: pushed DELETE key",
			oldGslbhr.ObjectMeta.Namespace, oldGslbhr.Spec.Fqdn, key)
	}

	// Add the hostrules for the new gs fqdn
	gsHostRulesList.BuildAndSetGSHostRulesForFQDN(newGslbhr)
	updateGSLBHR(newGslbhr, "", GslbHostRuleAccepted)
	gslbutils.Logf("ns: %s, gslbHostRule: %s, gsFqdn: %s, msg: accepted", newGslbhr.Namespace,
		newGslbhr.Name, newGslbhr.Spec.Fqdn)
	// push the new gsFqdn key to graph layer
	bkt := utils.Bkt(newGslbhr.Spec.Fqdn, numWorkers)
	key := gslbutils.GSFQDNKey(gslbutils.ObjectAdd, gslbutils.GSFQDNType, newGslbhr.Spec.Fqdn)
	k8swq[bkt].AddRateLimited(key)
	gslbutils.Logf("ns: %s, gsFqdn: %s, key: %s, msg: pushed ADD key", newGslbhr.ObjectMeta.Namespace,
		newGslbhr.Spec.Fqdn, key)
}

func UpdateGSLBHostRuleObj(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	oldGslbhr := old.(*gslbhralphav1.GSLBHostRule)
	newGslbhr := new.(*gslbhralphav1.GSLBHostRule)

	// Return if there's no change in the object
	if oldGslbhr.ObjectMeta.ResourceVersion == newGslbhr.ObjectMeta.ResourceVersion {
		return
	}

	// Validate GSLBHostRule
	err := ValidateGSLBHostRule(newGslbhr, false)
	if err != nil {
		updateGSLBHR(newGslbhr, err.Error(), GslbHostRuleRejected)
		gslbutils.Errf("Error in accepting GSLB Host Rule %s : %s", newGslbhr.ObjectMeta.Name, err.Error())
		// check if previous GSLB host rule was accepted, if yes, we need to add a delete key if this new
		// object is rejected
		if oldGslbhr.Status.Status == GslbHostRuleAccepted {
			deleteObjsForGSHostRule(oldGslbhr, k8swq, numWorkers)
		}
		return
	}
	gsHostRulesList := gslbutils.GetGSHostRulesList()

	// Handle the case where the fqdn has been changed in the GSLB HostRule.
	if oldGslbhr.Spec.Fqdn != newGslbhr.Spec.Fqdn {
		handleGSLBHostRuleFQDNUpdate(oldGslbhr, newGslbhr, k8swq, numWorkers, gsHostRulesList)
		return
	}

	// case where the update is for the same GS FQDN
	oldRulesForFqdn := gsHostRulesList.GetGSHostRulesForFQDN(newGslbhr.Spec.Fqdn)
	newRulesForFqdn := gslbutils.GetGSHostRuleForGSLBHR(newGslbhr)
	if oldRulesForFqdn != nil && oldRulesForFqdn.GetChecksum() == newRulesForFqdn.GetChecksum() {
		updateGSLBHR(newGslbhr, "", GslbHostRuleAccepted)
		gslbutils.Logf("ns: %s, gsFqdn: %s, msg: GSLB Host Rule unchanged", newGslbhr.Namespace,
			newGslbhr.Spec.Fqdn)
		return
	}

	// just set the updated HostRules for this gs fqdn
	gsHostRulesList.SetGSHostRulesForFQDN(newRulesForFqdn)

	updateGSLBHR(newGslbhr, "", GslbHostRuleAccepted)
	gslbutils.Logf("ns: %s, gslbHostRule: %s, gsFqdn: %s, msg: GSLB Host Rule updated", newGslbhr.Namespace,
		newGslbhr.Name, newGslbhr.Spec.Fqdn)
	// push the gs fqdn key
	bkt := utils.Bkt(newGslbhr.Spec.Fqdn, numWorkers)
	key := gslbutils.GSFQDNKey(gslbutils.ObjectUpdate, gslbutils.GSFQDNType, newGslbhr.Spec.Fqdn)
	k8swq[bkt].AddRateLimited(key)
	gslbutils.Logf("ns: %s, gsFqdn: %s, key: %s, msg: pushed UPDATE key",
		newGslbhr.ObjectMeta.Namespace, newGslbhr.Spec.Fqdn, key)
}

func DeleteGSLBHostRuleObj(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	gslbhr := obj.(*gslbhralphav1.GSLBHostRule)

	// check if the GSLB Host Rule was previously rejected
	if gslbhr.Status.Status == GslbHostRuleRejected {
		return
	}
	// if previously accepted, we have to remove it's entry from the gslb host rule list
	gsHostRuleList := gslbutils.GetGSHostRulesList()
	gsHostRuleList.DeleteGSHostRulesForFQDN(gslbhr.Spec.Fqdn)
	gslbutils.Logf("ns: %s, gslbHostRule: %s, gsFqdn: %s, msg: GSLB Host Rule deleted for fqdn",
		gslbhr.Namespace, gslbhr.Name, gslbhr.Spec.Fqdn)
	// push the delete key for this fqdn
	bkt := utils.Bkt(gslbhr.Spec.Fqdn, numWorkers)
	key := gslbutils.GSFQDNKey(gslbutils.ObjectDelete, gslbutils.GSFQDNType, gslbhr.Spec.Fqdn)
	k8swq[bkt].AddRateLimited(key)
	gslbutils.Logf("ns: %s, gsFqdn: %s, key: %s, msg: pushed DELETE key",
		gslbhr.ObjectMeta.Namespace, gslbhr.Spec.Fqdn, key)
}

func deleteObjsForGSHostRule(gslbhr *gslbhralphav1.GSLBHostRule, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
	gsHostRuleList := gslbutils.GetGSHostRulesList()
	gsHostRuleList.DeleteGSHostRulesForFQDN(gslbhr.Spec.Fqdn)
	gslbutils.Logf("ns: %s, gslbHostRule: %s, gsFqdn: %s, msg: GSLB Host Rule deleted for fqdn",
		gslbhr.Namespace, gslbhr.Name, gslbhr.Spec.Fqdn)
	// push the delete key for this fqdn
	bkt := utils.Bkt(gslbhr.Spec.Fqdn, numWorkers)
	key := gslbutils.GSFQDNKey(gslbutils.ObjectDelete, gslbutils.GSFQDNType, gslbhr.Spec.Fqdn)
	k8swq[bkt].AddRateLimited(key)
	gslbutils.Logf("ns: %s, gsFqdn: %s, key: %s, msg: pushed DELETE key", gslbhr.ObjectMeta.Namespace,
		gslbhr.Spec.Fqdn, key)
}

func InitializeGSLBHostRuleController(kubeclientset kubernetes.Interface,
	gslbhrclientset gslbcs.Interface,
	gslbInformerFactory gslbinformers.SharedInformerFactory,
	AddGSLBHostRuleObj AddDelGSLBHostRulefn,
	UpdateGSLBHostRuleObj UpdateGSLBHostRulefn, DeleteGSLBHostRuleObj AddDelGSLBHostRulefn) *GSLBHostRuleController {

	gslbhrInformer := gslbInformerFactory.Amko().V1alpha1().GSLBHostRules()
	gslbhrscheme.AddToScheme(scheme.Scheme)
	gslbutils.Logf("object: GSLBHostRuleController, msg: %s", "creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(utils.AviLog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})

	gslbhrController := &GSLBHostRuleController{
		kubeclientset:   kubeclientset,
		gslbhrclientset: gslbhrclientset,
		gslbhrLister:    gslbhrInformer.Lister(),
		gslbhrSynced:    gslbhrInformer.Informer().HasSynced,
	}
	gslbutils.Logf("object: GSLBHostRuleController, msg: %s", "setting up event handlers")
	k8sQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)

	// Event handlers for GSLBHostRuleController change
	gslbhrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			AddGSLBHostRuleObj(obj, k8sQueue.Workqueue, k8sQueue.NumWorkers)
		},
		UpdateFunc: func(old, new interface{}) {
			UpdateGSLBHostRuleObj(old, new, k8sQueue.Workqueue, k8sQueue.NumWorkers)
		},
		DeleteFunc: func(obj interface{}) {
			DeleteGSLBHostRuleObj(obj, k8sQueue.Workqueue, k8sQueue.NumWorkers)
		},
	})

	return gslbhrController
}
