package nodes

import (
	"sort"
	"sync"

	"gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
)

type GSMember struct {
	IPAddr string
	Weight int32
}

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

func (a *AviGSGraphLister) Delete(gsName string) {
	a.AviGSGraphStore.Delete(gsName)
}

// AviGSObjectGraph is a graph constructed using AviGSNode. It is a one-to-one mapping between
// the name of the object and the GSLB Model node.
type AviGSObjectGraph struct {
	Name        string
	Tenant      string
	DomainNames []string
	// Members is a list of IP addresses, for now. Will change when we add the traffic
	// weights to each of these members.
	Members       []GSMember
	GraphChecksum uint32
}

func (v *AviGSObjectGraph) GetChecksum() uint32 {
	// Calculate checksum for this graph and return
	v.CalculateChecksum()
	return v.GraphChecksum
}

func (v *AviGSObjectGraph) CalculateChecksum() {
	// A sum of fields for this model
	// A sum of fields for this GS
	var memberIPs []string
	var memberWeights []string

	for _, member := range v.Members {
		memberIPs = append(memberIPs, member.IPAddr)
		memberWeights = append(memberWeights, string(member.Weight))
	}
	sort.Strings(v.DomainNames)
	sort.Strings(memberIPs)
	sort.Strings(memberWeights)
	checksum := utils.Hash(utils.Stringify(v.DomainNames)) +
		utils.Hash(utils.Stringify(memberIPs)) + utils.Hash(utils.Stringify(memberWeights))
	v.GraphChecksum = checksum
}

func NewAviGSObjectGraph() *AviGSObjectGraph {
	return &AviGSObjectGraph{}
}

func (v *AviGSObjectGraph) ConstructAviGSGraph(gsName, key, hostName, ipAddr string, memberWeight int32) {
	hosts := []string{hostName}
	members := []GSMember{
		GSMember{
			IPAddr: ipAddr,
			Weight: memberWeight,
		},
	}
	// The GSLB service will be put into the admin tenant
	v.Name = gsName
	v.Tenant = utils.ADMIN_NS
	v.DomainNames = hosts
	v.Members = members
	gslbutils.Logf("key: %s, AviGSGraph: %s, msg: %s", key, v.Name, "created a new Avi GS graph")
}

func (v *AviGSObjectGraph) UpdateMember(ipAddr string, weight int32) {
	// if the member with the "ipAddr" exists, then just update the weight, else add a new member
	for idx, member := range v.Members {
		if ipAddr == member.IPAddr {
			v.Members[idx].Weight = weight
			return
		}
	}
	gsMember := GSMember{
		IPAddr: ipAddr,
		Weight: weight,
	}
	v.Members = append(v.Members, gsMember)
}

func (v *AviGSObjectGraph) DeleteMember(ipAddr string) bool {
	idx := -1
	for i, member := range v.Members {
		if ipAddr == member.IPAddr {
			idx = i
			break
		}
	}
	if idx == -1 {
		// no such element
		return false
	}
	v.Members = append(v.Members[:idx], v.Members[idx+1:]...)
	return true
}
