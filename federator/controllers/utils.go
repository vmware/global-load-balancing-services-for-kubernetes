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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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

func FederateObjects(ctx context.Context, memberClusters []KubeContextDetails, objList []client.Object) error {
	for _, m := range memberClusters {
		for _, obj := range objList {
			clusterClient := *m.client
			// decide whether to create or update
			var gcObj gslbalphav1.GSLBConfig
			var gdpObj gdpalphav2.GlobalDeploymentPolicy
			var err error

			// identify the type of object and put in the relevant variable
			if obj.GetObjectKind().GroupVersionKind() == gcGVK {
				err = clusterClient.Get(ctx, types.NamespacedName{
					Namespace: obj.GetNamespace(),
					Name:      obj.GetName(),
				}, &gcObj)
			} else if obj.GetObjectKind().GroupVersionKind() == gdpGVK {
				err = clusterClient.Get(ctx, types.NamespacedName{
					Namespace: obj.GetNamespace(),
					Name:      obj.GetName(),
				}, &gdpObj)
			} else {
				return fmt.Errorf("can't federate an unsupported object: %v", obj.GetObjectKind().GroupVersionKind())
			}

			// determine if create/update
			if errors.IsNotFound(err) {
				log.Log.Info("creating object on member cluster", "member cluster", m.clusterName)
				// create can't happen if resource version isn't set to ""
				obj.SetResourceVersion("")
				err := clusterClient.Create(ctx, obj)
				if err != nil {
					return fmt.Errorf("can't create %s object on cluster %s, obj name: %v",
						obj.GetObjectKind(), m.clusterName, err)
				}
			} else {
				log.Log.Info("updating object on member cluster", "member cluster", m.clusterName)
				if gcObj.Name != "" {
					newGCObj := obj.(*gslbalphav1.GSLBConfig)
					newGCObj.Spec.DeepCopyInto(&gcObj.Spec)
					err := clusterClient.Update(ctx, &gcObj, &client.UpdateOptions{
						FieldManager: "AMKO",
					})
					if err != nil {
						return fmt.Errorf("can't update GSLBConfig object on cluster %s, obj name: %v",
							m.clusterName, err)
					}
				} else if gdpObj.Name != "" {
					newGDPObj := obj.(*gdpalphav2.GlobalDeploymentPolicy)
					newGDPObj.Spec.DeepCopyInto(&gdpObj.Spec)
					err := clusterClient.Update(ctx, &gdpObj, &client.UpdateOptions{
						FieldManager: "AMKO",
					})
					if err != nil {
						return fmt.Errorf("can't update GDP object on cluster %s, obj name: %v",
							m.clusterName, err)
					}
				} else {
					return fmt.Errorf("can't update unknown object")
				}
			}
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

func getClusterContextDetails(membersKubeConfig string, memberClusters []string, skipContext string) []KubeContextDetails {
	var clusterDetails []KubeContextDetails

	for _, member := range memberClusters {
		if member == skipContext {
			continue
		}
		clusterDetails = append(clusterDetails, KubeContextDetails{
			clusterName: member,
			kubeconfig:  membersKubeConfig,
		})
	}
	return clusterDetails
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

func acceptGenerationChangePredicate() predicate.Predicate {
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
