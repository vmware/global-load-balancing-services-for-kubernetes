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
	"sync"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/k8sobjects"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
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
		deleteGSGraphStore := gslbutils.NewObjectMapStore()
		deleteGSGraphInstance = &AviGSGraphLister{AviGSGraphStore: deleteGSGraphStore}
	})
	return deleteGSGraphInstance
}

type AviGSGraphLister struct {
	AviGSGraphStore *gslbutils.ObjectMapStore
}

func SharedAviGSGraphLister() *AviGSGraphLister {
	avionce.Do(func() {
		aviGSGraphStore := gslbutils.NewObjectMapStore()
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
	Cluster   string
	ObjType   string
	Name      string
	Namespace string
	IPAddr    string
	Weight    int32
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
		Port:               gsk8sObj.Port,
		Proto:              gsk8sObj.Proto,
		TLS:                gsk8sObj.TLS,
		Paths:              paths,
		VirtualServiceUUID: gsk8sObj.VirtualServiceUUID,
		ControllerUUID:     gsk8sObj.ControllerUUID,
		SyncVIPOnly:        gsk8sObj.SyncVIPOnly,
	}
	return obj
}

type HealthMonitor struct {
	Name      string
	Protocol  string
	Port      int32
	Custom    bool
	PathNames []string
}

func (hm HealthMonitor) getChecksum() uint32 {
	return gslbutils.GetGSLBHmChecksum(hm.Name, hm.Protocol, hm.Port)
}

func (hm HealthMonitor) getCopy() HealthMonitor {
	pathNames := make([]string, len(hm.PathNames))
	copy(pathNames, hm.PathNames)

	hmObj := HealthMonitor{
		Name:      hm.Name,
		Protocol:  hm.Protocol,
		Port:      hm.Port,
		Custom:    hm.Custom,
		PathNames: pathNames,
	}
	return hmObj
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

func (v *AviGSObjectGraph) GetHmChecksum() uint32 {
	return v.Hm.getChecksum()
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
		memberAddrs = append(memberAddrs, server+"-"+strconv.Itoa(int(gsMember.Weight)))
		memberObjs = append(memberObjs, gsMember.ObjType+"/"+gsMember.Cluster+"/"+gsMember.Namespace+"/"+gsMember.Name)
	}

	hmNames := []string{}
	if len(v.HmRefs) > 0 {
		if v.Hm.Name != "" {
			hmNames = append(hmNames, v.Hm.Name)
		} else {
			hmNames = v.Hm.PathNames
		}
	} else {
		hmNames = make([]string, len(v.HmRefs))
		copy(hmNames, v.HmRefs)
	}

	v.GraphChecksum = gslbutils.GetGSLBServiceChecksum(memberAddrs, v.DomainNames, memberObjs, hmNames,
		v.SitePersistenceRef, v.TTL)
}

// GetMemberRouteList returns a list of member objects
func (v *AviGSObjectGraph) GetMemberObjList() []string {
	var memberObjs []string
	for _, obj := range v.MemberObjs {
		memberObjs = append(memberObjs, obj.ObjType+"/"+obj.Cluster+"/"+obj.Namespace+"/"+obj.Name)
	}
	return memberObjs
}

func NewAviGSObjectGraph() *AviGSObjectGraph {
	return &AviGSObjectGraph{RetryCount: gslbutils.DefaultRetryCount}
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
	v.Hm.PathNames = make([]string, 0)

	// add the member paths
	for _, member := range v.MemberObjs {
		for _, path := range member.Paths {
			hmName := gslbutils.BuildHmPathName(v.Name, path, ifSec)
			if gslbutils.PresentInList(hmName, v.Hm.PathNames) {
				continue
			}
			v.Hm.PathNames = append(v.Hm.PathNames, hmName)
		}
	}
	gslbutils.Debugf("gsName: %s, pathList: %v, msg: rebuilt path list for GS", v.Name, v.Hm.PathNames)
}

func (v *AviGSObjectGraph) buildNonPathHealthMonitor(metaObj k8sobjects.MetaObject, key string) {
	port, err := metaObj.GetPort()
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: port not found for this object", key, v.Name)
		return
	}

	if metaObj.IsPassthrough() {
		v.Hm.Name = gslbutils.SystemGslbHealthMonitorPassthrough
	} else {
		v.Hm.Name = gslbutils.BuildNonPathHmName(v.Name)
	}
	v.Hm.Port = port
	v.MemberObjs[0].Port = port
	v.Hm.Custom = true
	protocol, err := metaObj.GetProtocol()
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: protocol not found for this object", key, v.Name)
		return
	}
	v.MemberObjs[0].Proto = protocol
	hmType, err := gslbutils.GetHmTypeForProtocol(protocol)
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: can't create a health monitor for this GS graph %s", key,
			v.Name, err.Error())
	} else {
		v.Hm.Protocol = hmType
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
	v.Hm.Custom = true
	tls, err := metaObj.GetTLS()
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, msg: error in getting tls for object %s", key, v.Name, err.Error())
		return
	}
	if tls {
		v.Hm.Protocol = gslbutils.SystemGslbHealthMonitorHTTPS
	} else {
		v.Hm.Protocol = gslbutils.SystemGslbHealthMonitorHTTP
	}
}

