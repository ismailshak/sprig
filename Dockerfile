FROM --platform=$BUILDPLATFORM golang:1.27.1-bookworm AS build

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
# Shared by both go build lines because the std layer is only reused under the same CGO_ENABLED
ENV CGO_ENABLED=0

# Downloads the modules and compiles the standard library in a layer the source copy below does not invalidate
COPY go.mod go.sum ./
RUN go mod download && GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build std

COPY . .

# The e2e image passes dev for the development sign-in and the published image passes nothing
# Declared after the std layer because an ARG is part of the cache key of every RUN that follows it
ARG GO_TAGS=""

RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -tags "${GO_TAGS}" -ldflags='-s -w' -o /out/sprig ./cmd/sprig

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=build /out/sprig /sprig

# The base image's nonroot uid. Pinned so the base image bump cannot change who owns the photo directory
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/sprig"]
