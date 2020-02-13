FROM alpine:3.11
LABEL maintainers="Kubernetes Authors"
LABEL description="Barbican KMS Plugin"

ADD barbican-kms-plugin /bin/

CMD ["sh", "-c", "/bin/barbican-kms-plugin --socketpath ${socketpath} --cloud-config ${cloudconfig}"]
