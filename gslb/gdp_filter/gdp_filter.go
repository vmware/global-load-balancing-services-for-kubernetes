package filter

import (
	"sync"

	"amko/gslb/gslbutils"
	gdpv1alpha1 "amko/pkg/apis/avilb/v1alpha1"

	"github.com/avinetworks/container-lib/utils"
	"github.com/gobwas/glob"
)

var (
	// Need to keep this global since, it will be used across multiple layers and multiple handlers
	Gfi    *GlobalFilter
	gfOnce sync.Once
)

// GetGlobalFilter returns the existing global filter
func GetGlobalFilter() *GlobalFilter {
	gfOnce.Do(func() {
		Gfi = GetNewGlobalFilter()
	})
	return Gfi
}

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
	route, ok := obj.(gslbutils.RouteMeta)
	if !ok {
		gslbutils.Warnf("cname: %s, msg: not a route object, returning", cname)
		return false
	}
	ns := route.Namespace
	if ns == gslbutils.AVISystem {
		// routes in AVISystem are ignored
		gslbutils.Errf("cname: %s, ns: %s, route: %s, msg: routes in avi-system are ignored",
			cname, route.Namespace, route.Name)
		return false
	}
	// First see, if there's a namespace filter set for this route's namespace, if not, apply
	// the global filter.
	var passed bool
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()
	if nf, ok := gf.NSFilterMap[route.Namespace]; ok && nf != nil {
		passed = nf.ApplyFilter(route, cname)
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
	gslbutils.Logf("ns: %s, object: NSFilter, msg: added a new filter", nsFilter.ApplicableNamespace)
	// Check if cluster filter needs to be set
	if gdp.ObjectMeta.Namespace == gslbutils.AVISystem {
		gf.ClusterFilter = nsFilter
	}
}

func (gf *GlobalFilter) GetTrafficWeight(ns, cname string) int32 {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()
	nsFilter, ok := gf.NSFilterMap[ns]
	if !ok {
		gslbutils.Warnf("ns: %s, cname: %s, msg: no filter available for this namespace", ns, cname)
		return -1
	}
	val := nsFilter.GetTrafficWeight(cname)
	return val
}

func presentInList(cname string, clusterList []string) bool {
	for _, cluster := range clusterList {
		if cluster == cname {
			return true
		}
	}
	return false
}

func isTrafficWeightChanged(new, old *gdpv1alpha1.GlobalDeploymentPolicy) bool {
	// There are 3 conditions when a cluster traffic ratio is different between the old
	// and new GDP objects:
	// 1. Length of the Traffic Split elements is different between the two.
	// 2. Length is same, but a member from the old list is not found in the new list.
	// 3. Length is same, but a member has different ratios across both the objects.

	if len(old.Spec.TrafficSplit) != len(new.Spec.TrafficSplit) {
		return true
	}
	for _, oldMember := range old.Spec.TrafficSplit {
		found := false
		for _, newMember := range new.Spec.TrafficSplit {
			if oldMember.Cluster == newMember.Cluster {
				found = true
				if oldMember.Weight != newMember.Weight {
					return true
				}
			}
		}
		if found == false {
			// this member was not found in the new GDP, so return true
			return true
		}
	}
	return false
}

// UpdateGlobalFilter takes two arguments: the old and the new GDP objects, and verifies
// whether a change is required to any of the filters. If yes, it changes either the cluster
// filter or one of the namespace filters.
func (gf *GlobalFilter) UpdateGlobalFilter(oldGDP, newGDP *gdpv1alpha1.GlobalDeploymentPolicy) (bool, bool) {
	// Need to check for the NSFilterMap
	nf := GetNewNSFilter(newGDP)

	gslbutils.Logf("ns: %s, gdp: %s, msg: %s", oldGDP.ObjectMeta.Namespace, oldGDP.ObjectMeta.Name,
		"got an update event")
	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()
	gslbutils.Logf("old checksum: %d, new checksum: %d", gf.NSFilterMap[oldGDP.ObjectMeta.Namespace].GetChecksum(), nf.Checksum)
	if gf.NSFilterMap[oldGDP.ObjectMeta.Namespace].GetChecksum() == nf.Checksum {
		// No updates needed, just return
		return false, false
	}
	gslbutils.Logf("ns: %s, gdp: %s, object: filter, msg: %s", oldGDP.ObjectMeta.Namespace, oldGDP.ObjectMeta.Name,
		"filter changed, will update filter and re-evaluate routes")
	// Just replace the namespace filter with a new one.
	gf.NSFilterMap[oldGDP.ObjectMeta.Namespace] = nf
	// Also, see if the cluster filter needs to be updated
	if oldGDP.ObjectMeta.Namespace == gslbutils.AVISystem {
		gf.ClusterFilter = nf
	}
	trafficWeightChanged := isTrafficWeightChanged(newGDP, oldGDP)
	return true, trafficWeightChanged
}

// DeleteFromGlobalFilter deletes a filter pertaining to gdp.
func (gf *GlobalFilter) DeleteFromGlobalFilter(gdp *gdpv1alpha1.GlobalDeploymentPolicy) {
	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()
	if gdp.ObjectMeta.Namespace == gslbutils.AVISystem {
		gf.ClusterFilter = nil
	} else {
		delete(gf.NSFilterMap, gdp.ObjectMeta.Namespace)
	}
}

