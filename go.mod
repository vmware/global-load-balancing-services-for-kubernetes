module github.com/vmware/global-load-balancing-services-for-kubernetes

go 1.15

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.14.0
	github.com/openshift/api v3.9.0+incompatible
	github.com/openshift/client-go v0.0.0-20201020082437-7737f16e53fc
	github.com/vmware/alb-sdk v0.0.0-20210721142023-8e96475b833b
	github.com/vmware/load-balancer-and-ingress-services-for-kubernetes v0.0.0-20220211084755-6df4b1e258fd
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.21.3
	k8s.io/apiextensions-apiserver v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/client-go v0.21.3
	sigs.k8s.io/controller-runtime v0.9.6
)
