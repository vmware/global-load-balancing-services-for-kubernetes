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
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/vmware/alb-sdk/go/clients"
	"github.com/vmware/alb-sdk/go/models"
	"github.com/vmware/alb-sdk/go/session"

	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gslbhrinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions/amko/v1alpha1"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// InformersPerCluster is the number of informers per cluster
var InformersPerCluster *utils.AviCache

// LeaderClusterContext is the leader cluster where amko is deployed
var LeaderClusterContext string

type AMKOCrdInformers struct {
	GslbHostruleInformer gslbhrinformers.GSLBHostRuleInformer
}

var AMKOCrdInformer *AMKOCrdInformers

func SetInformersPerCluster(clusterName string, info *utils.Informers) {
	if InformersPerCluster == nil {
		InformersPerCluster = utils.NewAviCache()
	}
	InformersPerCluster.AviCacheAdd(clusterName, info)
}

func GetInformersPerCluster(clusterName string) *utils.Informers {
	info, ok := InformersPerCluster.AviCacheGet(clusterName)
	if !ok {
		utils.AviLog.Warnf("Failed to get informer for cluster %v", clusterName)
		return nil
	}
	return info.(*utils.Informers)
}

func SetAMKOCrdInformers(amkoCrdInformer *AMKOCrdInformers) {
	AMKOCrdInformer = amkoCrdInformer

}

func GetAMKOCRDInformer() *AMKOCrdInformers {
	return AMKOCrdInformer
}

func MultiClusterKey(operation, objType, clusterName, ns, objName, tenant string) string {
	compositeObjName := clusterName + "/" + ns + "/" + objName + "/" + tenant
	return MultiClusterKeyWithObjName(operation, objType, compositeObjName)
}

func MultiClusterKeyWithObjName(operation, objType, compositeName string) string {
	return operation + "/" + objType + "/" + compositeName
}

func GSLBHostRuleKey(operation, objType, objName string) string {
	return MultiClusterKeyWithObjName(operation, objType, objName)
}

func MultiClusterKeyForHostRule(operation, objType, clusterName, ns, objName, lfqdn, gfqdn, tenant string) string {
	return MultiClusterKeyWithObjName(operation, objType, clusterName+"/"+ns+"/"+objName+"/"+lfqdn+"/"+gfqdn+"/"+tenant)
}

func ExtractMultiClusterHostRuleKey(key string) (string, string, string, string, string, string, error) {
	seg := strings.Split(key, "/")
	if len(seg) != 6 {
		return "", "", "", "", "", "", fmt.Errorf("invalid key %s for host rule", key)
	}
	return seg[0], seg[1], seg[2], seg[3], seg[4], seg[5], nil
}

func ExtractGSLBHostRuleKey(key string) (string, string, string, string, string, error) {
	seg := strings.Split(key, "/")
	if len(seg) != 5 {
		return "", "", "", "", "", fmt.Errorf("invalid key %s for GSLBHostRule", key)
	}
	return seg[0], seg[1], seg[2], seg[3], seg[4], nil
}

func ExtractMultiClusterKey(key string) (string, string, string, string, string, string) {
	segments := strings.Split(key, "/")
	var operation, objType, cluster, ns, name, hostname, tenant string
	if segments[1] == IngressType ||
		segments[1] == MCIType {
		if len(segments) == IngMultiClusterKeyLen {
			operation, objType, cluster, ns, name, hostname, tenant = segments[0], segments[1], segments[2], segments[3], segments[4], segments[5], segments[6]
			name += "/" + hostname
		} else if len(segments) == HostRuleKeyLen {
			operation, objType, cluster, ns, name, tenant = segments[0], segments[1], segments[2], segments[3], segments[4]+"/"+segments[5], segments[7]
		}
	} else if len(segments) == MultiClusterKeyLen {
		operation, objType, cluster, ns, name, tenant = segments[0], segments[1], segments[2], segments[3], segments[4], segments[5]
	} else if len(segments) == GSFQDNKeyLen {
		operation, objType, name = segments[0], segments[1], segments[2]
	} else if len(segments) == HostRuleKeyLen {
		operation, objType, cluster, ns, name, tenant = segments[0], segments[1], segments[2], segments[3], segments[4], segments[7]
	}
	return operation, objType, cluster, ns, name, tenant
}

