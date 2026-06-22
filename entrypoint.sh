#!/bin/sh
set -e

DEFAULTS=/etc/gatus/config.yaml
OVERRIDES=/config/config.yaml
MERGED=/tmp/config.yaml

if [ -f "$OVERRIDES" ]; then
  yq eval-all 'select(fi==0) * select(fi==1)' "$DEFAULTS" "$OVERRIDES" > "$MERGED"
  echo "gatus: merged overrides from $OVERRIDES"
else
  cp "$DEFAULTS" "$MERGED"
  echo "gatus: no overrides, using defaults"
fi

# Build alerts block from configured providers in merged config
ALERT_TYPES=$(yq '.alerting | keys | .[]' "$MERGED" 2>/dev/null | sed 's/^/      - type: /' || true)
ALERTS_BLOCK=$(printf "    alerts:\n%s" "$ALERT_TYPES")

# Discover endpoints from Docker labels
ENDPOINTS=$(curl -sf --unix-socket /var/run/docker.sock http://localhost/containers/json | \
jq -r --arg alerts "$ALERTS_BLOCK" '.[] | select(.Labels["gatus.io/url"] != null) | {
  name: .Names[0][1:],
  url:        .Labels["gatus.io/url"],
  interval:   (.Labels["gatus.io/interval"]   // "1m"),
  conditions: (.Labels["gatus.io/conditions"] // "[STATUS] == 200")
} | "  - name: \(.name)\n    url: \(.url)\n    interval: \(.interval)\n    conditions:\n      - \"\(.conditions)\"\n\($alerts)"')

if [ -z "$ENDPOINTS" ]; then
  cat /etc/gatus/fallback.yaml >> "$MERGED"
else
  printf "\nendpoints:\n%s\n" "$ENDPOINTS" >> "$MERGED"
fi

exec /gatus
