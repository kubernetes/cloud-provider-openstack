FROM alpine:3.7
LABEL maintainers="Kubernetes Authors"
LABEL description="Cinder CSI Plugin"

# Install e4fsprogs for format
RUN apk add --no-cache ca-certificates e2fsprogs eudev

ADD cinder-csi-plugin /bin/

CMD ["/bin/cinder-csi-plugin"]
