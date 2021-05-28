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

package ingestion

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/apiserver"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"

	"github.com/golang/glog"
	oshiftclient "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/openshift/client-go/route/clientset/versioned/scheme"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gslbcs "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned"
	gslbscheme "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned/scheme"
	gslbinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/informers/externalversions"
	gslblisters "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/listers/amko/v1alpha1"

	gdpcs "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha2/clientset/versioned"
	gdpinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha2/informers/externalversions"
	corev1 "k8s.io/api/core/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	avicache "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"

	avirest "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/rest"
	aviretry "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/retry"

	hrcs "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	akoinformer "github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

const (
	BootupMsg              = "starting up amko"
	BootupSyncMsg          = "syncing all objects"
	BootupSyncEndMsg       = "synced all objects"
	AcceptedMsg            = "success: gslb config accepted"
	ControllerNotLeaderMsg = "error: controller not a leader"
	InvalidConfigMsg       = "error: invalid gslb config"
	EditRestartMsg         = "gslb config edited, amko needs a restart"
	AlreadySetMsg          = "error: can't add another gslbconfig"
	NoSecretMsg            = "error: secret object doesn't exist"
	KubeConfigErr          = "error: provided kubeconfig has an error"
	ControllerAPIErr       = "error: issue in connecting to the controller API"
	ClusterHealthCheckErr  = "error: cluster healthcheck failed, "
)

type KubeClusterDetails struct {
	clusterName string
	kubeconfig  string
	kubeapi     string
	informers   *utils.Informers
}

func GetNewKubeClusterDetails(clusterName, kubeConfig, kubeapi string, informers *utils.Informers) KubeClusterDetails {
	return KubeClusterDetails{
		clusterName: clusterName,
		kubeconfig:  kubeConfig,
		kubeapi:     kubeapi,
		informers:   informers,
	}
}

func (kc KubeClusterDetails) GetClusterContextName() string {
	return kc.clusterName
}

type K8SInformers struct {
	Cs kubernetes.Interface
}

type ClusterCache struct {
	clusterName string
}

type InitializeGSLBMemberClustersFn func(string, []gslbalphav1.MemberCluster) ([]*GSLBMemberController, error)
type GSLBConfigAddfn func(obj interface{}, f InitializeGSLBMemberClustersFn)

var (
	masterURL         string
	kubeConfig        string
	insideCluster     bool
	membersKubeConfig string
	stopCh            <-chan struct{}
	cacheOnce         sync.Once
	informerTimeout   int64
)

func GetStopChannel() <-chan struct{} {
	return stopCh
}

func SetInformerListTimeout(val int64) {
	informerTimeout = val
}

type GSLBConfigController struct {
	kubeclientset kubernetes.Interface
	gslbclientset gslbcs.Interface
	gslbLister    gslblisters.GSLBConfigLister
	gslbSynced    cache.InformerSynced
	workqueue     workqueue.RateLimitingInterface
	recorder      record.EventRecorder
}

func (gslbController *GSLBConfigController) Cleanup() {
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "cleaning up the entire GSLB configuration")

	// unset GSLBConfig and be prepared to take in the next GSLB config object
	gslbutils.SetGSLBConfig(false)
}

func (gslbController *GSLBConfigController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "starting the workers")
	<-stopCh
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "shutting down the workers")
	return nil
}

func initFlags() {
	gslbutils.Logf("object: main, msg: %s", "initializing the flags")
	defKubeConfig := os.Getenv("HOME") + "/.kube/config"
	flag.StringVar(&kubeConfig, "kubeconfig", defKubeConfig, "Path to kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the kubernetes API server. Overrides any value in kubeconfig. Overrides any value in kubeconfig, only required if out-of-cluster.")
	gslbutils.Logf("master: %s, kubeconfig: %s, msg: %s", masterURL, kubeConfig, "fetched from cmd")
}

func getGSLBConfigChecksum(gc *gslbalphav1.GSLBConfig) uint32 {
	var cksum uint32

	gcSpec := gc.Spec.DeepCopy()
	if gcSpec == nil {
		gslbutils.Errf("gslb config %s in namespace %s has no spec, can't calculate checksum", gc.GetObjectMeta().GetName(),
			gc.GetObjectMeta().GetNamespace())
		return cksum
	}

	cksum += utils.Hash(gcSpec.GSLBLeader.ControllerIP) + utils.Hash(gcSpec.GSLBLeader.ControllerVersion) +
		utils.Hash(gcSpec.GSLBLeader.Credentials)
	memberClusters := []string{}
	for _, c := range gcSpec.MemberClusters {
		memberClusters = append(memberClusters, c.ClusterContext)
	}
	sort.Strings(memberClusters)
	cksum += utils.Hash(utils.Stringify(memberClusters)) + utils.Hash(strconv.Itoa(gcSpec.RefreshInterval))
	return cksum
}

