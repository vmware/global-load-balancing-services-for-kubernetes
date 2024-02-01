/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"time"

	b64 "encoding/base64"

	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	amkovmwarecomv1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/api/v1alpha1"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var testEnv1 *envtest.Environment
var testEnv2 *envtest.Environment

const (
	Cluster1                 = "cluster1"
	Cluster2                 = "cluster2"
	TestAMKOVersion          = "1.4.2"
	TestAMKODifferentVersion = "1.5.1"
	TestAMKOClusterName      = "test-amko-cluster"
	TestGSLBSecret           = "gslb-config-secret"
	AMKOCRDs                 = "../../helm/amko/crds"
	TestGCName               = "test-gc"
	TestGDPName              = "test-gdp"
	TestLeaderIP             = "10.10.10.10"
)

const KubeConfigData = `
apiVersion: v1
clusters: []
contexts: []
kind: Config
preferences: {}
users: []
`

type ClustersKubeConfig struct {
	APIVersion string            `yaml:"apiVersion"`
	Clusters   []ClusterData     `yaml:"clusters"`
	Contexts   []KubeContextData `yaml:"contexts"`
	Kind       string            `yaml:"kind"`
	Users      []UserData        `yaml:"users"`
}

type ClusterData struct {
	Cluster ClusterServerData `yaml:"cluster"`
	Name    string            `yaml:"name"`
}

type ClusterServerData struct {
	CAData string `yaml:"certificate-authority-data"`
	Server string `yaml:"server"`
}

type KubeContextData struct {
	Context ContextData `yaml:"context"`
	Name    string      `yaml:"name"`
}

type ContextData struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

type UserData struct {
	Name string `yaml:"name"`
	User UserID `yaml:"user"`
}

type UserID struct {
	ClientCert string `yaml:"client-certificate-data"`
	ClientKey  string `yaml:"client-key-data"`
}

func BuildAndCreateTestKubeConfig(k8sClient1, k8sClient2 client.Client) {
	user1 := Cluster1 + "-user"
	user2 := Cluster2 + "-user"

	// kData := make(map[string]interface{})
	kData := ClustersKubeConfig{}
	Expect(yaml.Unmarshal([]byte(KubeConfigData), &kData)).Should(Succeed())

	kData.Clusters = []ClusterData{
		{
			Cluster: ClusterServerData{
				CAData: b64.StdEncoding.EncodeToString([]byte(testEnv1.Config.CAData)),
				Server: testEnv1.Config.Host,
			},
			Name: Cluster1,
		},
		{
			Cluster: ClusterServerData{
				CAData: b64.StdEncoding.EncodeToString([]byte(testEnv2.Config.CAData)),
				Server: testEnv2.Config.Host,
			},
			Name: Cluster2,
		},
	}

	kData.Contexts = []KubeContextData{
		{
			Context: ContextData{
				Cluster: Cluster1,
				User:    user1,
			},
			Name: Cluster1,
		},
		{
			Context: ContextData{
				Cluster: Cluster2,
				User:    user2,
			},
			Name: Cluster2,
		},
	}

	kData.Users = []UserData{
		{
			Name: user1,
			User: UserID{
				ClientCert: b64.StdEncoding.EncodeToString([]byte(testEnv1.Config.CertData)),
				ClientKey:  b64.StdEncoding.EncodeToString([]byte(testEnv1.Config.KeyData)),
			},
		},
		{
			Name: user2,
			User: UserID{
				ClientCert: b64.StdEncoding.EncodeToString([]byte(testEnv2.Config.CertData)),
				ClientKey:  b64.StdEncoding.EncodeToString([]byte(testEnv2.Config.KeyData)),
			},
		},
	}

	// generate a string out of kubeCfg
	kubeCfgData, err := yaml.Marshal(kData)
	Expect(err).NotTo(HaveOccurred())

	// create the "avi-system" namespace
	nsObj := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: AviSystemNS,
		},
	}
	Expect(k8sClient1.Create(context.TODO(), &nsObj)).Should(Succeed())
	Expect(os.Setenv("GSLB_CONFIG", string(kubeCfgData))).Should(Succeed())

	// create "avi-system" namespace on the other cluster as well
	nsObj.ObjectMeta.ResourceVersion = ""
	Expect(k8sClient2.Create(context.TODO(), &nsObj)).Should(Succeed())
}

func getTestAMKOClusterObj(currentContext string, isLeader bool) amkovmwarecomv1alpha1.AMKOCluster {
	return amkovmwarecomv1alpha1.AMKOCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestAMKOClusterName,
			Namespace: AviSystemNS,
		},
		Spec: amkovmwarecomv1alpha1.AMKOClusterSpec{
			ClusterContext: currentContext,
			IsLeader:       isLeader,
			Clusters:       []string{Cluster1, Cluster2},
			Version:        TestAMKOVersion,
		},
	}
}

func getTestAMKOClusterStatusReason(status amkovmwarecomv1alpha1.AMKOClusterStatus,
	statusType string) map[string]string {
	for _, condition := range status.Conditions {
		if condition.Type == statusType {
			return map[string]string{
				"reason": condition.Reason,
				"status": condition.Status,
			}
		}
	}
	return map[string]string{}
}

/*

func getTestAMKOClusterStatusMsg(status amkovmwarecomv1alpha1.AMKOClusterStatus, statusType string) string {
	for _, condition := range status.Conditions {
		if condition.Type == statusType {
			return condition.Status
		}
	}
	return ""
}*/