// sname for ingress is ingName/hostname
func GetIngressNameFromSname(sname string) string {
	return strings.Split(sname, "/")[0]
}

func GetObjectTypeFromKey(key string) (string, error) {
	segments := strings.Split(key, "/")
	if len(segments) < 2 {
		return "", fmt.Errorf("invalid key: %s", key)
	}
	return segments[1], nil
}

func GSFQDNKey(operation, objType, gsFqdn, namespace, tenant string) string {
	return operation + "/" + objType + "/" + namespace + "/" + tenant + "/" + gsFqdn
}

func SplitMultiClusterObjectName(name string) (string, string, string, error) {
	if name == "" {
		return "", "", "", errors.New("multi-cluster route/svc name is empty")
	}
	reqList := strings.Split(name, "/")

	if len(reqList) != 3 {
		return "", "", "", errors.New("multi-cluster route/svc name format is unexpected")
	}
	return reqList[0], reqList[1], reqList[2], nil
}

func SplitMultiClusterIngHostName(name string) (string, string, string, string, error) {
	if name == "" {
		return "", "", "", "", errors.New("multi-cluster ingress host name is empty")
	}
	reqList := strings.Split(name, "/")

	if len(reqList) != 4 {
		return "", "", "", "", errors.New("multi-cluster ingress name format is unexpected")
	}
	return reqList[0], reqList[1], reqList[2], reqList[3], nil
}

func SplitMultiClusterNS(name string) (string, string, error) {
	if name == "" {
		return "", "", errors.New("multi-cluster namespace is empty")
	}
	reqList := strings.Split(name, "/")
	if len(reqList) != 2 {
		return "", "", errors.New("multi-cluster namespace format is unexpected")
	}
	return reqList[0], reqList[1], nil
}

func RouteGetIPAddr(route *routev1.Route) (string, bool) {
	hostname := route.Spec.Host
	// Return true if the IP address is present in an route's status field, else return false
	routeStatus := route.Status
	for _, ingr := range routeStatus.Ingress {
		// check if the status message was populated by ako
		if !strings.HasPrefix(ingr.RouterName, "ako-") {
			continue
		}
		conditions := ingr.Conditions
		// check the hostname with the route's status hostname field
		if ingr.Host != hostname {
			continue
		}
		for _, condition := range conditions {
			// TODO: Check if the message field contains an IP address
			if condition.Message == "" {
				continue
			}
			// Check if this is a IP address
			addr := net.ParseIP(condition.Message)
			if addr != nil {
				return condition.Message, true
			}
		}
	}
	return "", false
}

type IngressHostIP struct {
	Hostname string
	IPAddr   string
}

func getHostListFromIngress(ingress *networkingv1.Ingress) []string {
	hostList := []string{}
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			hostList = append(hostList, rule.Host)
		}
	}
	return hostList
}

func IngressGetIPAddrs(ingress *networkingv1.Ingress) []IngressHostIP {
	ingHostIP := []IngressHostIP{}
	hostList := getHostListFromIngress(ingress)
	ingStatus := ingress.Status
	ingList := ingStatus.LoadBalancer.Ingress
	if len(ingList) == 0 {
		Logf("Ingress %s doesn't have the status field populated", ingress.GetObjectMeta().GetName())
		Debugf("Ingress: %v", ingress)
		return ingHostIP
	}
	for _, ingr := range ingList {
		// Check if this is a IP address
		addr := net.ParseIP(ingr.IP)
		if addr == nil {
			Warnf("Address %s is not an IP address: %s", addr)
			continue
		}
		// Found an IP address, return
		if ingr.Hostname == "" {
			Warnf("Hostname is empty in ingress %s", ingress.Name)
			continue
		}
		if utils.HasElem(hostList, ingr.Hostname) {
			ingHostIP = append(ingHostIP, IngressHostIP{
				Hostname: ingr.Hostname,
				IPAddr:   ingr.IP,
			})
		}
	}
	return ingHostIP
}

