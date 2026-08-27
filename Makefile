GIT_HOST = k8s.io

CONTAINER_ENGINE ?= docker

PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))
# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export GO111MODULE := on
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)
PKG := $(shell awk '/^module/ { print $$2 }' go.mod)
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)
SOURCES := Makefile go.mod go.sum $(shell find $(DEST) -name '*.go' 2>/dev/null)
HAS_GOX := $(shell command -v gox;)
GOX_PARALLEL ?= 3

TARGETS		?= linux/amd64 linux/386 linux/arm linux/arm64 linux/ppc64le linux/s390x
DIST_DIRS	= find * -type d -exec

TEMP_DIR	:=$(shell mktemp -d)
TAR_FILE	?= rootfs.tar

GOOS		?= $(shell go env GOOS)
GOPROXY		?= $(shell go env GOPROXY)
VERSION         ?= $(shell git describe --dirty --tags --match='v*')
GOARCH		:=
GOFLAGS		:=
TAGS		:=
LDFLAGS		:= "-w -s -X 'k8s.io/component-base/version.gitVersion=$(VERSION)' -X 'k8s.io/cloud-provider-openstack/pkg/version.Version=$(VERSION)'"
GOX_LDFLAGS	:= $(shell echo "$(LDFLAGS) -extldflags \"-static\"")
REGISTRY	?= registry.k8s.io/provider-os
IMAGE_OS	?= linux
IMAGE_NAMES	?= openstack-cloud-controller-manager \
				cinder-csi-plugin \
				k8s-keystone-auth \
				octavia-ingress-controller \
				manila-csi-plugin \
				barbican-kms-plugin \
				magnum-auto-healer
ARCH		?= amd64
ARCHS		?= amd64 arm arm64 ppc64le s390x
BUILD_CMDS	?= openstack-cloud-controller-manager \
				cinder-csi-plugin \
				k8s-keystone-auth \
				octavia-ingress-controller \
				manila-csi-plugin \
				barbican-kms-plugin \
				magnum-auto-healer \
				client-keystone-auth
GOLANGCI_LINT_VERSION?=v2.12.1

# CTI targets

$(GOBIN):
	echo "create gobin"
	mkdir -p $(GOBIN)

work: $(GOBIN)

build-all-archs:
	@for arch in $(ARCHS); do $(MAKE) ARCH=$${arch} build ; done

build: $(BUILD_CMDS)

$(BUILD_CMDS): $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOPROXY=${GOPROXY} go build \
		-trimpath \
		-ldflags $(LDFLAGS) \
		-o $@ \
		cmd/$@/main.go

test: unit functional

# if the golangci-lint steps fails with one of the following error messages:
#
#   directory prefix . does not contain main module or its selected dependencies
#
#   failed to initialize build cache at /root/.cache/golangci-lint: mkdir /root/.cache/golangci-lint: permission denied
#
# you probably have to fix the SELinux security context for root directory plus your cache
#
#   chcon -Rt svirt_sandbox_file_t .
#   chcon -Rt svirt_sandbox_file_t ~/.cache/golangci-lint
check:
ifdef CI
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run \
		-v --max-same-issues=50 --timeout=20m
else
	mkdir -p ~/.cache/golangci-lint/$(GOLANGCI_LINT_VERSION)
	$(CONTAINER_ENGINE) run -t --rm \
		--volume $(shell pwd):/data \
		--volume ~/.cache/golangci-lint/$(GOLANGCI_LINT_VERSION):/root/.cache \
		--workdir /data \
		--env GOFLAGS="-tags=acceptance" \
		golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run \
		-v --max-same-issues=50 --timeout=20m
	$(CONTAINER_ENGINE) run -t --rm \
		--volume $(shell pwd):/data \
		--workdir=/data \
		quay.io/helmpack/chart-testing:v3.14.0 ct lint \
		--all \
		--chart-dirs charts/cinder-csi-plugin \
		--chart-dirs charts/manila-csi-plugin \
		--chart-dirs charts/openstack-cloud-controller-manager
endif
.PHONY: check

unit: work
	go test -tags=unit $(shell go list ./... | sed -e '/sanity/ { N; d; }' | sed -e '/tests/ {N; d;}') $(TESTARGS)

functional:
	@echo "$@ not yet implemented"

test-cinder-csi-sanity: work
	go test $(GIT_HOST)/$(BASE_DIR)/tests/sanity/cinder

test-manila-csi-sanity: work
	go test $(GIT_HOST)/$(BASE_DIR)/tests/sanity/manila

# kept for compatibility reasons.
fmt: check
lint: check
vet: check

cover: work
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
	rm -rf _dist .bindep
	@echo "clean builds binary"
	@for binary in $(BUILD_CMDS); do rm -rf $${binary}*; done

realclean: clean
	rm -rf vendor
	if [ "$(GOPATH)" = "$(GOPATH_DEFAULT)" ]; then \
		rm -rf $(GOPATH); \
	fi

shell:
	$(SHELL) -i

# Build a single image for the local default platform and push to the local
# container engine
build-local-image-%:
	$(CONTAINER_ENGINE) buildx build --output type=docker \
		--build-arg VERSION=$(VERSION) \
		--tag $(REGISTRY)/$*:$(VERSION) \
		--target $* \
		.

# Build all images locally
build-local-images: $(addprefix build-local-image-,$(IMAGE_NAMES))

# Build a single image for all architectures in ARCHS and push it to REGISTRY
push-multiarch-image-%:
	$(CONTAINER_ENGINE) buildx build --output type=registry \
		--build-arg VERSION=$(VERSION) \
		--tag $(REGISTRY)/$*:$(VERSION) \
		--platform $(shell echo $(addprefix linux/,$(ARCHS)) | sed 's/ /,/g') \
		--target $* \
		.

# Push all multiarch images
push-multiarch-images: $(addprefix push-multiarch-image-,$(IMAGE_NAMES))

version:
	@echo ${VERSION}

.PHONY: build-cross
build-cross: work
ifndef HAS_GOX
	echo "installing gox"
	go install github.com/mitchellh/gox
endif
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/openstack-cloud-controller-manager/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/cinder-csi-plugin/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/k8s-keystone-auth/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/client-keystone-auth/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/octavia-ingress-controller/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/manila-csi-plugin/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/magnum-auto-healer/

.PHONY: dist
dist: build-cross
	( \
		cd _dist && \
		$(DIST_DIRS) cp ../LICENSE {} \; && \
		$(DIST_DIRS) cp ../README.md {} \; && \
		$(DIST_DIRS) tar -zcf cloud-provider-openstack-$(VERSION)-{}.tar.gz {} \; && \
		$(DIST_DIRS) zip -r cloud-provider-openstack-$(VERSION)-{}.zip {} \; \
	)

.PHONY: bindep build clean cover work docs fmt functional lint realclean \
	relnotes test translation version build-cross dist codeclimate
