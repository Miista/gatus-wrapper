# gatus-wrapper

A wrapper image around [TwiN/gatus](https://github.com/TwiN/gatus) that adds support for auto-discovering monitored endpoints from Docker container labels.

## The problem

Gatus requires all endpoints to be defined statically in a config file. This means every time you add a new service, you need to update a separate config file — the monitoring config lives apart from the service it monitors.

## How it works

This image ships a minimal default config internally. At startup, it:

1. Deep-merges an optional `/config/config.yaml` on top of the defaults using [yq](https://github.com/mikefarah/yq)
2. Scans all running Docker containers for `gatus.io/*` labels
3. Generates endpoint definitions from those labels
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

## Supported labels

| Label | Description | Default |
|---|---|---|
| `gatus.io/url` | URL Gatus will probe. Presence of this label enables monitoring. | — |
| `gatus.io/enabled` | Set to `"false"` to disable monitoring even if `gatus.io/url` is set. | `"true"` |
| `gatus.io/interval` | How often to check | `1m` |
| `gatus.io/conditions` | Gatus condition expression | `[STATUS] == 200` |

## Limitations

- Each container supports only one monitored endpoint. For multiple checks on the same service, add them manually to `config.yaml`.
- Gatus and the containers it monitors must share the same Docker network.
- The Docker socket must be mounted into the gatus-wrapper container.

## Automatic updates

A GitHub Actions workflow runs daily and checks for new releases of the upstream Gatus image. When a new version is detected, it automatically builds and publishes a new multi-arch image (`linux/amd64`, `linux/arm64`) to GHCR.
