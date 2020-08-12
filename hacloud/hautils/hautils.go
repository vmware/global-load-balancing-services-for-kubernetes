package hautils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	containerutils "github.com/avinetworks/ako/pkg/utils"
)

const (
	MAX_CLUSTERS int = 10
)

var InformersPerCluster *containerutils.AviCache

func SetInformersPerCluster(clustername string, info *containerutils.Informers) {
	InformersPerCluster.AviCacheAdd(clustername, info)
}

func GetInformersPerCluster(clustername string) *containerutils.Informers {
	info, ok := InformersPerCluster.AviCacheGet(clustername)
	if !ok {
		containerutils.AviLog.Warnf("Failed to get informer for cluster %v", clustername)
		return nil
	}
	return info.(*containerutils.Informers)
}

func MultiClusterKey(objtype string, clustername string, objname interface{}) string {
	key := objtype + clustername + "/" + containerutils.ObjKey(objname)
	return key
}

func ExtractMultiClusterKey(key string) (string, string, string, string) {
	segments := strings.Split(key, "/")
	var objtype, cluster, ns, name string
	if len(segments) == 4 {
		objtype, cluster, ns, name = segments[0], segments[1], segments[2], segments[3]
	}
	return objtype, cluster, ns, name
}

func ShardVSName(text, vsPrefix string, numShards int) (string, bool) {
	hasher := md5.New()
	hasher.Write([]byte(text))
	vsHex := hex.EncodeToString(hasher.Sum(nil))
	vsNum, _ := strconv.ParseUint(vsHex, 16, 32)
	if numShards > 0 {
		return fmt.Sprintf("%s%d", vsPrefix, int(vsNum)%numShards), true
	} else {
		containerutils.AviLog.Warn("numShards not >= zero")
		return "", false
	}
}