func getTestGCObj() gslbalphav1.GSLBConfig {
	return gslbalphav1.GSLBConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestGCName,
			Namespace: AviSystemNS,
			Annotations: map[string]string{
				"amko.vmware.com/amko-uuid": "3e328a5c-a717-11ed-a422-0a580a80025b",
			},
		},
		Spec: gslbalphav1.GSLBConfigSpec{
			GSLBLeader: gslbalphav1.GSLBLeader{
				Credentials:       "test-creds",
				ControllerVersion: "20.1.4",
				ControllerIP:      TestLeaderIP,
			},
			MemberClusters: []gslbalphav1.MemberCluster{
				{
					ClusterContext: Cluster1,
				},
				{
					ClusterContext: Cluster2,
				},
			},
			RefreshInterval: 3600,
			LogLevel:        "INFO",
		},
	}
}

func getTestGDPObject() gdpalphav2.GlobalDeploymentPolicy {
	label := make(map[string]string)
	label["key"] = "value"
	return gdpalphav2.GlobalDeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestGDPName,
			Namespace: AviSystemNS,
		},
		Spec: gdpalphav2.GDPSpec{
			MatchRules: gdpalphav2.MatchRules{
				AppSelector: gdpalphav2.AppSelector{
					Label: label,
				},
			},
			MatchClusters: []gdpalphav2.ClusterProperty{
				{
					Cluster: Cluster1,
				},
				{
					Cluster: Cluster2,
				},
			},
			TTL: getGDPTTLPtr(300),
		},
	}
}

func getGDPTTLPtr(val int) *int {
	ttl := val
	return &ttl
}

func createTestGCAndGDPObjs(ctx context.Context, k8sClient client.Client, gc *gslbalphav1.GSLBConfig, gdp *gdpalphav2.GlobalDeploymentPolicy) {
	Expect(k8sClient.Create(ctx, gc)).Should(Succeed())
	Expect(k8sClient.Create(ctx, gdp)).Should(Succeed())
}

func deleteTestGCAndGDPObj(ctx context.Context, k8sClient client.Client, gc *gslbalphav1.GSLBConfig, gdp *gdpalphav2.GlobalDeploymentPolicy) {
	err := k8sClient.Delete(ctx, gc)
	if err != nil && k8serrors.IsNotFound(err) {
		return
	}
	Expect(err).ToNot(HaveOccurred())
	err = k8sClient.Delete(ctx, gdp)
	if err != nil && k8serrors.IsNotFound(err) {
		return
	}
	Expect(err).ToNot(HaveOccurred())
}

func TestGCGDPNotFederated(k8sClient client.Client) {
	var gcList gslbalphav1.GSLBConfigList
	ctx := context.Background()
	Expect(k8sClient.List(ctx, &gcList)).Should(Succeed())
	Expect(len(gcList.Items)).Should(BeZero())

	var gdpList gdpalphav2.GlobalDeploymentPolicyList
	Expect(k8sClient.List(ctx, &gdpList)).Should(Succeed())
	Expect(len(gdpList.Items)).Should(BeZero())
}

func TestGCGDPExist(k8sClient client.Client) {
	var gcList gslbalphav1.GSLBConfigList
	ctx := context.Background()
	Expect(k8sClient.List(ctx, &gcList)).Should(Succeed())
	Expect(len(gcList.Items)).Should(Equal(1))

	var gdpList gdpalphav2.GlobalDeploymentPolicyList
	Expect(k8sClient.List(ctx, &gdpList)).Should(Succeed())
	Expect(len(gdpList.Items)).Should(Equal(1))
}

func VerifyTestAMKOClusterStatus(k8sClient client.Client, statusType, statusMsg, failureMsg string) {
	Eventually(func() map[string]string {
		var obj amkovmwarecomv1alpha1.AMKOCluster

		Expect(k8sClient.Get(context.TODO(),
			types.NamespacedName{
				Name:      TestAMKOClusterName,
				Namespace: AviSystemNS},
			&obj)).Should(Succeed())

		fmt.Printf("status of AMKOCluster: %v\n", obj.Status)
		return getTestAMKOClusterStatusReason(obj.Status, statusType)
	}, 30*time.Second, 1*time.Second).Should(Equal(map[string]string{"reason": failureMsg,
		"status": statusMsg,
	}))
}

func CleanupTestObjects(k8sClient1, k8sClient2 client.Client,
	amkoCluster1, amkoCluster2 *amkovmwarecomv1alpha1.AMKOCluster,
	gcObj *gslbalphav1.GSLBConfig, gdpObj *gdpalphav2.GlobalDeploymentPolicy) {

	ctx := context.Background()
	Expect(k8sClient1.Delete(ctx, amkoCluster1)).Should(Succeed())
	deleteTestGCAndGDPObj(ctx, k8sClient1, gcObj, gdpObj)
	Expect(k8sClient2.Delete(ctx, amkoCluster2)).Should(Succeed())
	deleteTestGCAndGDPObj(ctx, k8sClient2, gcObj, gdpObj)
}

func VerifySuccessForAllStatusFields(k8sClient client.Client) {
	VerifyTestAMKOClusterStatus(k8sClient, CurrentAMKOClusterValidationStatusField,
		StatusMsgValidAMKOCluster, "")
	VerifyTestAMKOClusterStatus(k8sClient, ClusterContextsStatusField,
		StatusMsgClusterClientsSuccess, "")
	VerifyTestAMKOClusterStatus(k8sClient, MemberValidationStatusField,
		StatusMembersValidationSuccess, "")
	VerifyTestAMKOClusterStatus(k8sClient, GSLBConfigFederationStatusField,
		StatusGSLBConfigFederationSuccess, "")
	VerifyTestAMKOClusterStatus(k8sClient, GDPFederationStatusField,
		StatusGDPFederationSuccess, "")
}
