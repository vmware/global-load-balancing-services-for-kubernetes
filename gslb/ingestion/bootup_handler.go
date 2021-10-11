/*
 * Copyright 2021 VMware, Inc.
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
	"context"
	"fmt"
	"os"

	amkov1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/api/v1alpha1"
	federator "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/controllers"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/apiserver"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const metricsAddr = ":9090"

var clusterScheme *k8sruntime.Scheme

func InitializeClusterClient(cfg *restclient.Config) (client.Client, error) {
	clusterScheme = k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(clusterScheme))
	utilruntime.Must(amkov1alpha1.AddToScheme(clusterScheme))

	c, err := client.New(cfg, client.Options{
		Scheme: clusterScheme,
	})
	if err != nil {
		return nil, fmt.Errorf("error in getting client: %v", err)
	}

	return c, nil
}

type AMKOClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func CreateController() {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		MetricsBindAddress: metricsAddr,
		Scheme:             clusterScheme,
	})
	if err != nil {
		gslbutils.Errf("unable to create manager for AMKOCluster controller: %v", err)
		os.Exit(1)
	}

	if err = (&AMKOClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		gslbutils.Errf("unable to create reconciler for AMKOCluster controller: %v", err)
		os.Exit(1)
	}
	go func() {
		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			gslbutils.Errf("unable to start AMKOCluster controller manager: %v", err)
			os.Exit(1)
		}
	}()
}

var currentLeader bool

func (r *AMKOClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	gslbutils.Debugf("Starting AMKOCluster reconciliation")

	var amkoClusterList amkov1alpha1.AMKOClusterList
	err := r.List(ctx, &amkoClusterList, &client.ListOptions{
		Namespace: federator.AviSystemNS,
	})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("AMKOClusterObjects can't be listed, err: %v", err)
	}
	if len(amkoClusterList.Items) != 1 {
		return reconcile.Result{}, fmt.Errorf("only one AMKOClusterObject allowed per cluster")
	}

	amkoCluster := amkoClusterList.Items[0]
	if currentLeader != amkoCluster.Spec.IsLeader {
		gslbutils.Warnf("AMKO leader flag changed, API server would be shut down")
		gslbutils.AMKOControlConfig().PodEventf(corev1.EventTypeWarning, gslbutils.AMKOShutdown, "AMKO leader flag changed.")
		apiserver.GetAmkoAPIServer().ShutDown()
		return ctrl.Result{}, nil
	}

	currentLeader = amkoCluster.Spec.IsLeader
	if !currentLeader {
		gslbutils.Warnf("AMKO is not a leader, will return")
		return ctrl.Result{}, nil
	}

	// verify the basic sanity of the AMKOCluster object
	if err := federator.VerifyAMKOClusterSanity(&amkoCluster); err != nil {
		gslbutils.Errf("validation of AMKOCluster object failed with error: %v", err)
		return ctrl.Result{}, err
	}

	memberClusters, errClusters, err := federator.FetchMemberClusterContexts(ctx, amkoCluster.DeepCopy())
	if err != nil {
		gslbutils.Warnf("Error in fetching member cluster contexts: %v", err)
		return reconcile.Result{}, err
	}
	if len(errClusters) != 0 {
		gslbutils.Warnf("Error in fetching some member cluster contexts: %v, will ignore these",
			errClusters)
	}
	gslbutils.Debugf("memberClusters obtained during reconciliation: %v", memberClusters)

	_, errClusters, err = federator.ValidateMemberClusters(ctx, memberClusters, amkoCluster.Spec.Version)
	if err != nil {
		gslbutils.Logf("ns: %s, AMKOCluster: %s, msg: validation error: %v, shutting down AMKO",
			amkoCluster.Namespace, amkoCluster.Name, err)
		gslbutils.AMKOControlConfig().PodEventf(corev1.EventTypeWarning, gslbutils.AMKOShutdown, "AMKOCluster %s/%s, Validation error: %s",
			amkoCluster.Namespace, amkoCluster.Name, err.Error())
		apiserver.GetAmkoAPIServer().ShutDown()
		return reconcile.Result{}, fmt.Errorf("error in validating the member clusters: %v", err)
	}
	if len(errClusters) != 0 {
		gslbutils.Warnf("some member clusters are invalid: %v, will ignore these", errClusters)
	}

	gslbutils.Debugf("AMKOCluster validation done")
	return ctrl.Result{}, nil
}

func (r *AMKOClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Watches(&source.Kind{Type: &amkov1alpha1.AMKOCluster{}},
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      o.GetName(),
							Namespace: o.GetNamespace(),
						},
					},
				}
			}),
		).
		For(&amkov1alpha1.AMKOCluster{}).
		WithEventFilter(federator.AcceptGenerationChangePredicate()).
		Complete(r)
}

func HandleBootup(cfg *restclient.Config) (bool, error) {
	clusterClient, err := InitializeClusterClient(cfg)
	if err != nil {
		return false, fmt.Errorf("error in initializing amkocluster client: %v", err)
	}
	var amkoClusterList amkov1alpha1.AMKOClusterList
	err = clusterClient.List(context.TODO(), &amkoClusterList, &client.ListOptions{
		Namespace: federator.AviSystemNS,
	})
	if err != nil {
		return false, fmt.Errorf("error in listing amkocluster objects: %v", err)
	}

	if len(amkoClusterList.Items) == 0 {
		gslbutils.Logf("No AMKOCluster object found, AMKO would start as leader")
		return true, nil
	}
	if len(amkoClusterList.Items) > 1 {
		return false, fmt.Errorf("only one AMKOClusterObject allowed per cluster")
	}

	amkoCluster := amkoClusterList.Items[0]
	if amkoCluster.Spec.IsLeader {
		currentLeader = true
		gslbutils.Logf("AMKOCluster object found and AMKO would start as leader")
	} else {
		gslbutils.Logf("AMKOCluster object found and AMKO would start as follower")
		return false, nil
	}

	// verify the basic sanity of the AMKOCluster object
	if err := federator.VerifyAMKOClusterSanity(&amkoCluster); err != nil {
		return false, err
	}

	memberClusters, errClusters, err := federator.FetchMemberClusterContexts(context.TODO(), amkoCluster.DeepCopy())
	if err != nil {
		return false, fmt.Errorf("unrecoverable error, error in fetching member cluster contexts: %v", err)
	}
	if len(errClusters) != 0 {
		gslbutils.Warnf("some member cluster contexts couldn't be fetched: %s, will ignore these", federator.GetClusterErrMsg(errClusters))
	}
	gslbutils.Logf("memberClusters list found from amkoCluster object: %v", memberClusters)

	_, errClusters, err = federator.ValidateMemberClusters(context.TODO(), memberClusters, amkoCluster.Spec.Version)
	if err != nil {
		return false, fmt.Errorf("error in validating the member clusters: %v", err)
	}
	if len(errClusters) != 0 {
		gslbutils.Warnf("some member clusters are invalid: %v, will ignore these", federator.GetClusterErrMsg(errClusters))
	}
	return currentLeader, nil
}