func (v *AviGSObjectGraph) ConstructAviGSGraph(gsName, key string, metaObj k8sobjects.MetaObject, memberWeight int32) {
	v.Lock.Lock()
	defer v.Lock.Unlock()
	hosts := []string{metaObj.GetHostname()}
	tls, _ := metaObj.GetTLS()
	paths, err := metaObj.GetPaths()
	if err != nil {
		// for LB type services and passthrough routes, the path list will be empty
		gslbutils.Debugf("key: %s, gsName: %s, msg: path list not available for object %s", key, gsName, err.Error())
	}
	cname := metaObj.GetCluster()
	gf := gslbutils.GetGlobalFilter()
	syncVIPOnly, err := gf.IsClusterSyncVIPOnly(cname)
	if err != nil {
		gslbutils.Errf("key: %s, gsName: %s, cluster: %s, msg: error in getting the sync type for cluster: %v",
			key, gsName, cname, err)
		return
	}
	if !syncVIPOnly && (metaObj.GetControllerUUID() == "" || metaObj.GetVirtualServiceUUID() == "") {
		gslbutils.Errf("gsName: %s, cluster: %s, namespace: %s, msg: controller UUID or VS UUID missing from the object, won't add member",
			v.Name, cname, metaObj.GetNamespace, metaObj.GetName())
		return
	}
	memberRoutes := []AviGSK8sObj{
		{
			Cluster:            metaObj.GetCluster(),
			ObjType:            metaObj.GetType(),
			IPAddr:             metaObj.GetIPAddr(),
			Weight:             memberWeight,
			Name:               metaObj.GetName(),
			Namespace:          metaObj.GetNamespace(),
			TLS:                tls,
			Paths:              paths,
			VirtualServiceUUID: metaObj.GetVirtualServiceUUID(),
			ControllerUUID:     metaObj.GetControllerUUID(),
			SyncVIPOnly:        syncVIPOnly,
		},
	}
	// The GSLB service will be put into the admin tenant
	v.Name = gsName
	v.Tenant = utils.ADMIN_NS
	v.DomainNames = hosts
	v.MemberObjs = memberRoutes
	v.RetryCount = gslbutils.DefaultRetryCount
	v.HmRefs = gf.GetAviHmRefs()
	v.SitePersistenceRef = gf.GetSitePersistence()
	v.TTL = gf.GetTTL()
	v.buildHmPathList()
	// Determine the health monitor(s) for this GS
	v.buildAndAttachHealthMonitors(metaObj, key)

	v.GetChecksum()
	gslbutils.Logf("key: %s, AviGSGraph: %s, msg: %s", key, v.Name, "created a new Avi GS graph")
}

