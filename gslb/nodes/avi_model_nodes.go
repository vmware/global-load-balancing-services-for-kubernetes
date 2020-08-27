/*
* [2013] - [2020] Avi Networks Incorporated
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

	"github.com/avinetworks/amko/gslb/gslbutils"
	"github.com/avinetworks/amko/gslb/k8sobjects"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

var aviGSGraphInstance *AviGSGraphLister
var avionce sync.Once

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
}

type HealthMonitor struct {
	Name     string
	Protocol string
	Port     int32
	Custom   bool
}

func (hm HealthMonitor) getChecksum() uint32 {
	//func GetGSLBHmChecksum(name, hmType string, port int32) uint32 {
	return gslbutils.GetGSLBHmChecksum(hm.Name, hm.Protocol, hm.Port)
}

// AviGSObjectGraph is a graph constructed using AviGSNode. It is a one-to-one mapping between
// the name of the object and the GSLB Model node.
type AviGSObjectGraph struct {
	Name        string
	Tenant      string
	DomainNames []string
	// MemberObjs is a list of K8s/openshift objects from which this AviGS was built.
	MemberObjs    []AviGSK8sObj
	GraphChecksum uint32
	RetryCount    int
	Hm            HealthMonitor
	Lock          sync.RWMutex
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
	var memberIPs []string
	var memberObjs []string

	for _, gsMember := range v.MemberObjs {
		memberIPs = append(memberIPs, gsMember.IPAddr+"-"+strconv.Itoa(int(gsMember.Weight)))
		memberObjs = append(memberObjs, gsMember.ObjType+"/"+gsMember.Cluster+"/"+gsMember.Namespace+"/"+gsMember.Name)
	}

	v.GraphChecksum = gslbutils.GetGSLBServiceChecksum(memberIPs, v.DomainNames, memberObjs, v.Hm.Name)
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

func (v *AviGSObjectGraph) ConstructAviGSGraph(gsName, key string, metaObj k8sobjects.MetaObject, memberWeight int32) {
	v.Lock.Lock()
	defer v.Lock.Unlock()
	hosts := []string{metaObj.GetHostname()}
	memberRoutes := []AviGSK8sObj{
		{
			Cluster:   metaObj.GetCluster(),
			ObjType:   metaObj.GetType(),
			IPAddr:    metaObj.GetIPAddr(),
			Weight:    memberWeight,
			Name:      metaObj.GetName(),
			Namespace: metaObj.GetNamespace(),
		},
	}
	// The GSLB service will be put into the admin tenant
	v.Name = gsName
	v.Tenant = utils.ADMIN_NS
	v.DomainNames = hosts
	v.MemberObjs = memberRoutes

	port, err := metaObj.GetPort()
	if err != nil {
		// for objects other than service type load balancer
		v.Hm.Name = gslbutils.SystemGslbHealthMonitorTCP
		v.Hm.Custom = false
	} else {
		// for svc type load balancer objects
		v.Hm.Name = "amko-hm-" + gsName
		v.Hm.Port = port
		v.Hm.Custom = true
		protocol, _ := metaObj.GetProtocol()
		switch protocol {
		case gslbutils.ProtocolTCP:
			v.Hm.Protocol = gslbutils.SystemHealthMonitorTypeTCP
		case gslbutils.ProtocolUDP:
			v.Hm.Protocol = gslbutils.SystemHealthMonitorTypeUDP
		default:
			gslbutils.Errf("unrecognized protocol for service, can't create a health monitor for this GSLB service graph")
		}
	}
	v.GetChecksum()
	gslbutils.Logf("key: %s, AviGSGraph: %s, msg: %s", key, v.Name, "created a new Avi GS graph")
}

func (v *AviGSObjectGraph) UpdateGSMember(metaObj k8sobjects.MetaObject, weight int32) {
	v.Lock.Lock()
	defer v.Lock.Unlock()
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
		v.MemberObjs[idx].IPAddr = metaObj.GetIPAddr()
		v.MemberObjs[idx].Weight = weight
		return
	}

	// We reach here only if a new member needs to be created, so create and append
	gsMember := AviGSK8sObj{
		Cluster:   metaObj.GetCluster(),
		Namespace: metaObj.GetNamespace(),
		Name:      metaObj.GetName(),
		IPAddr:    metaObj.GetIPAddr(),
		Weight:    weight,
		ObjType:   metaObj.GetType(),
	}
	v.MemberObjs = append(v.MemberObjs, gsMember)
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

// GetUniqueMemberList returns a non-duplicated list of objects, uniqueness is checked by the IPAddr
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
			Cluster:   memberObj.Cluster,
			ObjType:   memberObj.ObjType,
			Name:      memberObj.Name,
			Namespace: memberObj.Namespace,
			IPAddr:    memberObj.IPAddr,
			Weight:    memberObj.Weight,
		})
		memberVips = append(memberVips, memberObj.IPAddr)
	}
	return uniqueObjs
}
