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
	"errors"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

	ctrlResultRequeue := ctrl.Result{
		RequeueAfter: time.Second * 10,
	}
	ctrlResultNoRequeue := ctrl.Result{
		Requeue: false,
	}

	// check how many amkocluster objects are present, only 1 allowed per cluster
	var amkoClusterList amkov1alpha1.AMKOClusterList
	err := r.List(ctx, &amkoClusterList)
	if err != nil {
		return ctrlResultNoRequeue, fmt.Errorf("AMKOClusterObjects can't be listed, err: %v", err)
	}
	if len(amkoClusterList.Items) > 1 {
		return ctrlResultNoRequeue, fmt.Errorf("only one AMKOClusterObject allowed per cluster")
	}

	if len(amkoClusterList.Items) == 0 {
		log.Log.Info("No AMKOCluster object available on this cluster, nothing to do")
		return ctrlResultNoRequeue, nil
	}

	amkoCluster := amkoClusterList.Items[0]
	updatedAMKOCluster := amkoCluster.DeepCopy()
	// empty out the status
	updatedAMKOCluster.Status.Conditions = []amkov1alpha1.AMKOClusterCondition{}

	defer r.UpdateStatus(updatedAMKOCluster)

	// the Reconcile function can be called for 3 objects: AMKOCluster, GC and GDP objects
	// we have to determine what kind of an object this function is getting called for.
	if IsObjAMKOClusterType(ctx, req.Name) {
		if err != nil && k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		} else if err != nil {
			// don't requeue, it will get called when AMKOCluster is fixed
			return ctrl.Result{}, err
		}
	}

	// check if this AMKO is the leader
	if !amkoCluster.Spec.IsLeader {
		log.Log.Info("AMKO is not a leader, will return")
		if statusErr := r.UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
			StatusMsgNotALeader, "AMKO not a leader", nil, updatedAMKOCluster); statusErr != nil {
			return ctrlResultRequeue, statusErr
		}
		// don't requeue if not a leader
		return ctrlResultNoRequeue, nil
	}

	// verify the basic sanity of the AMKOCluster object
	if err := r.ValidateAMKOClusterSanityAndUpdateStatus(ctx, updatedAMKOCluster); err != nil {
		// don't requeue, since Reconcile will get called anyway once the error is fixed
		// in the AMKOCluster object
		return ctrlResultNoRequeue, err
	}

	// fetch the member clusters' contexts and update status
	memberClusters, err := r.FetchMemberClusterContextsAndUpdateStatus(ctx, updatedAMKOCluster)
	if err != nil {
		return ctrlResultRequeue, err
	}

	// validate member clusters and update status
	validClusters, err := r.ValidateMemberClustersAndUpdateStatus(ctx, memberClusters, updatedAMKOCluster)
	if err != nil {
		return ctrlResultRequeue, err
	}

	// Federate the GSLBConfig object on all member clusters
	if err := r.FederateGSLBConfigAndUpdateStatus(ctx, validClusters, updatedAMKOCluster); err != nil {
		return ctrlResultRequeue, err
	}

	// Federate the GDP object on all member clusters
	if err := r.FederateGDPAndUpdateStatus(ctx, validClusters, updatedAMKOCluster); err != nil {
		return ctrlResultRequeue, err
	}

	return ctrl.Result{}, nil
}

func (r *AMKOClusterReconciler) FetchMemberClusterContextsAndUpdateStatus(ctx context.Context, amkoCluster *amkov1alpha1.AMKOCluster) ([]KubeContextDetails, error) {
	memberClusters, errClusters, err := FetchMemberClusterContexts(ctx, amkoCluster.DeepCopy())

	if err != nil {
		// update the error message in the AMKOCluster status field
		if statusErr := r.UpdateAMKOClusterStatus(ctx, ClusterContextsStatusType, "", err.Error(),
			errClusters, amkoCluster); statusErr != nil {
			return nil, statusErr
		}
		// errors on which the execution should be stopped here and retried:
		// - bad kubeconfig
		// - error in generating a temporary kubeconfig file
		return nil, fmt.Errorf("error in fetching member cluster contexts: %v", err)
	} else {
		if statusErr := r.UpdateAMKOClusterStatus(ctx, ClusterContextsStatusType, "", "",
			errClusters, amkoCluster); statusErr != nil {
			return nil, statusErr
		}
	}

	// if memberClusters list is empty, clear the rest of the status fields and return
	if len(memberClusters) == 0 {
		return nil, fmt.Errorf("no valid cluster contexts found")
	}

	return memberClusters, nil
}

