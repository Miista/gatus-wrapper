ARG GATUS_VERSION=latest
FROM ghcr.io/miista/gatus:${GATUS_VERSION} AS gatus

FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o gatus-wrapper .

FROM alpine:latest
# tzdata: without it, TZ=Europe/Copenhagen silently falls back to UTC for
# anything reading the OS timezone database (container `date`, log
# timestamps) — the Gatus UI itself is unaffected since it converts to the
# browser's local time client-side, but `docker logs` stayed UTC-only,
# which caused a real mixup reconciling log lines against wall-clock times.
RUN apk add --no-cache tzdata
COPY --from=gatus /gatus /gatus
COPY --from=builder /build/gatus-wrapper /gatus-wrapper
ENV GATUS_CONFIG_PATH=/tmp/config.yaml
COPY entrypoint.sh /entrypoint.sh
COPY fallback.yaml /etc/gatus/fallback.yaml
COPY config.yaml /etc/gatus/config.yaml
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/gatus-wrapper"]
