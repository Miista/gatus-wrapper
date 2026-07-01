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

  # Discover endpoints from Docker labels
  CONTAINER_NAMES=$(docker ps --format '{{.Names}}')
  DNS_RESOLVER=$(yq '.client["dns-resolver"] // ""' "$MERGED")
  DEFAULT_INTERVAL=$(yq '.default.endpoints.interval // ""' "$MERGED")
  ENDPOINTS=$(docker ps -q | xargs -r docker inspect | \
  jq -r --argjson names "$(echo "$CONTAINER_NAMES" | jq -R -s 'split("\n") | map(select(length > 0))')" --arg resolver "$DNS_RESOLVER" --arg default_interval "$DEFAULT_INTERVAL" '
    .[] | select(.Config.Labels["gatus.io/url"] != null) | select((.Config.Labels["gatus.io/enabled"] // "true") == "true") | . as $c |
    (.Config.Labels["gatus.io/url"] | split(" ")) as $urls |
    (.Config.Labels["gatus.io/interval"] // (if $default_interval != "" then $default_interval else "1m" end)) as $interval |
    (.Config.Labels["gatus.io/conditions"] // "[STATUS] == 200") as $conditions |
    (.Config.Labels["gatus.io/dns-resolver"] // null) as $label_resolver |
    (($urls | length) > 1) as $multi |
    $urls | to_entries[] |
    (.value | capture("^https?://(?<h>[^/:]+)") | .h) as $host |
    ($names | map(select(. == $host)) | length > 0) as $is_internal |
    ($label_resolver // (if ($is_internal or ($host | contains(".") | not)) or $resolver == "" then null else $resolver end)) as $effective_resolver |
    {
      name: (if $multi then .value else $c.Name[1:] end),
      url: .value,
      interval: $interval,
      conditions: $conditions,
      dns_resolver: $effective_resolver
    } | "  - name: \(.name)\n    url: \(.url)\n    interval: \(.interval)\n    conditions:\n      - \"\(.conditions)\"\(if .dns_resolver then "\n    client:\n      dns-resolver: \(.dns_resolver)" else "" end)"')

  HAS_MANUAL=$(yq '.endpoints // [] | length' "$MERGED" 2>/dev/null || echo 0)

  if [ -z "$ENDPOINTS" ] && [ "$HAS_MANUAL" = "0" ]; then
    cat /etc/gatus/fallback.yaml >> "$MERGED"
  elif [ -n "$ENDPOINTS" ]; then
    LABEL_YAML=$(printf "endpoints:\n%s\n" "$ENDPOINTS")
    echo "$LABEL_YAML" | yq eval-all 'select(fi==0).endpoints = ((select(fi==0).endpoints // []) + select(fi==1).endpoints) | select(fi==0)' "$MERGED" - > "$MERGED.tmp" && mv "$MERGED.tmp" "$MERGED"
  fi

  # Auto-inject configured alerting providers into every endpoint missing an alerts block
  yq -i '
    (.alerting // {} | keys | map({"type": .})) as $defaults |
    .endpoints |= map(.alerts = (.alerts // $defaults))
  ' "$MERGED"

  # Auto-inject client.dns-resolver into endpoints whose hostname is not a container name
  if [ -n "$DNS_RESOLVER" ]; then
    export _YQ_RESOLVER="$DNS_RESOLVER"
    yq -i 'with(.endpoints[] | select((.url | test("^https?://[^/]*\\.")) and (.client == null)); .client["dns-resolver"] = env(_YQ_RESOLVER))' "$MERGED"
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
