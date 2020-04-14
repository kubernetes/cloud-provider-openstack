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
export GO111MODULE := on
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)
PKG := $(shell awk '/^module/ { print $$2 }' go.mod)
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)
SOURCES := $(shell find $(DEST) -name '*.go' 2>/dev/null)
HAS_LINT := $(shell command -v golint;)
HAS_GOX := $(shell command -v gox;)
HAS_IMPORT_BOSS := $(shell command -v import-boss;)
GOX_PARALLEL ?= 3

TARGETS		?= darwin/amd64 linux/amd64 linux/386 linux/arm linux/arm64 linux/ppc64le linux/s390x
DIST_DIRS	= find * -type d -exec

TEMP_DIR	:=$(shell mktemp -d)
TAR_FILE	?= rootfs.tar

GOOS		?= $(shell go env GOOS)
VERSION		?= $(shell git describe --exact-match 2> /dev/null || \
			   git describe --match=$(git rev-parse --short=8 HEAD) --always --dirty --abbrev=8)
ALPINE_ARCH	:=
DEBIAN_ARCH	:=
QEMUARCH	:=
QEMUVERSION	:= "v4.2.0-4"
GOARCH		:=
GOFLAGS		:=
TAGS		:=
LDFLAGS		:= "-w -s -X 'k8s.io/cloud-provider-openstack/pkg/version.Version=${VERSION}'"
REGISTRY	?= k8scloudprovider
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

# This option is for running docker manifest command
export DOCKER_CLI_EXPERIMENTAL := enabled

# CTI targets

$(GOBIN):
	echo "create gobin"
	mkdir -p $(GOBIN)

work: $(GOBIN)

ifeq ($(ARCH),arm)
    DEBIAN_ARCH=$(ARCH)
    GOARCH=$(ARCH)
    QEMUARCH=$(ARCH)
    ALPINE_ARCH=arm32v7
else ifeq ($(ARCH),arm64)
    DEBIAN_ARCH=$(ARCH)
    GOARCH=$(ARCH)
    QEMUARCH=aarch64
    ALPINE_ARCH=arm64v8
else
    DEBIAN_ARCH=$(ARCH)
    GOARCH=$(ARCH)
    QEMUARCH=$(ARCH)
    ALPINE_ARCH=$(ARCH)
endif

build-all-archs:
	@for arch in $(ARCHS); do $(MAKE) ARCH=$${arch} build ; done

build: $(addprefix build-cmd-,$(BUILD_CMDS))

# Remove individual go build targets, once we migrate openlab-zuul-jobs
# to use new build-cmd-% targets.
cinder-csi-plugin: work $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o cinder-csi-plugin \
		cmd/cinder-csi-plugin/main.go

manila-csi-plugin: work $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o manila-csi-plugin \
		cmd/manila-csi-plugin/main.go

# Remove this individual go build target, once we remove
# image-controller-manager below.
openstack-cloud-controller-manager: work $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o openstack-cloud-controller-manager-$(ARCH) \
		cmd/openstack-cloud-controller-manager/main.go

# Remove individual image builder once we migrate openlab-zuul-jobs
# to use new image-openstack-cloud-controller-manager target.
image-controller-manager: work openstack-cloud-controller-manager
ifeq ($(GOOS),linux)
	cp -r cluster/images/openstack-cloud-controller-manager $(TEMP_DIR)
	cp openstack-cloud-controller-manager-$(ARCH) $(TEMP_DIR)/openstack-cloud-controller-manager
	cp $(TEMP_DIR)/openstack-cloud-controller-manager/Dockerfile.build $(TEMP_DIR)/openstack-cloud-controller-manager/Dockerfile
	docker build -t $(REGISTRY)/openstack-cloud-controller-manager:$(VERSION) $(TEMP_DIR)/openstack-cloud-controller-manager
	rm -rf $(TEMP_DIR)/openstack-cloud-controller-manager
else
	$(error Please set GOOS=linux for building the image)
endif

build-cmd-%: work $(SOURCES)
	@# Keep binary with no arch mark. We should remove this once we correct
	@# openlab-zuul-jobs.
	CGO_ENABLED=0 GOOS=$(GOOS) go build \
		-ldflags $(LDFLAGS) \
		-o $* \
		cmd/$*/main.go
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags $(LDFLAGS) \
		-o $*-$(ARCH) \
		cmd/$*/main.go

test: unit functional

check: work fmt vet lint import-boss

unit: work
	go test -tags=unit $(shell go list ./... | sed -e '/sanity/ { N; d; }' | sed -e '/tests/ {N; d;}' | sed -e '/test/ {N; d;}') $(TESTARGS)

functional:
	@echo "$@ not yet implemented"

test-cinder-csi-sanity: work
	go test $(GIT_HOST)/$(BASE_DIR)/pkg/csi/cinder/sanity/

