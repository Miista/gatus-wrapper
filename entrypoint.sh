#!/bin/sh
set -e

DEFAULTS=/etc/gatus/config.yaml
OVERRIDES=/config/config.yaml
MERGED=/tmp/config.yaml

generate_config() {
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
  ENDPOINTS=$(docker ps -q | xargs -r docker inspect | \
  jq -r --arg alerts "$ALERTS_BLOCK" '.[] | select(.Config.Labels["gatus.io/url"] != null) | select((.Config.Labels["gatus.io/enabled"] // "true") == "true") | {
    name: .Name[1:],
    url:        .Config.Labels["gatus.io/url"],
    interval:   (.Config.Labels["gatus.io/interval"]   // "1m"),
    conditions: (.Config.Labels["gatus.io/conditions"] // "[STATUS] == 200")
  } | "  - name: \(.name)\n    url: \(.url)\n    interval: \(.interval)\n    conditions:\n      - \"\(.conditions)\"\n\($alerts)"')

  if [ -z "$ENDPOINTS" ]; then
    cat /etc/gatus/fallback.yaml >> "$MERGED"
  else
    printf "\nendpoints:\n%s\n" "$ENDPOINTS" >> "$MERGED"
  fi
}

# Generate initial config and start gatus
generate_config
/gatus &
GATUS_PID=$!

# Watch for container changes and regenerate config (gatus hot-reloads on file change)
docker events --filter type=container --filter event=start --filter event=die | while read -r event; do
  echo "gatus: container event detected, regenerating config"
  generate_config
done &

# Exit when gatus exits (Docker will restart the container)
wait $GATUS_PID
