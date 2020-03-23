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

.PHONY: test
test:
		$(GOTEST) -v -mod=vendor ./gslb/ingestion -failfast
		$(GOTEST) -v -mod=vendor ./gslb/nodes -failfast
