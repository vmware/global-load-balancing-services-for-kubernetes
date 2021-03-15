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
	"encoding/json"
	"fmt"
	"net"

	"github.com/avinetworks/sdk/go/models"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"

	avictrl "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"
	gslbhralphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gslbcs "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned"
	gslbhrscheme "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned/scheme"
	gslbinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/informers/externalversions"
	gslbHostRuleListers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/listers/amko/v1alpha1"

	"github.com/openshift/client-go/route/clientset/versioned/scheme"
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

type AddDelGSLBHostRulefn func(obj interface{})

type UpdateGSLBHostRulefn func(old, new interface{})

type GSLBHostRuleController struct {
	kubeclientset   kubernetes.Interface
	gslbhrclientset gslbcs.Interface
	gslbhrLister    gslbHostRuleListers.GSLBHostRuleLister
	gslbhrSynced    cache.InformerSynced
	workqueue       workqueue.RateLimitingInterface
	recorder        record.EventRecorder
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
	obj, updateErr := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBHostRules(gslbhr.ObjectMeta.Namespace).Update(gslbhr)
	if updateErr != nil {
		gslbutils.Errf("Error is updating the GSLBHostRules status object %v : %s", obj, updateErr)
	}
}

func isSitePersistenceProfilePresent(gslbhr *gslbhralphav1.GSLBHostRule, profileName string) bool {
	// Check if the profile mentioned in gslbHostRule are present as application persistence profile on the gslb leader
	aviClient := avictrl.SharedAviClients().AviClient[0]
	uri := "/api/applicationpersistenceprofile?name=" + profileName
	result, err := aviClient.AviSession.GetCollectionRaw(uri)
	if err != nil {
		gslbutils.Errf("Error getting Site Persistent Profile : %s", err)
		return false
	}
	if result.Count == 0 {
		gslbutils.Errf("Site Persistent Profile %s does not exist", profileName)
		return false
	}

	return true
}

func isHealthMonitorRefPresent(gslbhr *gslbhralphav1.GSLBHostRule, refName string) bool {
	// Check if the health monitors mentioned in gslbHostRule are present on the gslb leader
	aviClient := avictrl.SharedAviClients().AviClient[0]
	uri := "/api/healthmonitor?name=" + refName
	result, err := aviClient.AviSession.GetCollectionRaw(uri)
	if err != nil {
		gslbutils.Errf("Error getting Health Monitor Refs : %s", err)
		return false
	}
	if result.Count == 0 {
		gslbutils.Errf("Health Monitor %s does not exist", refName)
		return false
	}
	return true
}

