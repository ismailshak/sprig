FROM --platform=$BUILDPLATFORM golang:1.27.1-bookworm AS build

WORKDIR /src

COPY . .

ARG TARGETOS
ARG TARGETARCH
# compose passes dev so the e2e suite has the development sign-in, and the published image passes nothing
ARG GO_TAGS=""

# Mounts Go's module and build caches to keep rebuilds short.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -tags "${GO_TAGS}" -ldflags='-s -w' -o /out/sprig ./cmd/sprig

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

# Copies the compiled binary over from the build stage above.
COPY --from=build /out/sprig /sprig

EXPOSE 8080

ENTRYPOINT ["/sprig"]
