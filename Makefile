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

VERSION ?= v0.1.0
REGISTRY ?= openstack

# CTI targets

depend: work
	cd $(DEST) && glide install --strip-vendor

depend-update: work
	cd $(DEST) && glide update

build: depend
	cd $(DEST) && CGO_ENABLED=0 GOOS=linux go build -o openstack-cloud-controller-manager main.go

test: unit functional

unit: depend
	cd $(DEST) && go test -tags=unit $(shell glide novendor)

functional:
	@echo "$@ not yet implemented"

fmt: work
	cd $(DEST) && CGO_ENABLED=0 go fmt ./...

lint:
	@echo "$@ not yet implemented"

cover:
	@echo "$@ not yet implemented"

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
	rm -rf .bindep openstack-cloud-controller-manager

realclean: clean
	rm -rf vendor
	if [ "$(GOPATH)" = "$(GOPATH_DEFAULT)" ]; then \
		rm -rf $(GOPATH); \
	fi

shell: work
	cd $(DEST) && $(SHELL) -i

build-image: build
	docker build -t $(REGISTRY)/openstack-cloud-controller-manager:$(VERSION) .

.PHONY: bindep build clean cover depend docs fmt functional lint realclean \
	relnotes test translation
