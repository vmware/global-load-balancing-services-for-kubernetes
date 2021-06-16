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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	amkov1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/federator/api/v1alpha1"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	gdpalphav2 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha2"
)

// AMKOClusterReconciler reconciles a AMKOCluster object
type AMKOClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=amko.vmware.com,resources=amkoclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=amko.vmware.com,resources=amkoclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=amko.vmware.com,resources=amkoclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=amko.vmware.com,resources=gslbconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=amko.vmware.com,resources=globaldeploymentpolicies,verbs=get;list;watch;create;update;patch;delete

func (r *AMKOClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// check how many amkocluster objects are present, only 1 allowed per cluster
	var amkoClusterList amkov1alpha1.AMKOClusterList
	err := r.List(ctx, &amkoClusterList)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("AMKOClusterObjects can't be listed, err: %v", err)
	}
	if len(amkoClusterList.Items) > 1 {
		return reconcile.Result{}, fmt.Errorf("only one AMKOClusterObject allowed per cluster")
	}

	// the Reconcile function can be called for 3 objects: AMKOCluster, GC and GDP objects
	// we have to determine what kind of an object this function is getting called for.
	// var amkoClusterName, amkoClusterNS string
	// amkoClusterPresent := false
	var amkoCluster amkov1alpha1.AMKOCluster
	if len(amkoClusterList.Items) == 1 {
		amkoCluster = amkoClusterList.Items[0]
		// update the status of AMKOCluster
		statusErr := r.UpdateAMKOClusterStatus(ctx, FederationTypeStatus, StatusMsgFederating, "", &amkoCluster)
		if statusErr != nil {
			return ctrl.Result{
				RequeueAfter: time.Second * 5,
			}, statusErr
		}
	}

	if IsObjAMKOClusterType(ctx, req.Name) {
		if err != nil && errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		} else if err != nil {
			return ctrl.Result{}, err
		}
	}

	// check if this AMKO is the leader
	if !amkoCluster.Spec.IsLeader {
		log.Log.Info("AMKO is not a leader, will return")
		statusErr := r.UpdateAMKOClusterStatus(ctx, FederationTypeStatus, StatusMsgNotALeader, "AMKO not a leader", &amkoCluster)
		if statusErr != nil {
			return ctrl.Result{
				RequeueAfter: time.Second * 5,
			}, statusErr
		}
		return ctrl.Result{}, nil
	}

	// verify the basic sanity of the AMKOCluster object
	err = r.VerifyAMKOClusterSanity(&amkoCluster)
	if err != nil {
		statusErr := r.UpdateAMKOClusterStatus(ctx, FederationTypeStatus, StatusMsgFederationFailure, err.Error(), &amkoCluster)
		if statusErr != nil {
			return ctrl.Result{
				RequeueAfter: time.Second * 5,
			}, statusErr
		}
		return reconcile.Result{}, err
	}

	// fetch the member clusters' contexts
	memberClusters, err := FetchMemberClusterContexts(ctx, amkoCluster.DeepCopy())
	if err != nil {
		statusErr := r.UpdateAMKOClusterStatus(ctx, FederationTypeStatus, StatusMsgFederationFailure, err.Error(), &amkoCluster)
		if statusErr != nil {
			return ctrl.Result{
				RequeueAfter: time.Second * 5,
			}, statusErr
		}
		return reconcile.Result{}, fmt.Errorf("error in fetcing member cluster contexts: %v", err)
	}

	// validate member clusters
	err = ValidateMemberClusters(ctx, memberClusters)
	if err != nil {
		statusErr := r.UpdateAMKOClusterStatus(ctx, FederationTypeStatus, StatusMsgFederationFailure, err.Error(), &amkoCluster)
		if statusErr != nil {
			return ctrl.Result{
				RequeueAfter: time.Second * 5,
			}, statusErr
		}
		return reconcile.Result{}, fmt.Errorf("error in validating the member clusters: %v", err)
	}

	// Get the object list to be federated
	objList, err := r.GetObjectsToBeFederated(ctx)
	if err != nil {
		statusErr := r.UpdateAMKOClusterStatus(ctx, FederationTypeStatus, StatusMsgFederationFailure, err.Error(), &amkoCluster)
		if statusErr != nil {
			return ctrl.Result{
				RequeueAfter: time.Second * 5,
			}, statusErr
		}
		return reconcile.Result{}, fmt.Errorf("error in getting the required objects: %v", err)
	}

	// federated the object list in objList across all member clusters
	err = FederateObjects(ctx, memberClusters, objList)
	if err != nil {
		statusErr := r.UpdateAMKOClusterStatus(ctx, FederationTypeStatus, StatusMsgFederationFailure, err.Error(), &amkoCluster)
		if statusErr != nil {
			return ctrl.Result{
				RequeueAfter: time.Second * 5,
			}, statusErr
		}
		return reconcile.Result{}, fmt.Errorf("error in federating objects: %v", err)
	}

	// update the status of AMKOCluster
	err = r.UpdateAMKOClusterStatus(ctx, FederationTypeStatus, StatusMsgFederationSuccess, "", &amkoCluster)
	if err != nil {
		return ctrl.Result{
			RequeueAfter: time.Second * 5,
		}, err
	}

	return ctrl.Result{}, nil
}

