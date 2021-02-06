FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.15-openshift-4.8
COPY . /go/src/k8s.io/cloud-provider-openstack
RUN cd /go/src/k8s.io/cloud-provider-openstack && \
    go build -o manila-csi-plugin cmd/manila-csi-plugin/main.go

FROM registry.ci.openshift.org/ocp/4.8:base

COPY --from=0 /go/src/k8s.io/cloud-provider-openstack/manila-csi-plugin /usr/bin/

ENTRYPOINT ["/usr/bin/manila-csi-plugin"]
