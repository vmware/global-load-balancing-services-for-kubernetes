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
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	gdpv1alpha2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"

	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"
)

type GDPObj struct {
	Namespace string
	Name      string
	GDPLock   sync.RWMutex
}

var gdpObj GDPObj

func SetGDPObj(name, ns string) {
	gdpObj.GDPLock.Lock()
	defer gdpObj.GDPLock.Unlock()
	gdpObj.Name = name
	gdpObj.Namespace = ns
}

func GetGDPObj() (string, string) {
	gdpObj.GDPLock.RLock()
	defer gdpObj.GDPLock.RUnlock()
	return gdpObj.Name, gdpObj.Namespace
}

func IsEmpty() bool {
	gdpObj.GDPLock.RLock()
	defer gdpObj.GDPLock.RUnlock()

	if gdpObj.Name == "" && gdpObj.Namespace == "" {
		return true
	}
	return false
}

var (
	// Need to keep this global since, it will be used across multiple layers and multiple handlers
	Gfi    *GlobalFilter
	gfOnce sync.Once
)

// ClusterProperties contains the properties for a cluster.
type ClusterProperties struct {
	// SyncVipsOnly advises AMKO to sync only the VIPs of the member objects of a GS
	SyncVipsOnly bool
}

// GlobalFilter is all the filters at one place. It also holds a list of ApplicableClusters
// to which all the filters are applicable. This list cannot be empty.
type GlobalFilter struct {
	// AppFilter contains rules for selecting applications
	AppFilter *AppFilter
	// NamespaceRules contains NamespaceSelector rules
	NSFilter *NamespaceFilter
	// TrafficSplit provides weights of traffic routed to different clusters
	TrafficSplit []ClusterTraffic
	// ApplicableClusters contain the list of clusters on which the filters
	// will be applicable
	ApplicableClusters map[string]ClusterProperties
	// List of health monitors to be attached to all the GSs
	HealthMonitorRefs []string
	// Site Persistence properties to be applied to all the GSs
	SitePersistence gdpv1alpha2.SitePersistence
	// Time To Live value for each fqdn
	TTL      *int
	Checksum uint32
	// Respective filters for the namespaces.
	// NSFilterMap map[string]*NSFilter
	// GlobalLock is locked before accessing any of the filters.
	GlobalLock sync.RWMutex
}

// GetGlobalFilter returns the existing global filter
func GetGlobalFilter() *GlobalFilter {
	gfOnce.Do(func() {
		Gfi = GetNewGlobalFilter()
	})
	return Gfi
}

func (gf *GlobalFilter) GetNSFilterLabel() (Label, error) {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	if gf.NSFilter == nil {
		return Label{}, errors.New("no NSFilter present")
	}

	return gf.NSFilter.GetFilterLabel(), nil
}

func (gf *GlobalFilter) GetAppFilterLabel() (Label, error) {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	if gf.AppFilter == nil {
		return Label{}, errors.New("no appFilter present")
	}

	return gf.AppFilter.Label, nil
}

func (gf *GlobalFilter) IsClusterAllowed(cname string) bool {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	if ClusterContextPresentInList(cname, gf.ApplicableClusters) {
		return true
	}
	return false
}

func (gf *GlobalFilter) AddNSToNSFilter(cname, ns string) error {
	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()

	if gf.NSFilter == nil {
		return errors.New("NSFilter empty in GlobalFilter, can't add namespace")
	}
	gf.NSFilter.AddNS(cname, ns)

	return nil
}

func (gf *GlobalFilter) GetAviHmRefs() []string {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	aviHmRefs := make([]string, len(gf.HealthMonitorRefs))
	copy(aviHmRefs, gf.HealthMonitorRefs)
	return aviHmRefs
}

func (gf *GlobalFilter) GetSitePersistence() string {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()
	if gf.SitePersistence.Enabled {
		return gf.SitePersistence.ProfileRef
	}
	return ""
}

func (gf *GlobalFilter) GetTTL() *int {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	return gf.TTL
}

type AppFilter struct {
	Label
}

