# Production Deployment Guide

## Overview

`ai-efficiency` now ships with two production deployment paths:

- Docker Compose with the updater sidecar
- Linux systemd with backend binary self-update

The deploy assets in this directory cover two modes:

- bundled mode: local `postgres` + `redis` containers
- external mode: existing external `postgres` + `redis`

## Bundled Mode

```bash
cp deploy/.env.example deploy/.env
bash deploy/docker-deploy.sh
docker-compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
```

`deploy/docker-deploy.sh` will:

- create `deploy/.env` if it does not exist
- auto-generate `AE_AUTH_JWT_SECRET` if blank
- auto-generate `AE_ENCRYPTION_KEY` if blank
- auto-generate `POSTGRES_PASSWORD` for bundled mode if blank
- validate relay reachability and compose parsing before startup

## External Mode

```bash
cp deploy/.env.example deploy/.env
bash deploy/docker-deploy.sh external
docker-compose --env-file deploy/.env -f deploy/docker-compose.external.yml up -d
```

## Required Variables

At minimum, set these in `deploy/.env` before first deploy:

- `AE_RELAY_URL`
- `COMPOSE_PROJECT_NAME`
- `AE_UPDATER_IMAGE_REPOSITORY`
- `AE_UPDATER_IMAGE_TAG`

For external mode, also set:

- `AE_DB_DSN`
- `AE_REDIS_ADDR`

These can be left blank on first run because `deploy/docker-deploy.sh` will generate them:

- `AE_AUTH_JWT_SECRET`
- `AE_ENCRYPTION_KEY`
- `POSTGRES_PASSWORD`

## Health And Status

After startup:

- public liveness: `GET /api/v1/health/live`
- public readiness: `GET /api/v1/health/ready`
- admin deployment status: `GET /api/v1/settings/deployment`

## Online Update

Admin users can use the Settings page to:

- check for updates
- apply an update
- trigger rollback
- request a service restart

Docker/Compose mode routes update and rollback through the updater sidecar.

Linux systemd mode downloads the backend bundle from GitHub Releases, verifies `checksums.txt`, replaces `/opt/ai-efficiency/ai-efficiency-server`, keeps `.backup` for rollback, and uses `systemctl restart ai-efficiency` for restarts.

## GitHub Release Artifacts

After the first tagged GitHub release, the public repository will publish:

- `ai-efficiency-backend_<version>_<os>_<arch>.tar.gz|zip`
- `ae-cli_<version>_<os>_<arch>.tar.gz|zip`
- `checksums.txt`

## GHCR Images

Release images will be published to:

- `ghcr.io/lichking-2234/ai-efficiency:<tag>`
- `ghcr.io/lichking-2234/ai-efficiency:latest`

Examples:

```bash
docker pull ghcr.io/lichking-2234/ai-efficiency:v0.2.0
docker pull ghcr.io/lichking-2234/ai-efficiency:latest
```

## Linux Systemd Install

After the first tagged GitHub release, Linux hosts can install with:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/deploy/install.sh | sudo bash
```

The installer downloads the backend bundle, verifies checksums, installs under `/opt/ai-efficiency`, writes the systemd service, and enables it.

Edit `/etc/ai-efficiency/config.yaml` before first start.

For binary/systemd mode set:

- `deployment.mode: systemd`
- production `db.dsn`
- production `redis.addr`
- relay connection settings
