ARG GATUS_VERSION=latest
FROM ghcr.io/miista/gatus:${GATUS_VERSION} AS gatus

# --platform=$BUILDPLATFORM pins this stage to the host arch (no QEMU) even
# when buildx is asked for other target platforms; TARGETOS/TARGETARCH (set
# automatically by buildx per platform in the build matrix) tell the native
# Go toolchain what to cross-compile for instead. Go's cross-compilation is
# fast and needs no emulation, unlike running the whole build under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o gatus-wrapper .

FROM alpine:latest
COPY --from=gatus /gatus /gatus
COPY --from=builder /build/gatus-wrapper /gatus-wrapper
ENV GATUS_CONFIG_PATH=/tmp/config.yaml
COPY entrypoint.sh /entrypoint.sh
COPY fallback.yaml /etc/gatus/fallback.yaml
COPY config.yaml /etc/gatus/config.yaml
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/gatus-wrapper"]