func (v *AviGSObjectGraph) checkAndUpdateNonPathHealthMonitor(objType string, isPassthrough bool) {
	// this function has to be called only for LB service type members or passthrough route members
	if len(v.MemberObjs) <= 0 {
		gslbutils.Errf("gsName: %s, no member objects for this avi gs, can't check the health monitor", v.Name)
		return
	}

	if isPassthrough {
		v.Hm.Name = gslbutils.SystemGslbHealthMonitorPassthrough
	} else {
		v.Hm.Name = gslbutils.BuildNonPathHmName(v.Name)
	}
	v.Hm.Custom = true

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
	// overwrite the new minimum port and protocol
	v.Hm.Port = newPort
	hmType, err := gslbutils.GetHmTypeForProtocol(newProto)
	if err != nil {
		gslbutils.Errf("can't change the health monitor for gs %s, port: %d, protocol %s: %s", v.Name, newPort,
			newProto, err.Error())
	} else {
		v.Hm.Protocol = hmType
	}
}

func (v *AviGSObjectGraph) updateGSHmPathListAndProtocol() {
	// build the path based health monitor list
	v.buildHmPathList()
	gslbutils.Debugf("gsName: %s, added path HMs to the gslb hm path list, path hm list: %v", v.Name, v.Hm.PathNames)

	// protocol change required?
	// protocol will only be changed only if the current protocol doesn't match any of the members' protocol
	currProtocol := v.Hm.Protocol
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
		v.Hm.Protocol = gslbutils.GetHmTypeForTLS(v.MemberObjs[0].TLS)
	}
}

