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

package nodes

import (
	"strconv"
	"strings"
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/store"

	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

const (
	HmNamePrefix  = "amko--"
	CreatedByAMKO = "created by: amko"
	NonPathHM     = "NonPathHM"
	PathHM        = "PathHM"
)

var aviGSGraphInstance *AviGSGraphLister
var avionce sync.Once

// deleteGSGraphInstance is only used as a delete cache between layer 2 and layer 3.
// If a GS object is marked for deletion, layer 2 puts it into the delete cache and
// removes it from aviGSGraphInstance.
var deleteGSGraphInstance *AviGSGraphLister
var deleteOnce sync.Once

func SharedDeleteGSGraphLister() *AviGSGraphLister {
	deleteOnce.Do(func() {
		deleteGSGraphStore := store.NewObjectMapStore()
		deleteGSGraphInstance = &AviGSGraphLister{AviGSGraphStore: deleteGSGraphStore}
	})
	return deleteGSGraphInstance
}

type AviGSGraphLister struct {
	AviGSGraphStore *store.ObjectMapStore
}

func SharedAviGSGraphLister() *AviGSGraphLister {
	avionce.Do(func() {
		aviGSGraphStore := store.NewObjectMapStore()
		aviGSGraphInstance = &AviGSGraphLister{AviGSGraphStore: aviGSGraphStore}
	})
	return aviGSGraphInstance
}

func (a *AviGSGraphLister) Save(gsName string, graph interface{}) {
	gslbutils.Logf("gsName: %s, msg: %s", gsName, "saving GSLB graph")

	a.AviGSGraphStore.AddOrUpdate(gsName, graph)
}

func (a *AviGSGraphLister) Get(gsName string) (bool, interface{}) {
	ok, obj := a.AviGSGraphStore.Get(gsName)
	return ok, obj
}

func (a *AviGSGraphLister) GetAll() []string {
	return a.AviGSGraphStore.GetAllObjectNames()
}

func (a *AviGSGraphLister) Delete(gsName string) {
	a.AviGSGraphStore.Delete(gsName)
}

// AviGSK8sObj represents a K8S/Openshift object from which a GS was built.
type AviGSK8sObj struct {
	Cluster       string
	ObjType       string
	Name          string
	Namespace     string
	IPAddr        string
	Weight        int32
	Priority      int32
	IsPassthrough bool
	// Port and protocol will be only used by LB service
	Port               int32
	Proto              string
	TLS                bool
	Paths              []string
	VirtualServiceUUID string
	ControllerUUID     string
	SyncVIPOnly        bool
}

func (gsk8sObj AviGSK8sObj) getCopy() AviGSK8sObj {
	paths := make([]string, len(gsk8sObj.Paths))
	copy(paths, gsk8sObj.Paths)
	obj := AviGSK8sObj{
		Cluster:            gsk8sObj.Cluster,
		ObjType:            gsk8sObj.ObjType,
		Name:               gsk8sObj.Name,
		Namespace:          gsk8sObj.Namespace,
		IPAddr:             gsk8sObj.IPAddr,
		Weight:             gsk8sObj.Weight,
		Priority:           gsk8sObj.Priority,
		Port:               gsk8sObj.Port,
		Proto:              gsk8sObj.Proto,
		TLS:                gsk8sObj.TLS,
		Paths:              paths,
		VirtualServiceUUID: gsk8sObj.VirtualServiceUUID,
		ControllerUUID:     gsk8sObj.ControllerUUID,
		SyncVIPOnly:        gsk8sObj.SyncVIPOnly,
		IsPassthrough:      gsk8sObj.IsPassthrough,
	}
	return obj
}

type PathHealthMonitorDetails struct {
	Name            string
	IngressProtocol string
	Path            string
}

func (pathHm PathHealthMonitorDetails) GetPathHMDescription(gsName string) string {
	return CreatedByAMKO + ", gsname: " + gsName + ", path: " + pathHm.Path + ", protocol: " + pathHm.IngressProtocol
}

type HealthMonitor struct {
	Name       string // used for non path HMs
	HMProtocol string
	Port       int32
	Type       string
	PathHM     []PathHealthMonitorDetails // used for path based HMs
}

func (hm HealthMonitor) GetHMDescription(gsName string) []string {
	desc := []string{}
	descPrefix := CreatedByAMKO + ", gsname: " + gsName
	desc = append(desc, descPrefix)
	if hm.Type == NonPathHM {
		return desc
	} else if hm.Type == PathHM {
		for _, pathHm := range hm.PathHM {
			desc = append(desc, descPrefix+", path: "+pathHm.Path+", protocol: "+pathHm.IngressProtocol)
		}
		return desc
	}
	gslbutils.Debugf("cannot generate hm description, hm type not recognised : %s", hm.Type)
	return []string{}
}

func (hm HealthMonitor) GetPathHMDescription(gsName, path string) string {
	for _, pathHm := range hm.PathHM {
		if pathHm.Path == path {
			return pathHm.GetPathHMDescription(gsName)
		}
	}
	return ""
}

func (hm HealthMonitor) getChecksum(hmDescription []string) uint32 {
	return gslbutils.GetGSLBHmChecksum(hm.HMProtocol, hm.Port, hmDescription)
}

func (hm HealthMonitor) getCopy() HealthMonitor {
	pathDetails := make([]PathHealthMonitorDetails, len(hm.PathHM))
	copy(pathDetails, hm.PathHM)
	hmObj := HealthMonitor{
		Name:       hm.Name,
		HMProtocol: hm.HMProtocol,
		Port:       hm.Port,
		Type:       hm.Type,
		PathHM:     pathDetails,
	}
	return hmObj
}

type ThirdPartyMember struct {
	Site string
	VIP  string
}

// AviGSObjectGraph is a graph constructed using AviGSNode. It is a one-to-one mapping between
// the name of the object and the GSLB Model node.
type AviGSObjectGraph struct {
	Name        string
	Tenant      string
	DomainNames []string
	// MemberObjs is a list of K8s/openshift objects from which this AviGS was built.
	MemberObjs         []AviGSK8sObj
	GraphChecksum      uint32
	RetryCount         int
	Hm                 HealthMonitor
	HmRefs             []string
	SitePersistenceRef *string
	TTL                *int
	GslbPoolAlgorithm  *gslbalphav1.PoolAlgorithmSettings
	Lock               sync.RWMutex
}

func (v *AviGSObjectGraph) SetRetryCounter(num ...int) {
	v.Lock.Lock()
	defer v.Lock.Unlock()
	if len(num) > 0 {
		v.RetryCount = num[0]
		return
	}
	v.RetryCount = gslbutils.DefaultRetryCount
}

func (v *AviGSObjectGraph) GetRetryCounter() int {
	v.Lock.RLock()
	defer v.Lock.RUnlock()
	return v.RetryCount
}

func (v *AviGSObjectGraph) DecrementRetryCounter() {
	v.Lock.Lock()
	defer v.Lock.Unlock()
	if v.RetryCount > 0 {
		v.RetryCount--
	}
}

func (v *AviGSObjectGraph) GetChecksum() uint32 {
	// Calculate checksum for this graph and return
	v.CalculateChecksum()
	return v.GraphChecksum
}

func (v *AviGSObjectGraph) GetHmChecksum(hmDescription []string) uint32 {
	return v.Hm.getChecksum(hmDescription)
}

func (v *AviGSObjectGraph) CalculateChecksum() {
	// A sum of fields for this GS
	var memberObjs []string
	var memberAddrs []string

	for _, gsMember := range v.MemberObjs {
		var server string
		if !gsMember.SyncVIPOnly {
			server = gsMember.VirtualServiceUUID + "-" + gsMember.ControllerUUID
		} else {
			server = gsMember.IPAddr
		}
		memberAddrs = append(memberAddrs, server+"-"+strconv.Itoa(int(gsMember.Weight))+
			"-"+strconv.Itoa(int(gsMember.Priority)))
		if gsMember.ObjType == gslbutils.ThirdPartyMemberType {
			continue
		}
		memberObjs = append(memberObjs, gsMember.ObjType+"/"+gsMember.Cluster+"/"+gsMember.Namespace+"/"+gsMember.Name)
	}

	hmNames := []string{}
	if len(v.HmRefs) == 0 {
		if v.Hm.Name != "" {
			hmNames = append(hmNames, v.Hm.Name)
		} else {
			hmNames = v.GetHmPathNamesList()
		}
	} else {
		hmNames = make([]string, len(v.HmRefs))
		copy(hmNames, v.HmRefs)
	}

	v.GraphChecksum = gslbutils.GetGSLBServiceChecksum(memberAddrs, v.DomainNames, memberObjs, hmNames,
		v.SitePersistenceRef, v.TTL, v.GslbPoolAlgorithm)
}

// GetMemberRouteList returns a list of member objects
func (v *AviGSObjectGraph) GetMemberObjList() []string {
	var memberObjs []string
	for _, obj := range v.MemberObjs {
		if obj.ObjType == gslbutils.ThirdPartyMemberType {
			continue
		}
		memberObjs = append(memberObjs, obj.ObjType+"/"+obj.Cluster+"/"+obj.Namespace+"/"+obj.Name)
	}
	return memberObjs
}

func NewAviGSObjectGraph() *AviGSObjectGraph {
	return &AviGSObjectGraph{RetryCount: gslbutils.DefaultRetryCount}
}

func (v *AviGSObjectGraph) BuildPathHM(gsName, path string, isSec bool) PathHealthMonitorDetails {
	ingProtocol := "http"
	if isSec {
		ingProtocol = "https"
	}
	encodedHMName := gslbutils.EncodeHMName(ingProtocol + "--" + gsName + "--" + path)
	if gslbutils.CheckNameLength(encodedHMName, HmNamePrefix) {
		pathHm := PathHealthMonitorDetails{
			Name:            HmNamePrefix + encodedHMName,
			IngressProtocol: ingProtocol,
			Path:            path,
		}
		return pathHm
	}
	gslbutils.Errf("hm: %s, msg: hm name could not be encoded", gsName+path)
	return PathHealthMonitorDetails{}
}

func (v *AviGSObjectGraph) buildHmPathList() {
	// if any member object is TLS, we put HTTPS health monitors for the entire object, basically,
	// TLS takes precedence
	ifSec := false
	for _, member := range v.MemberObjs {
		if member.TLS {
			ifSec = true
		}
	}
	// clear out all path based HM names first
	v.Hm.PathHM = make([]PathHealthMonitorDetails, 0)

	// add the member paths
	for _, member := range v.MemberObjs {
		for _, path := range member.Paths {
			pathHM := v.BuildPathHM(v.Name, path, ifSec)
			if PresentInHealthMonitorPathList(pathHM, v.Hm.PathHM) {
				continue
			}
			v.Hm.PathHM = append(v.Hm.PathHM, pathHM)
		}
	}
	gslbutils.Debugf("gsName: %s, pathHMList: %v, msg: rebuilt path list for GS", v.Name, v.Hm.PathHM)
}

func (v *AviGSObjectGraph) BuildNonPathHmName(gsName string) string {
	encodedHMName := gslbutils.EncodeHMName(gsName)
	if gslbutils.CheckNameLength(encodedHMName, HmNamePrefix) {
		return HmNamePrefix + encodedHMName
	}
	gslbutils.Errf("hm: %s, msg: hm name could not be encoded", gsName)
	return ""
}

func (v *AviGSObjectGraph) buildNonPathHealthMonitorFromObj(port int32, isPassthrough bool, protocol, key string) {
	hmName := ""
	if isPassthrough {
		hmName = gslbutils.SystemGslbHealthMonitorPassthrough
	} else {
		hmName = v.BuildNonPathHmName(v.Name)
	}
	hmProtocol, err := gslbutils.GetHmTypeForProtocol(protocol)
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: can't create a health monitor for this GS graph %s", key,
			v.Name, err.Error())
		hmProtocol = ""
	}
	v.Hm = HealthMonitor{
		Name:       hmName,
		HMProtocol: hmProtocol,
		Port:       port,
		Type:       NonPathHM,
	}
	v.MemberObjs[0].Port = port
	v.MemberObjs[0].Proto = protocol
}

