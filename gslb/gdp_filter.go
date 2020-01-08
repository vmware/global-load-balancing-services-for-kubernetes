package gslb

import (
	"github.com/gobwas/glob"
	routev1 "github.com/openshift/api/route/v1"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	gdpv1alpha1 "gitlab.eng.vmware.com/orion/mcc/pkg/apis/avilb/v1alpha1"
	"sync"
)

// GlobalFilter is all the filters at one place. It also holds a list of ApplicableClusters
// to which all the filters are applicable. This list cannot be empty.
type GlobalFilter struct {
	// Cluster scoped filter, essentially holds a reference to one of the filters in NSFilterMap for "avi-system"
	// namespace.
	ClusterFilter *NSFilter
	// Respective filters for the namespaces.
	NSFilterMap map[string]*NSFilter
	// GlobalLock is locked before accessing any of the filters.
	GlobalLock sync.RWMutex
}

// ApplyFilter applies the local namespace filter first to an object, if the namespace
// filter is not present or if the object is rejected by the namespace filter, apply
// the cluster filter if present. Default action is to reject the object.
func (gf *GlobalFilter) ApplyFilter(obj interface{}, cname string) bool {
	route := obj.(*routev1.Route)
	ns := route.ObjectMeta.Namespace
	if ns == AVISystem {
		// routes in AVISystem are ignored
		utils.AviLog.Error.Print("Routes in avi-system namespace are ignored, returning...")
		return false
	}
	// First see, if there's a namespace filter set for this route's namespace, if not, apply
	// the global filter.
	var passed bool
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()
	if nf, ok := gf.NSFilterMap[route.ObjectMeta.Namespace]; ok && nf != nil {
		passed = nf.ApplyFilter(route, cname)
		// utils.AviLog.Info.Printf("route %s passed: %v", route.ObjectMeta.Name, passed)
		if passed {
			return true
		}
	}
	// If rejected by the namespace filter or a namespace filter isn't there, then
	// let's apply the cluster filter
	if gf.ClusterFilter != nil {
		return gf.ClusterFilter.ApplyFilter(route, cname)
	}

	// Else, return false
	return false
}

// AddToGlobalFilter handles creation of new filters, cluster or otherwise.
// Each namespace can have only one GDP object and one filter respectively, this is
// taken care of in the admission controller.
func (gf *GlobalFilter) AddToGlobalFilter(gdp *gdpv1alpha1.GlobalDeploymentPolicy) {
	nsFilter := GetNewNSFilter(gdp)
	// Assigning this directly to the ns filter map, and not checking whether there exists
	// another filter before this. Admission controller will ensure that for a namespace,
	// only one GDP exists, hence, only one filter object per namespace.
	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()
	gf.NSFilterMap[gdp.ObjectMeta.Namespace] = nsFilter
	utils.AviLog.Info.Printf("Added a new filter for ns: %s\n", nsFilter.ApplicableNamespace)
	// Check if cluster filter needs to be set
	if gdp.ObjectMeta.Namespace == AVISystem {
		gf.ClusterFilter = nsFilter
	}
}

func presentInList(cname string, clusterList []string) bool {
	for _, cluster := range clusterList {
		if cluster == cname {
			return true
		}
	}
	return false
}

// UpdateGlobalFilter takes two arguments: the old and the new GDP objects, and verifies
// whether a change is required to any of the filters. If yes, it changes either the cluster
// filter or one of the namespace filters.
func (gf *GlobalFilter) UpdateGlobalFilter(old, new *gdpv1alpha1.GlobalDeploymentPolicy) bool {
	// Need to check for the NSFilterMap
	nf := GetNewNSFilter(new)

	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()
	if gf.NSFilterMap[old.ObjectMeta.Namespace].GetChecksum() == nf.Checksum {
		// No updates needed, just return
		return false
	}
	utils.AviLog.Info.Printf("Namespace %s's filter just changed, need to update the filter and re-evaluate the routes.", nf.ApplicableNamespace)
	// Just replace the namespace filter with a new one.
	gf.NSFilterMap[old.ObjectMeta.Namespace] = nf
	// Also, see if the cluster filter needs to be updated
	if old.ObjectMeta.Namespace == AVISystem {
		gf.ClusterFilter = nf
	}
	return true
}

