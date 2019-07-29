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
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)
PKG := $(shell awk  -F "\"" '/^ignored = / { print $$2 }' Gopkg.toml)
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)
SOURCES := $(shell find $(DEST) -name '*.go')
HAS_MERCURIAL := $(shell command -v hg;)
HAS_DEP := $(shell command -v dep;)
HAS_LINT := $(shell command -v golint;)
HAS_GOX := $(shell command -v gox;)
HAS_IMPORT_BOSS := $(shell command -v import-boss;)
GOX_PARALLEL ?= 3
TARGETS ?= darwin/amd64 linux/amd64 linux/386 linux/arm linux/arm64 linux/ppc64le
DIST_DIRS         = find * -type d -exec

GOOS ?= $(shell go env GOOS)
VERSION ?= $(shell git describe --exact-match 2> /dev/null || \
                 git describe --match=$(git rev-parse --short=8 HEAD) --always --dirty --abbrev=8)
GOFLAGS   :=
TAGS      :=
LDFLAGS   := "-w -s -X 'k8s.io/cloud-provider-openstack/pkg/version.Version=${VERSION}'"
REGISTRY ?= k8scloudprovider

ifneq ("$(DEST)", "$(PWD)")
    $(error Please run 'make' from $(DEST). Current directory is $(PWD))
endif

# CTI targets

$(GOBIN):
	echo "create gobin"
	mkdir -p $(GOBIN)

work: $(GOBIN)

depend: work
ifndef HAS_MERCURIAL
	pip install Mercurial
endif
ifndef HAS_DEP
	curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
endif
	dep ensure -v

depend-update: work
	dep ensure -update -v

build: openstack-cloud-controller-manager cinder-provisioner cinder-flex-volume-driver cinder-csi-plugin k8s-keystone-auth client-keystone-auth octavia-ingress-controller manila-provisioner manila-csi-plugin barbican-kms-plugin magnum-auto-healer

openstack-cloud-controller-manager: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o openstack-cloud-controller-manager \
		cmd/openstack-cloud-controller-manager/main.go

cinder-provisioner: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o cinder-provisioner \
		cmd/cinder-provisioner/main.go

cinder-csi-plugin: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o cinder-csi-plugin \
		cmd/cinder-csi-plugin/main.go

cinder-flex-volume-driver: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o cinder-flex-volume-driver \
		cmd/cinder-flex-volume-driver/main.go

k8s-keystone-auth: depend $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o k8s-keystone-auth \
		cmd/k8s-keystone-auth/main.go

client-keystone-auth: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o client-keystone-auth \
		cmd/client-keystone-auth/main.go

octavia-ingress-controller: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o octavia-ingress-controller \
		cmd/octavia-ingress-controller/main.go

manila-provisioner: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o manila-provisioner \
		cmd/manila-provisioner/main.go

manila-csi-plugin: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o manila-csi-plugin \
		cmd/manila-csi-plugin/main.go

barbican-kms-plugin: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o barbican-kms-plugin \
		cmd/barbican-kms-plugin/main.go

magnum-auto-healer: depend $(SOURCES)
	cd $(DEST) && CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o magnum-auto-healer \
		cmd/magnum-auto-healer/main.go

test: unit functional

check: depend fmt vet lint import-boss

unit: depend
	go test -tags=unit $(shell go list ./... | sed -e '/sanity/ { N; d; }') $(TESTARGS)

functional:
	@echo "$@ not yet implemented"

test-csi-sanity: depend
	go test $(GIT_HOST)/$(BASE_DIR)/pkg/csi/cinder/sanity/

fmt:
	hack/verify-gofmt.sh

lint:
ifndef HAS_LINT
		go get -u golang.org/x/lint/golint
		echo "installing lint"
endif
	hack/verify-golint.sh

import-boss:
ifndef HAS_IMPORT_BOSS
		go get -u k8s.io/code-generator/cmd/import-boss
		echo "installing import-boss"
endif
	hack/verify-import-boss.sh

vet:
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

.bindep:
	virtualenv .bindep
	.bindep/bin/pip install -i https://pypi.python.org/simple bindep

bindep: .bindep
	@.bindep/bin/bindep -b -f bindep.txt || true

install-distro-packages:
	tools/install-distro-packages.sh

