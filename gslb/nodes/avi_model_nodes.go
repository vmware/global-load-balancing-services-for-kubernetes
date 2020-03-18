package nodes

import (
	"strconv"
	"sync"

	"gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
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

func (a *AviGSGraphLister) Delete(gsName string) {
	a.AviGSGraphStore.Delete(gsName)
}

func (a *AviGSGraphLister) GetAll() map[string]interface{} {
	return a.AviGSGraphStore.GetAllObjectNames()
}

// AviGSRoute represents a route from which a GS was built.
type AviGSRoute struct {
	Cluster   string
	Name      string
	Namespace string
	IPAddr    string
	Weight    int32
}

// AviGSObjectGraph is a graph constructed using AviGSNode. It is a one-to-one mapping between
// the name of the object and the GSLB Model node.
type AviGSObjectGraph struct {
	Name        string
	Tenant      string
	DomainNames []string
	// Routes is a list of routes from which this AviGS was built.
	MemberRoutes  []AviGSRoute
	GraphChecksum uint32
	Lock          sync.RWMutex
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

	for _, gsMember := range v.MemberRoutes {
		memberIPs = append(memberIPs, gsMember.IPAddr+"-"+strconv.Itoa(int(gsMember.Weight)))
		memberRoutes = append(memberRoutes, gsMember.Cluster+"/"+gsMember.Namespace+"/"+gsMember.Name)
	}

	v.GraphChecksum = gslbutils.GetGSLBServiceChecksum(memberIPs, v.DomainNames, memberRoutes)
}

// GetMemberRouteList returns a list of member routes
func (v *AviGSObjectGraph) GetMemberRouteList() []string {
	var memberRoutes []string
	for _, route := range v.MemberRoutes {
		memberRoutes = append(memberRoutes, route.Cluster+"/"+route.Namespace+"/"+route.Name)
	}
	return memberRoutes
}

func NewAviGSObjectGraph() *AviGSObjectGraph {
	return &AviGSObjectGraph{}
}

func (v *AviGSObjectGraph) ConstructAviGSGraph(gsName, key string, route gslbutils.RouteMeta, memberWeight int32) {
	v.Lock.Lock()
	defer v.Lock.Unlock()
	hosts := []string{route.Hostname}
	memberRoutes := []AviGSRoute{
		AviGSRoute{
			Cluster:   route.Cluster,
			IPAddr:    route.IPAddr,
			Weight:    memberWeight,
			Name:      route.Name,
			Namespace: route.Namespace,
		},
	}
	// The GSLB service will be put into the admin tenant
	v.Name = gsName
	v.Tenant = utils.ADMIN_NS
	v.DomainNames = hosts
	v.MemberRoutes = memberRoutes
	v.GetChecksum()
	gslbutils.Logf("key: %s, AviGSGraph: %s, msg: %s", key, v.Name, "created a new Avi GS graph")
}

func (v *AviGSObjectGraph) UpdateGSMember(route gslbutils.RouteMeta, weight int32) {
	v.Lock.Lock()
	defer v.Lock.Unlock()
	// if the member with the "ipAddr" exists, then just update the weight, else add a new member
	for _, memberRoute := range v.MemberRoutes {
		if route.Cluster != memberRoute.Cluster {
			continue
		}
		if route.Namespace != memberRoute.Namespace {
			continue
		}
		if route.Name != memberRoute.Name {
			continue
		}

		if route.IPAddr == memberRoute.IPAddr {
			if weight == memberRoute.Weight {
				// Nothing to update
				return
			} else {
				// weight is different
				memberRoute.Weight = weight
				return
			}
		} else {
			// IP Address is different
			memberRoute.IPAddr = route.IPAddr
			return
		}
	}

	// We reach here only if a new member needs to be created, so create and append
	gsMember := AviGSRoute{
		Cluster:   route.Cluster,
		Namespace: route.Namespace,
		Name:      route.Name,
		IPAddr:    route.IPAddr,
		Weight:    weight,
	}
	v.MemberRoutes = append(v.MemberRoutes, gsMember)
}

func (v *AviGSObjectGraph) DeleteMember(cname, ns, name string) {
	idx := -1
	v.Lock.Lock()
	defer v.Lock.Unlock()
	for i, memberRoute := range v.MemberRoutes {
		if cname == memberRoute.Cluster && ns == memberRoute.Namespace && name == memberRoute.Name {
			idx = i
			break
		}
	}
	if idx == -1 {
		gslbutils.Warnf("gsGraph: %s, route: %v, msg: couldn't find route member in GS")
		return
	}
	// Delete the member route
	v.MemberRoutes = append(v.MemberRoutes[:idx], v.MemberRoutes[idx+1:]...)
}

func (v *AviGSObjectGraph) MembersLen() int {
	v.Lock.RLock()
	defer v.Lock.RUnlock()
	return len(v.MemberRoutes)
}

func (v *AviGSObjectGraph) GetGSMember(cname, ns, name string) AviGSRoute {
	v.Lock.RLock()
	defer v.Lock.RUnlock()
	for _, member := range v.MemberRoutes {
		if member.Cluster == cname && member.Namespace == ns && member.Name == name {
			return member
		}
	}
	return AviGSRoute{}
}

func (v *AviGSObjectGraph) GetMemberRouteObjs() []AviGSRoute {
	v.Lock.RLock()
	v.Lock.RUnlock()
	routeObjs := make([]AviGSRoute, len(v.MemberRoutes))
	for idx := range v.MemberRoutes {
		routeObjs[idx].Cluster = v.MemberRoutes[idx].Cluster
		routeObjs[idx].Name = v.MemberRoutes[idx].Name
		routeObjs[idx].Namespace = v.MemberRoutes[idx].Namespace
		routeObjs[idx].IPAddr = v.MemberRoutes[idx].IPAddr
		routeObjs[idx].Weight = v.MemberRoutes[idx].Weight
	}
	return routeObjs
}
