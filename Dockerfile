FROM twinproduction/gatus:latest AS gatus
FROM alpine:latest
RUN apk add --no-cache curl jq yq
COPY --from=gatus /gatus /gatus
ENV GATUS_CONFIG_PATH=/tmp/config.yaml
COPY entrypoint.sh /entrypoint.sh
COPY fallback.yaml /etc/gatus/fallback.yaml
COPY config.yaml /etc/gatus/config.yaml
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