// Logf is aliased to utils' Info.Printf
var Logf = utils.AviLog.Infof

// Errf is aliased to utils' Error.Printf
var Errf = utils.AviLog.Errorf

// Warnf is aliased to utils' Warning.Printf
var Warnf = utils.AviLog.Warnf

// Debugf is aliased to utils' Debug.Printf
var Debugf = utils.AviLog.Debugf

func GetGSLBServiceChecksum(serverList, domainList, memberObjs, hmNames []string,
	persistenceProfileRef *string, ttl *uint32, pa *gslbalphav1.PoolAlgorithmSettings,
	downResponse *gslbalphav1.DownResponse, pkiProfileRef *string, createdBy string) uint32 {

	sort.Strings(serverList)
	sort.Strings(domainList)
	sort.Strings(memberObjs)
	sort.Strings(hmNames)

	// checksum has to take into consideration the non-path HMs and the path based HMs
	cksum := utils.Hash(utils.Stringify(serverList)) +
		utils.Hash(utils.Stringify(domainList)) +
		utils.Hash(utils.Stringify(memberObjs)) +
		utils.Hash(utils.Stringify(hmNames)) +
		utils.Hash(utils.Stringify(createdBy))
	if ttl != nil {
		cksum += utils.Hash(utils.Stringify(*ttl))
	}
	// TODO: verify if this affects the existing GSs

	if persistenceProfileRef != nil {
		cksum += utils.Hash(*persistenceProfileRef)
	}

	if pkiProfileRef != nil {
		cksum += utils.Hash(*pkiProfileRef)
	}
	cksum += getChecksumForPoolAlgorithm(pa)
	cksum += getChecksumForDownResponse(downResponse)
	return cksum
}

// description is taken as []string
// For path based Hms, the checksum is computed for all paths
func GetGSLBHmChecksum(hmType string, port int32, description []string, createdBy string) uint32 {
	portStr := strconv.FormatInt(int64(port), 10)
	checksum := utils.Hash(hmType) + utils.Hash(portStr)
	sort.Strings(description)
	for _, desc := range description {
		checksum = checksum + utils.Hash(desc)
	}
	checksum += utils.Hash(createdBy)
	return checksum
}

func GetTenantRef(tenant string) string {
	return "https://" + os.Getenv("GSLB_CTRL_IPADDRESS") + "/api/tenant/?name=" + tenant
}

// GSLBConfigObj is global and is initialized only once
type GSLBConfigObj struct {
	configObj  *gslbalphav1.GSLBConfig
	configLock sync.RWMutex
}

var gcObj GSLBConfigObj

// gslbConfigObjectAdded is a channel which halts the initialization operation until a GSLB config object
// is added. Even the GDP informers are started after this operation goes through.
// This channel's usage can be found in gslb.go.
var gslbConfigObjectAdded chan bool
var chanOnce sync.Once

func GetGSLBConfigObjectChan() *chan bool {
	chanOnce.Do(func() {
		gslbConfigObjectAdded = make(chan bool, 1)
	})
	return &gslbConfigObjectAdded
}

func GetGSLBConfigNameAndNS() (string, string) {
	gcObj.configLock.Lock()
	defer gcObj.configLock.Unlock()

	if gcObj.configObj == nil {
		return "", ""
	}
	return gcObj.configObj.Name, gcObj.configObj.Namespace
}

func updateGSLBConfigStatusMsg(msg string) {
	gcObj.configLock.Lock()
	defer gcObj.configLock.Unlock()

	gcObj.configObj.Status.State = msg
}

func SetGSLBConfigObj(gc *gslbalphav1.GSLBConfig) {
	gcObj.configLock.Lock()
	defer gcObj.configLock.Unlock()

	gcObj.configObj = gc
}