// GetNewController builds the GSLB Controller which has an informer for GSLB Config object
func GetNewController(kubeclientset kubernetes.Interface, gslbclientset gslbcs.Interface,
	gslbInformerFactory gslbinformers.SharedInformerFactory,
	AddGSLBConfigFunc GSLBConfigAddfn,
	initializeMemberClusters InitializeGSLBMemberClustersFn) *GSLBConfigController {

	gslbInformer := gslbInformerFactory.Amko().V1alpha1().GSLBConfigs()
	// Create event broadcaster
	gslbscheme.AddToScheme(scheme.Scheme)
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "gslb-controller"})

	gslbController := &GSLBConfigController{
		kubeclientset: kubeclientset,
		gslbclientset: gslbclientset,
		gslbLister:    gslbInformer.Lister(),
		gslbSynced:    gslbInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "gslb-configs"),
		recorder:      recorder,
	}
	gslbutils.Logf("object: GSLBConfigController, msg: %s", "setting up event handlers")
	// Event handler for when GSLB Config change
	gslbInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			AddGSLBConfigFunc(obj, initializeMemberClusters)
		},
		// Update not allowed for the GSLB Cluster Config object
		DeleteFunc: func(obj interface{}) {
			gcObj := obj.(*gslbalphav1.GSLBConfig)
			// Cleanup everything
			gcName, gcNS := gslbutils.GetGSLBConfigNameAndNS()
			if gcName != gcObj.GetObjectMeta().GetName() || gcNS != gcObj.GetObjectMeta().GetNamespace() {
				// not the GSLBConfig object which was accepted
				return
			}
			gslbController.Cleanup()
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			oldGc := oldObj.(*gslbalphav1.GSLBConfig)
			newGc := newObj.(*gslbalphav1.GSLBConfig)
			if oldGc.ResourceVersion == newGc.ResourceVersion {
				return
			}
			existingGCName, existingGCNamespace := gslbutils.GetGSLBConfigNameAndNS()
			if existingGCName != oldGc.GetObjectMeta().GetName() || existingGCNamespace != oldGc.GetObjectMeta().GetNamespace() {
				gslbutils.Warnf("a GSLBConfig %s already exists in namespace %s, ignoring the updates to this object", existingGCName,
					existingGCNamespace)
				return
			}

			if oldGc.Spec.LogLevel != newGc.Spec.LogLevel {
				gslbutils.Logf("log level changed")
				if gslbutils.IsLogLevelValid(newGc.Spec.LogLevel) {
					utils.AviLog.SetLevel(newGc.Spec.LogLevel)
					gslbutils.Logf("setting the new log level as %s", newGc.Spec.LogLevel)
				} else {
					gslbutils.Errf("log level %s unrecognized", newGc.Spec.LogLevel)
				}
			}

			if getGSLBConfigChecksum(oldGc) == getGSLBConfigChecksum(newGc) {
				return
			}
			gslbutils.Warnf("an update has been made to the GSLBConfig object, AMKO needs a reboot to register the changes")
			gslbutils.UpdateGSLBConfigStatus(EditRestartMsg)
		},
	})
	return gslbController
}

// CheckAcceptedGSLBConfigAndInitalize checks whether there's already an accepted GSLBConfig object that
// exists. If yes, we take that and set as our GSLB configuration.
func CheckAcceptedGSLBConfigAndInitalize(gcList *gslbalphav1.GSLBConfigList) (bool, error) {
	gcObjs := gcList.Items

	var acceptedGC *gslbalphav1.GSLBConfig
	for _, gcObj := range gcObjs {
		if gcObj.Status.State == AcceptedMsg {
			if acceptedGC == nil {
				acceptedGC = &gcObj
			} else {
				// there are more than two accepted GSLBConfig objects, which pertains to an undefined state
				gslbutils.Errf("ns: %s, msg: more than one GSLBConfig objects which were accepted, undefined state, can't do a full sync",
					gslbutils.AVISystem)
				return false, errors.New("more than one GSLBConfig objects in accepted state")
			}
		}
	}

	if acceptedGC != nil {
		AddGSLBConfigObject(acceptedGC, InitializeGSLBMemberClusters)
		return true, nil
	}
	return false, nil
}

