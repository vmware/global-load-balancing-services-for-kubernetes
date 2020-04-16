/*
* [2013] - [2020] Avi Networks Incorporated
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
	gslbingestion "amko/gslb/ingestion"
	gslbalphav1 "amko/pkg/apis/avilb/v1alpha1"
	gslbfake "amko/pkg/client/clientset/versioned/fake"
	gslbinformers "amko/pkg/client/informers/externalversions"
	"testing"
	"time"

	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
)

// Test the GDP controller initialization.
func TestGDPNewController(t *testing.T) {
	gdpKubeClient := k8sfake.NewSimpleClientset()
	gdpClient := gslbfake.NewSimpleClientset()
	gdpInformerFactory := gslbinformers.NewSharedInformerFactory(gdpClient, time.Second*30)
	gdpCtrl := gslbingestion.InitializeGDPController(gdpKubeClient, gdpClient, gdpInformerFactory, addSomething, updateSomething,
		addSomething)
	if gdpCtrl == nil {
		t.Fatalf("GDP controller not set")
	}
}

// addSomething is a dummy function used to initialize the GDP controller
func addSomething(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {

}

// updateSomething is a dummy function used to initialize the GDP controller
func updateSomething(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {

}

func updateTestGDPObject(gdp *gslbalphav1.GlobalDeploymentPolicy, clusterList []string, version string) {
	var matchClusters []gslbalphav1.MemberCluster
	for _, cname := range clusterList {
		member := gslbalphav1.MemberCluster{
			ClusterContext: cname,
		}
		matchClusters = append(matchClusters, member)
	}

	gdp.Spec.MatchClusters = matchClusters
	gdp.ObjectMeta.ResourceVersion = version
}