func (v *AviGSObjectGraph) buildNonPathHealthMonitor(metaObj k8sobjects.MetaObject, key string) {
	port, err := metaObj.GetPort()
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: port not found for this object", key, v.Name)
		return
	}
	hmName := ""
	if metaObj.IsPassthrough() {
		hmName = gslbutils.SystemGslbHealthMonitorPassthrough
	} else {
		hmName = v.BuildNonPathHmName(v.Name)
	}
	protocol, err := metaObj.GetProtocol()
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: protocol not found for this object", key, v.Name)
		return
	}

	hmProtocol, err := gslbutils.GetHmTypeForProtocol(protocol)
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: can't create a health monitor for this GS graph %s", key,
			v.Name, err.Error())
		hmProtocol = ""
	}
	v.Hm = HealthMonitor{
		Name:       hmName,
		HMProtocol: hmProtocol,
		Port:       port,
		Type:       NonPathHM,
	}
	v.MemberObjs[0].Port = port
	v.MemberObjs[0].Proto = protocol
}

func (v *AviGSObjectGraph) buildAndAttachHealthMonitorsFromObj(obj AviGSK8sObj, key string) {
	objType := obj.ObjType
	if objType == gslbutils.SvcType {
		v.buildNonPathHealthMonitorFromObj(obj.Port, obj.IsPassthrough, obj.Proto, key)
		return
	}

	// for objects other than service type load balancer
	// check if its a non-path based route (passthrough route)
	if obj.IsPassthrough {
		// we have a passthrough route here, build a non-path based hm and return
		gslbutils.Debugf("key: %s, gsName: %s, msg: passthrough route, will build a non-path hm", key, v.Name)
		v.buildNonPathHealthMonitorFromObj(obj.Port, obj.IsPassthrough, obj.Proto, key)
		return
	}
	// else other secure/insecure route
	tls := obj.TLS
	if tls {
		v.Hm.HMProtocol = gslbutils.SystemGslbHealthMonitorHTTPS
		v.Hm.Port = gslbutils.DefaultHTTPHealthMonitorPort
	} else {
		v.Hm.HMProtocol = gslbutils.SystemGslbHealthMonitorHTTP
		v.Hm.Port = gslbutils.DefaultHTTPSHealthMonitorPort
	}
}

