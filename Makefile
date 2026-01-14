GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOTEST=$(GOCMD) test
AMKO_BIN=amko
FEDERATOR_BIN=amko-federator
SERVICE_DISCOVERY_BIN=amko-service-discovery
PACKAGE_PATH=github.com/vmware/global-load-balancing-services-for-kubernetes
AMKO_REL_PATH=github.com/vmware/global-load-balancing-services-for-kubernetes/cmd/gslb
FEDERATOR_REL_PATH=github.com/vmware/global-load-balancing-services-for-kubernetes/federator
SERVICE_DISCOVERY_REL_PATH=github.com/vmware/global-load-balancing-services-for-kubernetes/cmd/service_discovery
GOLANG_UT_IMAGE=golang:bullseye

ifdef GOLANG_SRC_REPO
	BUILD_GO_IMG=$(GOLANG_SRC_REPO)
else
	BUILD_GO_IMG=golang:latest
endif

.PHONY: all
all: vendor build

.PHONY: build-amko
build-amko:
	sudo docker run \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(BUILD_GO_IMG) \
	go build \
	-o /go/src/$(PACKAGE_PATH)/bin/$(AMKO_BIN) \
	-buildvcs=false \
	-mod=vendor \
	/go/src/$(AMKO_REL_PATH)

.PHONY: build-amko-federator
build-amko-federator:
	sudo docker run \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(BUILD_GO_IMG) \
	go build \
	-o /go/src/$(PACKAGE_PATH)/bin/$(FEDERATOR_BIN) \
	-buildvcs=false \
	-mod=vendor \
	/go/src/$(FEDERATOR_REL_PATH)

.PHONY: build-amko-service-discovery
build-amko-service-discovery:
	sudo docker run \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(BUILD_GO_IMG) \
	go build \
	-o /go/src/$(PACKAGE_PATH)/bin/$(SERVICE_DISCOVERY_BIN) \
	-buildvcs=false \
	-mod=vendor \
	/go/src/$(SERVICE_DISCOVERY_REL_PATH)

.PHONY: build
build: build-amko build-amko-federator build-amko-service-discovery

.PHONY: clean
clean:
		$(GOCLEAN) -mod=vendor $(AMKO_REL_PATH)
		rm -f bin/$(AMKO_BIN)
		rm -f bin/$(FEDERATOR_BIN)
		rm -rf bin/$(SERVICE_DISCOVERY_BIN)

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

.PHONY: amko-service-discovery-docker
amko-service-discovery-docker:
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
	sudo docker build -t $(SERVICE_DISCOVERY_BIN):latest --label "BUILD_TAG=$(BUILD_TAG)" --label "BUILD_TIME=$(BUILD_TIME)" $(BUILD_ARG_GOLANG) $(BUILD_ARG_PHOTON) -f Dockerfile.amko-service-discovery .

.PHONY: docker
docker: amko-docker amko-federator-docker amko-service-discovery-docker

.PHONY: ingestion_test
ingestion_test:
	sudo docker run \
	-e KUBEBUILDER_ASSETS="/go/src/$(PACKAGE_PATH)/kubebuilder/bin" \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(GOLANG_UT_IMAGE) \
	$(GOTEST) -v -mod=vendor ./gslb/test/ingestion -failfast -coverprofile=coverage_ingestion.out -coverpkg=./...

.PHONY: graph_test
graph_test:
	sudo docker run \
	-e KUBEBUILDER_ASSETS="/go/src/$(PACKAGE_PATH)/kubebuilder/bin" \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(GOLANG_UT_IMAGE) \
	$(GOTEST) -v -mod=vendor ./gslb/test/graph -failfast -coverprofile=coverage_graph.out -coverpkg=./...

.PHONY: rest_test
rest_test:
	sudo docker run \
	-e KUBEBUILDER_ASSETS="/go/src/$(PACKAGE_PATH)/kubebuilder/bin" \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(GOLANG_UT_IMAGE) \
	$(GOTEST) -v -mod=vendor ./gslb/test/restlayer -failfast -coverprofile=coverage_rest.out -coverpkg=./...
 
