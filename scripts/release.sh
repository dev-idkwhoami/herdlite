#!/usr/bin/env sh
set -eu

APP="${APP:-herdlite}"
VERSION="${VERSION:-dev}"
OS="${GOOS:-linux}"
ARCH="${GOARCH:-amd64}"
DIST_DIR="${DIST_DIR:-dist}"
BIN_DIR="${BIN_DIR:-bin}"

case "$OS" in
  linux) ;;
  *)
    echo "release.sh: unsupported GOOS for release packaging: $OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  amd64|arm64) ;;
  *)
    echo "release.sh: unsupported GOARCH for release packaging: $ARCH" >&2
    exit 1
    ;;
esac

mkdir -p "$DIST_DIR"

BIN_DIR="$BIN_DIR" APP="$APP" VERSION="$VERSION" ./scripts/build.sh

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT INT TERM

package="herdlite-${OS}-${ARCH}"
mkdir -p "$workdir/$package"
cp "$BIN_DIR/$APP" "$workdir/$package/herdlite"
cp README.md "$workdir/$package/README.md" 2>/dev/null || true
cp LICENSE "$workdir/$package/LICENSE" 2>/dev/null || true

tarball="$DIST_DIR/$package.tar.gz"
tar -C "$workdir" -czf "$tarball" "$package"

(cd "$DIST_DIR" && sha256sum "$package.tar.gz" > "$package.tar.gz.sha256")

echo "$tarball"

