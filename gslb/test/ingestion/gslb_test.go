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

package ingestion

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/ingestion"
	gslbingestion "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/ingestion"

	gslbfake "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned/fake"

	gslbinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions"

	k8sfake "k8s.io/client-go/kubernetes/fake"
)

type GSLBTestConfigAddfn func(obj interface{})

// Unit test to create a new GSLB client, a kube client and see if a GSLB controller can be created
// using these.
func TestGSLBNewController(t *testing.T) {
	gslbKubeClient := k8sfake.NewSimpleClientset()
	gslbClient := gslbfake.NewSimpleClientset()
	gslbInformerFactory := gslbinformers.NewSharedInformerFactory(gslbClient, time.Second*30)
	gslbCtrl := gslbingestion.GetNewController(gslbKubeClient, gslbClient, gslbInformerFactory, addGSLBTestConfigObject,
		ingestion.InitializeGSLBMemberClusters)
	if gslbCtrl == nil {
		t.Fatalf("GSLB Controller not set")
	}
	go gslbCtrl.Run(gslbingestion.GetStopChannel())
}

// Unit test to see if a kube config can be generated from a encoded secret.
func TestGSLBKubeConfig(t *testing.T) {
	kubeconfigData, err := ioutil.ReadFile("./testdata/test-kube-config")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GSLB_CONFIG", string(kubeconfigData))
	err = gslbingestion.GenerateKubeConfig()
	if err != nil {
		t.Fatalf("Failure in generating GSLB Kube config: %s", err.Error())
	}
}