// CheckGSLBConfigsAndInitialize iterates through all the GSLBConfig objects in the system and does:
// 1. add a GSLBConfig object if only one GSLBConfig object exists with accepted state.
// 2. add a GSLBConfig object if only one GSLBConfig object (in non-accepted state).
// 3. returns if there was an error on either of the above two conditions.
func CheckGSLBConfigsAndInitialize() {
	gcList, err := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBConfigs(gslbutils.AVISystem).List(context.TODO(), metav1.ListOptions{TimeoutSeconds: &informerTimeout})
	if err != nil {
		gslbutils.Errf("ns: %s, error in listing the GSLBConfig objects, %s, %s", gslbutils.AVISystem,
			err.Error(), "can't do a full sync")
		return
	}

	if len(gcList.Items) == 0 {
		gslbutils.Logf("ns: %s, no GSLBConfig objects found during bootup, will skip fullsync", gslbutils.AVISystem)
		return
	}

	added, err := CheckAcceptedGSLBConfigAndInitalize(gcList)
	if err != nil || added {
		return
	}

	if len(gcList.Items) > 1 {
		// more than one GC objects exist and none of them were already accepted, we panic
		panic("more than one GSLBConfig objects in " + gslbutils.AVISystem + " exist, please add only one")
	}

	gslbutils.Logf("ns: %s, msg: found a GSLBConfig object", gslbutils.AVISystem)
	AddGSLBConfigObject(&gcList.Items[0], InitializeGSLBMemberClusters)
}

// IsGSLBConfigValid returns true if the the GSLB Config object was created
// in "avi-system" namespace.
// TODO: Validate the controllers inside the config object
func IsGSLBConfigValid(obj interface{}) (*gslbalphav1.GSLBConfig, error) {
	config := obj.(*gslbalphav1.GSLBConfig)
	if config.ObjectMeta.Namespace == gslbutils.AVISystem {
		return config, nil
	}
	if gslbutils.IsLogLevelValid(config.Spec.LogLevel) {
		return config, nil
	}
	return nil, errors.New("invalid gslb config, namespace can only be avi-system")
}

func PublishChangeToRestLayer(gsKey interface{}, sharedQ *utils.WorkerQueue) {
	aviCacheKey, ok := gsKey.(avicache.TenantName)
	if !ok {
		gslbutils.Errf("CacheKey: %v, msg: cache key malformed, not publishing to rest layer", gsKey)
		return
	}
	nodes.PublishKeyToRestLayer(aviCacheKey.Tenant, aviCacheKey.Name, aviCacheKey.Name+"/"+aviCacheKey.Tenant, sharedQ)
}

func CheckAndSetGslbLeader() error {
	var leader bool
	leader, err := avicache.IsAviSiteLeader()
	if err != nil {
		gslbutils.SetResyncRequired(true)
		return err
	}
	if leader {
		gslbutils.SetControllerAsLeader()
		return nil
	}
	gslbutils.SetControllerAsFollower()
	return errors.New("AVI site is not the GSLB leader")
}

func ResyncNodesToRestLayer() {
	prevStateCtrl := gslbutils.IsControllerLeader()
	err := CheckAndSetGslbLeader()
	if err != nil {
		// controller details can't be fetched due to some error, so return
		gslbutils.Errf("error fetching Gslb leader details, %s", err.Error())
		gslbutils.SetResyncRequired(true)
		return
	}
	newStateCtrl := gslbutils.IsControllerLeader()

	if newStateCtrl == false {
		// controller is a follower, set resync and return
		gslbutils.Errf("controller is a follower, can't re-sync")
		// will try to re-sync next time
		gslbutils.SetResyncRequired(true)
		return
	}

	// controller is the leader
	if prevStateCtrl != newStateCtrl {
		gslbutils.Logf("Gslb controller state has changed from follower to leader")
		gslbutils.SetResyncRequired(true)
	}

	if !gslbutils.IsResyncRequired() {
		gslbutils.Logf("resync not required")
		return
	}

	// re-sync is required anyway
	gslbutils.Logf("Gslb leader controller re-sync required, will perform re-sync now")

	nodes.PublishAllGraphKeys()
	// once syncing is done, no further resync required
	gslbutils.SetResyncRequired(false)
}

