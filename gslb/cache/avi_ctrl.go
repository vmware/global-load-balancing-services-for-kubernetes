package cache

import (
	"sync"

	"gitlab.eng.vmware.com/orion/container-lib/utils"
	"gitlab.eng.vmware.com/orion/mcc/gslb/gslbutils"
)

var AviClientInstance *utils.AviRestClientPool

var clientOnce sync.Once

// SharedAviClients initializes a pool of connections to the avi controller
func SharedAviClients() *utils.AviRestClientPool {
	var err error

	ctrlCfg := gslbutils.GetAviConfig()
	if ctrlCfg.Username == "" || ctrlCfg.Password == "" || ctrlCfg.IPAddr == "" {
		utils.AviLog.Error.Panic("AVI Controller information is missing, update them in kubernetes secret or via environment variable.")
	}
	AviClientInstance, err = utils.NewAviRestClientPool(utils.NumWorkersGraph, ctrlCfg.IPAddr, ctrlCfg.Username, ctrlCfg.Password)
	if err != nil {
		utils.AviLog.Error.Printf("AVI Controller Initialization failed, %s", err)
	}
	return AviClientInstance
}
