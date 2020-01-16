package nodes

import (
	"sync"

	"gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
)

// AviGSNode is a representation of how a route gets translated into a GSLB
// service. Only the necessary fields are part of this node, which means, only
// the fields that get changed if a route gets added/changed/deleted.
type AviGSNode struct {
	Name        string
	Tenant      string
	DomainNames []string
	// Members is a list of IP addresses, for now. Will change when we add the traffic
	// weights to each of these members.
	Members             []string
	CloudConfigChecksum uint32
}

func (gs *AviGSNode) GetChecksum() uint32 {
	// Calculate checksum and return
	gs.CalculateChecksum()
	return gs.CloudConfigChecksum
}

func (gs *AviGSNode) CalculateChecksum() {
	// A sum of fields for this GS
	checksum := utils.Hash(utils.Stringify(gs.DomainNames)) +
		utils.Hash(utils.Stringify(gs.Members))
	gs.CloudConfigChecksum = checksum
}

func (gs *AviGSNode) GetNodeType() string {
	return "GSLBServiceNode"
}

func (gs *AviGSNode) UpdateMember(ipAddr string) {
	gs.Members = append(gs.Members, ipAddr)
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
	gslbutils.Logf("gsName: %s, msg: %s", gsName, "saving model")

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
	GSNode        *AviGSNode
	Name          string
	GraphChecksum uint32
}

func (v *AviGSObjectGraph) GetChecksum() uint32 {
	// Calculate checksum for this graph and return
	v.CalculateChecksum()
	return v.GraphChecksum
}

func (v *AviGSObjectGraph) CalculateChecksum() {
	// A sum of fields for this model
	v.GraphChecksum = v.GSNode.GetChecksum()
}

func NewAviGSObjectGraph() *AviGSObjectGraph {
	return &AviGSObjectGraph{}
}

func (v *AviGSObjectGraph) AddGSNode(node *AviGSNode) {
	v.GSNode = node
}

func (v *AviGSObjectGraph) ConstructAviGSNode(gsName, key, hostName, ipAddr string) {
	var gsNode AviGSNode
	hosts := []string{hostName}
	members := []string{ipAddr}
	// The GSLB service will be put into the admin tenant
	gsNode = AviGSNode{
		Name:        gsName,
		Tenant:      utils.ADMIN_NS,
		DomainNames: hosts,
		Members:     members,
	}
	v.AddGSNode(&gsNode)
	gslbutils.Logf("key: %s, AviGSNode: %s, msg: %s", key, gsNode.Name, "created a new Avi GS node")
}

func (v *AviGSObjectGraph) BuildAviGSGraph(key, name string) {
	v.Name = name
	gslbutils.Logf("key: %s, AviGSGraph: %s, msg: %s", key, v.Name, "created a new Avi GS graph")
}
