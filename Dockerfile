FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG DIST=dist # Compose overrides this to use the development build
ARG TARGETOS
ARG TARGETARCH

COPY ${DIST}/${TARGETOS}/${TARGETARCH}/sprig /sprig

# The base image's nonroot uid. Pinned so the base image bump cannot change who owns the photo directory
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/sprig"]
