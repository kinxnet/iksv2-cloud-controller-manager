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
LDFLAGS		:= "-w -s -X 'github.com/kinxnet/cloud-provider-kinx/pkg/version.Version=${VERSION}'"
REGISTRY	?= ghcr.io/kinxnet/cloud-provider-kinx
IMAGE_NAMES	?= kinx-cloud-controller-manager

work: $(GOBIN)

# Remove this individual go build target, once we remove
# image-controller-manager below.
kinx-cloud-controller-manager: work $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o kinx-cloud-controller-manager \
		cmd/cloud-controller-manager/controller-manager.go

# Remove individual image builder once we migrate openlab-zuul-jobs
# to use new image-openstack-cloud-controller-manager target.
image-kinx-cloud-controller-manager: work kinx-cloud-controller-manager
ifeq ($(GOOS),linux)
	cp -r cluster/images/kinx-cloud-controller-manager $(TEMP_DIR)
	cp kinx-cloud-controller-manager $(TEMP_DIR)/kinx-cloud-controller-manager
	cp $(TEMP_DIR)/kinx-cloud-controller-manager/Dockerfile.build $(TEMP_DIR)/kinx-cloud-controller-manager/Dockerfile
	docker build -t $(REGISTRY)/kinx-cloud-controller-manager:$(VERSION) $(TEMP_DIR)/kinx-cloud-controller-manager
	rm -rf $(TEMP_DIR)/kinx-cloud-controller-manager
else
	$(error Please set GOOS=linux for building the image)
endif

build-images: $(addprefix image-,$(IMAGE_NAMES))

push-images: $(addprefix push-image-,$(IMAGE_NAMES))

push-image-%:
	@echo "push image $* to $(REGISTRY)"
ifneq ($(and $(DOCKER_USERNAME),$(DOCKER_PASSWORD)),)
	@docker login -u="$(DOCKER_USERNAME)" -p="$(DOCKER_PASSWORD)"
endif
	docker push $(REGISTRY)/$*:$(VERSION)

upload-image:
	$(MAKE) build-images push-images