func (r *AMKOClusterReconciler) GetObjectsToBeFederated(ctx context.Context) ([]client.Object, error) {
	// - List all gslb config objects (has to be only 1)
	// - List all GDP objects (has to be only 1)
	// - append them to a client.Object list
	// - return this list

	objList := []client.Object{}

	var gcList gslbalphav1.GSLBConfigList
	err := r.List(ctx, &gcList, &client.ListOptions{
		Namespace: AviSystemNS,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list GSLBConfig list on current cluster in avi-system namespace: %v", err)
	}
	if len(gcList.Items) > 1 {
		return nil, fmt.Errorf("more than one GSLBConfig objects are present in current cluster")
	}

	gcObj := gcList.Items[0].DeepCopy()
	objList = append(objList, gcObj)

	var gdpList gdpalphav2.GlobalDeploymentPolicyList
	err = r.List(ctx, &gdpList, &client.ListOptions{
		Namespace: AviSystemNS,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list GDP list on current cluster in avi-system namespace: %v", err)
	}

	if len(gdpList.Items) > 1 {
		return nil, fmt.Errorf("more than one GDP objects are present in current cluster")
	}
	gdpObj := gdpList.Items[0].DeepCopy()
	objList = append(objList, gdpObj)

	return objList, nil
}

func (r *AMKOClusterReconciler) VerifyAMKOClusterSanity(amkoCluster *amkov1alpha1.AMKOCluster) error {
	log.Log.V(1).Info("Performing sanity checks on AMKOCluster object")
	// namespace for AMKOCluster object has to be avi-system
	if amkoCluster.Namespace != AviSystemNS {
		return fmt.Errorf("AMKOCluster's namespace is not %s", AviSystemNS)
	}
	// check the current context
	if amkoCluster.Spec.ClusterContext == "" {
		return fmt.Errorf("clusterContext field can't be empty in AMKOCluster object")
	}
	// check the version field
	if amkoCluster.Spec.Version == "" {
		return fmt.Errorf("version field can't be empty in AMKOCluster object")
	}

	return nil
}

func (r *AMKOClusterReconciler) UpdateAMKOClusterStatus(ctx context.Context, statusType, statusMsg, reason string, amkoCluster *amkov1alpha1.AMKOCluster) error {
	updatedAMKOCluster := amkoCluster.DeepCopy()
	log.Log.Info("updating status", "status type", statusType, "status msg", statusMsg, "reason", reason)
	// get the previous status
	conditions := amkoCluster.Status.Conditions
	if len(conditions) == 0 {
		// initialize a new set
		updatedAMKOCluster.Status.Conditions = []amkov1alpha1.AMKOClusterCondition{
			getStatusCondition(statusType, statusMsg, reason),
		}
		return r.PatchAMKOClusterStatus(ctx, amkoCluster, updatedAMKOCluster)
	}

	// conditions already present, update the one that we need for statusType
	for idx, c := range conditions {
		if c.Type == statusType {
			log.Log.Info("found the type, will update this status")
			updatedAMKOCluster.Status.Conditions[idx].Status = statusMsg
			updatedAMKOCluster.Status.Conditions[idx].Reason = reason
			return r.PatchAMKOClusterStatus(ctx, amkoCluster, updatedAMKOCluster)
		}
	}

	// no such condition with status type, add a new one
	amkoCluster.Status.Conditions = append(updatedAMKOCluster.Status.Conditions,
		getStatusCondition(statusType, statusMsg, reason))
	return r.PatchAMKOClusterStatus(ctx, amkoCluster, updatedAMKOCluster)
}

func (r *AMKOClusterReconciler) PatchAMKOClusterStatus(ctx context.Context, amkoCluster, updatedAMKOCluster *amkov1alpha1.AMKOCluster) error {
	amkoCluster.Status = amkov1alpha1.AMKOClusterStatus{}
	patch := client.MergeFrom(amkoCluster.DeepCopy())
	err := r.Status().Patch(ctx, updatedAMKOCluster, patch)
	if err != nil {
		return err
	}
	log.Log.V(1).Info("updated status of AMKOCluster")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AMKOClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Watches(&source.Kind{Type: &gslbalphav1.GSLBConfig{}},
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      o.GetName() + GCSuffix,
							Namespace: o.GetNamespace(),
						},
					},
				}
			}),
		).
		Watches(&source.Kind{Type: &gdpalphav2.GlobalDeploymentPolicy{}},
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      o.GetName() + GDPSuffix,
							Namespace: o.GetNamespace(),
						},
					},
				}
			}),
		).
		For(&amkov1alpha1.AMKOCluster{}).
		WithEventFilter(acceptGenerationChangePredicate()).
		Complete(r)
}