func (v *AviGSObjectGraph) buildAndAttachHealthMonitors(metaObj k8sobjects.MetaObject, key string) {
	objType := metaObj.GetType()
	if objType == gslbutils.SvcType {
		v.buildNonPathHealthMonitor(metaObj, key)
		return
	}

	// for objects other than service type load balancer
	// check if its a non-path based route (passthrough route)
	if metaObj.IsPassthrough() {
		// we have a passthrough route here, build a non-path based hm and return
		gslbutils.Debugf("key: %s, gsName: %s, msg: passthrough route, will build a non-path hm", key, v.Name)
		v.buildNonPathHealthMonitor(metaObj, key)
		return
	}
	// else other secure/insecure route
	tls, err := getTLSFromObj(metaObj)
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: error in getting tls for object %s", key, v.Name, err.Error())
		return
	}
	if tls {
		v.Hm.HMProtocol = gslbutils.SystemGslbHealthMonitorHTTPS
		v.Hm.Port = gslbutils.DefaultHTTPHealthMonitorPort
	} else {
		v.Hm.HMProtocol = gslbutils.SystemGslbHealthMonitorHTTP
		v.Hm.Port = gslbutils.DefaultHTTPSHealthMonitorPort
	}
}

func (v *AviGSObjectGraph) UpdateAviGSGraphWithGSFqdn(gsFqdn string, newObj bool, tls bool) {
	v.Lock.Lock()
	defer v.Lock.Unlock()

	// update the GSLB HostRule or GDP properties for the GS
	setGSLBPropertiesForGS(gsFqdn, v, false, tls)
	if !newObj {
		v.RetryCount = gslbutils.DefaultRetryCount
		v.CalculateChecksum()
		return
	}
	v.Name = gsFqdn
	v.Tenant = utils.ADMIN_NS
	v.RetryCount = gslbutils.DefaultRetryCount
	v.CalculateChecksum()
}

