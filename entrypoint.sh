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
  jq -r --arg alerts "$ALERTS_BLOCK" '.[] | select(.Config.Labels["gatus.io/url"] != null) | select((.Config.Labels["gatus.io/enabled"] // "true") == "true") | . as $c |
    (.Config.Labels["gatus.io/url"] | split(" ")) as $urls |
    (.Config.Labels["gatus.io/interval"] // "1m") as $interval |
    (.Config.Labels["gatus.io/conditions"] // "[STATUS] == 200") as $conditions |
    (($urls | length) > 1) as $multi |
    $urls | to_entries[] | {
      name: (if $multi then .value else $c.Name[1:] end),
      group: "",
      url: .value,
      interval: $interval,
      conditions: $conditions
    } | "  - name: \(.name)\n\(if .group != "" then "    group: \(.group)\n" else "" end)    url: \(.url)\n    interval: \(.interval)\n    conditions:\n      - \"\(.conditions)\"\n\($alerts)"')

  HAS_MANUAL=$(yq '.endpoints // [] | length' "$MERGED" 2>/dev/null || echo 0)

  if [ -z "$ENDPOINTS" ] && [ "$HAS_MANUAL" = "0" ]; then
    cat /etc/gatus/fallback.yaml >> "$MERGED"
  elif [ -n "$ENDPOINTS" ]; then
    LABEL_YAML=$(printf "endpoints:\n%s\n" "$ENDPOINTS")
    echo "$LABEL_YAML" | yq eval-all 'select(fi==0).endpoints = ((select(fi==0).endpoints // []) + select(fi==1).endpoints) | select(fi==0)' "$MERGED" - > "$MERGED.tmp" && mv "$MERGED.tmp" "$MERGED"
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
