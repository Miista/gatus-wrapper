# gatus-wrapper

A wrapper image around [TwiN/gatus](https://github.com/TwiN/gatus) that adds support for auto-discovering monitored endpoints from Docker container labels.

## The problem

Gatus requires all endpoints to be defined statically in a config file. This means every time you add a new service, you need to update a separate config file — the monitoring config lives apart from the service it monitors.

## How it works

This image ships a minimal default config internally. At startup, it:

1. Deep-merges an optional `/config/config.yaml` on top of the defaults using [yq](https://github.com/mikefarah/yq)
2. Scans all running Docker containers for `gatus.io/*` labels
3. Generates endpoint definitions from those labels and appends them to any `endpoints:` already defined in `config.yaml`
4. Launches Gatus with the combined config

This means monitoring config lives alongside the service it monitors — in the same `docker-compose.yml` entry.

If no config is mounted and no labeled containers are found, the container starts with a fallback self-check endpoint.

## Usage

```yaml
services:
  gatus:
    image: ghcr.io/miista/gatus-wrapper:latest
    volumes:
      - ./gatus.yaml:/config/config.yaml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

  my-service:
    image: my-service:latest
    labels:
      gatus.io/url: "http://my-service:8080"
```

`gatus.yaml` contains only what you want to configure — typically alerting:

```yaml
alerting:
  ntfy:
    topic: alerts
    url: "http://ntfy:80"
    token: "$NTFY_TOKEN"
    default-alert:
      failure-threshold: 3
      success-threshold: 2
      send-on-resolved: true
```

## Manually-defined endpoints

For endpoints that aren't backed by a Docker container (external services, third-party APIs), add them directly to `config.yaml` under `endpoints:`. They are merged with the label-discovered endpoints:

```yaml
endpoints:
  - name: external-api
    url: https://api.example.com/health
    interval: 5m
    conditions:
      - "[STATUS] == 200"
```

Any configured alerting provider is auto-injected into every endpoint that doesn't declare its own `alerts:` block. To opt out for a specific endpoint, set `alerts: []` explicitly.

## Supported labels

| Label | Description | Default |
|---|---|---|
| `gatus.io/url` | Space-separated list of URLs Gatus will probe. Multiple URLs generate one endpoint per URL. Presence of this label enables monitoring. | — |
| `gatus.io/enabled` | Set to `"false"` to disable monitoring even if `gatus.io/url` is set. | `"true"` |
| `gatus.io/interval` | How often to check | `1m` |
| `gatus.io/conditions` | Gatus condition expression. Override if your service returns a different status code (e.g. `[STATUS] == 204`). | `[STATUS] == 200` |

## Adding or updating a monitored service

After adding or changing `gatus.io/*` labels on a service, two steps are required:

1. Recreate the service container to apply the new labels: `docker compose up -d --force-recreate <service>`
2. Restart gatus to regenerate the config: `docker restart gatus`

## Limitations

- Multiple URLs share the same `interval` and `conditions`. For different settings per URL, add endpoints manually to `config.yaml` (see [Manually-defined endpoints](#manually-defined-endpoints)).
- Gatus and the containers it monitors must share the same Docker network.
- The Docker socket must be mounted into the gatus-wrapper container.

## Automatic updates

A GitHub Actions workflow runs daily and checks for new releases of the upstream Gatus image. When a new version is detected, it automatically builds and publishes a new multi-arch image (`linux/amd64`, `linux/arm64`) to GHCR.
