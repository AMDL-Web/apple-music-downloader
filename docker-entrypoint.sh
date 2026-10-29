#!/bin/sh
# Container entrypoint: syncs the example configs baked into the image to the
# config volume, then starts the backend as the target user. config.yaml is
# bootstrapped by the backend on first start from config.example.yaml in the
# same directory and may be rewritten in full by PUT /api/v1/config; this
# script never rewrites it.
#
# Runtime user: by default the image runs this script as root; after seeding
# and fixing volume ownership it drops to PUID:PGID (default 1000:1000) with
# su-exec to run the backend. When started as a non-root user with
# docker run --user (or compose's user:), PUID/PGID are ignored, the ownership
# fix is skipped, and the backend runs directly as that user.
set -eu

CONFIG_PATH="${AMDL_CONFIG:-/app/configs/config.yaml}"
CONFIG_DIR="$(dirname "$CONFIG_PATH")"
HOOKS_PATH="${AMDL_HOOKS_CONFIG:-/app/configs/hooks.yaml}"
DIST_DIR="/opt/amdl"
PUID="${PUID:-1000}"
PGID="${PGID:-1000}"

# Container defaults: two of the repository defaults do not work inside a
# container, so they are replaced on every start through two of the backend's
# environment overrides:
#   - server.listen   defaults to 127.0.0.1:18080, unreachable from outside
#                     the container; changed to :18080
#   - wrapper.address defaults to 127.0.0.1:8080, which points at the
#                     container itself; changed to the host
# Environment variables take precedence over config.yaml and are not written
# back to the file.
export AMDL_SERVER_LISTEN="${AMDL_SERVER_LISTEN:-:18080}"
export AMDL_WRAPPER_ADDRESS="${AMDL_WRAPPER_ADDRESS:-host.docker.internal:8080}"

mkdir -p "$CONFIG_DIR"

# config.example.yaml is field documentation that changes with each release,
# not user configuration; it is synced from the image on every start so that
# installs upgraded from the old two-file layout do not keep seeing an old
# example that lacks the runtime fields.
cp "$DIST_DIR/config.example.yaml" "$CONFIG_DIR/config.example.yaml"

# Without hooks.yaml the backend merely disables hooks; seed a fully commented
# template to make editing easy.
if [ ! -f "$HOOKS_PATH" ]; then
    mkdir -p "$(dirname "$HOOKS_PATH")"
    cp "$DIST_DIR/hooks.yaml" "$HOOKS_PATH"
fi

# When started as root, fix volume ownership and drop privileges; when started
# as a non-root user given by --user, run directly (the volumes must then be
# made writable by that user beforehand).
if [ "$(id -u)" = "0" ]; then
    # The config directory is small, so it is fixed in full on every start and
    # a PUID/PGID change takes effect immediately.
    chown -R "$PUID:$PGID" "$CONFIG_DIR"
    if [ -f "$HOOKS_PATH" ]; then
        chown "$PUID:$PGID" "$HOOKS_PATH"
    fi
    # Data directories are checked mount point by mount point: a bind-mounted
    # host directory is owned by root when Docker first creates it, and the
    # downloads directory may hold a large number of files, so ownership is
    # fixed recursively only once, when the top level does not match (the
    # first mount or a PUID/PGID change).
    for dir in /app/data /app/data/db /app/data/downloads; do
        mkdir -p "$dir"
        if [ "$(stat -c '%u:%g' "$dir")" != "$PUID:$PGID" ]; then
            echo "fixing ownership of $dir to $PUID:$PGID"
            chown -R "$PUID:$PGID" "$dir"
        fi
    done
    exec su-exec "$PUID:$PGID" /app/amdl-api "$@"
fi

exec /app/amdl-api "$@"
