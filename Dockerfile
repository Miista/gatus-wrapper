ARG GATUS_VERSION=latest
FROM twinproduction/gatus:${GATUS_VERSION} AS gatus

FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o gatus-wrapper .

FROM alpine:latest
COPY --from=gatus /gatus /gatus
COPY --from=builder /build/gatus-wrapper /gatus-wrapper
ENV GATUS_CONFIG_PATH=/tmp/config.yaml
COPY entrypoint.sh /entrypoint.sh
COPY fallback.yaml /etc/gatus/fallback.yaml
COPY config.yaml /etc/gatus/config.yaml
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/gatus-wrapper"]
