#!/bin/sh
set -e

DATA_DIR=/app/data

mkdir -p "$DATA_DIR/projects"
chown replychat:replychat "$DATA_DIR" "$DATA_DIR/projects" || true

find "$DATA_DIR/projects" -maxdepth 1 -mindepth 1 -type d -exec chown replychat:replychat {} \; || true

exec gosu replychat /usr/local/bin/replychat
