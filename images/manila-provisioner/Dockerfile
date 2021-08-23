FROM openshift/origin-release:golang-1.16
COPY . /go/src/k8s.io/cloud-provider-openstack
RUN cd /go/src/k8s.io/cloud-provider-openstack && \
    go build -o manila-provisioner cmd/manila-provisioner/main.go

FROM registry.svc.ci.openshift.org/ocp/4.9:base

COPY --from=0 /go/src/k8s.io/cloud-provider-openstack/manila-provisioner /usr/bin/

ENTRYPOINT ["/usr/bin/manila-provisioner"]