// DeleteFromGlobalFilter deletes a filter pertaining to gdp.
func (gf *GlobalFilter) DeleteFromGlobalFilter(gdp *gdpv1alpha1.GlobalDeploymentPolicy) {
	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()
	if gdp.ObjectMeta.Namespace == "AVISystem" {
		gf.ClusterFilter = nil
	} else {
		delete(gf.NSFilterMap, gdp.ObjectMeta.Namespace)
	}
}

// GetNewGlobalFilter returns a new GlobalFilter. It is to be called only once with the
// the GDP object as the input. Either the namespace of the GDP object is AVISystem
// or its some other namespace. Based on that this GlobalFilter is created.
func GetNewGlobalFilter(obj interface{}) *GlobalFilter {
	gdp := obj.(*gdpv1alpha1.GlobalDeploymentPolicy)

	// Check the namespace of the gdp object, if its AVISystem, then its a cluster
	// wide filter. Else, its a namespace specific filter.
	filter := GetNewNSFilter(gdp)
	nsFilterMap := make(map[string]*NSFilter, 0)
	nsFilterMap[gdp.ObjectMeta.Namespace] = filter
	gf := &GlobalFilter{
		NSFilterMap: nsFilterMap,
	}
	if gdp.ObjectMeta.Namespace == AVISystem {
		gf.ClusterFilter = filter
	}
	return gf
}

// GDPRule represents one MatchRule plus its checksum.
type GDPRule struct {
	MatchRule gdpv1alpha1.MatchRule
	Checksum  uint32
}

// GlobOperate applies glob operator on the route's parameters.
func (gr *GDPRule) GlobOperate(object interface{}) bool {
	mr := gr.MatchRule
	route := object.(*routev1.Route)
	var g glob.Glob
	// route's hostname has to match
	// If no hostname given, return false
	for _, host := range mr.Hosts {
		g = glob.MustCompile(host.HostName, '.')
		if g.Match(route.Spec.Host) {
			return true
		}
	}
	return false
}

// EqualOperate applies the Equals operator on the object's fields.
func (gr *GDPRule) EqualOperate(object interface{}) bool {
	mr := gr.MatchRule
	route := object.(*routev1.Route)
	if len(mr.Hosts) != 0 {
		// Host list is of non-zero length, which means has to be a host match expression
		for _, h := range mr.Hosts {
			if h.HostName == route.Spec.Host {
				return true
			}
		}
	} else {
		// Its a label key-value match
		routeLabels := route.ObjectMeta.Labels
		if value, ok := routeLabels[mr.Label.Key]; ok {
			if value == mr.Label.Value {
				return true
			}
		}
	}
	return false
}

// NotEqualOperate applies the NotEquals operator on the object's fields.
func (gr *GDPRule) NotEqualOperate(object interface{}) bool {
	mr := gr.MatchRule
	route := object.(*routev1.Route)
	if len(mr.Hosts) != 0 {
		// Host list is of non-zero length, which means it has to be a host match expression
		for _, h := range mr.Hosts {
			if h.HostName == route.Spec.Host {
				return false
			}
		}
		// Match not found for host, return true
		return true
	}
	// Its a label key-value match
	routeLabels := route.ObjectMeta.Labels
	if value, ok := routeLabels[mr.Label.Key]; ok {
		if value == mr.Label.Value {
			return false
		}
	}
	return true
}

// Apply operates on the obj object's fields and returns true/false depending on whether the
// operation worked.
func (gr *GDPRule) Apply(obj interface{}) bool {
	route := obj.(*routev1.Route)
	mr := gr.MatchRule
	// Basic sanity checks
	if len(mr.Hosts) == 0 && mr.Label.Key == "" {
		utils.AviLog.Error.Printf("Rule object doesn't have either hosts set or label key-value pair")
		return false
	}
	if len(mr.Hosts) > 0 && route.Spec.Host == "" {
		return false
	}
	switch mr.Op {
	case gdpv1alpha1.GlobOp:
		return gr.GlobOperate(route)
	case gdpv1alpha1.EqualsOp:
		return gr.EqualOperate(route)
	case gdpv1alpha1.NotequalsOp:
		return gr.NotEqualOperate(route)
	default:
		utils.AviLog.Error.Printf("Operation %s is invalid for route %s\n",
			gr.MatchRule.Op, route.ObjectMeta.Name)
	}
	return false
}