test-manila-csi-sanity: work
	go test $(GIT_HOST)/$(BASE_DIR)/pkg/csi/manila/sanity/

fmt:
	hack/verify-gofmt.sh

lint:
ifndef HAS_LINT
	echo "installing lint"
	GO111MODULE=off go get -u golang.org/x/lint/golint
endif
	hack/verify-golint.sh

import-boss:
ifndef HAS_IMPORT_BOSS
	echo "installing import-boss"
	go get -u k8s.io/code-generator/cmd/import-boss
endif
	hack/verify-import-boss.sh

vet:
	go vet ./...

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

push-manifest-%:
	docker manifest create --amend $(REGISTRY)/$*:$(VERSION) $(shell echo $(ARCHS) | sed -e "s~[^ ]*~$(REGISTRY)/$*\-&:$(VERSION)~g")
	@for arch in $(ARCHS); do docker manifest annotate --os $(IMAGE_OS) --arch $${arch} $(REGISTRY)/$*:${VERSION} $(REGISTRY)/$*-$${arch}:${VERSION}; done
	docker manifest push --purge $(REGISTRY)/$*:${VERSION}

push-all-manifest: $(addprefix push-manifest-,$(IMAGE_NAMES))

build-images: $(addprefix image-,$(IMAGE_NAMES))

push-images: $(addprefix push-image-,$(IMAGE_NAMES))

image-%: work
	$(MAKE) $(addprefix build-cmd-,$*)
ifeq ($(GOOS),linux)
	cp -r cluster/images/$* $(TEMP_DIR)

ifneq ($(ARCH),amd64)
	docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
	curl -sSL https://github.com/multiarch/qemu-user-static/releases/download/$(QEMUVERSION)/x86_64_qemu-$(QEMUARCH)-static.tar.gz | tar -xz -C $(TEMP_DIR)/$*
	@# Ensure we don't get surprised by umask settings
	chmod 0755 $(TEMP_DIR)/$*/qemu-$(QEMUARCH)-static
	sed "/^FROM .*/a COPY qemu-$(QEMUARCH)-static /usr/bin/" $(TEMP_DIR)/$*/Dockerfile.build > $(TEMP_DIR)/$*/Dockerfile.build.tmp
	mv $(TEMP_DIR)/$*/Dockerfile.build.tmp $(TEMP_DIR)/$*/Dockerfile.build
endif

	cp $*-$(ARCH) $(TEMP_DIR)/$*
	docker build --build-arg ALPINE_ARCH=$(ALPINE_ARCH) --build-arg ARCH=$(ARCH) --build-arg DEBIAN_ARCH=$(DEBIAN_ARCH) --pull -t build-$*-$(ARCH) -f $(TEMP_DIR)/$*/Dockerfile.build $(TEMP_DIR)/$*
	docker create --name build-$*-$(ARCH) build-$*-$(ARCH)
	docker export build-$*-$(ARCH) > $(TEMP_DIR)/$*/$(TAR_FILE)

	@echo "build image $(REGISTRY)/$*-$(ARCH)"
	docker build --build-arg ALPINE_ARCH=$(ALPINE_ARCH) --build-arg ARCH=$(ARCH) --build-arg DEBIAN_ARCH=$(DEBIAN_ARCH) --pull -t $(REGISTRY)/$*-$(ARCH):$(VERSION) $(TEMP_DIR)/$*

	rm -rf $(TEMP_DIR)/$*
	docker rm build-$*-$(ARCH)
	docker rmi build-$*-$(ARCH)
else
	$(error Please set GOOS=linux for building the image)
endif

push-image-%:
	@echo "push image $*-$(ARCH) to $(REGISTRY)"
ifneq ($(and $(DOCKER_USERNAME),$(DOCKER_PASSWORD)),)
	@docker login -u="$(DOCKER_USERNAME)" -p="$(DOCKER_PASSWORD)"
endif
	docker push $(REGISTRY)/$*-$(ARCH):$(VERSION)

images: $(addprefix build-arch-image-,$(ARCH))

images-all-archs: $(addprefix build-arch-image-,$(ARCHS))

build-arch-image-%:
	@echo "Building images for ARCH=$*"
	$(MAKE) ARCH=$* build-images

upload-image-%:
	$(MAKE) ARCH=$* build-images push-images

upload-images: $(addprefix upload-image-,$(ARCHS)) push-all-manifest

version:
	@echo ${VERSION}

.PHONY: build-cross
build-cross: LDFLAGS += -extldflags "-static"
build-cross: work
ifndef HAS_GOX
	echo "installing gox"
	GO111MODULE=off go get -u github.com/mitchellh/gox
endif
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/openstack-cloud-controller-manager/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/cinder-csi-plugin/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/k8s-keystone-auth/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/client-keystone-auth/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/octavia-ingress-controller/
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

.PHONY: bindep build clean cover work docs fmt functional lint import-boss realclean \
	relnotes test translation version build-cross dist
