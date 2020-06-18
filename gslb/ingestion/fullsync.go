package ingestion

import (
	filter "amko/gslb/gdp_filter"
	"amko/gslb/gslbutils"
	"amko/gslb/k8sobjects"
	"amko/gslb/nodes"
	gslbalphav1 "amko/pkg/apis/amko/v1alpha1"
	"errors"

	"github.com/avinetworks/container-lib/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func fetchAndApplyAllIngresses(c *GSLBMemberController, nsList *corev1.NamespaceList) {
	var ingList []*v1beta1.Ingress

	acceptedIngStore := gslbutils.GetAcceptedIngressStore()
	rejectedIngStore := gslbutils.GetRejectedIngressStore()

	switch c.informers.IngressVersion {
	case utils.CoreV1IngressInformer:
		for _, namespace := range nsList.Items {
			objList, err := c.informers.ClientSet.NetworkingV1beta1().Ingresses(namespace.Name).List(metav1.ListOptions{})
			if err != nil {
				gslbutils.Errf("process: fullsync, namespace: %s, msg: error in fetching the ingress list, %s",
					namespace.Name, err.Error())
				continue
			}
			for _, obj := range objList.Items {
				ingObj, ok := utils.ToNetworkingIngress(&obj)
				if !ok {
					gslbutils.Errf("process: fullsync, namespace: %s, msg: unable to convert obj to ingress")
					continue
				}
				ingList = append(ingList, ingObj)
			}
		}
	case utils.ExtV1IngressInformer:
		for _, namespace := range nsList.Items {
			objList, err := c.informers.ClientSet.ExtensionsV1beta1().Ingresses(namespace.Name).List(metav1.ListOptions{})
			if err != nil {
				gslbutils.Errf("process: fullsync, namespace: %s, msg: error in fetching the ingress list, %s",
					namespace.Name, err.Error())
				continue
			}
			for _, obj := range objList.Items {
				ingObj, ok := utils.ToNetworkingIngress(&obj)
				if !ok {
					gslbutils.Errf("process: fullsync, namespace: %s, msg: error in fetching the ingress list, %s",
						namespace.Name, err.Error())
					continue
				}
				ingList = append(ingList, ingObj)
			}
		}
	}
	for _, ing := range ingList {
		ihms := k8sobjects.GetIngressHostMeta(ing, c.GetName())
		filterAndAddIngressMeta(ihms, c, acceptedIngStore, rejectedIngStore, 0, true)
	}
}

func fetchAndApplyAllServices(c *GSLBMemberController, nsList *corev1.NamespaceList) {
	acceptedLBSvcStore := gslbutils.GetAcceptedLBSvcStore()
	rejectedLBSvcStore := gslbutils.GetRejectedLBSvcStore()

	for _, namespace := range nsList.Items {
		svcList, err := c.informers.ClientSet.CoreV1().Services(namespace.Name).List(metav1.ListOptions{})
		if err != nil {
			gslbutils.Errf("process: fullsync, namespace: %s, msg: error in fetching the service list, %s",
				namespace.Name, err.Error())
			continue
		}
		for _, svc := range svcList.Items {
			if !isSvcTypeLB(&svc) {
				continue
			}
			svcMeta, ok := k8sobjects.GetSvcMeta(&svc, c.GetName())
			if !ok {
				gslbutils.Logf("cluster: %s, namespace: %s, svc: %s, msg: couldn't get meta object for service",
					c.GetName(), namespace.Name, svc.Name, err.Error())
				continue
			}
			if !filter.ApplyFilter(svcMeta, c.GetName()) {
				AddOrUpdateLBSvcStore(rejectedLBSvcStore, &svc, c.GetName())
				gslbutils.Logf("cluster: %s, ns: %s, svc: %s, msg: %s", c.GetName(), namespace.Name,
					svc.Name, "rejected ADD svc key because it couldn't pass through the filter")
				continue
			}
			AddOrUpdateLBSvcStore(acceptedLBSvcStore, &svc, c.GetName())
		}
	}

}