func (v *AviGSObjectGraph) UpdateGSMember(metaObj k8sobjects.MetaObject, weight int32) {
	v.Lock.Lock()
	defer v.Lock.Unlock()

	var svcPort int32
	var svcProtocol, objType string

	gf := gslbutils.GetGlobalFilter()

	// Update the GS fields
	if ttl := gf.GetTTL(); ttl != nil {
		v.TTL = ttl
	}

	v.SitePersistenceRef = gf.GetSitePersistence()

	paths, err := metaObj.GetPaths()
	if err != nil {
		// for LB type services and passthrough routes
		gslbutils.Debugf("gsName: %s, msg: path list not available for object %s", v.Name, err.Error())
	}

	objType = metaObj.GetType()
	if objType == gslbutils.SvcType || metaObj.IsPassthrough() {
		svcPort, _ = metaObj.GetPort()
		svcProtocol, _ = metaObj.GetProtocol()
	}

	cname := metaObj.GetCluster()
	syncVIPOnly, err := gf.IsClusterSyncVIPOnly(cname)
	if err != nil {
		gslbutils.Errf("gsName: %s, cluster: %s, msg: couldn't find the sync type for member: %v",
			v.Name, cname, err)
		return
	}
	// custom health monitor refs and the default path/non-path based health monitors are an ex-or
	// of each other.
	// Transition cases:
	// 1. If user has provided Hm refs via the GDP object, remove the default Hms from the GS graph.
	//    Add the user provided Hm refs to the GS graph.
	// 2. If user hasn't provided any Hm refs in the GDP object, add the default Hm, and remove the
	//    pre-existing Hm refs from the GS graph (if any).
	var userProvidedHmRefs bool
	hmRefs := gf.GetAviHmRefs()
	if len(hmRefs) > 0 {
		v.HmRefs = make([]string, len(hmRefs))
		copy(v.HmRefs, hmRefs)
		// set the previous path based health monitor to empty
		v.Hm = HealthMonitor{}
		userProvidedHmRefs = true
	} else {
		v.HmRefs = []string{}
	}

	// if the member with the "ipAddr" exists, then just update the weight, else add a new member
	for idx, memberObj := range v.MemberObjs {
		if metaObj.GetType() != memberObj.ObjType {
			continue
		}
		if metaObj.GetCluster() != memberObj.Cluster {
			continue
		}
		if metaObj.GetNamespace() != memberObj.Namespace {
			continue
		}
		if metaObj.GetName() != memberObj.Name {
			continue
		}
		// if we reach here, it means this is the member we need to update
		if !syncVIPOnly && (metaObj.GetControllerUUID() == "" || metaObj.GetVirtualServiceUUID() == "") {
			gslbutils.Errf("gsName: %s, cluster: %s, namespace: %s, msg: controller UUID or VS UUID missing from the object, won't update member",
				v.Name, cname, metaObj.GetNamespace, metaObj.GetName())
			return
		}
		v.MemberObjs[idx].VirtualServiceUUID = metaObj.GetVirtualServiceUUID()
		v.MemberObjs[idx].ControllerUUID = metaObj.GetControllerUUID()
		v.MemberObjs[idx].SyncVIPOnly = syncVIPOnly
		v.MemberObjs[idx].IPAddr = metaObj.GetIPAddr()
		v.MemberObjs[idx].Weight = weight
		gslbutils.Debugf("gsName: %s, msg: updating member for type %s", v.Name, metaObj.GetType())
		if objType == gslbutils.SvcType || metaObj.IsPassthrough() {
			v.MemberObjs[idx].Port = svcPort
			v.MemberObjs[idx].Proto = svcProtocol
			if !userProvidedHmRefs {
				v.checkAndUpdateNonPathHealthMonitor(metaObj.GetType(), metaObj.IsPassthrough())
			}
		} else {
			tls, err := metaObj.GetTLS()
			if err != nil {
				gslbutils.Errf("gsName: %s, msg: didn't get tls value for this object %s", err.Error())
				return
			}
			v.MemberObjs[idx].TLS = tls
			v.MemberObjs[idx].Paths = paths
			if !userProvidedHmRefs {
				v.updateGSHmPathListAndProtocol()
			}
		}
		return
	}

	// We reach here only if a new member needs to be created, so create and append
	gsMember := AviGSK8sObj{
		Cluster:            metaObj.GetCluster(),
		Namespace:          metaObj.GetNamespace(),
		Name:               metaObj.GetName(),
		IPAddr:             metaObj.GetIPAddr(),
		Weight:             weight,
		ObjType:            metaObj.GetType(),
		Port:               svcPort,
		Proto:              svcProtocol,
		Paths:              paths,
		VirtualServiceUUID: metaObj.GetVirtualServiceUUID(),
		ControllerUUID:     metaObj.GetControllerUUID(),
		SyncVIPOnly:        syncVIPOnly,
	}
	// if we reach here, it means this is the member we need to update
	if !syncVIPOnly && (metaObj.GetControllerUUID() == "" || metaObj.GetVirtualServiceUUID() == "") {
		gslbutils.Errf("gsName: %s, cluster: %s, namespace: %s, msg: controller UUID or VS UUID missing from the object, won't add member",
			v.Name, cname, metaObj.GetNamespace, metaObj.GetName())
		return
	}

	v.MemberObjs = append(v.MemberObjs, gsMember)
	if objType == gslbutils.SvcType || metaObj.IsPassthrough() {
		v.checkAndUpdateNonPathHealthMonitor(objType, metaObj.IsPassthrough())
	} else {
		v.updateGSHmPathListAndProtocol()
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

func (v *AviGSObjectGraph) IsHmTypeCustom() bool {
	v.Lock.RLock()
	defer v.Lock.RUnlock()
	return v.Hm.Custom
}

func (v *AviGSObjectGraph) GetHmPathNamesList() []string {
	v.Lock.RLock()
	defer v.Lock.RUnlock()

	gslbutils.Debugf("gs object and its path names: %v, paths: %v", v, v.Hm.PathNames)
	return v.Hm.PathNames
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
	}
	gsObjCopy.TTL = &ttl
	gsObjCopy.HmRefs = make([]string, len(v.HmRefs))
	copy(gsObjCopy.HmRefs, v.HmRefs)
	gsObjCopy.SitePersistenceRef = v.SitePersistenceRef

	gsObjCopy.MemberObjs = make([]AviGSK8sObj, 0)
	for _, memberObj := range v.MemberObjs {
		gsObjCopy.MemberObjs = append(gsObjCopy.MemberObjs, memberObj.getCopy())
	}
	return &gsObjCopy
}
