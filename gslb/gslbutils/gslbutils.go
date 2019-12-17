package gslbutils

import (
	"strings"

	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
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

func MultiClusterKey(objType string, clusterName string, objName interface{}) string {
	key := objType + clusterName + "/" + containerutils.ObjKey(objName)
	return key
}

func ExtractMultiClusterKey(key string) (string, string, string, string) {
	segments := strings.Split(key, "/")
	var objType, cluster, ns, name string
	if len(segments) == 4 {
		objType, cluster, ns, name = segments[0], segments[1], segments[2], segments[3]
	}
	return objType, cluster, ns, name
}