// CacheRefreshRoutine fetches the objects in the AVI controller and finds out
// the delta between the existing and the new objects.
func CacheRefreshRoutine() {
	gslbutils.Logf("starting AVI cache refresh...\ncreating a new AVI cache")

	// Check if the controller is leader or not, return if not.
	err := CheckAndSetGslbLeader()
	if err != nil {
		gslbutils.Errf("error in verifying site as GSLB leader: %s", err.Error())
		return
	}

	newAviCache := avicache.PopulateGSCache(false)
	existingAviCache := avicache.GetAviCache()

	sharedQ := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	// The refresh cycle builds a new set of AVI objects in `newAviCache` and compares them with
	// the existing avi cache. If a discrepancy is found, we just write the key to layer 3.
	for key, obj := range existingAviCache.Cache {
		existingGSObj, ok := obj.(*avicache.AviGSCache)
		if !ok {
			gslbutils.Errf("CacheKey: %v, CacheObj: %v, msg: existing GSLB Object in avi cache malformed", key, existingGSObj)
			continue
		}
		newGS, found := newAviCache.AviCacheGet(key)
		if !found {
			existingAviCache.AviCacheAdd(key, nil)
			PublishChangeToRestLayer(key, sharedQ)
			continue
		}
		newGSObj, ok := newGS.(*avicache.AviGSCache)
		if !ok {
			gslbutils.Warnf("CacheKey: %v, CacheObj: %v, msg: new GSLB object in avi cache malformed, will update", key,
				newGSObj)
			continue
		}
		if existingGSObj.CloudConfigCksum != newGSObj.CloudConfigCksum {
			gslbutils.Logf("CacheKey: %v, CacheObj: %v, msg: GSLB Service has changed in AVI, will update", key, obj)
			// First update the newly fetched avi cache in the existing avi cache key
			existingAviCache.AviCacheAdd(key, newGSObj)
			PublishChangeToRestLayer(key, sharedQ)
		}
	}

	gslbutils.Logf("AVI Cache refresh done")
}

// GenerateKubeConfig reads the kubeconfig given through the environment variable
// decodes it and then writes to a temporary file.
func GenerateKubeConfig() error {
	membersKubeConfig = os.Getenv("GSLB_CONFIG")
	if membersKubeConfig == "" {
		utils.AviLog.Fatal("GSLB_CONFIG environment variable not set, exiting...")
		return errors.New("GSLB_CONFIG environment variable not set, exiting")
	}
	f, err := os.Create(gslbutils.GSLBKubePath)
	if err != nil {
		return errors.New("Error in creating file: " + err.Error())
	}

	_, err = f.WriteString(membersKubeConfig)
	if err != nil {
		return errors.New("Error in writing to config file: " + err.Error())
	}
	return nil
}

func parseControllerDetails(gc *gslbalphav1.GSLBConfig) error {
	// Read the gslb leader's credentials
	leaderIP := gc.Spec.GSLBLeader.ControllerIP
	leaderVersion := gc.Spec.GSLBLeader.ControllerVersion
	leaderSecret := gc.Spec.GSLBLeader.Credentials

	if leaderIP == "" {
		gslbutils.Errf("controllerIP: %s, msg: Invalid controller IP for the leader", leaderIP)
		gslbutils.UpdateGSLBConfigStatus(InvalidConfigMsg + " with controller IP " + leaderIP)
		return errors.New("invalid leader IP")
	}
	if leaderSecret == "" {
		gslbutils.Errf("credentials: %s, msg: Invalid controller secret for leader", leaderSecret)
		gslbutils.UpdateGSLBConfigStatus(InvalidConfigMsg + " with leaderSecret " + leaderSecret)
		return errors.New("invalid leader secret")
	}

	secretObj, err := gslbutils.GlobalKubeClient.CoreV1().Secrets(gslbutils.AVISystem).Get(context.TODO(), leaderSecret, metav1.GetOptions{})
	if err != nil || secretObj == nil {
		gslbutils.Errf("Error in fetching leader controller secret %s in namespace %s, can't initialize controller",
			leaderSecret, gslbutils.AVISystem)
		gslbutils.UpdateGSLBConfigStatus(NoSecretMsg + " " + leaderSecret)
		return errors.New("error in fetching leader secret")
	}
	ctrlUsername := secretObj.Data["username"]
	ctrlPassword := secretObj.Data["password"]
	gslbutils.NewAviControllerConfig(string(ctrlUsername), string(ctrlPassword), leaderIP, leaderVersion)

	return nil
}

