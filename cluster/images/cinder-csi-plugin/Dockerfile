# Based on centos
FROM centos:7.4.1708
LABEL maintainers="Kubernetes Authors"
LABEL description="Cinder CSI Plugin"

# Install e4fsprogs for format
RUN yum -y install e4fsprogs \
    && yum clean all

# NOTE(flaper87): This should be removed in the future
# as soon as there's an RPM for python-brick-cinderclient-ext
# Not merging into the RUN above to avoid invalidating the
# image layer when this section will be removed
RUN yum install -y epel-release \
    && yum -y install python-pip \
    && pip install python-brick-cinderclient-ext \
    && yum clean all

ADD cinder-csi-plugin /bin/

CMD ["/bin/cinder-csi-plugin"]