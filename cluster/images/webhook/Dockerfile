FROM ubuntu:16.04
MAINTAINER Saverio Proto <zioproto@gmail.com>
RUN apt-get update && \
    apt-get install -y software-properties-common
RUN add-apt-repository -y ppa:masterminds/glide
RUN apt-get update && \
    apt-get install -y glide git build-essential golang && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
RUN git clone https://github.com/dims/k8s-keystone-auth
WORKDIR k8s-keystone-auth
RUN make depend && make build
EXPOSE 8443
CMD ./bin/k8s-keystone-auth --tls-cert-file ${API_SERVER_CERT} --tls-private-key-file ${API_SERVER_KEY} --keystone-url ${OPENSTACK_KEYSTONE_URL}

