FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
ARG TARGETARCH
COPY dist/oci-vault-resolver-linux-${TARGETARCH} /oci-vault-resolver
USER 65532:65532
ENTRYPOINT ["/oci-vault-resolver"]
