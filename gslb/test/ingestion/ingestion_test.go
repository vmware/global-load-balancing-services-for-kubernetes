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
	"os"
	"testing"

	containerutils "github.com/avinetworks/container-lib/utils"
)

const kubeConfigPath = "/tmp/gslb-kubeconfig"

func syncFuncForTest(key string) error {
	keyChan <- key
	return nil
}

func setupQueue(testStopCh <-chan struct{}) {
	ingestionQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	ingestionQueue.SyncFunc = syncFuncForTest
	ingestionQueue.Run(testStopCh)
}

func TestMain(m *testing.M) {
	setUp()
	ret := m.Run()
	os.Exit(ret)
}

func setUp() {
	os.Setenv("INGRESS_API", "extensionv1")

	testStopCh = containerutils.SetupSignalHandler()
	keyChan = make(chan string)

	setupQueue(testStopCh)
}
