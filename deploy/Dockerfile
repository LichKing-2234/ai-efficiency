# Stage 1: Build frontend
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install --registry=https://registry.npmmirror.com
COPY frontend/ ./
ARG VITE_WEB_VITALS_SAMPLE_RATE=0.1
ENV VITE_WEB_VITALS_SAMPLE_RATE=${VITE_WEB_VITALS_SAMPLE_RATE}
RUN npm run build

# Stage 2: Build backend
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS backend-builder
RUN apk add --no-cache git
WORKDIR /app/backend
ARG TARGETOS
ARG TARGETARCH
ARG APP_VERSION=dev
ARG APP_COMMIT=unknown
ARG APP_BUILD_TIME=
COPY backend/go.mod backend/go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY backend/ ./
# Embed frontend dist into the backend package tree before building.
COPY --from=frontend-builder /app/frontend/dist ./internal/web/dist/
RUN export GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-$(go env GOARCH)}" && \
	export LDFLAGS="-X github.com/ai-efficiency/backend/internal/buildinfo.BuildVersion=${APP_VERSION} -X github.com/ai-efficiency/backend/internal/buildinfo.BuildCommit=${APP_COMMIT} -X github.com/ai-efficiency/backend/internal/buildinfo.BuildTime=${APP_BUILD_TIME}" && \
	CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -o /app/server ./cmd/server/ && \
	CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -o /app/prewarmer ./cmd/prewarmer/

# Stage 3: Final image
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata git openssh-client
WORKDIR /app
COPY --from=backend-builder /app/server /app/ai-efficiency-server
COPY --from=backend-builder /app/prewarmer /app/ai-efficiency-prewarmer
COPY deploy/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh /app/ai-efficiency-server /app/ai-efficiency-prewarmer
EXPOSE 8081
ENTRYPOINT ["/docker-entrypoint.sh"]