// AddGSLBConfigObject parses the gslb config object and starts informers
// for the member clusters.
func AddGSLBConfigObject(obj interface{}, initializeGSLBMemberClusters InitializeGSLBMemberClustersFn) {
	gslbObj := obj.(*gslbalphav1.GSLBConfig)
	existingName, existingNS := gslbutils.GetGSLBConfigNameAndNS()
	if existingName == "" && existingNS == "" {
		gslbutils.SetGSLBConfigObj(gslbObj)
	}

	if gslbutils.IsGSLBConfigSet() {
		// first check, if we have the same GSLB config which is set, if yes, no need to do anything
		if existingName == gslbObj.GetObjectMeta().GetName() && existingNS == gslbObj.GetObjectMeta().GetNamespace() {
			gslbutils.Logf("GSLB object set during bootup, ignoring this")
			return
		}
		// else, populate the status field with an error message
		gslbutils.Errf("GSLB configuration is set already, can't change it. Delete and re-create the GSLB config object.")
		gslbObj.Status.State = AlreadySetMsg
		_, updateErr := gslbutils.GlobalGslbClient.AmkoV1alpha1().GSLBConfigs(gslbObj.Namespace).Update(context.TODO(), gslbObj, metav1.UpdateOptions{})
		if updateErr != nil {
			gslbutils.Errf("error in updating the status field of GSLB Config object %s in %s namespace",
				gslbObj.GetObjectMeta().GetName(), gslbObj.GetObjectMeta().GetNamespace())
		}
		return
	}

	gc, err := IsGSLBConfigValid(obj)
	if err != nil {
		gslbutils.Warnf("ns: %s, gslbConfig: %s, msg: %s, %s", gc.ObjectMeta.Namespace, gc.ObjectMeta.Name,
			"invalid format", err)
		gslbutils.UpdateGSLBConfigStatus(InvalidConfigMsg + err.Error())
		return
	}
	utils.AviLog.SetLevel(gc.Spec.LogLevel)
	gslbutils.SetCustomFqdnMode(gc.Spec.UseCustomGlobalFqdn)

	gslbutils.Debugf("ns: %s, gslbConfig: %s, msg: %s", gc.ObjectMeta.Namespace, gc.ObjectMeta.Name,
		"got an add event")

	// parse and set the controller configuration
	err = parseControllerDetails(gc)
	if err != nil {
		gslbutils.Errf("error while parsing controller details: %s", err.Error())
		return
	}
	err = avicache.VerifyVersion()
	if err != nil {
		gslbutils.UpdateGSLBConfigStatus(ControllerAPIErr + ", " + err.Error())
		return
	}

	// check if the controller details provided are for a leader site
	isLeader, err := avicache.IsAviSiteLeader()
	if err != nil {
		gslbutils.Errf("error fetching Gslb leader site details, %s", err.Error())
		return
	}
	if !isLeader {
		gslbutils.Errf("Controller details provided are not for a leader, returning")
		gslbutils.UpdateGSLBConfigStatus(ControllerNotLeaderMsg)
		gslbutils.SetControllerAsFollower()
		return
	}
	gslbutils.SetControllerAsLeader()

	cacheRefreshInterval := gc.Spec.RefreshInterval
	if cacheRefreshInterval <= 0 {
		gslbutils.Warnf("Invalid refresh interval provided, will set it to default %d seconds", gslbutils.DefaultRefreshInterval)
		cacheRefreshInterval = gslbutils.DefaultRefreshInterval
	}
	gslbutils.Debugf("Cache refresh interval: %d seconds", cacheRefreshInterval)
	// Secret created with name: "gslb-config-secret" and environment variable to set is
	// GSLB_CONFIG.
	err = GenerateKubeConfig()
	if err != nil {
		utils.AviLog.Fatalf("Error in generating the kubeconfig file: %s", err.Error())
		gslbutils.UpdateGSLBConfigStatus(KubeConfigErr + " " + err.Error())
		return
	}

	aviCtrlList, err := initializeGSLBMemberClusters(gslbutils.GSLBKubePath, gc.Spec.MemberClusters)
	if err != nil {
		gslbutils.Errf("couldn't initialize the kubernetes/openshift clusters: %s, returning", err.Error())
		gslbutils.UpdateGSLBConfigStatus(ClusterHealthCheckErr + err.Error())
		// shutdown the api server to let k8s/openshift restart the pod back up
		apiserver.GetAmkoAPIServer().ShutDown()
		return
	}

	gslbutils.UpdateGSLBConfigStatus(BootupSyncMsg)

	// TODO: Change the GSLBConfig CRD to take full sync interval as an input and fetch that
	// value before going into full sync
	// boot up time cache population
	gslbutils.Logf("will populate avi cache now...")
	avicache.PopulateHMCache(true)
	avicache.PopulateSPCache()
	newCache := avicache.PopulateGSCache(true)

	bootupSync(aviCtrlList, newCache)

	gslbutils.UpdateGSLBConfigStatus(BootupSyncEndMsg)

	// Initialize a periodic worker running full sync
	resyncNodesWorker := gslbutils.NewFullSyncThread(time.Duration(cacheRefreshInterval))
	resyncNodesWorker.SyncFunction = ResyncNodesToRestLayer
	go resyncNodesWorker.Run()

	gcChan := gslbutils.GetGSLBConfigObjectChan()
	*gcChan <- true

	// Start the informers for the member controllers
	for _, aviCtrl := range aviCtrlList {
		aviCtrl.Start(stopCh)
	}

	// GSLB Configuration successfully done
	gslbutils.SetGSLBConfig(true)
	gslbutils.UpdateGSLBConfigStatus(AcceptedMsg)

	// Set the workers for the node/graph layer
	// During test mode, the graph layer workers are already initialized
	if !gslbutils.InTestMode() {
		StartGraphLayerWorkers()
	}
}

