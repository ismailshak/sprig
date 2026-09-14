FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG DIST=dist # Compose overrides this to use the development build
ARG TARGETOS
ARG TARGETARCH

COPY ${DIST}/${TARGETOS}/${TARGETARCH}/sprig /sprig

# The base image's nonroot uid. Pinned so the base image bump cannot change who owns the photo directory
USER 65532:65532

EXPOSE 8080

# The image has no shell, curl or wget, so the binary checks itself: sprig
# health GETs /healthz on SPRIG_ADDR. During the start period the check runs
# every second, so compose up --wait returns as soon as the server is up
# instead of five minutes later.
HEALTHCHECK --interval=5m --timeout=5s --start-period=30s --start-interval=1s CMD ["/sprig", "health"]

ENTRYPOINT ["/sprig"]
