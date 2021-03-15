GOCMD=/usr/local/go/bin/go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOTEST=$(GOCMD) test
AMKO_BIN=amko
AMKO_REL_PATH=github.com/vmware/global-load-balancing-services-for-kubernetes/cmd/gslb

.PHONY: all
all: vendor build

.PHONY: build
build:
		$(GOBUILD) -o bin/$(AMKO_BIN) -mod=vendor $(AMKO_REL_PATH)

.PHONY: clean
clean:
		$(GOCLEAN) -mod=vendor $(AMKO_REL_PATH)
		rm -f bin/$(AMKO_BIN)

.PHONY: vendor
vendor:
		$(GOMOD) vendor

.PHONY: docker
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

.PHONY: ingestion_test
ingestion_test:
		$(GOTEST) -v -mod=vendor ./gslb/test/ingestion -failfast

.PHONY: graph_test
graph_test:
		$(GOTEST) -v -mod=vendor ./gslb/test/graph -failfast

.PHONY: rest_test
rest_test:
		$(GOTEST) -v -mod=vendor ./gslb/test/restlayer -failfast

.PHONY: test
test:
		$(GOTEST) -v -mod=vendor ./gslb/test/ingestion -failfast
		$(GOTEST) -v -mod=vendor ./gslb/test/graph -failfast
		$(GOTEST) -v -mod=vendor ./gslb/test/restlayer -failfast

.PHONY: gen-clientsets
codegen:
		hack/update-codegen-amkocrd.sh v1alpha1
		hack/update-codegen-amkocrd.sh v1alpha2
