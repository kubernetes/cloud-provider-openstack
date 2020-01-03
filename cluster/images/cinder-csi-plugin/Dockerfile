FROM k8s.gcr.io/debian-base-amd64:1.0.0
LABEL maintainers="Kubernetes Authors"
LABEL description="Cinder CSI Plugin"

# Install e4fsprogs for format
RUN clean-install ca-certificates e2fsprogs mount xfsprogs udev

ADD cinder-csi-plugin /bin/

CMD ["/bin/cinder-csi-plugin"]