func UpdateAmkoUuidGSLBConfig(gc *gslbalphav1.GSLBConfig, uuid string) error {
	annotations := gc.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AmkoUuid] = uuid

	patchPayload := map[string]interface{}{
		"metadata": map[string]map[string]string{
			"annotations": annotations,
		},
	}
	payloadBytes, err := json.Marshal(patchPayload)
	if err != nil {
		return fmt.Errorf("error in marshalling patchpayload: %v", patchPayload)
	}
	if _, err := AMKOControlConfig().GSLBClientset().AmkoV1alpha1().GSLBConfigs(gcObj.configObj.Namespace).Patch(context.TODO(),
		gcObj.configObj.Name, types.MergePatchType, payloadBytes, metav1.PatchOptions{}); err != nil {
		return err
	}
	return nil
}

func UpdateGSLBConfigStatus(msg string) error {
	if !AMKOControlConfig().PublishGSLBStatus() {
		return nil
	}

	// ensure that once the GSLBConfig object has been updated and the status already reflects
	// that, no other status updates can be done unless AMKO is rebooted
	if GetGslbConfigObjUpdated() {
		Logf("GSLBConfig object has been updated, AMKO reboot is required")
		return nil
	}
	updateGSLBConfigStatusMsg(msg)
	gcStatus := gslbalphav1.GSLBConfigStatus{
		State: msg,
	}
	patchPayload, err := json.Marshal(map[string]interface{}{
		"status": gcStatus,
	})
	if err != nil {
		Errf("Error in marshalling status for GC object: %v", err)
		return nil
	}
	updatedGC, updateErr := AMKOControlConfig().GSLBClientset().AmkoV1alpha1().GSLBConfigs(gcObj.configObj.Namespace).Patch(context.TODO(),
		gcObj.configObj.Name, types.MergePatchType, patchPayload, metav1.PatchOptions{})
	if updateErr != nil {
		Errf("error in updating the GSLBConfig object: %s", updateErr.Error())
		return errors.New("error in GSLBConfig object update, " + updateErr.Error())
	}
	SetGSLBConfigObj(updatedGC)
	return nil
}

// gslbConfigSet and its setter and getter functions, to be used by the AddGSLBConfig method. This value
// is set to true once a GSLB Configuration has been successfully done.
var gslbConfigSet bool = false

func IsGSLBConfigSet() bool {
	return gslbConfigSet
}

func SetGSLBConfig(value bool) {
	gslbConfigSet = value
}

type AviControllerConfig struct {
	Username string
	Password string
	IPAddr   string
	Version  string
	Tenant   string
}

var gslbLeaderConfig AviControllerConfig
var leaderConfig sync.Once

func NewAviControllerConfig(username, password, ipAddr, version string, tenant string) *AviControllerConfig {
	leaderConfig.Do(func() {
		gslbLeaderConfig = AviControllerConfig{
			Username: username,
			Password: password,
			IPAddr:   ipAddr,
			Version:  version,
			Tenant:   tenant,
		}
	})
	return &gslbLeaderConfig
}

func GetAviConfig() AviControllerConfig {
	return gslbLeaderConfig
}

func GetTenant() string {
	return gslbLeaderConfig.Tenant
}

func GetTenantInNamespace(namespace, cname string) string {
	nsObj, err := GetInformersPerCluster(cname).NSInformer.Lister().Get(namespace)
	if err != nil {
		utils.AviLog.Warnf("Failed to GET the namespace details falling back to the default tenant, namespace: %s, error :%s", namespace, err.Error())
		return GetTenant()
	}
	tenant, ok := nsObj.Annotations[TenantAnnotation]
	if !ok || tenant == "" {
		return GetTenant()
	}
	return tenant
}
func GetTenantInNamespaceAnnotation(namespace, cname string) string {
	nsObj, err := GetInformersPerCluster(cname).NSInformer.Lister().Get(namespace)
	if err != nil {
		utils.AviLog.Warnf("Failed to GET the namespace details falling back to the default tenant, namespace: %s, error :%s", namespace, err.Error())
		return GetTenant()
	}
	tenant, _ := nsObj.Annotations[TenantAnnotation]
	return tenant
}

var allClusterContexts []string

func AddClusterContext(cc string) {
	if IsClusterContextPresent(cc) {
		return
	}
	allClusterContexts = append(allClusterContexts, cc)
}

func IsClusterContextPresent(cc string) bool {
	for _, context := range allClusterContexts {
		if context == cc {
			return true
		}
	}
	return false
}