.PHONY: federator_test
federator_test:
	sudo docker run \
	-e KUBEBUILDER_ASSETS="/go/src/$(PACKAGE_PATH)/kubebuilder/bin" \
	-e ACK_GINKGO_DEPRECATIONS=1.24.2 \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(GOLANG_UT_IMAGE) \
	$(GOTEST) -v -mod=vendor ./federator/controllers -ginkgo.v -coverprofile=coverage_federator.out -coverpkg=./...

.PHONY: bootup_test
bootup_test:
	sudo docker run \
	-e KUBEBUILDER_ASSETS="/go/src/$(PACKAGE_PATH)/kubebuilder/bin" \
	-e ACK_GINKGO_DEPRECATIONS=1.24.2 \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(GOLANG_UT_IMAGE) \
	$(GOTEST) -v -mod=vendor ./gslb/test/bootuptest -ginkgo.v -ginkgo.seed=1624910766 -coverprofile=coverage_bootup.out -coverpkg=./...

.PHONY: custom_fqdn_test
custom_fqdn_test:
	sudo docker run \
	-e KUBEBUILDER_ASSETS="/go/src/$(PACKAGE_PATH)/kubebuilder/bin" \
	-e ACK_GINKGO_DEPRECATIONS=1.24.2 \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(GOLANG_UT_IMAGE) \
	$(GOTEST) -v -mod=vendor ./gslb/test/integration/custom_fqdn -failfast -coverprofile=coverage_custom_fqdn.out -coverpkg=./...

.PHONY: third_party_vips_test
third_party_vips_test:
	sudo docker run \
	-e KUBEBUILDER_ASSETS="/go/src/$(PACKAGE_PATH)/kubebuilder/bin" \
	-e ACK_GINKGO_DEPRECATIONS=1.24.2 \
	-w=/go/src/$(PACKAGE_PATH) \
	-v $(PWD):/go/src/$(PACKAGE_PATH) $(GOLANG_UT_IMAGE) \
	$(GOTEST) -v -mod=vendor ./gslb/test/integration/third_party_vips -failfast -coverprofile=coverage_third_party_vips.out -coverpkg=./...

.PHONY: int_test
int_test: federator_test bootup_test custom_fqdn_test third_party_vips_test

K8S_VERSION=1.24.2
URL=https://storage.googleapis.com/kubebuilder-tools/kubebuilder-tools-${K8S_VERSION}-linux-amd64.tar.gz
envtest_setup:
	curl -sSLo /tmp/envtest-bins.tar.gz ${URL}
	tar -zvxf /tmp/envtest-bins.tar.gz -C $(PWD)

.PHONY: test
test: envtest_setup int_test ingestion_test graph_test rest_test

.PHONY: coverage
coverage:
	@echo "Merging coverage reports..."
	@echo "mode: set" > coverage_merged.out
	@grep -h -v "^mode:" coverage_*.out 2>/dev/null | \
		grep -v "/vendor/" | \
		grep -v "zz_generated" | \
		grep -v "/pkg/client/" | \
		grep -v "/test/" | \
		grep -v "/service_discovery/" | \
		grep -v "/federator/controllers/test_utils.go" | \
		sort -u >> coverage_merged.out || true
	@echo "Filtering out vendor, test, service_discovery, and autogenerated files..."
	@echo "Generating coverage report..."
	@go tool cover -func=coverage_merged.out
	@echo ""
	@echo "Generating HTML coverage report..."
	@go tool cover -html=coverage_merged.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@echo ""
	@echo "Individual coverage files:"
	@ls -lh coverage_*.out 2>/dev/null || echo "No coverage files found"

.PHONY: gen-clientsets
codegen:
		hack/update-codegen-amkocrd.sh v1alpha1
		hack/update-codegen-amkocrd.sh v1alpha2

# linting and formatting
GO_FILES := $(shell find . -type d -path ./vendor -prune -o -type f -name '*.go' -print)
.PHONY: fmt
fmt:
	@echo
	@echo "Formatting Go files"
	@gofmt -s -l -w $(GO_FILES)

.golangci-bin:
	@echo "Installing Golangci-lint"
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $@ v1.64.7

.PHONY: golangci
golangci: .golangci-bin
	@echo "Running golangci"
	@GOOS=linux .golangci-bin/golangci-lint run -c .golangci.yml

.PHONY: golangci-fix
golangci-fix: .golangci-bin
	@echo "Running golangci-fix"
	@GOOS=linux .golangci-bin/golangci-lint run -c .golangci.yml --fix
