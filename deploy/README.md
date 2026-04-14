# Production Deployment Guide


## Overview

`ai-efficiency` now ships with two production deployment paths:

- Docker Compose with launcher-managed runtime binaries
- Linux systemd with backend binary self-update

In deployed images, the backend process serves both the API and the embedded frontend bundle.

The deploy assets in this directory cover two modes:

- bundled mode: local `postgres` + `redis` containers
- external mode: existing external `postgres` + `redis`

It also provides two non-production local validation paths inspired by `sub2api`:

- `docker-compose.dev.yml`: source-build local verification
- `docker-compose.local.yml`: directory-backed local verification

## Developer CLI

This guide covers backend deployment only. For the user-level CLI installer, see [`../ae-cli/README.md`](../ae-cli/README.md).

## Empty Directory Bootstrap

Use this when you want a `sub2api`-style deployment bootstrap from an empty directory:

The current remote bootstrap path is bundled-only and does not support external mode yet.

```bash
mkdir -p ai-efficiency-deploy && cd ai-efficiency-deploy
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/deploy/docker-deploy.sh | bash
```

`deploy/docker-deploy.sh` will:

- download deploy assets
- prepare `docker-compose.yml`, `.env.example`, and `.env`
- generate missing secrets
- create local data directories
- print a final summary with next steps

By default the Docker stack pulls `ghcr.io/lichking-2234/ai-efficiency:latest`.
Docker mode now runs the backend from a persistent runtime binary under `AE_DEPLOYMENT_STATE_DIR`.
Online update and rollback no longer depend on a separate updater sidecar or Docker socket access.
The production compose assets configure `backend` with `restart: unless-stopped` and use `GET /api/v1/health/live` for container health checks.
When `AE_CONFIG_PATH` is not set, the backend also materializes a writable runtime config at `${AE_DEPLOYMENT_STATE_DIR}/config.yaml` so admin-edited settings persist.

Before starting services, edit `.env` for operator-facing settings.
At minimum, set:

- `AE_RELAY_URL`
- `AE_RELAY_API_KEY`
- `AE_RELAY_ADMIN_API_KEY`

Then run the local preflight and start the stack:

```bash
bash deploy/docker-deploy.sh
docker compose up -d
```

To install a preview or a specific release:

```bash
TAG=v0.1.0-preview.2
curl -fsSL "https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/${TAG}/deploy/docker-deploy.sh" | TAG="$TAG" bash
bash deploy/docker-deploy.sh
docker compose up -d
```

`deploy/docker-deploy.sh` serves two roles:

- remote bootstrap from an empty directory
- local preflight for an already-prepared deployment directory

Bootstrap prepares files and secrets, but does not start services automatically.
The preflight path prints layout/mode context, validates compose configuration, checks relay health, and then prints the next startup command.

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

## Local Dev Mode

Use this when you want to build from the current checkout and verify a full local stack:

```bash
docker-compose --env-file deploy/.env.example -f deploy/docker-compose.dev.yml up --build
```

Notes:

- builds `backend` from the local repository
- starts local `postgres` and `redis`
- forces the container entrypoint to refresh the persisted runtime binary from the newly built bootstrap binary on each recreate
- disables deployment update/apply controls

## Local Persistent Mode

Use this when you want a longer-lived local environment with data stored under `deploy/`:

```bash
mkdir -p deploy/data deploy/postgres_data deploy/redis_data
docker-compose --env-file deploy/.env.example -f deploy/docker-compose.local.yml up --build
```

Notes:

- stores app state in `deploy/data`
- stores Postgres data in `deploy/postgres_data`
- stores Redis data in `deploy/redis_data`
- uses bind-mounted local state directories for the backend runtime binary and app data
- refreshes the persisted runtime binary from the newly built bootstrap binary on each recreate

## One-Time SQLite Bootstrap

If you have historical local data in `backend/ai_efficiency.db`, bootstrap it into the local Postgres environment once with:

```bash
bash deploy/migrate-sqlite-to-postgres.sh local
```

For the source-build stack, switch `local` to `dev`.

Behavior:

- starts the target Postgres service if needed
- refuses to import into a non-empty target database
- supports `--force-reset` when you explicitly want to recreate the local target schema
- uses a one-shot containerized migrator instead of requiring host database tools

## Required Variables

At minimum, set these in `deploy/.env` before first deploy:

- `AE_RELAY_URL`
- `POSTGRES_USER`
- `POSTGRES_DB`

For external mode, also set:

- `AE_DB_DSN`
- `AE_REDIS_ADDR`

These can be left blank on first run because `deploy/docker-deploy.sh` will generate them:

- `AE_AUTH_JWT_SECRET`
- `AE_ENCRYPTION_KEY`
- `POSTGRES_PASSWORD`

## Advanced Overrides

The default path hides image repository/tag and updater implementation details.
If you need to override them, append values such as these to `.env` manually:

- `AE_IMAGE_REPOSITORY`
- `AE_IMAGE_TAG`
- `AE_UPDATER_IMAGE_REPOSITORY`
- `AE_UPDATER_IMAGE_TAG`
- `COMPOSE_PROJECT_NAME`
- `AE_UPDATER_PROJECT_NAME`

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

Docker/Compose mode and non-Docker mode both use backend-managed binary self-update.
After an update or rollback request completes, restart the service/container to run the swapped binary.

Linux systemd mode downloads the backend bundle from GitHub Releases, verifies `checksums.txt`, replaces `/opt/ai-efficiency/ai-efficiency-server`, and keeps `.backup` for rollback.

The installer assigns ownership of `/opt/ai-efficiency` to the `ai-efficiency` service user, so binary replacement and rollback can happen in-place without extra write privileges.

Restarts do not shell out to `systemctl restart` by default. The backend acknowledges the restart request and then exits; the packaged `ai-efficiency.service` uses `Restart=always`, so systemd brings the process back automatically.

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
