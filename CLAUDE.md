# CLAUDE.md

## What this is

A drop-in wrapper image around [TwiN/gatus](https://github.com/TwiN/gatus) that auto-discovers
monitored endpoints from Docker container labels (`gatus.io/*`). Gatus normally requires every
endpoint to be declared statically in a config file; this wrapper lets monitoring config live
alongside the service it monitors (in the same `docker-compose.yml` entry) by scanning running
containers at startup and generating Gatus endpoint definitions from their labels.

## Build / run / test

```sh
# Build locally (defaults to GATUS_VERSION=latest)
docker build -t gatus-wrapper:latest .
docker build -t gatus-wrapper:latest --build-arg GATUS_VERSION=v5.36.0 .

# Run unit tests
go test ./...

# Run the test stack (gatus-wrapper + ntfy + a Caddy target with gatus.io/url labels)
NTFY_TOKEN=... docker compose -f docker-compose.test.yml up --build
# Gatus UI at http://localhost:8080 ; ntfy at http://localhost:8090
```

## Architecture

- **`Dockerfile`** — multi-stage build: copies `/gatus` from `twinproduction/gatus:${GATUS_VERSION}`, builds the Go wrapper (`main.go`) with `golang:1.26-alpine`, and produces a minimal `alpine` runtime image. `ENTRYPOINT` is `/gatus-wrapper`. `GATUS_CONFIG_PATH=/tmp/config.yaml`.
- **`main.go`** — the core logic as a Go program. Uses the Docker API directly (`github.com/docker/docker`) and `gopkg.in/yaml.v3` for YAML. All paths overridable via env vars (`DEFAULTS_PATH`, `OVERRIDES_PATH`, `MERGED_PATH`, `FALLBACK_PATH`, `GATUS_BIN`, `DOCKER_SOCKET`).
- **`main_test.go`** — 36 unit tests covering deep merge, label discovery, DNS resolver injection, alerting injection, fallback, and wrapper-only key consumption.
- **`config.yaml`** — internal default config baked in at `/etc/gatus/config.yaml` (just `server.port: 8080`).
- **`fallback.yaml`** — baked in at `/etc/gatus/fallback.yaml`; used only when no endpoints exist at all.
- **`entrypoint.sh`** — legacy shell entrypoint, kept for reference but no longer the `ENTRYPOINT`.

`generateConfig` flow:
1. Deep-merge `/config/config.yaml` (user overrides) on top of `/etc/gatus/config.yaml` (defaults) into `/tmp/config.yaml`.
2. Extract wrapper-only keys: `client.dns-resolver` and `default.endpoints.interval` (consumed by the wrapper, not passed to gatus).
3. Discover running containers via Docker API; find those with `gatus.io/url` label and `gatus.io/enabled != "false"`, emitting one endpoint per space-separated URL.
4. For label-discovered endpoints: inject `client.dns-resolver` if the URL hostname contains a dot (external) and no `client` block is set. `gatus.io/dns-resolver` label overrides per endpoint.
5. If no endpoints at all, append `fallback.yaml`.
6. Auto-inject configured alerting providers into every endpoint missing an `alerts:` block.
7. Inject `client.dns-resolver` into all endpoints (including manually-defined ones) with external hostnames and no existing `client:` block.
8. Write `/tmp/config.yaml`. Gatus hot-reloads on file change.

The program then launches `/gatus` as a subprocess, watches Docker events (`start`/`die`), and regenerates config on changes. Exits when gatus exits.

## Conventions / supported labels

- `gatus.io/url` — space-separated URLs; presence enables monitoring. Multiple URLs → one endpoint per URL (named by URL); single URL uses the container name.
- `gatus.io/enabled` — `"false"` disables (default `"true"`).
- `gatus.io/interval` — check interval; overrides `default.endpoints.interval` from config (default `1m`).
- `gatus.io/conditions` — default `[STATUS] == 200`.
- `gatus.io/dns-resolver` — per-endpoint DNS resolver (e.g. `udp://1.1.1.1:53`); overrides global `client.dns-resolver`.
- Multiple URLs share one interval/conditions; for per-URL settings, add endpoints manually to `config.yaml`.
- Gatus and monitored containers must share a Docker network; the Docker socket must be mounted (`/var/run/docker.sock:ro`).
- After changing labels: recreate the target (`docker compose up -d --force-recreate <svc>`) — config regenerates automatically via Docker event watch.

## Wrapper-only config keys

These keys in the user-mounted `config.yaml` are consumed by the wrapper and stripped before gatus sees the config:

- `client.dns-resolver` — injected as `client.dns-resolver` on every endpoint whose URL hostname contains a dot (external) and has no existing `client:` block.
- `default.endpoints.interval` — default check interval for label-discovered endpoints.

## Releases / tagging

`.github/workflows/build.yml` runs daily (and on push / manual dispatch). It checks the upstream
Gatus latest release against `.version`; if newer (or on a push to `main`) it builds a multi-arch
(`amd64`/`arm64`) image and pushes to `ghcr.io/miista/gatus-wrapper` with **three** tags:

| Tag | Example | Mutable | Use for |
|-----|---------|---------|---------|
| `<version>-g<sha>` | `v5.36.0-ga1b2c3d` | No (never reused) | Pinning / reproducible deploys |
| `<version>` | `v5.36.0` | Yes (moves to newest build of that version) | Drop-in upstream tracking |
| `latest` | `latest` | Yes | Always-newest |

For reproducibility, **pin to the immutable `<version>-g<sha>` tag plus its `@sha256:` digest** —
the plain `<version>` tag is mutable and gets reassigned if the wrapper is rebuilt against the
same Gatus version. `.version` is auto-committed by the workflow on scheduled/manual upstream
bumps (not on plain pushes).
