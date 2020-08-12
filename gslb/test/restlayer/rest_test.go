package restlayer

import (
	"os"
	"sync"
	"testing"

	"github.com/avinetworks/amko/gslb/gslbutils"
	"github.com/avinetworks/amko/gslb/nodes"
	"github.com/avinetworks/amko/gslb/rest"
	"github.com/avinetworks/amko/gslb/test/mockaviserver"

	"github.com/avinetworks/amko/internal/apis/amko/v1alpha1"

	"github.com/avinetworks/container-lib/utils"
	"github.com/onsi/gomega"

	avicache "github.com/avinetworks/amko/gslb/cache"
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

func setUp() {
	// testStopCh = utils.SetupSignalHandler()
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

	gsGraph := nodes.AviGSObjectGraph{
		Name:        host,
		Tenant:      utils.ADMIN_NS,
		DomainNames: []string{host},
		MemberObjs:  memberObjs,
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
	agl := nodes.SharedAviGSGraphLister()
	agl.Save(modelName, &gsGraph)
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
	gsGraph := buildTestGSGraph(clusterList, ipList, names, host, v1alpha1.IngressObj)
	saveSyncAndVerify(t, modelName, gsGraph, false)
}

func TestUpdateGS(t *testing.T) {
	host := "host2.avi.com"
	clusterList := []string{"foo"}
	ipList := []string{"10.10.10.21"}
	names := []string{"ing1" + "/" + host}
	modelName := utils.ADMIN_NS + "/" + host
	gsGraph := buildTestGSGraph(clusterList, ipList, names, host, v1alpha1.IngressObj)
	saveSyncAndVerify(t, modelName, gsGraph, false)

	// update the graph
	newMember := nodes.AviGSK8sObj{
		Cluster:   "bar",
		ObjType:   v1alpha1.IngressObj,
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
	gsGraph := buildTestGSGraph(clusterList, ipList, names, host, v1alpha1.IngressObj)
	saveSyncAndVerify(t, modelName, gsGraph, false)

	gsGraph.SetRetryCounter()
	agl := nodes.SharedAviGSGraphLister()
	agl.Save(modelName, nil)
	rest.SyncFromNodesLayer(gsGraph.Tenant+"/"+gsGraph.Name, &sync.WaitGroup{})

	gsGraph.DeleteMember("foo", DefaultNS, names[0], v1alpha1.IngressObj)
	gsGraph.DeleteMember("bar", DefaultNS, names[1], v1alpha1.IngressObj)

	saveSyncAndVerify(t, modelName, gsGraph, true)
}