func checkGDPsAndInitialize() error {
	gdpList, err := gslbutils.GlobalGslbClient.AmkoV1alpha1().GlobalDeploymentPolicies(gslbutils.AVISystem).List(metav1.ListOptions{})
	if err != nil {
		return nil
	}

	// if no GDP objects, then simply return
	if len(gdpList.Items) == 0 {
		return nil
	}

	// check if any of these GDP objects have "success" in their fields
	var successGDP *gslbalphav1.GlobalDeploymentPolicy

	for _, gdp := range gdpList.Items {
		if gdp.Status.ErrorStatus == GDPSuccess {
			if successGDP != nil {
				successGDP = &gdp
			} else {
				// there are more than two accepted GDPs, which pertains to an undefined state
				gslbutils.Errf("ns: %s, msg: more than one GDP objects which were accepted, undefined state, can't do a full sync",
					gslbutils.AVISystem)
				return errors.New("more than one GDP objects in accepted state")
			}
		}
	}

	if successGDP != nil {
		AddGDPObj(successGDP, nil, 0)
	}

	// no success GDPs, check if only one exists
	if len(gdpList.Items) > 1 {
		return errors.New("more than one GDP objects")
	}

	AddGDPObj(&gdpList.Items[0], nil, 0)
	return nil
}

func bootupSync(ctrlList []*GSLBMemberController) {
	gslbutils.Logf("Starting boot up sync, will sync all ingresses and services from all member clusters")

	// add a GDP object
	err := checkGDPsAndInitialize()
	if err != nil {
		// Undefined state, panic
		panic(err.Error())
	}

	gf := gslbutils.GetGlobalFilter()

	acceptedNSStore := gslbutils.GetAcceptedNSStore()
	rejectedNSStore := gslbutils.GetRejectedNSStore()

	for _, c := range ctrlList {
		gslbutils.Logf("syncing for cluster %s", c.GetName())
		if !gf.IsClusterAllowed(c.name) {
			gslbutils.Logf("cluster %s is not allowed via GDP", c.name)
			continue
		}
		// get all namespaces
		selectedNamespaces, err := c.informers.ClientSet.CoreV1().Namespaces().List(metav1.ListOptions{})
		if err != nil {
			gslbutils.Errf("cluster: %s, error in fetching namespaces, %s", c.name, err.Error())
			return
		}
		gslbutils.Logf("selected namespaces: %v", selectedNamespaces.Items)

		if len(selectedNamespaces.Items) == 0 {
			gslbutils.Errf("namespaces list is empty, can't do a full sync, returning")
			return
		}

		for _, ns := range selectedNamespaces.Items {
			_, err := gf.GetNSFilterLabel()
			if err == nil {
				nsMeta := k8sobjects.GetNSMeta(&ns, c.GetName())
				if !filter.ApplyFilter(nsMeta, c.GetName()) {
					AddOrUpdateNSStore(rejectedNSStore, &ns, c.GetName())
					gslbutils.Logf("cluster: %s, ns: %s, msg: %s\n", c.GetName(), nsMeta.Name,
						"ns didn't pass through the filter, adding to rejected list")
					continue
				}
				AddOrUpdateNSStore(acceptedNSStore, &ns, c.GetName())
			} else {
				gslbutils.Logf("no namespace filter present, will sync the applications now")
			}
		}
		if c.informers.IngressInformer != nil {
			fetchAndApplyAllIngresses(c, selectedNamespaces)
		}

		if c.informers.ServiceInformer != nil {
			fetchAndApplyAllServices(c, selectedNamespaces)
		}
	}

	// Generate models
	GenerateModels()
	gslbutils.Logf("boot up sync completed")
}

func GenerateModels() {
	gslbutils.Logf("will generate GS graphs from all accepted lists")
	acceptedIngStore := gslbutils.GetAcceptedIngressStore()
	acceptedLBSvcStore := gslbutils.GetAcceptedLBSvcStore()

	ingList := acceptedIngStore.GetAllClusterNSObjects()
	for _, ingName := range ingList {
		nodes.DequeueIngestion(gslbutils.MultiClusterKeyWithObjName(gslbutils.ObjectAdd,
			gslbutils.IngressType, ingName))
	}

	svcList := acceptedLBSvcStore.GetAllClusterNSObjects()
	for _, svcName := range svcList {
		nodes.DequeueIngestion(gslbutils.MultiClusterKeyWithObjName(gslbutils.ObjectAdd,
			gslbutils.SvcType, svcName))
	}
	gslbutils.Logf("keys for GS graphs published to layer 3")
}
