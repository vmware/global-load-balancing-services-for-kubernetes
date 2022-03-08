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

package restlayer

import (
	"os"
	"sync"
	"testing"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/nodes"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/rest"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/mockaviserver"

	gdpv1alpha2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"

	"github.com/onsi/gomega"
	"github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/pkg/utils"

	avicache "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/cache"
)

const (
	DefaultNS = "default"
)

var (
	keyChan chan string
)

func TestMain(m *testing.M) {
	setUp()
	ret := m.Run()
	os.Exit(ret)
}

func syncFuncForRetryTest(key interface{}, wg *sync.WaitGroup) error {
	keyStr, ok := key.(string)
	if !ok {
		gslbutils.Errf("unexpected object type: expected string, got %T", key)
		return nil
	}

	keyChan <- keyStr
	return nil
}

func setupQueue(testCh <-chan struct{}) {
	slowRetryQParams := utils.WorkerQueue{NumWorkers: 1, WorkqueueName: gslbutils.SlowRetryQueue, SlowSyncTime: gslbutils.SlowSyncTime}
	utils.SharedWorkQueue(&slowRetryQParams)

	slowRetryQ := utils.SharedWorkQueue().GetQueueByName(gslbutils.SlowRetryQueue)
	slowRetryQ.SyncFunc = syncFuncForRetryTest
	slowRetryQ.Run(testCh, &sync.WaitGroup{})
}

func setUp() {
	testStopCh := utils.SetupSignalHandler()
	setupQueue(testStopCh)

	gslbutils.SetControllerAsLeader()
	mockaviserver.NewAviMockAPIServer()
	url := mockaviserver.GetMockServerURL()
	gslbutils.NewAviControllerConfig("admin", "admin", url, "18.2.9")
}

func buildTestGSGraph(clusterList, ipList, objNames []string, host, objType string) nodes.AviGSObjectGraph {
	memberObjs := []nodes.AviGSK8sObj{}
	for idx, _ := range clusterList {
		memberObj := nodes.AviGSK8sObj{
			Cluster:   clusterList[idx],
			ObjType:   objType,
			Name:      objNames[idx],
			Namespace: DefaultNS,
			IPAddr:    ipList[idx],
			Weight:    10,
		}
		memberObjs = append(memberObjs, memberObj)
	}
	path := "/"
	protocol := "https"
	gsGraph := nodes.AviGSObjectGraph{
		Name:        host,
		Tenant:      utils.ADMIN_NS,
		DomainNames: []string{host},
		MemberObjs:  memberObjs,
		Hm: nodes.HealthMonitor{
			Name:       "",
			HMProtocol: gslbutils.SystemGslbHealthMonitorHTTPS,
			Port:       443,
			Type:       nodes.PathHM,
			PathHM: []nodes.PathHealthMonitorDetails{
				{
					Name:            "amko--d32527f936da2c6c888e4c53d19e1eda52735f5c",
					IngressProtocol: protocol,
					Path:            path,
				},
			},
		},
	}
	gsGraph.GetChecksum()
	return gsGraph
}

func verifyMembersMatch(g *gomega.WithT, gsGraph nodes.AviGSObjectGraph, gsCacheObj *avicache.AviGSCache) {
	for _, member := range gsCacheObj.Members {
		matched := false
		for _, graphMember := range gsGraph.MemberObjs {
			if member.IPAddr == graphMember.IPAddr && member.Weight == graphMember.Weight {
				matched = true
				break
			}
		}
		g.Expect(matched).To(gomega.Equal(true))
	}
}

