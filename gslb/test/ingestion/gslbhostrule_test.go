package ingestion

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	gslbingestion "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/ingestion"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/mockaviserver"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gslbfake "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned/fake"
	gslbinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
)

const (
	gslbhrTestObjName   = "test-gslbhr"
	gslbhrTestNamespace = gslbutils.AVISystem
	gslbhrTestFqdn      = "mygslbhr.avi.internal"
)

func AddDelSomething(obj interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
}

func UpdateSomething(old, new interface{}, k8swq []workqueue.RateLimitingInterface, numWorkers uint32) {
}

func TestGSLBHostRuleController(t *testing.T) {
	gslbhrKubeClient := k8sfake.NewSimpleClientset()
	gslbhrClient := gslbfake.NewSimpleClientset()
	gslbhrInformerFactory := gslbinformers.NewSharedInformerFactory(gslbhrClient, time.Second*30)
	gslbhrCtrl := gslbingestion.InitializeGSLBHostRuleController(gslbhrKubeClient, gslbhrClient, gslbhrInformerFactory,
		AddDelSomething, UpdateSomething, AddDelSomething)
	if gslbhrCtrl == nil {
		t.Fatalf("GSLBHostRule controller not set")
	}
}

func TestGSLBHostRuleValidThirdPartyMember(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	var gslbhrThirdPartyMembers []gslbalphav1.ThirdPartyMember
	gslbhrTpm1 := gslbalphav1.ThirdPartyMember{
		VIP:  "10.10.10.10",
		Site: "test-third-party-member",
	}
	gslbhrThirdPartyMembers = []gslbalphav1.ThirdPartyMember{gslbhrTpm1}
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.ThirdPartyMembers = gslbhrThirdPartyMembers
	t.Logf("Adding GSLBHostRule with Valid Third Party Members")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).To(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleInvalidThirdPartyMember(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	var gslbhrThirdPartyMembers []gslbalphav1.ThirdPartyMember
	gslbhrTpm1 := gslbalphav1.ThirdPartyMember{
		VIP:  "10.10.10.10",
		Site: "test-site-" + mockaviserver.InvalidObjectNameSuffix,
	}
	gslbhrThirdPartyMembers = []gslbalphav1.ThirdPartyMember{gslbhrTpm1}
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.ThirdPartyMembers = gslbhrThirdPartyMembers
	t.Logf("Adding GSLBHostRule with invalid Third Party Members")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleValidSitePersistence(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	gslbhrsp := &gslbalphav1.SitePersistence{
		Enabled:    true,
		ProfileRef: "test-profile-ref",
	}
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.SitePersistence = gslbhrsp
	t.Logf("Adding GSLBHostRule with Valid Site Persistences Profiles")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).To(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleInvalidSitePersistence(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	gslbhrsp := &gslbalphav1.SitePersistence{
		Enabled:    true,
		ProfileRef: "test-profile-ref-" + mockaviserver.InvalidObjectNameSuffix,
	}
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.SitePersistence = gslbhrsp
	t.Logf("Adding GSLBHostRule with invalid Site Persistences Profiles")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleValidHealthMonitors(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	gslbhrHealthMonitorRefs := []string{"test-health-monitor"}
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.HealthMonitorRefs = gslbhrHealthMonitorRefs
	t.Logf("Adding GSLBHostRule with Valid Health Monitor Refs")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).To(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleInvalidHealthMonitors(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	gslbhrHealthMonitorRefs := []string{"test-hm-" + mockaviserver.InvalidObjectNameSuffix}
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.HealthMonitorRefs = gslbhrHealthMonitorRefs
	t.Logf("Adding GSLBHostRule with invalid Health Monitor Refs")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleValidHealthMonitorTemplate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.HealthMonitorRefs = nil
	gslbhrHealthMonitorTemplate := "System-GSLB-HTTPS"
	gslbhrObj.Spec.HealthMonitorTemplate = &gslbhrHealthMonitorTemplate
	t.Logf("Adding GSLBHostRule with valid Health Monitor Template")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).To(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleInvalidHealthMonitorTemplate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.HealthMonitorRefs = nil
	gslbhrHealthMonitorTemplate := "test-hm-" + mockaviserver.InvalidObjectNameSuffix
	gslbhrObj.Spec.HealthMonitorTemplate = &gslbhrHealthMonitorTemplate
	t.Logf("Adding GSLBHostRule with invalid Health Monitor Template")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleBothHmRefAndHmTemplate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.HealthMonitorRefs = []string{"test-health-monitor"}
	gslbhrHealthMonitorTemplate := "System-GSLB-HTTPS"
	gslbhrObj.Spec.HealthMonitorTemplate = &gslbhrHealthMonitorTemplate
	t.Logf("Adding GSLBHostRule with both Health Monitor Reference and Health Monitor Template")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}