var graphOnce sync.Once

func StartGraphLayerWorkers() {
	graphOnce.Do(func() {
		ingestionSharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.ObjectIngestionLayer)
		ingestionSharedQueue.SyncFunc = nodes.SyncFromIngestionLayer
		ingestionSharedQueue.Run(stopCh, gslbutils.GetWaitGroupFromMap(gslbutils.WGIngestion))
	})
}

// Initialize initializes the first controller which looks for GSLB Config
func Initialize() {
	initFlags()
	flag.Parse()
	if logfilepath := os.Getenv("LOG_FILE_PATH"); logfilepath != "" {
		flag.Lookup("log_dir").Value.Set(logfilepath)
	} else {
		flag.Lookup("logtostderr").Value.Set("true")
	}

	stopCh = utils.SetupSignalHandler()
	// Check if we are running inside kubernetes
	cfg, err := rest.InClusterConfig()
	if err != nil {
		gslbutils.Warnf("object: main, msg: %s, %s", "not running inside kubernetes cluster", err)
	} else {
		gslbutils.Logf("object: main, msg: %s", "running inside kubernetes cluster, won't use config files")
		insideCluster = true
	}
	if insideCluster == false {
		cfg, err = clientcmd.BuildConfigFromFlags(masterURL, kubeConfig)
		gslbutils.Logf("masterURL: %s, kubeconfigPath: %s, msg: %s", masterURL, kubeConfig,
			"built from flags")
		if err != nil {
			panic("object: main, msg: " + err.Error() + ", error building kubeconfig")
		}
	}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic("error building kubernetes clientset: " + err.Error())
	}

	gslbutils.SetWaitGroupMap()
	gslbutils.GlobalKubeClient = kubeClient
	gslbClient, err := gslbcs.NewForConfig(cfg)
	if err != nil {
		panic("error building gslb config clientset: " + err.Error())
	}
	gslbutils.GlobalGslbClient = gslbClient

	gdpClient, err := gdpcs.NewForConfig(cfg)
	if err != nil {
		panic("error building gdp clientset: " + err.Error())
	}
	gslbutils.GlobalGdpClient = gdpClient
	// required to publish the GDP status, the reason we need this is because, during unit tests, we don't
	// traverse this path and hence we don't initialize GlobalGslbClient, and hence, we can't update the
	// status of the GDP object. Always check this flag before updating the status.
	gslbutils.PublishGDPStatus = true
	gslbutils.PublishGSLBStatus = true

	SetInformerListTimeout(120)

	numIngestionWorkers := utils.NumWorkersIngestion
	ingestionQueueParams := utils.WorkerQueue{NumWorkers: numIngestionWorkers, WorkqueueName: utils.ObjectIngestionLayer}
	graphQueueParams := utils.WorkerQueue{NumWorkers: gslbutils.NumRestWorkers, WorkqueueName: utils.GraphLayer}
	slowRetryQParams := utils.WorkerQueue{NumWorkers: 1, WorkqueueName: gslbutils.SlowRetryQueue, SlowSyncTime: gslbutils.SlowSyncTime}
	fastRetryQParams := utils.WorkerQueue{NumWorkers: 1, WorkqueueName: gslbutils.FastRetryQueue}
	ingestionRetryQParams := utils.WorkerQueue{NumWorkers: 1, WorkqueueName: gslbutils.IngestionRetryQueue, SlowSyncTime: gslbutils.SlowSyncTime}

	utils.SharedWorkQueue(&ingestionQueueParams, &graphQueueParams, &slowRetryQParams, &fastRetryQParams, &ingestionRetryQParams)

	// Set workers for ingestion queue retry workers
	ingestionRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.IngestionRetryQueue)
	ingestionRetryQueue.SyncFunc = IngestionRetryAddUpdate
	ingestionRetryQueue.Run(stopCh, gslbutils.GetWaitGroupFromMap(gslbutils.WGIngestionRetry))

	// Set workers for layer 3 (REST layer)
	graphSharedQueue := utils.SharedWorkQueue().GetQueueByName(utils.GraphLayer)
	graphSharedQueue.SyncFunc = avirest.SyncFromNodesLayer
	graphSharedQueue.Run(stopCh, gslbutils.GetWaitGroupFromMap(gslbutils.WGGraph))

	// Set up retry Queue
	slowRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
	slowRetryQueue.SyncFunc = aviretry.SyncFromRetryLayer
	slowRetryQueue.Run(stopCh, gslbutils.GetWaitGroupFromMap(gslbutils.WGSlowRetry))
	fastRetryQueue := utils.SharedWorkQueue().GetQueueByName(gslbutils.FastRetryQueue)
	fastRetryQueue.SyncFunc = aviretry.SyncFromRetryLayer
	fastRetryQueue.Run(stopCh, gslbutils.GetWaitGroupFromMap(gslbutils.WGFastRetry))

	gslbInformerFactory := gslbinformers.NewSharedInformerFactory(gslbClient, time.Second*30)

	gslbController := GetNewController(kubeClient, gslbClient, gslbInformerFactory,
		AddGSLBConfigObject, InitializeGSLBMemberClusters)

	// check whether we already have a GSLBConfig object created which was previously accepted
	// this is to make sure that after a reboot, we don't pick a different GSLBConfig object which
	// wasn't accepted.
	CheckGSLBConfigsAndInitialize()

	// Start the informer for the GDP controller
	gslbInformer := gslbInformerFactory.Amko().V1alpha1().GSLBConfigs()

	go gslbInformer.Informer().Run(stopCh)

	gslbutils.Logf("waiting for a GSLB config object to be added")

	// Wait till a GSLB config object is added
	gcChan := gslbutils.GetGSLBConfigObjectChan()
	<-*gcChan

	gdpInformerFactory := gdpinformers.NewSharedInformerFactory(gdpClient, time.Second*30)
	gdpCtrl := InitializeGDPController(kubeClient, gdpClient, gdpInformerFactory, AddGDPObj,
		UpdateGDPObj, DeleteGDPObj)

	// Start the informer for the GDP controller
	gdpInformer := gdpInformerFactory.Amko().V1alpha2().GlobalDeploymentPolicies()
	go gdpInformer.Informer().Run(stopCh)

	gslbhrCtrl := InitializeGSLBHostRuleController(kubeClient, gslbClient, gslbInformerFactory,
		AddGSLBHostRuleObj, UpdateGSLBHostRuleObj, DeleteGSLBHostRuleObj)

	gslbhrInformer := gslbInformerFactory.Amko().V1alpha1().GSLBHostRules()
	go gslbhrInformer.Informer().Run(stopCh)

	go RunControllers(gslbController, gdpCtrl, gslbhrCtrl, stopCh)
	<-stopCh
	gslbutils.WaitForWorkersToExit()
}

