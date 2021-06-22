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
	"strings"

	amkov1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/api/v1alpha1"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	FederationTypeStatus       = "Federation"
	AviSystemNS                = "avi-system"
	MembersKubePath            = "/tmp/members-kubeconfig"
	GCSuffix                   = "--amko.gslbconfig-"
	GDPSuffix                  = "--amko.gdp-"
	AMKOGroup                  = "amko.vmware.com"
	GCKind                     = "GSLBConfig"
	GDPKind                    = "GlobalDeploymentPolicy"
	GCVersion                  = "v1alpha1"
	GDPVersion                 = "v1alpha2"
	StatusMsgFederating        = "Federating objects"
	StatusMsgFederationFailure = "Failure in federating objects"
	StatusMsgFederationSuccess = "Federation successful"
	StatusMsgNotALeader        = "Won't federate"
	ErrInitClientContext       = "error in initializing member custer context"
)

var gcGVK schema.GroupVersionKind = schema.GroupVersionKind{
	Group:   AMKOGroup,
	Kind:    GCKind,
	Version: GCVersion,
}

var gdpGVK schema.GroupVersionKind = schema.GroupVersionKind{
	Group:   AMKOGroup,
	Kind:    GDPKind,
	Version: GDPVersion,
}

func IsObjAMKOClusterType(ctx context.Context, name string) bool {
	if strings.HasSuffix(name, GCSuffix) || strings.HasSuffix(name, GDPSuffix) {
		return false
	}
	return true
}

func DeleteObjsOnAllMemberClusters(ctx context.Context, memberClusters []KubeContextDetails, namespace string, obj client.Object) error {
	for _, m := range memberClusters {
		clusterClient := *m.client
		if err := clusterClient.DeleteAllOf(ctx, obj, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				Namespace: namespace,
			},
		}); err != nil {
			return fmt.Errorf("error in deleting all objects of kind %s in %s namespace and in %s cluster: %v",
				obj.GetObjectKind().GroupVersionKind().Kind, namespace, m.clusterName, err)
		}
	}
	return nil
}

func UpdateObjOnMemberCluster(ctx context.Context, c client.Client, source,
	target client.Object, cname string) error {

	sourceGVK := source.GetObjectKind().GroupVersionKind()
	targetGVK := target.GetObjectKind().GroupVersionKind()
	if sourceGVK != targetGVK {
		return fmt.Errorf("can't update object %s/%s, source and targets are different, source type: %v, target type: %v",
			source.GetNamespace(), source.GetName(), sourceGVK, targetGVK)
	}
	switch sourceGVK {
	case gcGVK:
		sourceGC := source.(*gslbalphav1.GSLBConfig)
		targetGC := target.(*gslbalphav1.GSLBConfig)
		sourceGC.Spec.DeepCopyInto(&targetGC.Spec)
	case gdpGVK:
		sourceGDP := source.(*gdpalphav2.GlobalDeploymentPolicy)
		targetGDP := target.(*gdpalphav2.GlobalDeploymentPolicy)
		sourceGDP.Spec.DeepCopyInto(&targetGDP.Spec)
	default:
		return fmt.Errorf("can't federate an unsupported object, object type: %v", sourceGVK)
	}

	if err := c.Update(ctx, target, &client.UpdateOptions{
		FieldManager: "AMKO",
	}); err != nil {
		return fmt.Errorf("can't update object %s/%s on cluster %s: %v", source.GetNamespace(),
			source.GetName(), cname, err)
	}

	return nil
}

func DeleteObjInMemberCluster(ctx context.Context, c client.Client, obj client.Object,
	cname string) error {
	if err := c.Delete(ctx, obj); err != nil {
		if k8serrors.IsNotFound(err) {
			// object is already removed, continue
			return nil
		}
		return fmt.Errorf("can't delete %s object %s/%s on cluster %s: %v",
			obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(),
			obj.GetName(), cname, err)
	}
	return nil
}

