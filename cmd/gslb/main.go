package main

import (
	"github.com/avinetworks/amko/gslb/gslbutils"
	"github.com/avinetworks/amko/gslb/ingestion"
)

func main() {
	gslbutils.InitAmkoAPIServer()
	ingestion.Initialize()
}
