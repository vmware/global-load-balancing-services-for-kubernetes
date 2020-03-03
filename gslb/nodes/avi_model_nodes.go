package nodes

import (
	"strconv"
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

// AviGSRoute represents a route from which a GS was built.
type AviGSRoute struct {
	Cluster   string
	Name      string
	Namespace string
}

// AviGSObjectGraph is a graph constructed using AviGSNode. It is a one-to-one mapping between
// the name of the object and the GSLB Model node.
type AviGSObjectGraph struct {
	Name        string
	Tenant      string
	DomainNames []string
	// Members is a list of IP addresses, for now. Will change when we add the traffic
	// weights to each of these members.
	Members []GSMember
	// Routes is a list of routes from which this AviGS was built.
	Routes        []AviGSRoute
	GraphChecksum uint32
}

func (v *AviGSObjectGraph) GetChecksum() uint32 {
	// Calculate checksum for this graph and return
	v.CalculateChecksum()
	return v.GraphChecksum
}

func (v *AviGSObjectGraph) CalculateChecksum() {
	// A sum of fields for this GS
	var memberIPs []string
	var memberRoutes []string

	for _, gsMember := range v.Members {
		memberIPs = append(memberIPs, gsMember.IPAddr+"-"+strconv.Itoa(int(gsMember.Weight)))
	}
	for _, memberRoute := range v.Routes {
		memberRoutes = append(memberRoutes, memberRoute.Cluster+"/"+
			memberRoute.Namespace+"/"+memberRoute.Name)
	}

	v.GraphChecksum = gslbutils.GetGSLBServiceChecksum(memberIPs, v.DomainNames, memberRoutes)
}

// GetMemberRouteList returns a list of member routes
func (v *AviGSObjectGraph) GetMemberRouteList() []string {
	var memberRoutes []string
	for _, route := range v.Routes {
		memberRoutes = append(memberRoutes, route.Cluster+"/"+route.Namespace+"/"+route.Name)
	}
	return memberRoutes
}

func NewAviGSObjectGraph() *AviGSObjectGraph {
	return &AviGSObjectGraph{}
}

func (v *AviGSObjectGraph) ConstructAviGSGraph(gsName, key string, route gslbutils.RouteMeta, memberWeight int32) {
	hosts := []string{route.Hostname}
	members := []GSMember{
		GSMember{
			IPAddr: route.IPAddr,
			Weight: memberWeight,
		},
	}
	routes := []AviGSRoute{
		AviGSRoute{
			Cluster:   route.Cluster,
			Name:      route.Name,
			Namespace: route.Namespace,
		},
	}
	// The GSLB service will be put into the admin tenant
	v.Name = gsName
	v.Tenant = utils.ADMIN_NS
	v.DomainNames = hosts
	v.Members = members
	v.Routes = routes
	v.GetChecksum()
	gslbutils.Logf("key: %s, AviGSGraph: %s, msg: %s", key, v.Name, "created a new Avi GS graph")
}

func (v *AviGSObjectGraph) UpdateGSMember(ipAddr string, weight int32) {
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

func (v *AviGSObjectGraph) UpdateMemberRoute(route gslbutils.RouteMeta) {
	// check if the route already exists for this GS
	for _, memberRoute := range v.Routes {
		if route.Cluster == memberRoute.Cluster && route.Namespace == memberRoute.Namespace && route.Name == memberRoute.Name {
			return
		}
	}
	// the member route doesn't exist, update
	memberRoute := AviGSRoute{
		Cluster:   route.Cluster,
		Namespace: route.Namespace,
		Name:      route.Name,
	}
	v.Routes = append(v.Routes, memberRoute)
}

func gsDeleteGSMember(gs *AviGSObjectGraph, ipAddr string) bool {
	idx := -1
	for i, gsMember := range gs.Members {
		if ipAddr == gsMember.IPAddr {
			idx = i
			break
		}
	}
	if idx == -1 {
		// no such element
		return false
	}
	// Delete the member
	gs.Members = append(gs.Members[:idx], gs.Members[idx+1:]...)
	return true
}

func gsDeleteRouteMember(gs *AviGSObjectGraph, route gslbutils.RouteMeta) bool {
	idx := -1
	for i, memberRoute := range gs.Routes {
		if route.Cluster == memberRoute.Cluster && route.Namespace == memberRoute.Namespace && route.Name == memberRoute.Name {
			idx = i
			break
		}
	}
	if idx == -1 {
		// no such element
		return false
	}
	// Delete the member route
	gs.Routes = append(gs.Routes[:idx], gs.Routes[idx+1:]...)
	return true

}

func (v *AviGSObjectGraph) DeleteMember(ipAddr string, route gslbutils.RouteMeta) {
	if !gsDeleteGSMember(v, ipAddr) {
		gslbutils.Warnf("gsGraph: %s, memberIP: %s, msg: couldn't find IP member in GS")
	}
	if !gsDeleteRouteMember(v, route) {
		gslbutils.Warnf("gsGraph: %s, route: %v, msg: couldn't find route member in GS")
	}
}
