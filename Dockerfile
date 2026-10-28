# syntax=docker/dockerfile:1

# ---- Build stage -------------------------------------------------------
# Cross-compilation runs on the build platform (--platform=$BUILDPLATFORM),
# so buildx can produce linux/amd64 and linux/arm64 images directly.
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
# modernc.org/sqlite is pure Go, so CGO_ENABLED=0 produces a static binary.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/amdl-api ./cmd/amdl-api

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
# script fixes the ownership of mounted directories according to PUID/PGID
# (default 1000:1000) and then drops privileges to run the backend. A non-root
# user can also be set directly with docker run --user, in which case the
# privilege-drop logic is skipped.
RUN mkdir -p /app/configs /app/data && chown -R amdl:amdl /app

EXPOSE 18080

# The port follows AMDL_SERVER_LISTEN (the environment override of
# server.listen, applied on every start, default :18080). To change the listen
# port, change that environment variable; the health check derives its port
# from it.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD p="${AMDL_SERVER_LISTEN:-:18080}"; wget -qO /dev/null "http://127.0.0.1:${p##*:}/api/v1/health" || exit 1

ENTRYPOINT ["docker-entrypoint.sh"]
