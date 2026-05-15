#!/usr/bin/env sh
set -eu

APP="${APP:-herdlite}"
BIN_DIR="${BIN_DIR:-bin}"
VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || printf unknown)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
UI_DIR="${UI_DIR:-internal/daemon/ui}"

mkdir -p "$BIN_DIR"
mkdir -p "${GOCACHE:-.cache/go-build}"

export GOCACHE="${GOCACHE:-$(pwd)/.cache/go-build}"

if [ -f "$UI_DIR/package.json" ]; then
  (cd "$UI_DIR" && npm run build)
fi

go build \
  -trimpath \
  -ldflags "-s -w -X herdlite/internal/buildinfo.Version=$VERSION -X herdlite/internal/buildinfo.Commit=$COMMIT -X herdlite/internal/buildinfo.Date=$DATE" \
  -o "$BIN_DIR/$APP" \
  ./cmd/herdlite