clean:
	rm -rf _dist .bindep openstack-cloud-controller-manager cinder-flex-volume-driver cinder-provisioner cinder-csi-plugin k8s-keystone-auth client-keystone-auth octavia-ingress-controller manila-provisioner manila-csi-plugin magnum-auto-healer

realclean: clean
	rm -rf vendor
	if [ "$(GOPATH)" = "$(GOPATH_DEFAULT)" ]; then \
		rm -rf $(GOPATH); \
	fi

shell:
	$(SHELL) -i

images: image-controller-manager image-flex-volume-driver image-provisioner image-csi-plugin image-k8s-keystone-auth image-octavia-ingress-controller image-manila-provisioner image-manila-csi-plugin image-kms-plugin image-magnum-auto-healer

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

image-octavia-ingress-controller: depend octavia-ingress-controller
ifeq ($(GOOS),linux)
	cp octavia-ingress-controller cluster/images/octavia-ingress-controller
	docker build -t $(REGISTRY)/octavia-ingress-controller:$(VERSION) cluster/images/octavia-ingress-controller
	rm cluster/images/octavia-ingress-controller/octavia-ingress-controller
else
	$(error Please set GOOS=linux for building the image)
endif

image-manila-provisioner: depend manila-provisioner
ifeq ($(GOOS),linux)
	cp manila-provisioner cluster/images/manila-provisioner
	docker build -t $(REGISTRY)/manila-provisioner:$(VERSION) cluster/images/manila-provisioner
	rm cluster/images/manila-provisioner/manila-provisioner
else
	$(error Please set GOOS=linux for building the image)
endif

image-manila-csi-plugin: depend manila-csi-plugin
ifeq ($(GOOS),linux)
	cp manila-csi-plugin cluster/images/manila-csi-plugin
	docker build -t $(REGISTRY)/manila-csi-plugin:$(VERSION) cluster/images/manila-csi-plugin
	rm cluster/images/manila-csi-plugin/manila-csi-plugin
else
	$(error Please set GOOS=linux for building the image)
endif

image-kms-plugin: depend barbican-kms-plugin
ifeq ($(GOOS), linux)
	cp barbican-kms-plugin cluster/images/barbican-kms-plugin
	docker build -t $(REGISTRY)/barbican-kms-plugin:$(VERSION) cluster/images/barbican-kms-plugin
	rm cluster/images/barbican-kms-plugin/barbican-kms-plugin
else
	$(error Please set GOOS=linux for building the image)
endif

image-magnum-auto-healer: depend magnum-auto-healer
ifeq ($(GOOS),linux)
	cp magnum-auto-healer cluster/images/magnum-auto-healer
	docker build -t $(REGISTRY)/magnum-auto-healer:$(VERSION) cluster/images/magnum-auto-healer
	rm cluster/images/magnum-auto-healer/magnum-auto-healer
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
	docker push $(REGISTRY)/octavia-ingress-controller:$(VERSION)
	docker push $(REGISTRY)/manila-provisioner:$(VERSION)
	docker push $(REGISTRY)/manila-csi-plugin:$(VERSION)
	docker push $(REGISTRY)/magnum-auto-healer:$(VERSION)

version:
	@echo ${VERSION}

.PHONY: build-cross
build-cross: LDFLAGS += -extldflags "-static"
build-cross: depend
ifndef HAS_GOX
	go get -u github.com/mitchellh/gox
endif
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/openstack-cloud-controller-manager/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/cinder-provisioner/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/cinder-csi-plugin/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/cinder-flex-volume-driver/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/k8s-keystone-auth/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/client-keystone-auth/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/octavia-ingress-controller/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/manila-provisioner/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/manila-csi-plugin/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/magnum-auto-healer/

.PHONY: dist
dist: build-cross
	( \
		cd _dist && \
		$(DIST_DIRS) cp ../LICENSE {} \; && \
		$(DIST_DIRS) cp ../README.md {} \; && \
		$(DIST_DIRS) tar -zcf cloud-provider-openstack-$(VERSION)-{}.tar.gz {} \; && \
		$(DIST_DIRS) zip -r cloud-provider-openstack-$(VERSION)-{}.zip {} \; \
	)

.PHONY: bindep build clean cover depend docs fmt functional lint import-boss realclean \
	relnotes test translation version build-cross dist