var controllerIsLeader bool

func SetControllerAsLeader() {
	controllerIsLeader = true
}

func SetControllerAsFollower() {
	controllerIsLeader = false
}

func IsControllerLeader() bool {
	return controllerIsLeader
}

func GetKeyIdx(strList []string, key string) (int, bool) {
	for i, str := range strList {
		if str == key {
			return i, true
		}
	}
	return -1, false
}

var waitGroupMap map[string]*sync.WaitGroup
var wgSyncOnce sync.Once

const (
	WGIngestion = "ingestion"
	WGFastRetry = "fastretry"
	WGSlowRetry = "slowretry"
	WGGraph     = "graph"
)

func SetWaitGroupMap() {
	wgSyncOnce.Do(func() {
		waitGroupMap = make(map[string]*sync.WaitGroup)
		waitGroupMap[WGIngestion] = &sync.WaitGroup{}
		waitGroupMap[WGFastRetry] = &sync.WaitGroup{}
		waitGroupMap[WGGraph] = &sync.WaitGroup{}
		waitGroupMap[WGSlowRetry] = &sync.WaitGroup{}
	})
}

func GetWaitGroupFromMap(name string) *sync.WaitGroup {
	wg, ok := waitGroupMap[name]
	if !ok {
		return nil
	}
	return wg
}

func WaitForWorkersToExit() {
	timeoutChan := make(chan struct{})
	// timeout after 10 seconds
	timeout := 10 * time.Second
	go func() {
		defer close(timeoutChan)
		for _, wg := range waitGroupMap {
			wg.Wait()
		}
	}()
	select {
	case <-timeoutChan:
		return
	case <-time.After(timeout):
		return
	}
}

func IsLogLevelValid(level string) bool {
	_, ok := utils.LogLevelMap[level]
	return ok
}

type ResyncStatus struct {
	required bool
	lock     sync.RWMutex
}

var resync ResyncStatus
var resyncOnce sync.Once

func InitResync() {
	resyncOnce.Do(func() {
		resync.required = false
	})
}

func SetResyncRequired(value bool) {
	resync.lock.Lock()
	defer resync.lock.Unlock()
	resync.required = value
}

func IsResyncRequired() bool {
	resync.lock.RLock()
	defer resync.lock.RUnlock()
	return resync.required
}

func GetHmTypeForProtocol(protocol string) (string, error) {
	switch protocol {
	case ProtocolTCP:
		return SystemHealthMonitorTypeTCP, nil
	case ProtocolUDP:
		return SystemHealthMonitorTypeUDP, nil
	default:
		return "", errors.New("unrecognized protocol")
	}
}

func GetHmTypeForTLS(tls bool) string {
	if tls {
		return SystemGslbHealthMonitorHTTPS
	}
	return SystemGslbHealthMonitorHTTP
}

func CheckNameLength(name string, prefixToExclude string) bool {
	if len(name)+len(prefixToExclude) < 256 {
		return true
	}
	return false
}

func EncodeHMName(gsName string) string {
	gsNameHash := sha1.Sum([]byte(gsName))
	encodedHMName := hex.EncodeToString(gsNameHash[:])
	return encodedHMName
}

// hmCreatedByAMKO checks if the health monitor is created by AMKO by checking the prefix of the
// HM name. If the prefix "amko" exists for the HM name, we return true, else false.
func HMCreatedByAMKO(hmName string) bool {
	hmNameSplit := strings.Split(hmName, "--")
	if len(hmNameSplit) >= 2 && hmNameSplit[0] == "amko" {
		return true
	}
	return false
}

// HostRuleMeta stores a partial set of information stripped from the HostRule object,
// information only required for AMKO.
type HostRuleMeta struct {
	LFqdn   string
	GSFqdn  string
	TLS     bool
	Aliases []string
}

func GetHostRuleMeta(lFqdn, gsFqdn string, tls bool, aliases []string) HostRuleMeta {
	return HostRuleMeta{LFqdn: lFqdn, GSFqdn: gsFqdn, TLS: tls, Aliases: aliases}
}

var customFqdnMode bool
var fqdnOnce sync.Once