func (v *AviGSObjectGraph) GetGSMembersByCluster(cname string) []AviGSK8sObj {
	v.Lock.RLock()
	defer v.Lock.RUnlock()

	members := []AviGSK8sObj{}
	for _, m := range v.MemberObjs {
		members = append(members, m.getCopy())
	}
	return members
}

func (v *AviGSObjectGraph) ConstructAviGSGraph(gsFqdn, key string, memberObjs []AviGSK8sObj) {
	v.Lock.Lock()
	defer v.Lock.Unlock()

	// The GSLB service will be put into the admin tenant
	v.Name = gsFqdn
	v.Tenant = utils.ADMIN_NS
	v.DomainNames = []string{gsFqdn}
	v.MemberObjs = memberObjs
	v.RetryCount = gslbutils.DefaultRetryCount

	// set the GS properties according to the GSLBHostRule or GDP
	setGSLBPropertiesForGS(gsFqdn, v, true, memberObjs[0].TLS)

	if v.HmRefs == nil || len(v.HmRefs) == 0 {
		// Build the list of health monitors
		v.buildHmPathList()
		v.buildAndAttachHealthMonitorsFromObj(memberObjs[0], key)
	}
	v.GetChecksum()
	gslbutils.Logf("key: %s, AviGSGraph: %s, msg: %s", key, v.Name, "created a new Avi GS graph")
}

