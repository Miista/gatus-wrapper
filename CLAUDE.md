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

# Run the test stack (gatus-wrapper + ntfy + a Caddy target with gatus.io/url labels)
NTFY_TOKEN=... docker compose -f docker-compose.test.yml up --build
# Gatus UI at http://localhost:8080 ; ntfy at http://localhost:8090
```

There is no Go source or unit-test suite in this repo — the wrapper is a shell entrypoint plus
config. "Testing" means running `docker-compose.test.yml` and observing the generated endpoints
in the Gatus UI / API (`/api/v1/endpoints/statuses`).

## Architecture

- **`Dockerfile`** — copies the `/gatus` binary out of `twinproduction/gatus:${GATUS_VERSION}`
  into an `alpine` base with `jq`, `yq`, `docker-cli` added. `ENTRYPOINT` is `entrypoint.sh`.
  `GATUS_CONFIG_PATH=/tmp/config.yaml` (the generated/merged file).
- **`config.yaml`** — internal default config baked in at `/etc/gatus/config.yaml` (just
  `server.port: 8080`).
- **`fallback.yaml`** — baked in at `/etc/gatus/fallback.yaml`; a self-check endpoint used only
  when no overrides and no labeled containers exist.
- **`entrypoint.sh`** — the core logic. On startup and on every relevant Docker event it runs
  `generate_config`, then launches `/gatus` and watches `docker events`.

`generate_config` flow:
1. Deep-merge optional `/config/config.yaml` (user mount) on top of the baked-in defaults via
   `yq eval-all` into `/tmp/config.yaml`.
2. `docker ps | docker inspect | jq` to find containers with `gatus.io/url` set and
   `gatus.io/enabled != "false"`, emitting one endpoint per space-separated URL.
3. If no label endpoints **and** no manual `endpoints:` in the merged config, append
   `fallback.yaml`. Otherwise merge label endpoints into the existing `endpoints:` array.
4. Auto-inject every configured `alerting:` provider into any endpoint lacking its own `alerts:`
   block (opt out per endpoint with `alerts: []`).

Gatus hot-reloads on config-file change, so regenerating `/tmp/config.yaml` is enough to pick up
new containers; the entrypoint regenerates on container `start`/`die` events.

## Conventions / supported labels

- `gatus.io/url` — space-separated URLs; presence enables monitoring. Multiple URLs → one
  endpoint per URL (endpoint name is the URL; single URL uses the container name).
- `gatus.io/enabled` — `"false"` disables (default `"true"`).
- `gatus.io/interval` — default `1m`.
- `gatus.io/conditions` — default `[STATUS] == 200`.
- Multiple URLs share one interval/conditions; for per-URL settings, add endpoints manually to
  `config.yaml`.
- Gatus and monitored containers must share a Docker network; the Docker socket must be mounted
  (`/var/run/docker.sock:ro`).
- After changing labels: recreate the target (`docker compose up -d --force-recreate <svc>`)
  **and** restart gatus (`docker restart gatus`) to regenerate config.

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
