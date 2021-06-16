FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.16-openshift-4.9
COPY . /go/src/k8s.io/cloud-provider-openstack
RUN cd /go/src/k8s.io/cloud-provider-openstack && \
    go build -o cinder-csi-plugin cmd/cinder-csi-plugin/main.go

FROM registry.ci.openshift.org/ocp/4.9:base

# Get mkfs & blkid
RUN yum update -y && \
    yum install --setopt=tsflags=nodocs -y e2fsprogs xfsprogs util-linux systemd-udev && \
    yum clean all && rm -rf /var/cache/yum/*

COPY --from=0 /go/src/k8s.io/cloud-provider-openstack/cinder-csi-plugin /usr/bin/

ENTRYPOINT ["/usr/bin/cinder-csi-plugin"]
