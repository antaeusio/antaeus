# Release container for the antaeus command. It packages the already-built,
# checksummed release binary for each architecture; nothing is compiled here.
# Build through scripts/container-image or the release workflow.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG TARGETARCH
ARG VERSION=dev
ARG REVISION=unknown

LABEL org.opencontainers.image.title="antaeus" \
      org.opencontainers.image.description="Open-source semantic policy engine and CLI" \
      org.opencontainers.image.source="https://github.com/antaeusio/antaeus" \
      org.opencontainers.image.url="https://antaeus.io" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

COPY --chmod=0555 .tmp/dist/antaeus_linux_${TARGETARCH} /usr/local/bin/antaeus

# Numeric non-root user from the distroless base; it has CA certificates.
USER 65532:65532
WORKDIR /work
ENTRYPOINT ["/usr/local/bin/antaeus"]
CMD ["help"]
