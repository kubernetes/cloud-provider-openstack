# golang-client Makefile
# Follows the interface defined in the Golang CTI proposed
# in https://review.openstack.org/410355

#REPO_VERSION?=$(shell git describe --tags)

GIT_HOST = k8s.io

PROJECT_DIR := ${CURDIR}
BASE_DIR := $(shell basename $(PROJECT_DIR))
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)
PKG := $(shell awk  -F "\"" '/^ignored = / { print $$2 }' Gopkg.toml)
SOURCES := $(shell find $(PROJECT_DIR) -name '*.go')
HAS_MERCURIAL := $(shell command -v hg;)
HAS_DEP := $(shell command -v dep;)
HAS_LINT := $(shell command -v golint;)

GOOS ?= $(shell go env GOOS)
VERSION ?= $(shell git describe --exact-match 2> /dev/null || \
                 git describe --match=$(git rev-parse --short=8 HEAD) --always --dirty --abbrev=8)
REGISTRY ?= k8scloudprovider
export BIN_DIR ?= $(PROJECT_DIR)

# CTI targets

depend: work
ifneq ($(findstring $(GOPATH),$(PROJECT_DIR)),$(GOPATH))
	$(error current dir $(PROJECT_DIR) is not in your GOPATH $(GOPATH))
endif



ifndef HAS_MERCURIAL
	pip install Mercurial
endif
ifndef HAS_DEP
	curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
endif
	dep ensure

depend-update: work
	dep ensure -update

build: openstack-cloud-controller-manager cinder-provisioner cinder-flex-volume-driver cinder-csi-plugin k8s-keystone-auth

openstack-cloud-controller-manager: depend $(SOURCES)
	$(info $$GOOS is [${GOOS}])
	$(info $$GOPATH is [${GOPATH}])
	$(info $$GOBIN is [${GOBIN}])
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o ${BIN_DIR}/openstack-cloud-controller-manager \
		cmd/openstack-cloud-controller-manager/main.go

cinder-provisioner: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o ${BIN_DIR}/cinder-provisioner \
		cmd/cinder-provisioner/main.go

cinder-csi-plugin: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o ${BIN_DIR}/cinder-csi-plugin \
		cmd/cinder-csi-plugin/main.go

cinder-flex-volume-driver: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o ${BIN_DIR}/cinder-flex-volume-driver \
		cmd/cinder-flex-volume-driver/main.go

k8s-keystone-auth: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags "-X 'main.version=${VERSION}'" \
		-o ${BIN_DIR}/k8s-keystone-auth \
		cmd/k8s-keystone-auth/main.go

test: unit functional

check: depend fmt vet lint

unit: depend
	go test -tags=unit $(shell go list ./...) $(TESTARGS)

functional:
	@echo "$@ not yet implemented"

fmt: work
	hack/verify-gofmt.sh

lint: work
ifndef HAS_LINT
		go get -u github.com/golang/lint/golint
		echo "installing lint"
endif
	hack/verify-golint.sh

vet: work
	go vet ./...

cover: depend
	go test -tags=unit $(shell go list ./...) -cover

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
	@echo "PROJECT_DIR: $(PROJECT_DIR)"
	@echo "BASE_DIR: $(BASE_DIR)"
	@echo "GOPATH: $(GOPATH)"
	@echo "GOROOT: $(GOROOT)"
	@echo "PKG: $(PKG)"
	go version
	go env

# Get our dev/test dependencies in place
bootstrap:
	tools/test-setup.sh

work: $(GOPATH) $(GOBIN)

.bindep:
	virtualenv .bindep
	.bindep/bin/pip install -i https://pypi.python.org/simple bindep

bindep: .bindep
	@.bindep/bin/bindep -b -f bindep.txt || true

install-distro-packages:
	tools/install-distro-packages.sh

clean:
	rm -rf .bindep $(BIN_DIR)

realclean: clean
	rm -rf vendor
	if [ "$(GOPATH)" = "$(GOPATH_DEFAULT)" ]; then \
		rm -rf $(GOPATH); \
	fi

shell: work
	cd $(BIN_DIR) && $(SHELL) -i

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
