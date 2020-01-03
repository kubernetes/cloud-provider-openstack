FROM openshift/origin-release:golang-1.13
COPY . /go/src/k8s.io/cloud-provider-openstack
RUN cd /go/src/k8s.io/cloud-provider-openstack && \
    go build -o manila-csi-plugin cmd/manila-csi-plugin/main.go

FROM registry.svc.ci.openshift.org/ocp/4.2:base

COPY --from=0 /go/src/k8s.io/cloud-provider-openstack/manila-csi-plugin /usr/bin/

ENTRYPOINT ["/usr/bin/manila-csi-plugin"]
