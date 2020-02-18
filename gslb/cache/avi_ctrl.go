package cache

import (
	"os"
	"sync"

	"gitlab.eng.vmware.com/orion/container-lib/utils"
)

var AviClientInstance *utils.AviRestClientPool

var clientOnce sync.Once

// SharedAviClients initializes a pool of connections to the avi controller
func SharedAviClients() *utils.AviRestClientPool {
	var err error
	ctrlUsername := os.Getenv("GSLB_CTRL_USERNAME")
	ctrlPassword := os.Getenv("GSLB_CTRL_PASSWORD")
	ctrlIPAddress := os.Getenv("GSLB_CTRL_IPADDRESS")
	if ctrlUsername == "" || ctrlPassword == "" || ctrlIPAddress == "" {
		utils.AviLog.Error.Panic("AVI Controller information is missing, update them in kubernetes secret or via environment variable.")
	}
	AviClientInstance, err = utils.NewAviRestClientPool(utils.NumWorkersGraph, ctrlIPAddress, ctrlUsername, ctrlPassword)
	if err != nil {
		utils.AviLog.Error.Printf("AVI Controller Initialization failed, %s", err)
	}
	return AviClientInstance
}