func isThirdPartyMemberSitePresent(gslbhr *gslbhralphav1.GSLBHostRule, siteName string) bool {
	// Verify the presence of the third party member sites on the gslb leader
	aviClient := avictrl.SharedAviClients().AviClient[0]
	uri := "/api/gslb"
	result, err := aviClient.AviSession.GetCollectionRaw(uri)
	if err != nil {
		gslbutils.Errf("Error getting Third Party Member Site : %s", err)
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

func ValidateGSLBHostRule(gslbhr *gslbhralphav1.GSLBHostRule) error {
	gslbhrName := gslbhr.ObjectMeta.Name
	gslbhrSpec := gslbhr.Spec
	var errmsg string
	if gslbhrSpec.Fqdn == "" {
		errmsg = "GSFqdn missing for " + gslbhrName + " GSLBHostRule"
		updateGSLBHR(gslbhr, errmsg, GslbHostRuleRejected)
		return fmt.Errorf(errmsg)
	}

	sitePersistence := gslbhrSpec.SitePersistence

	sitePersistenceProfileName := sitePersistence.ProfileRef
	if sitePersistence.Enabled == true && isSitePersistenceProfilePresent(gslbhr, sitePersistenceProfileName) != true {
		errmsg = "SitePersistence Profile " + sitePersistenceProfileName + " error for " + gslbhrName + " GSLBHostRule"
		updateGSLBHR(gslbhr, errmsg, GslbHostRuleRejected)
		return fmt.Errorf(errmsg)
	}

	thirdPartyMembers := gslbhrSpec.ThirdPartyMembers
	for _, tpmember := range thirdPartyMembers {
		if vip := net.ParseIP(tpmember.VIP); vip == nil {
			errmsg := "Invalid VIP for thirdPartyMember site " + tpmember.Site + "," + gslbhrName + " GSLBHostRule (expecting IP address)"
			updateGSLBHR(gslbhr, errmsg, GslbHostRuleRejected)
			return fmt.Errorf(errmsg)
		}
		if isThirdPartyMemberSitePresent(gslbhr, tpmember.Site) != true {
			errmsg = "ThirdPartyMember site " + tpmember.Site + " does not exist for " + gslbhrName + " GSLBHostRule"
			updateGSLBHR(gslbhr, errmsg, GslbHostRuleRejected)
			return fmt.Errorf(errmsg)
		}
	}

	healthMonitorRefs := gslbhrSpec.HealthMonitorRefs
	for _, ref := range healthMonitorRefs {
		if isHealthMonitorRefPresent(gslbhr, ref) != true {
			errmsg = "Health Monitor Ref " + ref + " error for " + gslbhrName + " GSLBHostRule"
			updateGSLBHR(gslbhr, errmsg, GslbHostRuleRejected)
			return fmt.Errorf(errmsg)
		}
	}
	return nil
}

func AddGSLBHostRuleObj(obj interface{}) {
	gslbhr, ok := obj.(*gslbhralphav1.GSLBHostRule)
	if !ok {
		gslbutils.Errf("object added is not of type GSLB Host Rule")
		return
	}

	// GSLBHostRule for all other namespaces are rejected
	if gslbhr.ObjectMeta.Namespace != gslbutils.AVISystem {
		return
	}

	//Validate GSLBHostRule
	err := ValidateGSLBHostRule(gslbhr)
	if err != nil {
		gslbutils.Errf("Error in accepting GSLB Host Rule %s : %s", gslbhr.ObjectMeta.Name, err.Error())
		return
	}
	updateGSLBHR(gslbhr, "", GslbHostRuleAccepted)
	gslbutils.Logf("ns: %s, gslbhostrule: %s, msg: %s", gslbhr.ObjectMeta.Namespace, gslbhr.ObjectMeta.Name,
		"GSLBHostRule object added")
}

func UpdateGSLBHostRuleObj(old, new interface{}) {
	oldGslbhr := old.(*gslbhralphav1.GSLBHostRule)
	newGslbhr := new.(*gslbhralphav1.GSLBHostRule)

	// Return if there's no change in the object
	if oldGslbhr.ObjectMeta.ResourceVersion == newGslbhr.ObjectMeta.ResourceVersion {
		return
	}

	//Validate GSLBHostRule
	err := ValidateGSLBHostRule(newGslbhr)
	if err != nil {
		gslbutils.Errf("Error in accepting GSLB Host Rule %s : %s", newGslbhr.ObjectMeta.Name, err.Error())
		return
	}

	updateGSLBHR(newGslbhr, "", GslbHostRuleAccepted)
	gslbutils.Logf("GSLBHostRule %s updated", newGslbhr.ObjectMeta.Name)
}

func DeleteGSLBHostRuleObj(obj interface{}) {
	gslbhr := obj.(*gslbhralphav1.GSLBHostRule)

	gslbutils.Logf("GSLBHostRule %s deleted", gslbhr.ObjectMeta.Name)
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
	// Event handlers for GSLBHostRuleController change
	gslbhrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			AddGSLBHostRuleObj(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			UpdateGSLBHostRuleObj(old, new)
		},
		DeleteFunc: func(obj interface{}) {
			DeleteGSLBHostRuleObj(obj)
		},
	})

	return gslbhrController
}
