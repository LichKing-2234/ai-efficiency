# syntax=docker/dockerfile:1

FROM oven/bun:1-alpine AS base

WORKDIR /app

FROM base AS deps

COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

FROM base AS builder

ARG APP_VERSION=dev
ARG APP_COMMIT=unknown
ARG APP_BUILD_TIME=unknown

ENV VITE_PUBLIC_RELEASE=$APP_VERSION
ENV VITE_PUBLIC_GIT_COMMIT_HASH=$APP_COMMIT
ENV VITE_PUBLIC_BUILD_TIME=$APP_BUILD_TIME

COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN bun run build

FROM base AS runner

ENV NODE_ENV=production
ENV HOST=0.0.0.0
ENV PORT=3000

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder --chown=app:app /app/.output ./.output

USER app

EXPOSE 3000

CMD ["bun", ".output/server/index.mjs"]
