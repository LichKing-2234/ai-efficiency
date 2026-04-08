# Production Deployment Guide

## Overview

`ai-efficiency` now ships with an official Docker Compose deployment path and a dedicated updater sidecar.

The deploy assets in this directory cover two modes:

- bundled mode: local `postgres` + `redis` containers
- external mode: existing external `postgres` + `redis`

## Bundled Mode

```bash
cp deploy/.env.example deploy/.env
bash deploy/docker-deploy.sh
docker-compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
```

## External Mode

```bash
cp deploy/.env.example deploy/.env
bash deploy/docker-deploy.sh external
docker-compose --env-file deploy/.env -f deploy/docker-compose.external.yml up -d
```

## Required Variables

At minimum, set these in `deploy/.env` before first deploy:

- `AE_RELAY_URL`
- `AE_AUTH_JWT_SECRET`
- `AE_ENCRYPTION_KEY`
- `COMPOSE_PROJECT_NAME`
- `AE_UPDATER_IMAGE_REPOSITORY`
- `AE_UPDATER_IMAGE_TAG`

For external mode, also set:

- `AE_DB_DSN`
- `AE_REDIS_ADDR`

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

The updater sidecar performs the actual Compose operations.
