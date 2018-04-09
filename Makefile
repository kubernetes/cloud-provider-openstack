# golang-client Makefile
# Follows the interface defined in the Golang CTI proposed
# in https://review.openstack.org/410355

#REPO_VERSION?=$(shell git describe --tags)

GIT_HOST = k8s.io

PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))
# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)
PKG := $(shell awk  '/^package: / { print $$2 }' glide.yaml)
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)
DEST := $(GOPATH)/src/$(PKG)
SOURCES := $(shell find $(DEST) -name '*.go')
HAS_MERCURIAL := $(shell command -v hg;)
HAS_GLIDE := $(shell command -v glide;)
HAS_LINT := $(shell command -v golint;)

GOOS ?= $(shell go env GOOS)
VERSION ?= $(shell git describe --exact-match 2> /dev/null || \
                 git describe --match=$(git rev-parse --short=8 HEAD) --always --dirty --abbrev=8)
REGISTRY ?= k8scloudprovider

# CTI targets

depend: work
ifndef HAS_MERCURIAL
	pip install Mercurial
endif
ifndef HAS_GLIDE
	go get -u github.com/Masterminds/glide
endif
ifeq ($(wildcard $(DEST)/vendor/.*),)
		cd $(DEST) && glide install --strip-vendor
endif

depend-update: work
	cd $(DEST) && glide update --strip-vendor

build: openstack-cloud-controller-manager cinder-provisioner cinder-flex-volume-driver cinder-csi-plugin k8s-keystone-auth

openstack-cloud-controller-manager: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o openstack-cloud-controller-manager \
		cmd/openstack-cloud-controller-manager/main.go

cinder-provisioner: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o cinder-provisioner \
		cmd/cinder-provisioner/main.go

cinder-csi-plugin: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o cinder-csi-plugin \
		cmd/cinder-csi-plugin/main.go

cinder-flex-volume-driver: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o cinder-flex-volume-driver \
		cmd/cinder-flex-volume-driver/main.go

k8s-keystone-auth: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o k8s-keystone-auth \
		cmd/k8s-keystone-auth/main.go

test: unit functional

check: depend fmt vet lint

unit: depend
	cd $(DEST) && go test -tags=unit $(shell glide novendor) $(TESTARGS)

functional:
	@echo "$@ not yet implemented"

fmt: work
	cd $(DEST) && hack/verify-gofmt.sh

lint: work
ifndef HAS_LINT
		go get -u github.com/golang/lint/golint
		echo "installing lint"
endif
	cd $(DEST) && hack/verify-golint.sh

vet: work
	cd $(DEST) && go vet ./...

cover: depend
	cd $(DEST) && go test -tags=unit $(shell glide novendor) -cover

docs:
	@echo "$@ not yet implemented"

godoc:
	@echo "$@ not yet implemented"

releasenotes:
	@echo "Reno not yet implemented for this repo"

translation:
	@echo "$@ not yet implemented"

# Do the work here

# Set up the development environment
env:
	@echo "PWD: $(PWD)"
	@echo "BASE_DIR: $(BASE_DIR)"
	@echo "GOPATH: $(GOPATH)"
	@echo "GOROOT: $(GOROOT)"
	@echo "DEST: $(DEST)"
	@echo "PKG: $(PKG)"
	go version
	go env

# Get our dev/test dependencies in place
bootstrap:
	tools/test-setup.sh

work: $(GOPATH) $(DEST)

$(GOPATH):
	mkdir -p $(GOPATH)

$(DEST): $(GOPATH)
	mkdir -p $(shell dirname $(DEST))
	ln -s $(PWD) $(DEST)

.bindep:
	virtualenv .bindep
	.bindep/bin/pip install -i https://pypi.python.org/simple bindep

bindep: .bindep
	@.bindep/bin/bindep -b -f bindep.txt || true

install-distro-packages:
	tools/install-distro-packages.sh

clean:
	rm -rf .bindep openstack-cloud-controller-manager cinder-flex-volume-driver cinder-provisioner cinder-csi-plugin k8s-keystone-auth

realclean: clean
	rm -rf vendor
	if [ "$(GOPATH)" = "$(GOPATH_DEFAULT)" ]; then \
		rm -rf $(GOPATH); \
	fi

shell: work
	cd $(DEST) && $(SHELL) -i

images: image-controller-manager image-flex-volume-driver image-provisioner image-csi-plugin image-k8s-keystone-auth

image-controller-manager: depend openstack-cloud-controller-manager
ifeq ($(GOOS),linux)
	cp openstack-cloud-controller-manager cluster/images/controller-manager
	docker build -t $(REGISTRY)/openstack-cloud-controller-manager:$(VERSION) cluster/images/controller-manager
	rm cluster/images/controller-manager/openstack-cloud-controller-manager
else
	$(error Please set GOOS=linux for building the image)
endif

image-flex-volume-driver: depend cinder-flex-volume-driver
ifeq ($(GOOS),linux)
	cp cinder-flex-volume-driver cluster/images/flex-volume-driver
	docker build -t $(REGISTRY)/cinder-flex-volume-driver:$(VERSION) cluster/images/flex-volume-driver
	rm cluster/images/flex-volume-driver/cinder-flex-volume-driver
else
	$(error Please set GOOS=linux for building the image)
endif

image-provisioner: depend cinder-provisioner
ifeq ($(GOOS),linux)
	cp cinder-provisioner cluster/images/cinder-provisioner
	docker build -t $(REGISTRY)/cinder-provisioner:$(VERSION) cluster/images/cinder-provisioner
	rm cluster/images/cinder-provisioner/cinder-provisioner
else
	$(error Please set GOOS=linux for building the image)
endif

image-csi-plugin: depend cinder-csi-plugin
ifeq ($(GOOS),linux)
	cp cinder-csi-plugin cluster/images/cinder-csi-plugin
	docker build -t $(REGISTRY)/cinder-csi-plugin:$(VERSION) cluster/images/cinder-csi-plugin
	rm cluster/images/cinder-csi-plugin/cinder-csi-plugin
else
	$(error Please set GOOS=linux for building the image)
endif

image-k8s-keystone-auth: depend k8s-keystone-auth
ifeq ($(GOOS),linux)
	cp k8s-keystone-auth cluster/images/webhook
	docker build -t $(REGISTRY)/k8s-keystone-auth:$(VERSION) cluster/images/webhook
	rm cluster/images/webhook/k8s-keystone-auth
else
	$(error Please set GOOS=linux for building the image)
endif

upload-images: images
	@echo "push images to $(REGISTRY)"
	docker login -u="$(DOCKER_USERNAME)" -p="$(DOCKER_PASSWORD)";
	docker push $(REGISTRY)/openstack-cloud-controller-manager:$(VERSION)
	docker push $(REGISTRY)/cinder-flex-volume-driver:$(VERSION)
	docker push $(REGISTRY)/cinder-provisioner:$(VERSION)
	docker push $(REGISTRY)/cinder-csi-plugin:$(VERSION)
	docker push $(REGISTRY)/k8s-keystone-auth:$(VERSION)

version:
	@echo ${VERSION}

.PHONY: bindep build clean cover depend docs fmt functional lint realclean \
	relnotes test translation version
