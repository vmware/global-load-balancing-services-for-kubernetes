package ingestion

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"

	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/gslbutils"
	gslbingestion "github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/ingestion"
	"github.com/vmware/global-load-balancing-services-for-kubernetes/gslb/test/mockaviserver"
	gslbalphav1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	gslbfake "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned/fake"
	gslbinformers "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions"
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

func TestGSLBHostRuleValidDownResponse(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)

	// GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_EMPTY
	gslbhrObj.Spec.DownResponse = &gslbalphav1.DownResponse{
		Type: gslbalphav1.GSLBServiceDownResponseEmpty,
	}
	t.Logf("Adding GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_EMPTY")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).To(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")

	// GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_FALLBACK_IP and with a fallbackIP
	gslbhrObj.Spec.DownResponse = &gslbalphav1.DownResponse{
		Type:       gslbalphav1.GSLBServiceDownResponseFallbackIP,
		FallbackIP: "10.10.1.1",
	}
	t.Logf("Adding GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_FALLBACK_IP and with a fallbackIP")
	err = gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).To(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleInvalidDownResponse(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_FALLBACK_IP and empty fallbackIP.
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.DownResponse = &gslbalphav1.DownResponse{
		Type: gslbalphav1.GSLBServiceDownResponseFallbackIP,
	}
	t.Logf("Adding GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_FALLBACK_IP and empty fallbackIP")
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")

	// GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_ALL_RECORDS and with a fallbackIP
	gslbhrObj.Spec.DownResponse = &gslbalphav1.DownResponse{
		Type:       gslbalphav1.GSLBServiceDownResponseAllRecords,
		FallbackIP: "10.10.1.2",
	}
	t.Logf("Adding GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_ALL_RECORDS and with a fallbackIP")
	err = gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")

	// GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_FALLBACK_IP and with an invalid IP address as fallbackIP
	gslbhrObj.Spec.DownResponse = &gslbalphav1.DownResponse{
		Type:       gslbalphav1.GSLBServiceDownResponseFallbackIP,
		FallbackIP: "INVALID",
	}
	t.Logf("Adding GSLBHostRule with down response of type GSLB_SERVICE_DOWN_RESPONSE_FALLBACK_IP and with an invalid IP address as fallbackIP")
	err = gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	g.Expect(err.Error()).Should(gomega.Equal("Fallback IP INVALID is not valid"))
	t.Logf("Verified GSLBHostRule")
}

func TestGSLBHostRuleValidPublicIP(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	buildAndAddTestGSLBObject(t)
	// GSLBHostRule with valid publicIP v4.
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.PublicIP = []gslbalphav1.PublicIPElem{{Cluster: "cluster1", IP: "10.23.23.45"}, {Cluster: "cluster2", IP: "10.23.23.46"}}
	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).To(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")
	// GSLBHostRule with valid publicIP v6.
	gslbhrObj.Spec.PublicIP = []gslbalphav1.PublicIPElem{{Cluster: "cluster1", IP: "2001:0:3238:DFE1:63::FEFB"}, {Cluster: "cluster2", IP: "10.23.23.46"}}
	err = gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).To(gomega.BeNil())
	t.Logf("Verified GSLBHostRule")

}

func TestGSLBHostRuleInvalidPublicIP(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	buildAndAddTestGSLBObject(t)
	// GSLBHostRule with invalid cluster in PublicIP
	gslbhrObj := getTestGSLBHRObject(gslbhrTestObjName, gslbhrTestNamespace, gslbhrTestFqdn)
	gslbhrObj.Spec.PublicIP = []gslbalphav1.PublicIPElem{{Cluster: "k8", IP: "10.23.23.45"}, {Cluster: "cluster2", IP: "10.23.23.46"}}

	err := gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	g.Expect(err.Error()).Should(gomega.Equal("cluster k8 in Public IP  not present in GSLBConfig"))
	// GSLBHostRule with invalid IP address v4 in PublicIP
	gslbhrObj.Spec.PublicIP = []gslbalphav1.PublicIPElem{{Cluster: "cluster1", IP: "10.23.23"}, {Cluster: "cluster2", IP: "10.23.23.46"}}
	err = gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	g.Expect(err.Error()).Should(gomega.Equal("Invalid IP for site cluster1," + gslbhrTestObjName + " GSLBHostRule (expecting IP address)"))
	// GSLBHostRule with invalid IP address v6 in PublicIP
	gslbhrObj.Spec.PublicIP = []gslbalphav1.PublicIPElem{{Cluster: "cluster1", IP: "2001:db8:a0b:12f0::::0:1"}, {Cluster: "cluster2", IP: "10.23.23.46"}}
	err = gslbingestion.ValidateGSLBHostRule(gslbhrObj, false)
	t.Logf("Verifying GSLBHostRule")
	g.Expect(err).NotTo(gomega.BeNil())
	g.Expect(err.Error()).Should(gomega.Equal("Invalid IP for site cluster1," + gslbhrTestObjName + " GSLBHostRule (expecting IP address)"))

}
