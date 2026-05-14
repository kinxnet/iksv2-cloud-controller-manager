## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KO             ?= $(LOCALBIN)/ko

.PHONY: ko
ko: $(KO) ## Download ko locally if necessary.
$(KO): $(LOCALBIN)
	test -s $(LOCALBIN)/ko || GOBIN=$(LOCALBIN) CGO_ENABLED=0 go install -ldflags="-s -w" github.com/google/ko@v0.18.1
	

# golang-client Makefile

GIT_HOST = github.com/kinxnet
PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))
# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(HOME)/go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)

DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)
SOURCES := $(shell find $(DEST) -name '*.go' 2>/dev/null)

TEMP_DIR	:=$(shell mktemp -d)

GOOS		?= $(shell go env GOOS)
VERSION		?= "v1.0.0"
LDFLAGS		:= "-w -s -X 'github.com/kinxnet/iksv2-cloud-controller-manager/pkg/version.Version=${VERSION}'"
REGISTRY	?= nexus.kinxcloud.net:8443/iks-infra-docker/iksv2/infra/docker/iksv2-cloud-controller-manager
IMAGE_NAMES	?= kinx-cloud-controller-manager

# ko image builder settings
KO_DOCKER_REPO ?= $(REGISTRY)/$(IMAGE_NAMES)

work: $(GOBIN)

# Remove this individual go build target, once we remove
# image-controller-manager below.
kinx-cloud-controller-manager: work $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o kinx-cloud-controller-manager \
		cmd/cloud-controller-manager/controller-manager.go

# ko-based image build targets
# Builds the image and loads it into the local Docker daemon.
# Requires: ko (go install github.com/google/ko@latest)
ko-build: $(KO)
	VERSION=$(VERSION) $(KO) build ./cmd/cloud-controller-manager \
		--image-refs=.ko-image-refs \
		--platform=linux/amd64 \
		--sbom=none \
		--tags=$(VERSION)

# Builds the image and pushes it to the registry specified by KO_DOCKER_REPO (defaults to REGISTRY).
ko-publish: $(KO)
	VERSION=$(VERSION) KO_DOCKER_REPO=$(KO_DOCKER_REPO) \
		$(KO) build ./cmd/cloud-controller-manager \
		--platform=linux/amd64 \
		--sbom=none \
		--tags=$(VERSION),latest \
		--bare \
		--push=true