func SetCustomFqdnMode(custom *bool) {
	value := false
	if custom != nil {
		value = *custom
	}
	fqdnOnce.Do(func() {
		customFqdnMode = value
	})
}

func GetCustomFqdnMode() bool {
	return customFqdnMode
}

var isTestMode bool

func SetTestMode(t bool) {
	isTestMode = t
}

func InTestMode() bool {
	return isTestMode == true
}

// GetUriFromAvi is a wrapper over Avi SDK's GetCollectionRaw which keeps on calling the get uri
// till we either get a result or a 404. It retries infinitely for calls which have infiniteRetry
// set. For others, it retries 3 times.
func GetUriFromAvi(uri string, aviClient *clients.AviClient, infiniteRetry bool) (*session.AviCollectionResult, error) {
	var result session.AviCollectionResult
	var err error

	for i := 0; ; i++ {
		result, err = aviClient.AviSession.GetCollectionRaw(uri)
		if err == nil {
			return &result, nil
		}
		aviError, ok := err.(session.AviError)
		if !ok {
			Errf("error in parsing the web api error to avi error: %v, will retry %d", err, i)
			// TODO: change this to an exponential backoff logic
			time.Sleep(RestSleepTime)
			continue
		}
		// For 404, return
		if aviError.HttpStatusCode == 404 {
			return nil, fmt.Errorf("%s", *aviError.Message)
		}
		// All other errors, retry
		Errf("uri: %s, aviErr: %v, will retry %d", uri, aviError, i)
		if !infiniteRetry && i == 2 {
			return nil, err
		}
		time.Sleep(RestSleepTime)
	}
}

// LogAndPanic first logs an error message and then calls panic to stop the execution.
// This ensures that logs messages are not missing when PVC is used for storing the logs
func LogAndPanic(panicMsg string) {
	Errf(panicMsg)
	AMKOControlConfig().PodEventf(corev1.EventTypeWarning, AMKOShutdown, panicMsg)
	panic(panicMsg)
}

var gslbConfigObjUpdated bool
var updatedOnce sync.Once

func SetGslbConfigObjUpdated(value bool) {
	updatedOnce.Do(func() {
		gslbConfigObjUpdated = true
	})
}

func GetGslbConfigObjUpdated() bool {
	return gslbConfigObjUpdated
}

// Difference compares two slices a & b, returns the elements in `a` that aren't in `b`.
func SetDifference(a, b []string) []string {
	setA := sets.NewString(a...)
	setB := sets.NewString(b...)
	return setA.Difference(setB).List()
}

func SetEqual(a, b []string) bool {
	setA := sets.NewString(a...)
	setB := sets.NewString(b...)
	return setA.Equal(setB)
}

func IsDefaultSecretEnabled(annotations map[string]string) bool {
	_, ok := annotations[DefaultSecretEnabled]
	return ok
}

func GetDefaultPKI(aviClient *clients.AviClient) *string {
	pki := models.PKIprofile{}
	baseURI := "/api/pkiprofile"
	uri := baseURI + "?page_size=10"
	result, err := GetUriFromAvi(uri+"&is_federated=true", aviClient, false)
	if err != nil {
		Warnf("object: AviPkiProfileCache, msg: Pkiprofile get URI %s returned error: %v", uri, err.Error())
	} else {
		elems := make([]json.RawMessage, result.Count)
		err = json.Unmarshal(result.Results, &elems)
		if err != nil {
			Warnf("failed to unmarshal pki  profile ref, err: " + err.Error())
		} else {
			for i := 0; i < len(elems); i++ {
				err := json.Unmarshal(elems[i], &pki)
				if err != nil {
					Warnf("failed to unmarshal pki profile element, err: %s", err.Error())
					continue
				}
				pkiProfileRef := "/api/pkiprofile?name=" + *pki.Name
				return &pkiProfileRef
			}
		}
	}
	return nil
}

const (
	VSAnnotation         = "ako.vmware.com/host-fqdn-vs-uuid-map"
	ControllerAnnotation = "ako.vmware.com/controller-cluster-uuid"
	TenantAnnotation     = "ako.vmware.com/tenant-name"
)