func RunControllers(gslbController *GSLBConfigController, gdpController *GDPController, gslbhrCtrl *GSLBHostRuleController, stopCh <-chan struct{}) {
	if err := gslbController.Run(stopCh); err != nil {
		panic("error running GSLB Controller: " + err.Error())
	}

	if err := gdpController.Run(stopCh); err != nil {
		panic("error running GDP Controller: " + err.Error())
	}

	if err := gslbhrCtrl.Run(stopCh); err != nil {
		panic("error running GSLBHostRule Controller: " + err.Error())
	}
}

// BuildContextConfig builds the kubernetes/openshift context config
func BuildContextConfig(kubeconfigPath, context string) (*restclient.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

func InformersToRegister(oclient *oshiftclient.Clientset, kclient *kubernetes.Clientset, cname string) ([]string, error) {

	allInformers := []string{}
	_, err := kclient.CoreV1().Services("").List(context.TODO(), metav1.ListOptions{TimeoutSeconds: &informerTimeout})
	if err != nil {
		gslbutils.Errf("can't access the services api for cluster %s, error : %v", cname, err)
		return allInformers, errors.New("cluster " + cname + " health check failed, can't access the services api")
	}
	_, err = oclient.RouteV1().Routes("").List(context.TODO(), metav1.ListOptions{TimeoutSeconds: &informerTimeout})
	gslbutils.Debugf("cluster: %s, msg: checking if cluster has a route informer %v", cname, err)
	if err == nil {
		// Openshift cluster with route support, we will just add service informer
		allInformers = append(allInformers, utils.RouteInformer)
	} else {
		// Kubernetes cluster
		allInformers = append(allInformers, utils.IngressInformer)
	}

	allInformers = append(allInformers, utils.ServiceInformer)
	allInformers = append(allInformers, utils.NSInformer)
	return allInformers, nil
}

func InitializeMemberCluster(cfg *restclient.Config, cluster KubeClusterDetails,
	clients map[string]*kubernetes.Clientset) (*GSLBMemberController, error) {

	informersArg := make(map[string]interface{})

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error in creating kubernetes clientset: %v", err)
	}
	oshiftClient, err := oshiftclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error in creating openshift clientset: %v", err)
	}
	informersArg[utils.INFORMERS_OPENSHIFT_CLIENT] = oshiftClient
	informersArg[utils.INFORMERS_INSTANTIATE_ONCE] = false
	registeredInformers, err := InformersToRegister(oshiftClient, kubeClient, cluster.clusterName)
	if err != nil {
		return nil, fmt.Errorf("error in initializing informers: %v", err)
	}
	if len(registeredInformers) == 0 {
		return nil, fmt.Errorf("no informers available for this cluster")
	}
	gslbutils.Logf("Informers for cluster %s: %v", cluster.clusterName, registeredInformers)
	informerInstance := utils.NewInformers(utils.KubeClientIntf{
		ClientSet: kubeClient},
		registeredInformers,
		informersArg)
	clients[cluster.clusterName] = kubeClient

	var aviCtrl GSLBMemberController
	if gslbutils.GetCustomFqdnMode() {
		hrClient, err := hrcs.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialize clientset for HostRule: %v", err)
		}

		akoInformerFactory := akoinformer.NewSharedInformerFactory(hrClient, time.Second*30)
		hostRuleInformer := akoInformerFactory.Ako().V1alpha1().HostRules()

		aviCtrl = GetGSLBMemberController(cluster.clusterName, informerInstance, &hostRuleInformer)
		aviCtrl.hrClientSet = hrClient
		_, err = hrClient.AkoV1alpha1().HostRules("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("HostRule API not available for cluster: %v", err)
		}
	} else {
		aviCtrl = GetGSLBMemberController(cluster.clusterName, informerInstance, nil)
	}

	gslbutils.AddClusterContext(cluster.clusterName)
	aviCtrl.SetupEventHandlers(K8SInformers{Cs: clients[cluster.clusterName]})
	return &aviCtrl, nil
}