func verifyInAviCache(t *testing.T, gsGraph nodes.AviGSObjectGraph, deleteCase bool) {
	cache := avicache.GetAviCache()
	cacheKey := avicache.TenantName{
		Tenant: gsGraph.Tenant,
		Name:   gsGraph.Name,
	}

	gsCache, found := cache.AviCacheGet(cacheKey)
	g := gomega.NewGomegaWithT(t)
	if deleteCase {
		g.Expect(found).To(gomega.Equal(false))
		return
	}
	g.Expect(found).To(gomega.Equal(true))
	gsCacheObj, ok := gsCache.(*avicache.AviGSCache)
	g.Expect(ok).To(gomega.Equal(true))
	g.Expect(gsCacheObj.Name).To(gomega.Equal(gsGraph.Name))
	g.Expect(gsCacheObj.Tenant).To(gomega.Equal(utils.ADMIN_NS))
	g.Expect(gsCacheObj.K8sObjects).To(gomega.HaveLen(len(gsGraph.MemberObjs)))
	verifyMembersMatch(g, gsGraph, gsCacheObj)
}

func saveSyncAndVerify(t *testing.T, modelName string, gsGraph nodes.AviGSObjectGraph, deleteCase bool) {
	gsGraph.SetRetryCounter()

	if !deleteCase {
		agl := nodes.SharedAviGSGraphLister()
		agl.Save(modelName, &gsGraph)
	}
	rest.SyncFromNodesLayer(gsGraph.Tenant+"/"+gsGraph.Name, &sync.WaitGroup{})
	verifyInAviCache(t, gsGraph, deleteCase)
}

func TestCreateGS(t *testing.T) {
	host := "host1.avi.com"
	clusterList := []string{"foo", "bar"}
	ipList := []string{"10.10.10.11", "10.10.10.21"}
	names := []string{"ing1/host1.foo.com", "ing2/host1.foo.com"}
	modelName := utils.ADMIN_NS + "/" + host
	// build a AviGSObjectGraph
	gsGraph := buildTestGSGraph(clusterList, ipList, names, host, gdpv1alpha2.IngressObj)
	saveSyncAndVerify(t, modelName, gsGraph, false)
}

func TestUpdateGS(t *testing.T) {
	host := "host2.avi.com"
	clusterList := []string{"foo"}
	ipList := []string{"10.10.10.21"}
	names := []string{"ing1" + "/" + host}
	modelName := utils.ADMIN_NS + "/" + host
	gsGraph := buildTestGSGraph(clusterList, ipList, names, host, gdpv1alpha2.IngressObj)
	saveSyncAndVerify(t, modelName, gsGraph, false)

	// update the graph
	newMember := nodes.AviGSK8sObj{
		Cluster:   "bar",
		ObjType:   gdpv1alpha2.IngressObj,
		Name:      "ing2" + "/" + host,
		Namespace: DefaultNS,
		IPAddr:    "10.10.10.22",
		Weight:    10,
	}
	gsGraph.MemberObjs = append(gsGraph.MemberObjs, newMember)
	saveSyncAndVerify(t, modelName, gsGraph, false)
}

func TestDeleteGS(t *testing.T) {
	host := "host3.avi.com"
	clusterList := []string{"foo", "bar"}
	ipList := []string{"10.10.10.31", "10.10.10.32"}
	names := []string{"ing1/" + host, "ing2/" + host}
	modelName := utils.ADMIN_NS + "/" + host
	// build a AviGSObjectGraph
	gsGraph := buildTestGSGraph(clusterList, ipList, names, host, gdpv1alpha2.IngressObj)
	saveSyncAndVerify(t, modelName, gsGraph, false)

	gsGraph.SetRetryCounter()

	dgl := nodes.SharedDeleteGSGraphLister()
	dgl.Save(modelName, &gsGraph)

	agl := nodes.SharedAviGSGraphLister()
	agl.Delete(modelName)
	rest.SyncFromNodesLayer(gsGraph.Tenant+"/"+gsGraph.Name, &sync.WaitGroup{})

	gsGraph.DeleteMember("foo", DefaultNS, names[0], gdpv1alpha2.IngressObj)
	gsGraph.DeleteMember("bar", DefaultNS, names[1], gdpv1alpha2.IngressObj)

	saveSyncAndVerify(t, modelName, gsGraph, true)
}