type NamespaceFilter struct {
	Label
	// SelectedNS contains a list of namespaces selected via this filter
	// updated by the namespace event handlers
	SelectedNS map[string][]string
	// Checksum to check for changes if GDP changes and to see if a
	// re-application of namespaces is required
	Checksum uint32
	Lock     sync.RWMutex
}

func (nsFilter *NamespaceFilter) GetChecksum() uint32 {
	nsFilter.Lock.RLock()
	defer nsFilter.Lock.RUnlock()
	return nsFilter.Checksum
}

func (nsFilter *NamespaceFilter) GetFilterLabel() Label {
	nsFilter.Lock.RLock()
	defer nsFilter.Lock.RUnlock()
	return nsFilter.Label
}

func (nsFilter *NamespaceFilter) AddNS(cname, ns string) {
	nsFilter.Lock.Lock()
	defer nsFilter.Lock.Unlock()

	nsList, ok := nsFilter.SelectedNS[cname]
	if !ok {
		nsFilter.SelectedNS[cname] = []string{ns}
		return
	}

	if !PresentInList(ns, nsList) {
		nsList = append(nsList, ns)
		nsFilter.SelectedNS[cname] = nsList
	}
}

type Label struct {
	Key   string
	Value string
}

func getLabelKeyAndValue(lbl map[string]string) (string, string) {
	for k, v := range lbl {
		return k, v
	}
	return "", ""
}

func createNewNSFilter(lbl map[string]string) *NamespaceFilter {
	k, v := getLabelKeyAndValue(lbl)
	nsFilter := NamespaceFilter{
		Label: Label{
			Key:   k,
			Value: v,
		},
	}
	// checksum for NSFilter only accounts for the key and label i.e., wrt
	// any GDP changes and not namespace changes
	cksum := utils.Hash(k + v)
	nsFilter.Checksum = cksum
	return &nsFilter
}

// AddToFilter handles creation of new filters, cluster or otherwise.
// Only one GDP object allowed per-cluster.
func (gf *GlobalFilter) AddToFilter(gdp *gdpv1alpha2.GlobalDeploymentPolicy) {
	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()
	if len(gdp.Spec.MatchRules.AppSelector.Label) == 1 {
		k, v := getLabelKeyAndValue(gdp.Spec.MatchRules.AppSelector.Label)
		appFilter := AppFilter{
			Label: Label{
				Key:   k,
				Value: v,
			},
		}
		gf.AppFilter = &appFilter
	}
	if len(gdp.Spec.MatchRules.NamespaceSelector.Label) == 1 {
		gf.NSFilter = createNewNSFilter(gdp.Spec.MatchRules.NamespaceSelector.Label)
	}

	if len(gf.ApplicableClusters) == 0 {
		gf.ApplicableClusters = make(map[string]ClusterProperties)
	}
	// Add applicable clusters
	for _, cluster := range gdp.Spec.MatchClusters {
		gf.ApplicableClusters[cluster.Cluster] = ClusterProperties{cluster.SyncVipOnly}
	}
	// Add traffic split
	for _, ts := range gdp.Spec.TrafficSplit {
		ct := ClusterTraffic{
			ClusterName: ts.Cluster,
			Weight:      int32(ts.Weight),
		}
		gf.TrafficSplit = append(gf.TrafficSplit, ct)
	}

	if len(gdp.Spec.HealthMonitorRefs) > 0 {
		gf.HealthMonitorRefs = make([]string, len(gdp.Spec.HealthMonitorRefs))
		copy(gf.HealthMonitorRefs, gdp.Spec.HealthMonitorRefs)
	}

	gf.TTL = gdp.Spec.TTL
	// The below copies by value as there are no non-primitive members inside SitePersistence
	// struct.
	gf.SitePersistence = gdp.Spec.SitePersistence

	gf.ComputeChecksum()
	Logf("ns: %s, object: NSFilter, msg: added/changed the global filter", gdp.ObjectMeta.Namespace)
}

