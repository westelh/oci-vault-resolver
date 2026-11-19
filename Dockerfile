FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
ARG TARGETARCH
COPY dist/oci-vault-resolver-linux-${TARGETARCH} /oci-vault-resolver
USER 65532:65532
ENTRYPOINT ["/oci-vault-resolver"]
