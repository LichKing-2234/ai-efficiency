# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install --registry=https://registry.npmmirror.com
COPY frontend/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.24-alpine AS backend-builder
RUN apk add --no-cache git
WORKDIR /app/backend
ARG APP_VERSION=dev
ARG APP_COMMIT=unknown
ARG APP_BUILD_TIME=
COPY backend/go.mod backend/go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY backend/ ./
# Embed frontend dist into the backend package tree before building.
COPY --from=frontend-builder /app/frontend/dist ./internal/web/dist/
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags "-X github.com/ai-efficiency/backend/internal/deployment.BuildVersion=${APP_VERSION} -X github.com/ai-efficiency/backend/internal/deployment.BuildCommit=${APP_COMMIT} -X github.com/ai-efficiency/backend/internal/deployment.BuildTime=${APP_BUILD_TIME}" \
  -o /app/server ./cmd/server/
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags "-X github.com/ai-efficiency/backend/internal/deployment.BuildVersion=${APP_VERSION} -X github.com/ai-efficiency/backend/internal/deployment.BuildCommit=${APP_COMMIT} -X github.com/ai-efficiency/backend/internal/deployment.BuildTime=${APP_BUILD_TIME}" \
  -o /app/updater ./cmd/updater/

# Stage 3: Final image
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata docker-cli docker-cli-compose
WORKDIR /app
COPY --from=backend-builder /app/server .
COPY --from=backend-builder /app/updater .
EXPOSE 8081
CMD ["./server"]
