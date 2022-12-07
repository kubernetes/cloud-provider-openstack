FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.19-openshift-4.13
WORKDIR /go/src/k8s.io/cloud-provider-openstack
COPY . .
RUN go build -o openstack-cloud-controller-manager ./cmd/openstack-cloud-controller-manager

FROM registry.ci.openshift.org/ocp/4.13:base

COPY --from=0 /go/src/k8s.io/cloud-provider-openstack/openstack-cloud-controller-manager /usr/bin/

ENTRYPOINT ["/usr/bin/openstack-cloud-controller-manager"]
