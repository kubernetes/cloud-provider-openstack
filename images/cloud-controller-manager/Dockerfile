FROM registry.svc.ci.openshift.org/ocp/builder:rhel-8-golang-1.15-openshift-4.6
WORKDIR /go/src/k8s.io/cloud-provider-openstack
COPY ./ ./
RUN go build -o openstack-cloud-controller-manager ./cmd/openstack-cloud-controller-manager

FROM registry.svc.ci.openshift.org/ocp/4.6:base

COPY --from=0 /go/src/k8s.io/cloud-provider-openstack/openstack-cloud-controller-manager /usr/bin/

ENTRYPOINT ["/usr/bin/openstack-cloud-controller-manager"]
