# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

################################################################################
##                               BUILD ARGS                                   ##
################################################################################
# This build arg allows the specification of a custom Golang image.
ARG GOLANG_IMAGE=golang:1.20.12

# The distroless image on which the CPI manager image is built.
#
# Please do not use "latest". Explicit tags should be used to provide
# deterministic builds. Follow what kubernetes uses to build
# kube-controller-manager, for example for 1.27.x:
# https://github.com/kubernetes/kubernetes/blob/release-1.27/build/common.sh#L99
ARG DISTROLESS_IMAGE=registry.k8s.io/build-image/go-runner:v2.3.1-go1.20.12-bullseye.0

# We use Alpine as the source for default CA certificates and some output
# images
ARG ALPINE_IMAGE=alpine:3.17.3

# cinder-csi-plugin uses Debian as a base image
ARG DEBIAN_IMAGE=registry.k8s.io/build-image/debian-base:bullseye-v1.4.3

################################################################################
##                              BUILD STAGE                                   ##
################################################################################

# Build an image containing a common ca-certificates used by all target images
# regardless of how they are built. We arbitrarily take ca-certificates from
# the amd64 Alpine image.
FROM --platform=linux/amd64 ${ALPINE_IMAGE} as certs
RUN apk add --no-cache ca-certificates


# Build all command targets. We build all command targets in a single build
# stage for efficiency. Target images copy their binary from this image.
# We use go's native cross compilation for multi-arch in this stage, so the
# builder itself is always amd64
FROM --platform=linux/amd64 ${GOLANG_IMAGE} as builder

ARG GOPROXY=https://goproxy.io,direct
ARG TARGETOS
ARG TARGETARCH
ARG VERSION

WORKDIR /build
COPY Makefile go.mod go.sum ./
COPY cmd/ cmd/
COPY pkg/ pkg/
RUN make build GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOPROXY=${GOPROXY} VERSION=${VERSION}


################################################################################
##                             TARGET IMAGES                                  ##
################################################################################

##
## openstack-cloud-controller-manager
##
FROM --platform=${TARGETPLATFORM} ${DISTROLESS_IMAGE} as openstack-cloud-controller-manager

COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY --from=builder /build/openstack-cloud-controller-manager /bin/openstack-cloud-controller-manager

LABEL name="openstack-cloud-controller-manager" \
      license="Apache Version 2.0" \
      maintainers="Kubernetes Authors" \
      description="OpenStack cloud controller manager" \
      distribution-scope="public" \
      summary="OpenStack cloud controller manager" \
      help="none"

CMD [ "/bin/openstack-cloud-controller-manager" ]

##
## barbican-kms-plugin
##
FROM --platform=${TARGETPLATFORM} ${ALPINE_IMAGE} as barbican-kms-plugin
# barbican-kms-plugin uses ALPINE instead of distroless because its entrypoint
# uses a shell for environment substitution. If there are no other uses this
# could be replaced by callers passing arguments explicitly.

COPY --from=builder /build/barbican-kms-plugin /bin/barbican-kms-plugin
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

LABEL name="barbican-kms-plugin" \
      license="Apache Version 2.0" \
      maintainers="Kubernetes Authors" \
      description="Barbican kms plugin" \
      distribution-scope="public" \
      summary="Barbican kms plugin" \
      help="none"

CMD ["sh", "-c", "/bin/barbican-kms-plugin --socketpath ${socketpath} --cloud-config ${cloudconfig}"]

##
## cinder-csi-plugin
##
FROM --platform=${TARGETPLATFORM} ${DEBIAN_IMAGE} as cinder-csi-plugin

# Install e4fsprogs for format
RUN clean-install btrfs-progs e2fsprogs mount udev xfsprogs

COPY --from=builder /build/cinder-csi-plugin /bin/cinder-csi-plugin
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

LABEL name="cinder-csi-plugin" \
      license="Apache Version 2.0" \
      maintainers="Kubernetes Authors" \
      description="Cinder CSI Plugin" \
      distribution-scope="public" \
      summary="Cinder CSI Plugin" \
      help="none"

CMD ["/bin/cinder-csi-plugin"]

##
## k8s-keystone-auth
##
FROM --platform=${TARGETPLATFORM} ${DISTROLESS_IMAGE} as k8s-keystone-auth

COPY --from=builder /build/k8s-keystone-auth /bin/k8s-keystone-auth
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

LABEL name="k8s-keystone-auth" \
      license="Apache Version 2.0" \
      maintainers="Kubernetes Authors" \
      description="K8s Keystone Auth" \
      distribution-scope="public" \
      summary="K8s Keystone Auth" \
      help="none"

EXPOSE 8443

CMD ["/bin/k8s-keystone-auth"]

##
## magnum-auto-healer
##
FROM --platform=${TARGETPLATFORM} ${DISTROLESS_IMAGE} as magnum-auto-healer

COPY --from=builder /build/magnum-auto-healer /bin/magnum-auto-healer
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

LABEL name="magnum-auto-healer" \
      license="Apache Version 2.0" \
      maintainers="Kubernetes Authors" \
      description="Magnum auto healer" \
      distribution-scope="public" \
      summary="Magnum auto healer" \
      help="none"

CMD ["/bin/magnum-auto-healer"]

##
## manila-csi-plugin
##
FROM --platform=${TARGETPLATFORM} ${ALPINE_IMAGE} as manila-csi-plugin
# manila-csi-plugin uses ALPINE because it pulls in jq and curl

RUN apk add --no-cache jq curl

COPY --from=builder /build/manila-csi-plugin /bin/manila-csi-plugin
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

LABEL name="manila-csi-plugin" \
      license="Apache Version 2.0" \
      maintainers="Kubernetes Authors" \
      description="Manila CSI Plugin" \
      distribution-scope="public" \
      summary="Manila CSI Plugin" \
      help="none"

ENTRYPOINT ["/bin/manila-csi-plugin"]

##
## octavia-ingress-controller
##
FROM --platform=${TARGETPLATFORM} ${DISTROLESS_IMAGE} as octavia-ingress-controller

COPY --from=builder /build/octavia-ingress-controller /bin/octavia-ingress-controller
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

LABEL name="octavia-ingress-controller" \
      license="Apache Version 2.0" \
      maintainers="Kubernetes Authors" \
      description="Octavia ingress controller" \
      distribution-scope="public" \
      summary="Octavia ingress controller" \
      help="none"

CMD ["/bin/octavia-ingress-controller"]
