package gslbutils

import (
	"errors"
	"net"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	"gitlab.eng.vmware.com/orion/container-lib/utils"
	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
	gslbalphav1 "gitlab.eng.vmware.com/orion/mcc/pkg/apis/avilb/v1alpha1"
)

const (
	// MaxClusters is the supported number of clusters
	MaxClusters int = 10
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

const (
	// GSLBKubePath is a temporary path to put the kubeconfig
	GSLBKubePath = "/tmp/gslb-kubeconfig"
	//AVISystem is the namespace where everything AVI related is created
	AVISystem = "avi-system"
)

// Logf is aliased to utils' Info.Printf
var Logf = utils.AviLog.Info.Printf

// Errf is aliased to utils' Error.Printf
var Errf = utils.AviLog.Error.Printf

// Warnf is aliased to utils' Warning.Printf
var Warnf = utils.AviLog.Warning.Printf

// Key operations
const (
	ObjectAdd    = "ADD"
	ObjectDelete = "DELETE"
	ObjectUpdate = "UPDATE"
)

// Cluster Routes store for all the route objects.
var (
	AcceptedRouteStore *ClusterStore
	RejectedRouteStore *ClusterStore
)

// Constants for object types
const (
	RouteType = "Route"
)

// GSLBConfigObj is global and is initialized only once
var GSLBConfigObj *gslbalphav1.GSLBConfig

// RouteMeta is the metadata for a route. It is the minimal information
// that we maintain for each route, accepted or rejected.
type RouteMeta struct {
	Name      string
	Namespace string
	Hostname  string
	IPAddr    string
	Labels    map[string]string
}

// GetRouteMeta returns a trimmed down version of a route
func GetRouteMeta(route *routev1.Route) RouteMeta {
	ipAddr, _ := RouteGetIPAddr(route)
	metaObj := RouteMeta{
		Name:      route.Name,
		Namespace: route.ObjectMeta.Namespace,
		Hostname:  route.Spec.Host,
		IPAddr:    ipAddr,
	}
	metaObj.Labels = make(map[string]string)
	for key, value := range route.GetLabels() {
		metaObj.Labels[key] = value
	}
	return metaObj
}
