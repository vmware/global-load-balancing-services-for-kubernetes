GOCMD=/usr/local/go/bin/go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOTEST=$(GOCMD) test
AMKO_BIN=amko
FEDERATOR_BIN=amko-federator
AMKO_REL_PATH=github.com/vmware/global-load-balancing-services-for-kubernetes/cmd/gslb
FEDERATOR_REL_PATH=github.com/vmware/global-load-balancing-services-for-kubernetes/federator

.PHONY: all
all: vendor build

.PHONY: build-amko
build-amko:
	$(GOBUILD) -o bin/$(AMKO_BIN) -mod=vendor $(AMKO_REL_PATH)

.PHONY: build-amko-federator
build-amko-federator:
	$(GOBUILD) -o bin/$(FEDERATOR_BIN) -mod=vendor $(FEDERATOR_REL_PATH)

.PHONY: build
build: build-amko build-amko-federator

.PHONY: clean
clean:
		$(GOCLEAN) -mod=vendor $(AMKO_REL_PATH)
		rm -f bin/$(AMKO_BIN)
		rm -f bin/$(FEDERATOR_BIN)

.PHONY: vendor
vendor:
		$(GOMOD) vendor

.PHONY: amko-docker
docker:
ifndef BUILD_TAG
		$(eval BUILD_TIME=$(shell date +%Y-%m-%d_%H:%M:%S_%Z))
endif
ifndef BUILD_TAG
		$(eval BUILD_TAG=$(shell ./hack/jenkins/get_build_version.sh "dummy" 0))
endif
ifdef GOLANG_SRC_REPO
	$(eval BUILD_ARG_GOLANG=--build-arg golang_src_repo=$(GOLANG_SRC_REPO))
else
	$(eval BUILD_ARG_GOLANG=)
endif
ifdef PHOTON_SRC_REPO
	$(eval BUILD_ARG_PHOTON=--build-arg photon_src_repo=$(PHOTON_SRC_REPO))
else
	$(eval BUILD_ARG_PHOTON=)
endif
	sudo docker build -t $(AMKO_BIN):latest --label "BUILD_TAG=$(BUILD_TAG)" --label "BUILD_TIME=$(BUILD_TIME)" $(BUILD_ARG_GOLANG) $(BUILD_ARG_PHOTON) -f Dockerfile.amko .


.PHONY: amko-federator-docker
amko-federator-docker:
ifndef BUILD_TAG
	$(eval BUILD_TIME=$(shell date +%Y-%m-%d_%H:%M:%S_%Z))
endif
ifndef BUILD_TAG
	$(eval BUILD_TAG=$(shell ./hack/jenkins/get_build_version.sh "dummy" 0))
endif
ifdef GOLANG_SRC_REPO
	$(eval BUILD_ARG_GOLANG=--build-arg golang_src_repo=$(GOLANG_SRC_REPO))
else
	$(eval BUILD_ARG_GOLANG=)
endif
ifdef PHOTON_SRC_REPO
	$(eval BUILD_ARG_PHOTON=--build-arg photon_src_repo=$(PHOTON_SRC_REPO))
else
	$(eval BUILD_ARG_PHOTON=)
endif
	sudo docker build -t $(FEDERATOR_BIN):latest --label "BUILD_TAG=$(BUILD_TAG)" --label "BUILD_TIME=$(BUILD_TIME)" $(BUILD_ARG_GOLANG) $(BUILD_ARG_PHOTON) -f Dockerfile.amko-federator .


.PHONY: docker
docker: amko-docker amko-federator-docker

.PHONY: ingestion_test
ingestion_test:
		$(GOTEST) -v -mod=vendor ./gslb/test/ingestion -failfast

.PHONY: graph_test
graph_test:
		$(GOTEST) -v -mod=vendor ./gslb/test/graph -failfast

.PHONY: rest_test
rest_test:
		$(GOTEST) -v -mod=vendor ./gslb/test/restlayer -failfast

.PHONY: int_test
int_test:
		ACK_GINKGO_DEPRECATIONS=1.16.4 $(GOTEST) -v -mod=vendor ./federator/controllers -ginkgo.v
		ACK_GINKGO_DEPRECATIONS=1.16.4 $(GOTEST) -v -mod=vendor ./gslb/test/bootuptest -ginkgo.v -ginkgo.seed=1624910766
		$(GOTEST) -v -mod=vendor ./gslb/test/integration/custom_fqdn -failfast
		$(GOTEST) -v -mod=vendor ./gslb/test/integration/third_party_vips -failfast

.PHONY: test
test: int_test
		$(GOTEST) -v -mod=vendor ./gslb/test/ingestion -failfast
		$(GOTEST) -v -mod=vendor ./gslb/test/graph -failfast
		$(GOTEST) -v -mod=vendor ./gslb/test/restlayer -failfast

.PHONY: gen-clientsets
codegen:
		hack/update-codegen-amkocrd.sh v1alpha1
		hack/update-codegen-amkocrd.sh v1alpha2