func (gf *GlobalFilter) ComputeChecksum() {
	var cksum uint32
	var hmRefs []string

	if gf.AppFilter != nil {
		cksum += utils.Hash(gf.AppFilter.Key + gf.AppFilter.Value)
	}
	if gf.NSFilter != nil {
		cksum += gf.NSFilter.GetChecksum()
	}
	for c, s := range gf.ApplicableClusters {
		cksum += utils.Hash(c) + utils.Hash(utils.Stringify(s.SyncVipsOnly))
	}
	for _, ts := range gf.TrafficSplit {
		cksum += utils.Hash(ts.ClusterName + strconv.Itoa(int(ts.Weight)))
	}
	if gf.SitePersistence.Enabled {
		cksum += utils.Hash(utils.Stringify(gf.SitePersistence.Enabled)) +
			utils.Hash(utils.Stringify(gf.SitePersistence.ProfileRef))
	}
	if gf.TTL != nil {
		cksum += utils.Hash(utils.Stringify(*gf.TTL))
	}
	if len(gf.HealthMonitorRefs) > 0 {
		hmRefs = make([]string, len(gf.HealthMonitorRefs))
		copy(hmRefs, gf.HealthMonitorRefs)
		sort.Strings(hmRefs)
		cksum += utils.Hash(utils.Stringify(hmRefs))
	}

	gf.Checksum = cksum
}

func (gf *GlobalFilter) GetTrafficWeight(ns, cname string) (int32, error) {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()
	for _, ts := range gf.TrafficSplit {
		if ts.ClusterName == cname {
			return ts.Weight, nil
		}
	}
	Logf("cname: %s, msg: no weight available for this cluster", cname)
	return 0, errors.New("no weight available for cluster " + cname)
}

func (gf *GlobalFilter) IsClusterSyncVIPOnly(cname string) (bool, error) {
	gf.GlobalLock.RLock()
	defer gf.GlobalLock.RUnlock()

	properties, exists := gf.ApplicableClusters[cname]
	if !exists {
		return false, fmt.Errorf("cluster %s not present in global filter", cname)
	}
	return properties.SyncVipsOnly, nil
}

func PresentInList(key string, strList []string) bool {
	for _, str := range strList {
		if str == key {
			return true
		}
	}
	return false
}

func ClusterContextPresentInList(key string, clusterProperties map[string]ClusterProperties) bool {
	for cluster := range clusterProperties {
		if cluster == key {
			return true
		}
	}
	return false
}

