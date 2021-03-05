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
	"errors"
	"net"
	"reflect"
	"strconv"

	"github.com/avinetworks/sdk/go/models"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"

	avictrl "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"
	gslbhralphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gslbcs "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/clientset/versioned"
	gslbhrscheme "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/clientset/versioned/scheme"
	gslbinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/informers/externalversions"
	gslbHostRuleListers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/listers/amko/v1alpha1"

	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const (
	GSLBHostRuleSuccess = "success"
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

// func updateGSLBHostRuleList(gslbhr *gslbhralphav1.GSLBHostRule) {
// 	gslbHostRuleListers.GSLBHostRuleLister.GSLBHostRules(gslbhr.ObjectMeta.Namespace).Get()

// 	// gslbhr.Status.Status = msg
// 	// if !gslbutils.PublishGSLBHostRuleStatus {
// 	// 	return
// 	// }
// 	// obj, updateErr := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBHostRules(gslbhr.ObjectMeta.Namespace).Update(gslbhr)
// 	// if updateErr != nil {
// 	// 	gslbutils.Errf("Error in updating the GSLBHostRule status object %v: %s", obj, updateErr)
// 	// }
// }

func isSitePersistenceProfilePresent(profileName string) bool {
	aviClient := avictrl.SharedAviClients().AviClient[0]
	uri := "/api/applicationpersistenceprofile?name=" + profileName
	result, err := aviClient.AviSession.GetCollectionRaw(uri)
	if err != nil {
		gslbutils.Errf("Error getting Site Persistent Profiles")
		return false
	}
	if result.Count <= 0 {
		gslbutils.Errf("Site Persistent Profile " + profileName + " doesnot exist")
		return false
	}
	return true
}

func isHealthMonitorRefPresent(refName string) bool {
	aviClient := avictrl.SharedAviClients().AviClient[0]
	uri := "/api/healthmonitor?name=" + refName
	result, err := aviClient.AviSession.GetCollectionRaw(uri)
	if err != nil {
		gslbutils.Errf("Error getting Health Monitor Refs")
		return false
	}
	if result.Count <= 0 {
		gslbutils.Errf("Health Monitor " + refName + " doesnot exist")
		return false
	}
	return true
}

func isThirdPartyMemberSitePresent(siteName string) bool {
	aviClient := avictrl.SharedAviClients().AviClient[0]
	uri := "/api/gslb"
	result, err := aviClient.AviSession.GetCollectionRaw(uri)
	if err != nil {
		gslbutils.Errf("Error getting Health Monitor Refs")
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
		tpms := gslb.ThirdPartySites
		for _, tpm := range tpms {
			if *tpm.Name == siteName {
				return true
			}
		}
	}
	return false
}

func ValidateGSLBHostRule(gslbhr *gslbhralphav1.GSLBHostRule) error {
	gslbhrName := gslbhr.ObjectMeta.Name
	gslbhrSpec := gslbhr.Spec
	if gslbhrSpec.Fqdn == "" {
		return errors.New("GSFqdn missing for " + gslbhrName + " GSLBHostRule")
	}
	//TTL needs to be an integer
	if reflect.TypeOf(gslbhrSpec.TTL).Kind() != reflect.Int {
		return errors.New("value of TTL should an integer for " + gslbhrName + " GSLBHostRule")
	}

	sitePersistences := gslbhrSpec.SitePersistence
	for _, site := range sitePersistences {
		if reflect.TypeOf(site.Enabled).Kind() != reflect.Bool {
			return errors.New("Site Persistence enable value should be a bool for " + gslbhrName + " GSLBHostRule")
		}
		sitePersistenceProfileName := site.ProfileRef
		if site.Enabled == true && isSitePersistenceProfilePresent(sitePersistenceProfileName) != true {
			return errors.New("SitePersistence Profile " + sitePersistenceProfileName + " error for " + gslbhrName + " GSLBHostRule")
		}
	}

	thirdPartyMembers := gslbhrSpec.ThirdPartyMembers
	gslbutils.Logf("Verifying thirdPartyMembers!!!")
	for _, tpmember := range thirdPartyMembers {
		if vip := net.ParseIP(tpmember.VIP); vip == nil {
			return errors.New("Invalid VIP for thirdPartyMember site " + tpmember.Site + ", " + gslbhrName + " GSLBHostRule (expecting IP address")
		}
		if isThirdPartyMemberSitePresent(tpmember.Site) != true {
			return errors.New("ThirdPartyMember site " + tpmember.Site + " doesnot exist for " + gslbhrName + " GSLBHostRule")
		}
		gslbutils.Logf("Verified thirdPartyMembers!!!")
	}
	gslbutils.Logf("Verified thirdPartyMembers!!!")

	healthMonitorRefs := gslbhrSpec.HealthMonitorRefs
	for _, ref := range healthMonitorRefs {
		if isHealthMonitorRefPresent(ref) != true {
			return errors.New("Health Monitor Ref " + ref + " error for " + gslbhrName + " GSLBHostRule")
		}
	}

	for _, tp := range gslbhrSpec.TrafficSplit {
		if tp.Weight < 1 || tp.Weight > 20 {
			return errors.New("traffic weight " + strconv.Itoa(int(tp.Weight)) + " must be between 1 and 20")
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

	gslbutils.Logf("ns: %s, gslbhostrule: %s, msg: %s", gslbhr.ObjectMeta.Namespace, gslbhr.ObjectMeta.Name,
		"GSLBHostRule object added")
}

func UpdateGSLBHostRuleObj(old, new interface{}) {
	oldGslbhr := old.(*gslbhralphav1.GSLBHostRule)
	newGslbhr := new.(*gslbhralphav1.GSLBHostRule)
	if oldGslbhr.ObjectMeta.ResourceVersion == newGslbhr.ObjectMeta.ResourceVersion {
		return
	}
	if oldGslbhr.ObjectMeta.Namespace != newGslbhr.ObjectMeta.Namespace {
		gslbutils.Errf("Namespace of GSLBHostRule " + newGslbhr.ObjectMeta.Name + " changed")
		return
	}
	//Validate GSLBHostRule
	err := ValidateGSLBHostRule(newGslbhr)
	if err != nil {
		gslbutils.Errf("Error in accepting GSLB Host Rule %s : %s", newGslbhr.ObjectMeta.Name, err.Error())
		return
	}
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
		// workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "gslbhr"),
		//recorder:      recorder,
	}
	gslbutils.Logf("object: GSLBHRController, msg: %s", "setting up event handlers")
	// Event handlers for GSLBHR change
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
