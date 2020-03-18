package gslbutils

import (
	"errors"
	"net"
	"os"
	"sort"
	"strings"
	"sync"

	routev1 "github.com/openshift/api/route/v1"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
	gslbalphav1 "gitlab.eng.vmware.com/orion/mcc/pkg/apis/avilb/v1alpha1"
	"k8s.io/client-go/kubernetes"
)

const (
	// MaxClusters is the supported number of clusters
	MaxClusters       int    = 10
	GSLBHealthMonitor string = "System-GSLB-Ping"
	// GSLBKubePath is a temporary path to put the kubeconfig
	GSLBKubePath = "/tmp/gslb-kubeconfig"
	//AVISystem is the namespace where everything AVI related is created
	AVISystem = "avi-system"
	// Ingestion layer operations
	ObjectAdd    = "ADD"
	ObjectDelete = "DELETE"
	ObjectUpdate = "UPDATE"
	// Ingestion layer objects
	RouteType = "Route"
	// Refresh cycle for AVI cache in seconds
	DefaultRefreshInterval = 600
)

// InformersPerCluster is the number of informers per cluster
var InformersPerCluster *containerutils.AviCache

func SetInformersPerCluster(clusterName string, info *containerutils.Informers) {
	InformersPerCluster.AviCacheAdd(clusterName, info)
}

func GetInformersPerCluster(clusterName string) *containerutils.Informers {
	info, ok := InformersPerCluster.AviCacheGet(clusterName)
	if !ok {
		containerutils.AviLog.Warning.Printf("Failed to get informer for cluster %v", clusterName)
		return nil
	}
	return info.(*containerutils.Informers)
}

func MultiClusterKey(operation, objType, clusterName, ns, routeName string) string {
	key := operation + "/" + objType + clusterName + "/" + ns + "/" + routeName
	return key
}

func ExtractMultiClusterKey(key string) (string, string, string, string, string) {
	segments := strings.Split(key, "/")
	var operation, objType, cluster, ns, name string
	if len(segments) == 5 {
		operation, objType, cluster, ns, name = segments[0], segments[1], segments[2], segments[3], segments[4]
	}
	return operation, objType, cluster, ns, name
}

func SplitMultiClusterRouteName(name string) (string, string, string, error) {
	if name == "" {
		return "", "", "", errors.New("multi-cluster route name is empty")
	}
	reqList := strings.Split(name, "/")

	if len(reqList) != 3 {
		return "", "", "", errors.New("multi-cluster route name format is unexpected")
	}
	return reqList[0], reqList[1], reqList[2], nil
}

func RouteGetIPAddr(route *routev1.Route) (string, bool) {
	// Return true if the IP address is present in an route's status field, else return false
	routeStatus := route.Status
	for _, ingr := range routeStatus.Ingress {
		conditions := ingr.Conditions
		for _, condition := range conditions {
			// TODO: Check if the message field contains an IP address
			if condition.Message == "" {
				continue
			}
			// Check if this is a IP address
			addr := net.ParseIP(condition.Message)
			if addr != nil {
				// Found an IP address, return
				return condition.Message, true
			}
		}
	}
	return "", false
}

// Logf is aliased to utils' Info.Printf
var Logf = utils.AviLog.Info.Printf

// Errf is aliased to utils' Error.Printf
var Errf = utils.AviLog.Error.Printf

// Warnf is aliased to utils' Warning.Printf
var Warnf = utils.AviLog.Warning.Printf

// Cluster Routes store for all the route objects.
var (
	AcceptedRouteStore *ClusterStore
	RejectedRouteStore *ClusterStore
)

// GSLBConfigObj is global and is initialized only once
var GSLBConfigObj *gslbalphav1.GSLBConfig

// RouteMeta is the metadata for a route. It is the minimal information
// that we maintain for each route, accepted or rejected.
type RouteMeta struct {
	Cluster   string
	Name      string
	Namespace string
	Hostname  string
	IPAddr    string
	Labels    map[string]string
}

// GetRouteMeta returns a trimmed down version of a route
func GetRouteMeta(route *routev1.Route, cname string) RouteMeta {
	ipAddr, _ := RouteGetIPAddr(route)
	metaObj := RouteMeta{
		Name:      route.Name,
		Namespace: route.ObjectMeta.Namespace,
		Hostname:  route.Spec.Host,
		IPAddr:    ipAddr,
		Cluster:   cname,
	}
	metaObj.Labels = make(map[string]string)
	for key, value := range route.GetLabels() {
		metaObj.Labels[key] = value
	}
	return metaObj
}

func GetGSLBServiceChecksum(ipList, domainList, routeMembers []string) uint32 {
	sort.Strings(ipList)
	sort.Strings(domainList)
	sort.Strings(routeMembers)
	return utils.Hash(utils.Stringify(ipList)) +
		utils.Hash(utils.Stringify(domainList)) +
		utils.Hash(utils.Stringify(routeMembers))
}

func GetAviAdminTenantRef() string {
	return "https://" + os.Getenv("GSLB_CTRL_IPADDRESS") + "/api/tenant/" + utils.ADMIN_NS
}

// gslbConfigObjectAdded is a channel which halts the initialization operation until a GSLB config object
// is added. Even the GDP informers are started after this operation goes through.
// This channel's usage can be found in gslb.go.
var gslbConfigObjectAdded chan bool
var chanOnce sync.Once

func GetGSLBConfigObjectChan() *chan bool {
	chanOnce.Do(func() {
		gslbConfigObjectAdded = make(chan bool, 1)
	})
	return &gslbConfigObjectAdded
}

// gslbConfigSet and its setter and getter functions, to be used by the AddGSLBConfig method. This value
// is set to true once a GSLB Configuration has been successfully done.
var gslbConfigSet bool = false

func IsGSLBConfigSet() bool {
	return gslbConfigSet
}

func SetGSLBConfig(value bool) {
	gslbConfigSet = value
}

var GlobalKubeClient *kubernetes.Clientset

type AviControllerConfig struct {
	Username string
	Password string
	IPAddr   string
	Version  string
}

var gslbLeaderConfig AviControllerConfig
var leaderConfig sync.Once

func NewAviControllerConfig(username, password, ipAddr, version string) *AviControllerConfig {
	leaderConfig.Do(func() {
		gslbLeaderConfig = AviControllerConfig{
			Username: username,
			Password: password,
			IPAddr:   ipAddr,
			Version:  version,
		}
	})
	return &gslbLeaderConfig
}

func GetAviConfig() AviControllerConfig {
	return gslbLeaderConfig
}