func isTrafficWeightChanged(new, old *gdpv1alpha2.GlobalDeploymentPolicy) bool {
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

func isSyncTypeChanged(new, old *gdpv1alpha2.GlobalDeploymentPolicy) []string {
	// Return a list of clusters for which the sync type has changed
	clustersToBeSynced := []string{}
	clusters := make(map[string]bool)
	for _, c := range old.Spec.MatchClusters {
		clusters[c.Cluster] = c.SyncVipOnly
	}

	for _, c := range new.Spec.MatchClusters {
		oldSyncType, exists := clusters[c.Cluster]
		if !exists {
			// cluster doesn't exist in the new gdp, it will be taken care of in the accepted/rejected
			// logic anyway, so just continue
			continue
		}
		if c.SyncVipOnly != oldSyncType {
			clustersToBeSynced = append(clustersToBeSynced, c.Cluster)
		}
	}
	return clustersToBeSynced
}

func isHmRefsChanged(new, old *gdpv1alpha2.GlobalDeploymentPolicy) bool {
	if len(old.Spec.HealthMonitorRefs) != len(new.Spec.HealthMonitorRefs) {
		return true
	}
	oldHmRefs := make(map[string]struct{})
	for _, hmRef := range old.Spec.HealthMonitorRefs {
		oldHmRefs[hmRef] = struct{}{}
	}
	for _, hmRef := range new.Spec.HealthMonitorRefs {
		if _, exists := oldHmRefs[hmRef]; !exists {
			return true
		}
	}
	return false
}

func isSitePersistenceChanged(new, old *gdpv1alpha2.GlobalDeploymentPolicy) bool {
	newSp := new.Spec.SitePersistence
	oldSp := old.Spec.SitePersistence
	if (newSp.Enabled == oldSp.Enabled) && (newSp.Enabled == true) {
		// if both are true, we check the profile ref value
		if newSp.ProfileRef != oldSp.ProfileRef {
			return true
		}
		return false
	} else if newSp.Enabled != oldSp.Enabled {
		// else, check for any change in the enabled property
		return true
	}
	return false
}

func isTTLChanged(new, old *gdpv1alpha2.GlobalDeploymentPolicy) bool {
	if new.Spec.TTL == nil && old.Spec.TTL != nil {
		return true
	} else if new.Spec.TTL != nil && old.Spec.TTL == nil {
		return true
	} else if new.Spec.TTL != nil && old.Spec.TTL != nil && *new.Spec.TTL != *old.Spec.TTL {
		return true
	}
	return false
}

func isAllGSPropertyChanged(new, old *gdpv1alpha2.GlobalDeploymentPolicy) bool {
	return isHmRefsChanged(old, new) || isSitePersistenceChanged(old, new) ||
		isTTLChanged(old, new) || isTrafficWeightChanged(new, old)
}

// UpdateGlobalFilter takes two arguments: the old and the new GDP objects, and verifies
// whether a change is required to any of the filters. If yes, it changes either the cluster
// filter or one of the namespace filters.
func (gf *GlobalFilter) UpdateGlobalFilter(oldGDP, newGDP *gdpv1alpha2.GlobalDeploymentPolicy) (bool, bool, []string) {
	// Need to check for the NSFilterMap
	nf := GetNewGlobalFilter()
	nf.AddToFilter(newGDP)

	Logf("ns: %s, gdp: %s, msg: %s", oldGDP.ObjectMeta.Namespace, oldGDP.ObjectMeta.Name,
		"got an update event")
	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()
	Debugf("old checksum: %d, new checksum: %d", gf.Checksum, nf.Checksum)
	if gf.Checksum == nf.Checksum {
		// No updates needed, just return
		return false, false, []string{}
	}
	Logf("ns: %s, gdp: %s, object: filter, msg: %s", oldGDP.ObjectMeta.Namespace, oldGDP.ObjectMeta.Name,
		"filter changed, will update filter and re-evaluate objects")
	// update the filter if the checksums changed
	gf.AppFilter = nf.AppFilter
	gf.NSFilter = nf.NSFilter
	gf.TrafficSplit = nf.TrafficSplit
	gf.ApplicableClusters = nf.ApplicableClusters
	gf.TTL = nf.TTL
	gf.SitePersistence = nf.SitePersistence
	gf.HealthMonitorRefs = nf.HealthMonitorRefs
	gf.Checksum = nf.Checksum

	clustersToBeSynced := isSyncTypeChanged(newGDP, oldGDP)

	return true, isAllGSPropertyChanged(newGDP, oldGDP), clustersToBeSynced
}

// DeleteFromGlobalFilter deletes a filter pertaining to gdp.
func (gf *GlobalFilter) DeleteFromGlobalFilter(gdp *gdpv1alpha2.GlobalDeploymentPolicy) {
	gf.GlobalLock.Lock()
	defer gf.GlobalLock.Unlock()
	gf.AppFilter = nil
	gf.NSFilter = nil
	gf.ApplicableClusters = make(map[string]ClusterProperties)
	gf.Checksum = 0
	gf.TrafficSplit = []ClusterTraffic{}
}

// GetNewGlobalFilter returns a new GlobalFilter. It is to be called only once with the
// the GDP object as the input. Either the namespace of the GDP object is AVISystem
// or its some other namespace. Based on that this GlobalFilter is created.
func GetNewGlobalFilter() *GlobalFilter {
	gf := &GlobalFilter{
		AppFilter:          nil,
		NSFilter:           nil,
		TrafficSplit:       []ClusterTraffic{},
		ApplicableClusters: make(map[string]ClusterProperties),
		HealthMonitorRefs:  []string{},
		TTL:                nil,
		SitePersistence:    gdpv1alpha2.SitePersistence{Enabled: false, ProfileRef: ""},
	}
	return gf
}

// ClusterTraffic determines the "Weight" of traffic routed to a cluster with name "ClusterName"
type ClusterTraffic struct {
	ClusterName string
	Weight      int32
}
