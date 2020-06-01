GOCMD=/usr/local/go/bin/go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOTEST=$(GOCMD) test
AMKO_BIN=amko
AMKO_REL_PATH=amko/cmd/gslb

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
		$(eval BUILD_TAG=$(shell ./tests/jenkins/get_build_version.sh "dummy" 0))
endif
		sudo docker build -t $(AMKO_BIN):latest --label "BUILD_TAG=$(BUILD_TAG)" --label "BUILD_TIME=$(BUILD_TIME)" -f Dockerfile.amko . 

.PHONY: ingestion_test
ingestion_test:
		$(GOTEST) -v -mod=vendor ./gslb/test/ingestion -failfast

.PHONY: graph_test
graph_test:
		$(GOTEST) -v -mod=vendor ./gslb/test/graph -failfast

.PHONY: test
test:
		$(GOTEST) -v -mod=vendor ./gslb/test/ingestion -failfast
		$(GOTEST) -v -mod=vendor ./gslb/test/graph -failfast
