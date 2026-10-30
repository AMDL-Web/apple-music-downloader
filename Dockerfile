# syntax=docker/dockerfile:1

# ---- Build stage -------------------------------------------------------
# Cross-compilation runs on the build platform (--platform=$BUILDPLATFORM),
# so buildx can produce linux/amd64 and linux/arm64 images directly.
FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
# Build identity reported by GET /api/v1/system. .dockerignore keeps .git out
# of the context, so these build args are its only source; without them the
# image reports version dev and no commit.
ARG VERSION=dev
ARG COMMIT=
ARG BUILD_DATE=
# modernc.org/sqlite is pure Go, so CGO_ENABLED=0 produces a static binary.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags="-s -w -X amdl/internal/buildinfo.Version=${VERSION} -X amdl/internal/buildinfo.Commit=${COMMIT} -X amdl/internal/buildinfo.BuildDate=${BUILD_DATE}" \
      -o /out/amdl-api ./cmd/amdl-api

# ---- Runtime stage -----------------------------------------------------
FROM alpine:3.24

# ffmpeg: remux flattening and the optional integrity check (tools.ffmpeg; the
#   default command name works as is).
# ca-certificates: HTTPS requests to the Apple Music API.
# tzdata: time zone support for log and hook timestamps.
# su-exec: the entrypoint runs as root to fix volume ownership, then drops to
#   PUID:PGID to run the backend.
RUN apk add --no-cache ca-certificates ffmpeg tzdata su-exec \
    && addgroup -g 1000 amdl \
    && adduser -D -u 1000 -G amdl amdl

WORKDIR /app

# The example configs are baked into /opt/amdl; on first start the entrypoint
# seeds them into the config directory /app/configs.
COPY configs/config.example.yaml configs/hooks.yaml /opt/amdl/
COPY --chmod=755 docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY --from=build /out/amdl-api /app/amdl-api

# Pre-create the config and data directories and hand them to the default
# runtime user (uid 1000). The container starts the entrypoint as root; the
# script gives the backend's own state (config files, database, data/tmp, never
# data/downloads) to PUID:PGID (default 1000:1000) and then drops privileges to
# run the backend. A non-root user can also be set directly with docker run
# --user (compose's user:), in which case the privilege drop is skipped. Any
# such uid must be able to create files in the directories that are not
# mounted, data/tmp above all, so they are world-writable with the sticky bit,
# like /tmp. A mount replaces them with the host directory and its permissions.
RUN mkdir -p /app/configs /app/data/db /app/data/tmp /app/data/downloads \
    && chown -R amdl:amdl /app \
    && chmod 1777 /app/configs /app/data /app/data/db /app/data/tmp /app/data/downloads

EXPOSE 18080

# The port follows AMDL_SERVER_LISTEN (the environment override of
# server.listen, applied on every start, default :18080). To change the listen
# port, change that environment variable; the health check derives its port
# from it.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD p="${AMDL_SERVER_LISTEN:-:18080}"; wget -qO /dev/null "http://127.0.0.1:${p##*:}/api/v1/health" || exit 1

ENTRYPOINT ["docker-entrypoint.sh"]
