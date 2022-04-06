module github.com/vmware/global-load-balancing-services-for-kubernetes

go 1.15

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/golang/glog v1.0.0
	github.com/google/uuid v1.3.0
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.19.0
	github.com/openshift/api v3.9.0+incompatible
	github.com/openshift/client-go v0.0.0-20201020082437-7737f16e53fc
	github.com/vmware/alb-sdk v0.0.0-20220405050634-1a01e2eee142
	github.com/vmware/load-balancer-and-ingress-services-for-kubernetes v0.0.0-20220405050344-3a6d72bbda3e
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.23.5
	k8s.io/apiextensions-apiserver v0.23.5
	k8s.io/apimachinery v0.23.5
	k8s.io/client-go v0.23.5
	sigs.k8s.io/controller-runtime v0.11.2
)

replace (
	k8s.io/api => k8s.io/api v0.21.3
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.3
	k8s.io/client-go => k8s.io/client-go v0.21.3
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.9.6
	github.com/onsi/gomega => github.com/onsi/gomega v1.14.0
)