// GetNewGlobalFilter returns a new GlobalFilter. It is to be called only once with the
// the GDP object as the input. Either the namespace of the GDP object is AVISystem
// or its some other namespace. Based on that this GlobalFilter is created.
func GetNewGlobalFilter() *GlobalFilter {
	nsFilterMap := make(map[string]*NSFilter, 0)
	gf := &GlobalFilter{
		NSFilterMap: nsFilterMap,
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
	route := object.(gslbutils.RouteMeta)
	var g glob.Glob
	// route's hostname has to match
	// If no hostname given, return false
	for _, host := range mr.Hosts {
		g = glob.MustCompile(host.HostName, '.')
		if g.Match(route.Hostname) {
			return true
		}
	}
	return false
}

// EqualOperate applies the Equals operator on the object's fields.
func (gr *GDPRule) EqualOperate(object interface{}) bool {
	mr := gr.MatchRule
	route := object.(gslbutils.RouteMeta)
	if len(mr.Hosts) != 0 {
		// Host list is of non-zero length, which means has to be a host match expression
		for _, h := range mr.Hosts {
			if h.HostName == route.Hostname {
				return true
			}
		}
	} else {
		// Its a label key-value match
		routeLabels := route.Labels
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
	route := object.(gslbutils.RouteMeta)
	if len(mr.Hosts) != 0 {
		// Host list is of non-zero length, which means it has to be a host match expression
		for _, h := range mr.Hosts {
			if h.HostName == route.Hostname {
				return false
			}
		}
		// Match not found for host, return true
		return true
	}
	// Its a label key-value match
	routeLabels := route.Labels
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
	route := obj.(gslbutils.RouteMeta)
	mr := gr.MatchRule
	// Basic sanity checks
	if len(mr.Hosts) == 0 && mr.Label.Key == "" {
		gslbutils.Errf("object: GDPRule, route: %s, msg: %s", route.Name,
			"GDPRule doesn't have either hosts set or label key-value pair")
		return false
	}
	if len(mr.Hosts) > 0 && route.Hostname == "" {
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
		gslbutils.Errf("object: GDPRule, route: %s, operation: %s, msg: %s",
			route.Name, gr.MatchRule.Op, "operation is invalid")
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

// ClusterTraffic determines the "Weight" of traffic routed to a cluster with name "ClusterName"
type ClusterTraffic struct {
	ClusterName string
	Weight      int32
}

// GetTrafficSplit fetches the traffic split elements from the GDP object and returns a list of ClusterTraffic
func getTrafficSplit(trafficSplit []gdpv1alpha1.TrafficSplitElem) ([]ClusterTraffic, uint32) {
	var ctList []ClusterTraffic
	var cksum uint32
	for _, elem := range trafficSplit {
		ct := ClusterTraffic{
			ClusterName: elem.Cluster,
			Weight:      int32(elem.Weight),
		}
		ctList = append(ctList, ct)
		cksum += utils.Hash(ct.ClusterName) + utils.Hash(string(ct.Weight))
	}
	// find the checksum for this new list
	return ctList, cksum
}

// NSFilter is kind of like the ClusterFilter but is only applicable to one namespace.
type NSFilter struct {
	// ApplicableClusters is the list of clusters in which this filter is applicable.
	ApplicableClusters []string
	// ApplicableNamespace is the namespace in which this filter is applicable.
	ApplicableNamespace string
	// NSRules is the list of AND separated rules. If no rules, an object cannot pass.
	NSRules []GDPRule
	// TrafficSplit is the list of Cluster traffic ratio
	TrafficSplit []ClusterTraffic
	// Checksum is the sum of checksums of all the NSRules.
	Checksum uint32
	// NSLock is locked before accessing any of the fields here.
	NSLock sync.RWMutex
}

// ApplyFilter applies the gdp rules in this NS filter on the route object "obj" in cluster "cname".
// Returns true/false depending on whether this route passes the filter or not.
func (nf *NSFilter) ApplyFilter(obj interface{}, cname string) bool {
	route := obj.(gslbutils.RouteMeta)
	nf.NSLock.RLock()
	defer nf.NSLock.RUnlock()
	if !presentInList(cname, nf.ApplicableClusters) {
		return false
	}

	for _, gdpRule := range nf.NSRules {
		if !gdpRule.Apply(route) {
			gslbutils.Logf("cluster: %s, ns: %s, route: %s, msg: route rejected because of GDPRule: %s",
				cname, route.Namespace, route.Name, gdpRule)
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

func (nf *NSFilter) GetTrafficWeight(cname string) int32 {
	nf.NSLock.RLock()
	defer nf.NSLock.RUnlock()
	for _, ct := range nf.TrafficSplit {
		if cname == ct.ClusterName {
			return ct.Weight
		}
	}
	return -1
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
	// get the traffic split list
	trafficSplit, trafficCksum := getTrafficSplit(gdp.Spec.TrafficSplit)
	cksum += trafficCksum
	// Checksum should also include the cluster name list
	cksum += utils.Hash(utils.Stringify(clusterList))
	// Build the list of gdpRules from the match rules in the GDP object
	return &NSFilter{
		ApplicableClusters:  clusterList,
		ApplicableNamespace: ns,
		NSRules:             *gdpRules,
		TrafficSplit:        trafficSplit,
		Checksum:            cksum,
	}
}