func (v *AviGSObjectGraph) ConstructAviGSGraphFromObjects(gsFqdn string, members []AviGSK8sObj, key string) {
	v.ConstructAviGSGraph(gsFqdn, key, members)
}

func (v *AviGSObjectGraph) ConstructAviGSGraphFromMeta(gsName, key string, metaObj k8sobjects.MetaObject) {
	menberObj, err := BuildGSMemberObjFromMeta(metaObj, gsName)
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: error in building member object from meta object: %v",
			key, gsName, err)
		return
	}
	v.ConstructAviGSGraph(gsName, key, []AviGSK8sObj{menberObj})
}

func (v *AviGSObjectGraph) checkAndUpdateNonPathHealthMonitor(objType string, isPassthrough bool) {
	// this function has to be called only for LB service type members or passthrough route members
	if len(v.MemberObjs) <= 0 {
		gslbutils.Errf("gsName: %s, no member objects for this avi gs, can't check the health monitor", v.Name)
		return
	}

	hmName := ""
	if isPassthrough {
		hmName = gslbutils.SystemGslbHealthMonitorPassthrough
	} else {
		hmName = v.BuildNonPathHmName(v.Name)
	}

	var newPort int32
	var newProto string

	for idx, member := range v.MemberObjs {
		if idx == 0 {
			newPort = member.Port
			newProto = member.Proto
		}
		if newPort > member.Port {
			newPort = member.Port
			newProto = member.Proto
		}
	}
	hmProtocol, err := gslbutils.GetHmTypeForProtocol(newProto)
	if err != nil {
		gslbutils.Errf("can't change the health monitor for gs %s, port: %d, protocol %s: %s", v.Name, newPort,
			newProto, err.Error())
		hmProtocol = ""
	}
	// overwrite the new minimum port and protocol
	v.Hm.Name = hmName
	v.Hm.Port = newPort
	v.Hm.HMProtocol = hmProtocol
}

func (v *AviGSObjectGraph) updateGSHmPathListAndProtocol() {
	// build the path based health monitor list
	v.buildHmPathList()
	gslbutils.Debugf("gsName: %s, added path HMs to the gslb hm path list, path hm list: %v", v.Name, v.Hm)

	// protocol change required?
	// protocol will only be changed only if the current protocol doesn't match any of the members' protocol
	currProtocol := v.Hm.HMProtocol
	idx := 0
	var member AviGSK8sObj
	for idx, member = range v.MemberObjs {
		if gslbutils.GetHmTypeForTLS(member.TLS) == currProtocol {
			break
		}
	}
	if idx < len(v.MemberObjs)-1 || gslbutils.GetHmTypeForTLS(v.MemberObjs[idx].TLS) == currProtocol {
		// no change required
		return
	} else {
		// update the Hm protocol from any one of the member objects
		v.Hm.HMProtocol = gslbutils.GetHmTypeForTLS(v.MemberObjs[0].TLS)
	}
}

func (v *AviGSObjectGraph) SetPropertiesForGS(gsFqdn string, tls bool) {
	v.Lock.Lock()
	defer v.Lock.Unlock()

	v.DomainNames = []string{v.Name}
	setGSLBPropertiesForGS(gsFqdn, v, false, tls)
}

