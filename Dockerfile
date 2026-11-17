FROM gcr.io/distroless/static-debian13:nonroot
ARG TARGETARCH
COPY dist/oci-vault-resolver-linux-${TARGETARCH} /oci-vault-resolver
USER 65532:65532
ENTRYPOINT ["/oci-vault-resolver"]