func FederateGCObjectOnMemberClusters(ctx context.Context, memberClusters []KubeContextDetails,
	currObj *gslbalphav1.GSLBConfig) error {

	namespace := currObj.Namespace

	for _, m := range memberClusters {
		clusterClient := *m.client
		objList := gslbalphav1.GSLBConfigList{}
		if err := clusterClient.List(ctx, &objList, &client.ListOptions{
			Namespace: namespace,
		}); err != nil {
			return fmt.Errorf("can't list GSLBConfigs in %s namespace for %s cluster: %v",
				namespace, m.clusterName, err)
		}

		// go through the list of GC objects in the namespace, if we find the relevant GC, just
		// update and return
		updated := false
		for _, remoteObj := range objList.Items {
			if remoteObj.Name == currObj.Name {
				if err := UpdateObjOnMemberCluster(ctx, clusterClient, currObj, remoteObj.DeepCopy(),
					m.clusterName); err != nil {
					return err
				}
				updated = true
			} else {
				// remove all other GC objects in this namespace
				if err := DeleteObjInMemberCluster(ctx, clusterClient, remoteObj.DeepCopy(),
					m.clusterName); err != nil {
					return err
				}
			}
		}

		if updated {
			// update is already done, return
			return nil
		}

		// reaching here would mean, we need to create the GSLBConfig object
		newObj := currObj.DeepCopy()
		newObj.ResourceVersion = ""
		if err := clusterClient.Create(ctx, newObj); err != nil {
			return fmt.Errorf("error in creating GSLBConfig %s/%s on cluster %s: %v",
				newObj.Namespace, newObj.Name, m.clusterName, err)
		}
	}

	return nil
}

func FederateGDPObjectOnMemberClusters(ctx context.Context, memberClusters []KubeContextDetails,
	currObj *gdpalphav2.GlobalDeploymentPolicy) error {

	namespace := currObj.Namespace
	for _, m := range memberClusters {
		clusterClient := *m.client
		objList := gdpalphav2.GlobalDeploymentPolicyList{}
		if err := clusterClient.List(ctx, &objList, &client.ListOptions{
			Namespace: namespace,
		}); err != nil {
			return fmt.Errorf("can't list GlobalDeploymentPolicies in %s namespace for %s cluster: %v",
				namespace, m.clusterName, err)
		}

		// go through the list of GDP objects in the namespace, if we find the relevant GDP, just
		// update and return
		updated := false
		for _, remoteObj := range objList.Items {
			if remoteObj.Name == currObj.Name {
				if err := UpdateObjOnMemberCluster(ctx, clusterClient,
					currObj, remoteObj.DeepCopy(), m.clusterName); err != nil {
					return err
				}
				updated = true
			} else {
				// delete rest of the GDPs (for which names don't match with the current cluster's GDP)
				if err := DeleteObjInMemberCluster(ctx, clusterClient, remoteObj.DeepCopy(),
					m.clusterName); err != nil {
					return err
				}
			}
		}

		if updated {
			// update is already done, return
			return nil
		}

		// reaching here would mean, we need to create the GDP object
		newObj := currObj.DeepCopy()
		newObj.ResourceVersion = ""
		if err := clusterClient.Create(ctx, newObj); err != nil {
			return fmt.Errorf("error in creating GSLBConfig object %s/%s on cluster %s: %v",
				newObj.Namespace, newObj.Name, m.clusterName, err)
		}
	}

	return nil
}

func InitializeMemberClusterClient(cfg *restclient.Config) (client.Client, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(amkov1alpha1.AddToScheme(scheme))
	utilruntime.Must(gslbalphav1.AddToScheme(scheme))
	utilruntime.Must(gdpalphav2.AddToScheme(scheme))

	c, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("error in getting client: %v", err)
	}

	return c, nil
}

// BuildContextConfig builds the kubernetes/openshift context config
func BuildContextConfig(kubeconfigPath, context string) (*restclient.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

type KubeContextDetails struct {
	clusterName string
	kubeconfig  string
	client      *client.Client
}

func getClusterContextDetails(membersKubeConfig string, memberClusters []string, skipContext string) ([]KubeContextDetails, error) {
	var clusterDetails []KubeContextDetails

	currentContextPresent := false
	for _, member := range memberClusters {
		if member == skipContext {
			currentContextPresent = true
			continue
		}
		clusterDetails = append(clusterDetails, KubeContextDetails{
			clusterName: member,
			kubeconfig:  membersKubeConfig,
		})
	}
	if !currentContextPresent {
		return nil, fmt.Errorf("current cluster context %s not part of member clusters", skipContext)
	}

	return clusterDetails, nil
}

func generateTempKubeConfig() error {
	membersKubeConfig := os.Getenv("GSLB_CONFIG")
	if membersKubeConfig == "" {
		return fmt.Errorf("error in fetching the GSLB_CONFIG env variable, this contains the members kube config")
	}
	f, err := os.Create(MembersKubePath)
	if err != nil {
		return fmt.Errorf("error in creating temporary members kubeconfig: %v", err)
	}

	_, err = f.WriteString(membersKubeConfig)
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("error in writing to a temporary file: %v", err)
	}
	return nil
}

func AcceptGenerationChangePredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			e.ObjectOld.GetGeneration()
			// skip if no generation change, applicable to all objects
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
	}
}

func getStatusCondition(statusType, statusMsg, reason string) amkov1alpha1.AMKOClusterCondition {
	return amkov1alpha1.AMKOClusterCondition{
		Type:   statusType,
		Status: statusMsg,
		Reason: reason,
	}
}

// TODO: Move functions used by both federator and main gslb to a common library
func ValidateMemberClusters(ctx context.Context, memberClusters []KubeContextDetails, currVersion string) error {
	// Perform validation checks
	// 1. Only one instance of AMKOCluster must be present in the avi-system namespace
	// 2. No other cluster should be leader if the current instance is leader
	// 3. No version mismatch
	for _, cluster := range memberClusters {
		if cluster.client == nil {
			log.Log.Info("client is nil", "cluster", cluster.clusterName)
			return fmt.Errorf("cluster client unavailable for cluster %s", cluster.clusterName)
		}
		clusterClient := *(cluster.client)
		var amkoCluster amkov1alpha1.AMKOClusterList
		err := clusterClient.List(ctx, &amkoCluster)
		if err != nil {
			return fmt.Errorf("error in getting AMKOCluster list for cluster %s: %v",
				cluster.clusterName, err)
		}

		if len(amkoCluster.Items) > 1 {
			return fmt.Errorf("more than one AMKOCluster objects present in cluster %s, can't federate",
				cluster.clusterName)
		}

		if len(amkoCluster.Items) == 0 {
			return fmt.Errorf("no AMKOCluster object present in cluster %s, can't federate", cluster.clusterName)
		}

		obj := amkoCluster.Items[0].DeepCopy()
		if obj.Namespace != AviSystemNS {
			return fmt.Errorf("AMKOCluster object not present in avi-system namespace in cluster %s, can't federate",
				cluster.clusterName)
		}

		if obj.Spec.IsLeader {
			return fmt.Errorf("AMKO in cluster %s is also a leader, conflicting state", cluster.clusterName)
		}

		if obj.Spec.Version != currVersion {
			return fmt.Errorf("version mismatch, current AMKO: %s, AMKO in cluster %s: %s", currVersion,
				cluster.clusterName, obj.Spec.Version)
		}
	}

	return nil
}

func FetchMemberClusterContexts(ctx context.Context, amkoCluster *amkov1alpha1.AMKOCluster) ([]KubeContextDetails, error) {
	err := generateTempKubeConfig()
	if err != nil {
		return nil, err
	}
	memberClusters, err := InitMemberClusterContexts(ctx, amkoCluster.Spec.ClusterContext, amkoCluster.Spec.Clusters)
	if err != nil {
		return nil, fmt.Errorf("error in initializing member cluster contexts: %v", err)
	}

	return memberClusters, nil
}

func InitMemberClusterContexts(ctx context.Context, currentContext string, clusterList []string) ([]KubeContextDetails, error) {
	// - obtain the list of all member cluster contexts from the kubeconfig
	// - remove the current context
	// - build the context config for the rest of them
	memberClusters, err := getClusterContextDetails(MembersKubePath, clusterList, currentContext)
	if err != nil {
		return nil, err
	}

	for idx, cluster := range memberClusters {
		log.Log.Info("member cluster", "cluster", cluster.clusterName)
		if currentContext == cluster.clusterName {
			// skip for current context
			continue
		}
		log.Log.Info("initializing cluster context", "cluster", cluster.clusterName)
		cfg, err := BuildContextConfig(cluster.kubeconfig, cluster.clusterName)
		if err != nil {
			return nil, fmt.Errorf("error in building context config for kubernetes cluster %s: %v",
				cluster.clusterName, err)
		}
		client, err := InitializeMemberClusterClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %v", ErrInitClientContext, cluster.clusterName, err)
		}
		memberClusters[idx].client = &client
	}
	return memberClusters, nil
}