// AddUpdateGSMember adds/updates a GS member according to the properties in newMember. Returns
// true if an existing member needs to be removed.
func (v *AviGSObjectGraph) AddUpdateGSMember(newMember AviGSK8sObj) bool {
	v.SetPropertiesForGS(v.Name, newMember.TLS)

	v.Lock.Lock()
	defer v.Lock.Unlock()

	// if the member with the "ipAddr" exists, then just update the weight, else add a new member
	for idx, memberObj := range v.MemberObjs {
		if newMember.ObjType != memberObj.ObjType {
			continue
		}
		if newMember.Cluster != memberObj.Cluster {
			continue
		}
		if newMember.Namespace != memberObj.Namespace {
			continue
		}
		if newMember.Name != memberObj.Name {
			continue
		}

		// if we reach here, it means this is the member we need to update
		if !newMember.SyncVIPOnly && (newMember.ControllerUUID == "" || newMember.VirtualServiceUUID == "") {
			// this error indicates that the annotation of the controller/vs uuid were removed from the ingress
			// object, which would indicate that we can't rely on old fields anymore, and we need to remove the
			// member
			gslbutils.Errf("gsName: %s, cluster: %s, namespace: %s, msg: controller UUID or VS UUID missing from the object, won't update member %s",
				v.Name, newMember.Cluster, newMember.Namespace, newMember.Name)
			return true
		}
		gslbutils.Debugf("gsName: %s, msg: updating member for type %s", v.Name, newMember.ObjType)
		v.MemberObjs[idx] = newMember
		if v.HmRefs == nil || len(v.HmRefs) == 0 {
			// update the health monitor(s)
			if newMember.ObjType == gslbutils.SvcType || newMember.IsPassthrough {
				v.checkAndUpdateNonPathHealthMonitor(newMember.ObjType, newMember.IsPassthrough)
			} else {
				v.updateGSHmPathListAndProtocol()
			}
		}
		return false
	}
	// new member object
	if !newMember.SyncVIPOnly && (newMember.ControllerUUID == "" || newMember.VirtualServiceUUID == "") {
		gslbutils.Errf("gsName: %s, cluster: %s, namespace: %s, member: %s, msg: controller UUID or VS UUID missing from the object, won't update member",
			v.Name, newMember.Cluster, newMember.Namespace, newMember.Name)
		return false
	}

	v.MemberObjs = append(v.MemberObjs, newMember)
	if v.HmRefs == nil || len(v.HmRefs) == 0 {
		// update the health monitors if hm refs is not nil or non-zero
		if newMember.ObjType == gslbutils.SvcType || newMember.IsPassthrough {
			v.checkAndUpdateNonPathHealthMonitor(newMember.ObjType, newMember.IsPassthrough)
		} else {
			v.updateGSHmPathListAndProtocol()
		}
	}
	return false
}

func (v *AviGSObjectGraph) UpdateGSMemberFromMetaObj(metaObj k8sobjects.MetaObject) {
	tls, _ := getTLSFromObj(metaObj)
	v.SetPropertiesForGS(v.Name, tls)

	member, err := BuildGSMemberObjFromMeta(metaObj, v.Name)
	if err != nil {
		gslbutils.Errf("gsName: %s, msg: error in building gs member from meta: %v", err)
		return
	}

	deleteMember := v.AddUpdateGSMember(member)
	if deleteMember {
		gslbutils.Logf("gsName: %s, msg: error in updating GS member, will delete the member")
		v.DeleteMember(member.Cluster, member.Namespace, member.Name, member.ObjType)
	}
}

