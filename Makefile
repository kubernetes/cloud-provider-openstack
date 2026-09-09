GIT_HOST = k8s.io

CONTAINER_ENGINE ?= docker

PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))
# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
export GO111MODULE := on
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)
PKG := $(shell awk '/^module/ { print $$2 }' go.mod)

GOOS		?= $(shell go env GOOS)
GOPROXY		?= $(shell go env GOPROXY)
VERSION         ?= $(shell git describe --dirty --tags --match='v*')
GOARCH		:=
LDFLAGS		:= "-w -s -X 'k8s.io/component-base/version.gitVersion=$(VERSION)' -X 'k8s.io/cloud-provider-openstack/pkg/version.Version=$(VERSION)'"
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
GOLANGCI_LINT_VERSION?=v2.13.2

# CTI targets

build-all-archs:
	@for arch in $(ARCHS); do $(MAKE) ARCH=$${arch} build ; done

.PHONY: build
build: $(BUILD_CMDS)

$(BUILD_CMDS):
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOPROXY=${GOPROXY} go build \
		-trimpath \
		-ldflags $(LDFLAGS) \
		-o $@ \
		cmd/$@/main.go

.PHONY: test
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
.PHONY: check
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

.PHONY: unit
unit:
	go test -tags=unit $(shell go list ./... | sed -e '/sanity/ { N; d; }' | sed -e '/tests/ {N; d;}') $(TESTARGS)

.PHONY: functional
functional:
	@echo "$@ not yet implemented"

.PHONY: test-cinder-csi-sanity
test-cinder-csi-sanity:
	go test $(GIT_HOST)/$(BASE_DIR)/tests/sanity/cinder

.PHONY: test-manila-csi-sanity
test-manila-csi-sanity:
	go test $(GIT_HOST)/$(BASE_DIR)/tests/sanity/manila

# kept for compatibility reasons.
.PHONY: fmt
fmt: check
.PHONY: lint
lint: check
.PHONY: vet
vet: check

.PHONY: cover
cover:
	go test -tags=unit $(shell go list ./...) -cover

# Do the work here

# Set up the development environment
env:
	@echo "PWD: $(PWD)"
	@echo "BASE_DIR: $(BASE_DIR)"
	@echo "GOPATH: $(GOPATH)"
	@echo "GOROOT: $(GOROOT)"
	@echo "PKG: $(PKG)"
	go version
	go env

# Get our dev/test dependencies in place
bootstrap:
	tools/test-setup.sh

.PHONY: clean
clean:
	@echo "clean builds binary"
	@for binary in $(BUILD_CMDS); do rm -rf $${binary}*; done

.PHONY: realclean
realclean: clean
	rm -rf vendor
	if [ "$(GOPATH)" = "$(GOPATH_DEFAULT)" ]; then \
		rm -rf $(GOPATH); \
	fi

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

.PHONY: version
version:
	@echo ${VERSION}