// UpdateChecksum calculates the checksum based on the MatchRule and updates in
// gr's Checksum field.
func (gr *GDPRule) UpdateChecksum() {
	// Checksum calculation has to be done on these candidates for a rule:
	// 1. Object
	// 2. Operation (Op)
	// 3. Hosts, if present
	// 4. Label key and value, if present
	gr.Checksum = utils.Hash(gr.MatchRule.Object + gr.MatchRule.Op +
		utils.Stringify(gr.MatchRule.Hosts) + utils.Stringify(gr.MatchRule.Label))
}

// GetGDPRules builds a list of GDPRule based on the matchRules as the input. It also
// calculates and updates the checksum.
func GetGDPRules(matchRules []gdpv1alpha1.MatchRule) (*[]GDPRule, uint32) {
	ruleList := make([]GDPRule, len(matchRules))
	var cksum uint32
	for idx, mr := range matchRules {
		mr.DeepCopy().DeepCopyInto(&ruleList[idx].MatchRule)
		ruleList[idx].UpdateChecksum()
		cksum += ruleList[idx].Checksum
	}
	return &ruleList, cksum
}

// NSFilter is kind of like the ClusterFilter but is only applicable to one namespace.
type NSFilter struct {
	// ApplicableClusters is the list of clusters in which this filter is applicable.
	ApplicableClusters []string
	// ApplicableNamespace is the namespace in which this filter is applicable.
	ApplicableNamespace string
	// NSRules is the list of AND separated rules. If no rules, an object cannot pass.
	NSRules []GDPRule
	// Checksum is the sum of checksums of all the NSRules.
	Checksum uint32
	// NSLock is locked before accessing any of the fields here.
	NSLock sync.RWMutex
}

// ApplyFilter applies the gdp rules in this NS filter on the route object "obj" in cluster "cname".
// Returns true/false depending on whether this route passes the filter or not.
func (nf *NSFilter) ApplyFilter(obj interface{}, cname string) bool {
	route := obj.(*routev1.Route)
	nf.NSLock.RLock()
	defer nf.NSLock.RUnlock()
	if !presentInList(cname, nf.ApplicableClusters) {
		// utils.AviLog.Error.Printf("Cluster name %s is not present in the list of applicable clusters, rejecting the route %s",
		// 	cname, route.ObjectMeta.Name)
		return false
	}

	for _, gdpRule := range nf.NSRules {
		if !gdpRule.Apply(route) {
			utils.AviLog.Info.Printf("route %s/%s/%s rejected because of gdprule: %v",
				cname, route.ObjectMeta.Namespace, route.ObjectMeta.Name, gdpRule)
			return false
		}
	}
	return true
}

// GetChecksum returns the checksum of the filter
func (nf *NSFilter) GetChecksum() uint32 {
	nf.NSLock.RLock()
	defer nf.NSLock.RUnlock()
	return nf.Checksum
}

// GetNewNSFilter takes a GDP object as the input and creates a new NSFilter object
// based on the GDP match rules and the namespace of the GDP object.
func GetNewNSFilter(gdp *gdpv1alpha1.GlobalDeploymentPolicy) *NSFilter {
	// Build the cluster list
	clusterList := make([]string, 0)
	for _, cluster := range gdp.Spec.MatchClusters {
		clusterList = append(clusterList, cluster.ClusterContext)
	}
	ns := gdp.ObjectMeta.Namespace
	gdpRules, cksum := GetGDPRules(gdp.Spec.MatchRules)
	// Checksum should also include the cluster name list
	cksum += utils.Hash(utils.Stringify(clusterList))
	// Build the list of gdpRules from the match rules in the GDP object
	return &NSFilter{
		ApplicableClusters:  clusterList,
		ApplicableNamespace: ns,
		NSRules:             *gdpRules,
		Checksum:            cksum,
	}
}