func (v *AviGSObjectGraph) DeleteMember(cname, ns, name, objType string) {
	idx := -1
	v.Lock.Lock()
	defer v.Lock.Unlock()
	for i, memberObj := range v.MemberObjs {
		if objType == memberObj.ObjType && cname == memberObj.Cluster && ns == memberObj.Namespace && name == memberObj.Name {
			idx = i
			break
		}
	}
	if idx == -1 {
		gslbutils.Warnf("gsGraph: %s, route: %v, msg: couldn't find route member in GS")
		return
	}
	// Delete the member route
	v.MemberObjs = append(v.MemberObjs[:idx], v.MemberObjs[idx+1:]...)
	if len(v.MemberObjs) == 0 {
		return
	}

	// check if the health monitor needs to be updated
	for _, member := range v.MemberObjs {
		// update non path based health monitor only for LB services or non-path based members
		isPassthrough := false
		if member.ObjType != gslbutils.SvcType && len(member.Paths) == 0 {
			// this is a passthrough member
			isPassthrough = true
		}
		if member.ObjType == gslbutils.SvcType || isPassthrough {
			v.checkAndUpdateNonPathHealthMonitor(member.ObjType, isPassthrough)
			return
		}
	}
	// if no members are services, then they must be routes/ingresses, so update the HM if required
	v.updateGSHmPathListAndProtocol()
}

func (v *AviGSObjectGraph) IsHmTypeCustom(hmName string) bool {
	return strings.HasPrefix(hmName, HmNamePrefix)
}

func (v *AviGSObjectGraph) GetHmPathNamesList() []string {
	gslbutils.Debugf("gs object and its path names: %v, paths: %v", v, v.Hm.PathHM)
	var hmNameList []string
	for _, hm := range v.Hm.PathHM {
		hmNameList = append(hmNameList, hm.Name)
	}
	return hmNameList
}

func (v *AviGSObjectGraph) MembersLen() int {
	v.Lock.RLock()
	defer v.Lock.RUnlock()
	return len(v.MemberObjs)
}

func (v *AviGSObjectGraph) GetGSMember(cname, ns, name string) AviGSK8sObj {
	v.Lock.RLock()
	defer v.Lock.RUnlock()
	for _, member := range v.MemberObjs {
		if member.Cluster == cname && member.Namespace == ns && member.Name == name {
			return member
		}
	}
	return AviGSK8sObj{}
}

func (v *AviGSObjectGraph) GetMemberObjs() []AviGSK8sObj {
	v.Lock.RLock()
	defer v.Lock.RUnlock()
	objs := make([]AviGSK8sObj, len(v.MemberObjs))
	for idx := range v.MemberObjs {
		objs[idx].Cluster = v.MemberObjs[idx].Cluster
		objs[idx].Name = v.MemberObjs[idx].Name
		objs[idx].Namespace = v.MemberObjs[idx].Namespace
		objs[idx].IPAddr = v.MemberObjs[idx].IPAddr
		objs[idx].Weight = v.MemberObjs[idx].Weight
		objs[idx].Priority = v.MemberObjs[idx].Priority
		objs[idx].ObjType = v.MemberObjs[idx].ObjType
	}
	return objs
}

// GetUniqueMemberObjs returns a non-duplicated list of objects, uniqueness is checked by the IPAddr
// TODO: Check the uniqueness depending on the member type (vip or vs uuid)
func (v *AviGSObjectGraph) GetUniqueMemberObjs() []AviGSK8sObj {
	v.Lock.RLock()
	defer v.Lock.RUnlock()

	memberVips := []string{}
	uniqueObjs := []AviGSK8sObj{}

	for _, memberObj := range v.MemberObjs {
		if gslbutils.PresentInList(memberObj.IPAddr, memberVips) {
			continue
		}
		uniqueObjs = append(uniqueObjs, AviGSK8sObj{
			Cluster:            memberObj.Cluster,
			ObjType:            memberObj.ObjType,
			Name:               memberObj.Name,
			Namespace:          memberObj.Namespace,
			IPAddr:             memberObj.IPAddr,
			Weight:             memberObj.Weight,
			Priority:           memberObj.Priority,
			ControllerUUID:     memberObj.ControllerUUID,
			VirtualServiceUUID: memberObj.VirtualServiceUUID,
			SyncVIPOnly:        memberObj.SyncVIPOnly,
		})
		memberVips = append(memberVips, memberObj.IPAddr)
	}
	return uniqueObjs
}

