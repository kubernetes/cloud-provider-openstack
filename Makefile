# golang-client Makefile
# Follows the interface defined in the Golang CTI proposed
# in https://review.openstack.org/410355

#REPO_VERSION?=$(shell git describe --tags)

GIT_HOST = git.openstack.org

PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))
# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
PKG := $(shell awk  '/^package: / { print $$2 }' glide.yaml)
DEST := $(GOPATH)/src/$(GIT_HOST)/openstack/$(BASE_DIR)
DEST := $(GOPATH)/src/$(PKG)
SOURCES := $(shell find $(DEST) -name '*.go')

GOOS ?= $(shell go env GOOS)
VERSION ?= $(shell git describe --exact-match 2> /dev/null || \
                 git describe --match=$(git rev-parse --short=8 HEAD) --always --dirty --abbrev=8)
REGISTRY ?= dims

# CTI targets

depend: work
	cd $(DEST) && glide install --strip-vendor

depend-update: work
	cd $(DEST) && glide update

build: depend openstack-cloud-controller-manager cinder-provisioner cinder-flex-volume-driver k8s-keystone-auth

openstack-cloud-controller-manager: $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o openstack-cloud-controller-manager \
		cmd/openstack-cloud-controller-manager/main.go

cinder-provisioner: $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o cinder-provisioner \
		cmd/cinder-provisioner/main.go

cinder-flex-volume-driver: $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o cinder-flex-volume-driver \
		cmd/cinder-flex-volume-driver/main.go

k8s-keystone-auth: $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o k8s-keystone-auth \
		cmd/k8s-keystone-auth/main.go

test: unit functional

unit: depend
	cd $(DEST) && go test -tags=unit $(shell glide novendor) $(TESTARGS)

functional:
	@echo "$@ not yet implemented"

fmt: work
	cd $(DEST) && go fmt ./...

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
	rm -rf .bindep openstack-cloud-controller-manager cinder-flex-volume-driver cinder-provisioner k8s-keystone-auth

realclean: clean
	rm -rf vendor
	if [ "$(GOPATH)" = "$(GOPATH_DEFAULT)" ]; then \
		rm -rf $(GOPATH); \
	fi

shell: work
	cd $(DEST) && $(SHELL) -i

build-image: build
ifeq ($(GOOS),linux)
	cp openstack-cloud-controller-manager cluster/images/controller-manager
	docker build -t $(REGISTRY)/openstack-cloud-controller-manager:$(VERSION) cluster/images/controller-manager
	rm cluster/images/controller-manager/openstack-cloud-controller-manager

	cp cinder-flex-volume-driver cluster/images/flex-volume-driver
	docker build -t $(REGISTRY)/cinder-flex-volume-driver:$(VERSION) cluster/images/flex-volume-driver
	rm cluster/images/flex-volume-driver/cinder-flex-volume-driver
else
	$(error Please set GOOS=linux for building the image)
endif

version:
	@echo ${VERSION}

.PHONY: bindep build clean cover depend docs fmt functional lint realclean \
	relnotes test translation version