func (r *AMKOClusterReconciler) ValidateAMKOClusterSanityAndUpdateStatus(ctx context.Context,
	amkoCluster *amkov1alpha1.AMKOCluster) error {

	err := r.VerifyAMKOClusterSanity(amkoCluster)
	if err != nil {
		if statusErr := r.UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
			StatusMsgInvalidAMKOCluster, err.Error(), nil, amkoCluster); statusErr != nil {
			return statusErr
		}
		return err
	}
	return r.UpdateAMKOClusterStatus(ctx, CurrentAMKOClusterValidationStatusType,
		"", "", nil, amkoCluster)
}

func (r *AMKOClusterReconciler) ValidateMemberClustersAndUpdateStatus(ctx context.Context, memberClusters []KubeContextDetails,
	amkoCluster *amkov1alpha1.AMKOCluster) ([]KubeContextDetails, error) {

	validClusters, errClusters, err := ValidateMemberClusters(ctx, memberClusters, amkoCluster.Spec.Version)

	if err != nil {
		if statusErr := r.UpdateAMKOClusterStatus(ctx, MemberValidationStatusType, "",
			err.Error(), errClusters, amkoCluster); statusErr != nil {
			return nil, statusErr
		}
		// errors on which the execution should be stopped here and retried:
		// - another cluster has a leader AMKO instance
		return nil, fmt.Errorf("error in validating the member clusters: %v", err)
	} else {
		if statusErr := r.UpdateAMKOClusterStatus(ctx, MemberValidationStatusType, "",
			"", errClusters, amkoCluster); statusErr != nil {
			return nil, statusErr
		}
	}

	// if no valid clusters left, no point continuing
	if len(validClusters) == 0 {
		log.Log.Info("no valid clusters to federate on")
		return nil, fmt.Errorf("no valid clusters left to federate on")
	}

	return validClusters, nil
}

func (r *AMKOClusterReconciler) FederateGSLBConfigAndUpdateStatus(ctx context.Context, validClusters []KubeContextDetails,
	amkoCluster *amkov1alpha1.AMKOCluster) error {
	errClusters, err := r.FederateGSLBConfig(ctx, validClusters)
	if statusErr := r.UpdateAMKOClusterStatus(ctx, GSLBConfigFederationStatusType, "",
		getErrorMsg(err), errClusters, amkoCluster); statusErr != nil {
		return statusErr
	}
	if err != nil {
		// errors on which the execution will stop here and will be retried (indicating that
		// the current cluster is in a bad shape):
		// - CRD for GSLBConfig is absent in the current cluster
		// - more than one GSLBConfig objects in the current cluster
		return fmt.Errorf("error in federating GSLBConfig object: %v", err)
	}
	return nil
}

func (r *AMKOClusterReconciler) FederateGDPAndUpdateStatus(ctx context.Context, validClusters []KubeContextDetails,
	amkoCluster *amkov1alpha1.AMKOCluster) error {
	// Federate the GDP object on all member clusters
	errClusters, err := r.FederateGDP(ctx, validClusters)
	if statusErr := r.UpdateAMKOClusterStatus(ctx, GDPFederationStatusType, "",
		getErrorMsg(err), errClusters, amkoCluster); statusErr != nil {
		return statusErr
	}
	if err != nil {
		// errors on which the execution will stop here and will be retried:
		// - CRD for GDP is absent in the current cluster
		// - more than one GDPs in the current cluster
		return fmt.Errorf("error in federating GDP object: %v", err)
	}
	return nil
}

func (r *AMKOClusterReconciler) FederateGSLBConfig(ctx context.Context, memberClusters []KubeContextDetails) ([]ClusterErrorMsg, error) {
	// Determine the state that we need to federate across all member clusters
	var currGCList gslbalphav1.GSLBConfigList
	err := r.List(ctx, &currGCList, &client.ListOptions{
		Namespace: AviSystemNS,
	})
	if err != nil {
		// current cluster's state is not right, need to stop here
		return nil, fmt.Errorf("cannot list GSLBConfig list on current cluster in %s namespace: %v",
			AviSystemNS, err)
	}

	if len(currGCList.Items) > 1 {
		// current cluster is in a state of error, have to stop here
		return nil, fmt.Errorf("more than one GSLBConfig objects are present in the current cluster")
	}

	// if no GC objects exist, we need to sync all member clusters to the same state
	// which would mean, we need to delete GCs on all member clusters (if any)
	if len(currGCList.Items) == 0 {
		return DeleteObjsOnAllMemberClusters(ctx, memberClusters, AviSystemNS, &gslbalphav1.GSLBConfig{}), nil
	}

	// if a GC object exists in the current cluster, we need to make sure that all the member
	// clusters have only this GC object in the avi-system namespace.
	return FederateGCObjectOnMemberClusters(ctx, memberClusters, currGCList.Items[0].DeepCopy()), nil
}