// InitializeGSLBClusters initializes the GSLB member clusters
func InitializeGSLBMemberClusters(membersKubeConfig string, memberClusters []gslbalphav1.MemberCluster) ([]*GSLBMemberController, error) {
	clusterDetails := loadClusterAccess(membersKubeConfig, memberClusters)
	clients := make(map[string]*kubernetes.Clientset)

	aviCtrlList := make([]*GSLBMemberController, 0)
	for _, cluster := range clusterDetails {
		gslbutils.Logf("cluster: %s, msg: %s", cluster.clusterName, "initializing")
		cfg, err := BuildContextConfig(cluster.kubeconfig, cluster.clusterName)
		if err != nil {
			gslbutils.Warnf("cluster: %s, msg: %s, %s", cluster.clusterName, "error in connecting to kubernetes API",
				err)
			continue
		} else {
			gslbutils.Logf("cluster: %s, msg: %s", cluster.clusterName, "successfully connected to kubernetes API")
		}
		aviCtrl, err := InitializeMemberCluster(cfg, cluster, clients)
		if err != nil {
			return nil, fmt.Errorf("error initializing member cluster %s: %s", cluster.clusterName, err)
		}
		if aviCtrl != nil {
			aviCtrlList = append(aviCtrlList, aviCtrl)
		}
	}
	return aviCtrlList, nil
}

func loadClusterAccess(membersKubeConfig string, memberClusters []gslbalphav1.MemberCluster) []KubeClusterDetails {
	var clusterDetails []KubeClusterDetails
	for _, memberCluster := range memberClusters {
		clusterDetails = append(clusterDetails, KubeClusterDetails{memberCluster.ClusterContext,
			membersKubeConfig, "", nil})
		gslbutils.Logf("cluster: %s, msg: %s", memberCluster.ClusterContext, "loaded cluster access")
	}
	return clusterDetails
}