func (v *AviGSObjectGraph) GetCopy() *AviGSObjectGraph {
	v.Lock.RLock()
	defer v.Lock.RUnlock()

	domainNames := make([]string, len(v.DomainNames))
	copy(domainNames, v.DomainNames)

	gsObjCopy := AviGSObjectGraph{
		Name:          v.Name,
		Tenant:        v.Tenant,
		DomainNames:   domainNames,
		GraphChecksum: v.GraphChecksum,
		RetryCount:    v.RetryCount,
		Hm:            v.Hm.getCopy(),
	}
	var ttl int
	if v.TTL != nil {
		ttl = *v.TTL
		gsObjCopy.TTL = &ttl
	} else {
		gsObjCopy.TTL = nil
	}
	gsObjCopy.HmRefs = make([]string, len(v.HmRefs))
	copy(gsObjCopy.HmRefs, v.HmRefs)
	gsObjCopy.SitePersistenceRef = v.SitePersistenceRef
	gsObjCopy.GslbPoolAlgorithm = v.GslbPoolAlgorithm.DeepCopy()

	gsObjCopy.MemberObjs = make([]AviGSK8sObj, 0)
	for _, memberObj := range v.MemberObjs {
		gsObjCopy.MemberObjs = append(gsObjCopy.MemberObjs, memberObj.getCopy())
	}
	return &gsObjCopy
}

func BuildGSMemberObjFromMeta(metaObj k8sobjects.MetaObject, gsFqdn string) (AviGSK8sObj, error) {
	// Update the GS fields
	var ghRules gslbutils.GSHostRules
	var svcPort int32
	var svcProtocol string

	weight := int32(-1)
	priority := int32(-1)
	cname := metaObj.GetCluster()
	ns := metaObj.GetNamespace()
	objType := metaObj.GetType()
	gf := gslbutils.GetGlobalFilter()

	gsHostRuleList := gslbutils.GetGSHostRulesList()
	if ghRulesForFqdn := gsHostRuleList.GetGSHostRulesForFQDN(gsFqdn); ghRulesForFqdn != nil {
		ghRulesForFqdn.DeepCopyInto(&ghRules)
	}

	// determine the GS member's weight
	for _, c := range ghRules.TrafficSplit {
		if c.Cluster == cname {
			weight = int32(c.Weight)
			priority = int32(c.Priority)
		}
	}
	if weight == -1 {
		weight = GetObjTrafficRatio(ns, cname)
	}

	if priority == -1 {
		priority = GetObjTrafficPriority(ns, cname)
	}

	paths, err := metaObj.GetPaths()
	if err != nil {
		// for LB type services and passthrough routes
		gslbutils.Debugf("gsName: %s, msg: path list not available for object %s", gsFqdn, err.Error())
	}

	if objType == gslbutils.SvcType || metaObj.IsPassthrough() {
		svcPort, _ = metaObj.GetPort()
		svcProtocol, _ = metaObj.GetProtocol()
	}

	syncVIPOnly, err := gf.IsClusterSyncVIPOnly(cname)
	if err != nil {
		gslbutils.Errf("gsName: %s, cluster: %s, msg: couldn't find the sync type for member: %v",
			gsFqdn, cname, err)
	}

	tls, _ := getTLSFromObj(metaObj)

	return AviGSK8sObj{
		Cluster:            cname,
		Namespace:          ns,
		Name:               metaObj.GetName(),
		IPAddr:             metaObj.GetIPAddr(),
		Weight:             weight,
		Priority:           priority,
		ObjType:            objType,
		Port:               svcPort,
		Proto:              svcProtocol,
		Paths:              paths,
		VirtualServiceUUID: metaObj.GetVirtualServiceUUID(),
		ControllerUUID:     metaObj.GetControllerUUID(),
		SyncVIPOnly:        syncVIPOnly,
		IsPassthrough:      metaObj.IsPassthrough(),
		TLS:                tls,
	}, nil
}

func getTLSFromObj(metaObj k8sobjects.MetaObject) (bool, error) {
	tls, err := metaObj.GetTLS()
	if err != nil {
		return false, err
	}
	if !tls {
		hrStore := store.GetHostRuleStore()
		obj, found := hrStore.GetClusterNSObjectByName(metaObj.GetCluster(), metaObj.GetNamespace(), metaObj.GetHostname())
		if found {
			metaObj := obj.(gslbutils.HostRuleMeta)
			tls = metaObj.TLS
		}
	}
	return tls, nil
}