func (r *AMKOClusterReconciler) FederateGDP(ctx context.Context, memberClusters []KubeContextDetails) ([]ClusterErrorMsg, error) {
	// Determine the state that we need to federate across all member clusters
	var currGDPList gdpalphav2.GlobalDeploymentPolicyList
	err := r.List(ctx, &currGDPList, &client.ListOptions{
		Namespace: AviSystemNS,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list GlobalDeploymentPolicy list on current cluster in %s namespace: %v",
			AviSystemNS, err)
	}

	if len(currGDPList.Items) > 1 {
		return nil, fmt.Errorf("more than one GlobalDeploymentPolicies are present in the current cluster")
	}

	// if no GDP objects exist, we need to sync all member clusters to the same state
	// which would mean, we need to delete GDPs on all member clusters (if any)
	if len(currGDPList.Items) == 0 {
		return DeleteObjsOnAllMemberClusters(ctx, memberClusters, AviSystemNS, &gdpalphav2.GlobalDeploymentPolicy{}), nil
	}
	// if a GDP object exists in the current cluster, we need to make sure that all the member
	// clusters have only this GDP object in the avi-system namespace.
	return FederateGDPObjectOnMemberClusters(ctx, memberClusters, currGDPList.Items[0].DeepCopy()), nil
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

func (r *AMKOClusterReconciler) FetchMemberClusterContexts(ctx context.Context, amkoCluster *amkov1alpha1.AMKOCluster) ([]KubeContextDetails, []ClusterErrorMsg, error) {
	err := generateTempKubeConfig()
	if err != nil {
		return nil, nil, err
	}
	memberClusters, errClusters, err := InitMemberClusterContexts(ctx, amkoCluster.Spec.ClusterContext, amkoCluster.Spec.Clusters)
	if err != nil {
		return nil, nil, fmt.Errorf("error in initialising member cluster contexts: %v", err)
	}

	return memberClusters, errClusters, nil
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

func (r *AMKOClusterReconciler) UpdateStatus(updatedAMKOCluster *amkov1alpha1.AMKOCluster) {
	currAMKOClusterList := amkov1alpha1.AMKOClusterList{}
	if err := r.List(context.TODO(), &currAMKOClusterList, &client.ListOptions{
		Namespace: AviSystemNS,
	}); err != nil {
		log.Log.Error(err, "unable to get AMKOCluster list on avi-system namespace")
		return
	}

	if len(currAMKOClusterList.Items) != 1 {
		log.Log.Error(errors.New("no AMKOCluster to be updated"), "unable to get AMKOCluster list on avi-system namespace")
		return
	}

	// currAMKOClusterList.Items[0].Status.Conditions = []amkov1alpha1.AMKOClusterCondition{}
	log.Log.Info("updated AMKO Cluster status", "status", updatedAMKOCluster.Status.Conditions)
	if err := r.PatchAMKOClusterStatus(context.TODO(), &currAMKOClusterList.Items[0],
		updatedAMKOCluster); err != nil {
		log.Log.Error(err, "error while patching AMKOCluster status")
	}
}

func (r *AMKOClusterReconciler) UpdateAMKOClusterStatus(ctx context.Context, statusType int,
	statusMsg, reason string, errClusters []ClusterErrorMsg,
	updatedAMKOCluster *amkov1alpha1.AMKOCluster) error {

	condition, err := getStatusCondition(statusType, statusMsg, reason, errClusters)
	if err != nil {
		log.Log.Error(err, "error while generating status condition")
		return err
	}
	log.Log.Info("status condition", "condition", condition)

	// get the previous status
	conditions := updatedAMKOCluster.Status.Conditions
	if len(conditions) == 0 {
		// initialise a new set
		updatedAMKOCluster.Status.Conditions = []amkov1alpha1.AMKOClusterCondition{
			condition,
		}
		return nil
	}

	// conditions already present, update the one that we need for statusType
	for idx, c := range conditions {
		if c.Type == condition.Type {
			updatedAMKOCluster.Status.Conditions[idx].Type = condition.Type
			updatedAMKOCluster.Status.Conditions[idx].Status = condition.Status
			updatedAMKOCluster.Status.Conditions[idx].Reason = condition.Reason
			return nil
		}
	}

	// no such condition with status type, add a new one
	updatedAMKOCluster.Status.Conditions = append(updatedAMKOCluster.Status.Conditions, condition)

	tmpObj := amkov1alpha1.AMKOCluster{}
	r.Get(ctx, types.NamespacedName{Name: tmpObj.Name, Namespace: tmpObj.Namespace}, &tmpObj)
	return nil
}

func (r *AMKOClusterReconciler) PatchAMKOClusterStatus(ctx context.Context, amkoCluster, updatedAMKOCluster *amkov1alpha1.AMKOCluster) error {
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
		WithEventFilter(AcceptGenerationChangePredicate()).
		Complete(r)
}
