# Based on centos
FROM centos:7.4.1708
LABEL maintainers="Kubernetes Authors"
LABEL description="Cinder CSI Plugin"

# Install e4fsprogs for format
RUN yum -y install e4fsprogs

ADD cinder-csi-plugin /bin/

CMD ["/bin/cinder-csi-plugin"]