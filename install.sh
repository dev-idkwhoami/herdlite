#!/bin/sh
set -eu

repo="${HERDLITE_REPO:-dev-idkwhoami/herdlite}"
bin_dir="${HERDLITE_BIN_DIR:-$HOME/.local/share/herdlite/bin}"
version="${HERDLITE_VERSION:-latest}"
run_install=0

usage() {
  cat <<EOF
Usage: install.sh [--version <tag>] [--bin-dir <path>]

Environment:
  HERDLITE_VERSION   Release tag, default: latest
  HERDLITE_BIN_DIR   Install directory, default: \$HOME/.local/share/herdlite/bin
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || { echo "install.sh: --version needs a value" >&2; exit 1; }
      version="$2"
      shift 2
      ;;
    --bin-dir|--prefix)
      [ "$#" -ge 2 ] || { echo "install.sh: $1 needs a value" >&2; exit 1; }
      bin_dir="$2"
      shift 2
      ;;
    --run-install)
      run_install=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "install.sh: missing required command: $1" >&2
    exit 1
  fi
}

warn_missing() {
  if ! command -v "$1" >/dev/null 2>&1; then
    missing_next="${missing_next}${missing_next:+ }$1"
  fi
}

write_zsh_hook() {
  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    zsh)
      rc_file="$HOME/.zshrc"
      ;;
    *)
      echo
      echo "Warning: $bin_dir is not currently on PATH."
      echo "Add this to your shell config:"
      echo "  export PATH=\"$bin_dir:\$PATH\""
      return 0
      ;;
  esac

  shell_dir="$HOME/.config/herdlite/shell"
  shell_file="$shell_dir/herdlite.zsh"
  mkdir -p "$shell_dir"
  cat > "$shell_file" <<'EOF'
# Herdlite shell integration.

case ":$PATH:" in
  *":$HOME/.local/share/herdlite/bin:"*) ;;
  *) export PATH="$HOME/.local/share/herdlite/bin:$PATH" ;;
esac
EOF

  start_marker="# >>> Herdlite shell integration >>>"
  end_marker="# <<< Herdlite shell integration <<<"
  source_line="source \"$shell_file\""
  if [ -f "$rc_file" ] && grep -F "$start_marker" "$rc_file" >/dev/null 2>&1; then
    echo
    echo "Herdlite zsh hook already exists in $rc_file."
    echo "Open a new shell before running herdlite."
    return 0
  fi

  mkdir -p "$(dirname "$rc_file")"
  {
    echo
    echo "$start_marker"
    echo "$source_line"
    echo "$end_marker"
  } >> "$rc_file"

  echo
  echo "Added Herdlite zsh hook to $rc_file."
  echo "Open a new shell before running herdlite, or run:"
  echo "  source \"$shell_file\""
}

need curl
need tar
need uname
need mktemp
need find
need head
need mkdir
need cp
need chmod

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
machine="$(uname -m)"

case "$os" in
  linux) os="linux" ;;
  *)
    echo "install.sh: unsupported OS: $os" >&2
    exit 1
    ;;
esac

case "$machine" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "install.sh: unsupported architecture: $machine" >&2
    exit 1
    ;;
esac

asset="herdlite-${os}-${arch}.tar.gz"
base_url="https://github.com/${repo}/releases"
if [ "$version" = "latest" ]; then
  url="${base_url}/latest/download/${asset}"
  checksum_url="${base_url}/latest/download/${asset}.sha256"
else
  url="${base_url}/download/${version}/${asset}"
  checksum_url="${base_url}/download/${version}/${asset}.sha256"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading ${repo} ${version} (${os}/${arch})..."
curl -fsSL "$url" -o "$tmp/$asset"

if command -v sha256sum >/dev/null 2>&1; then
  if curl -fsSL "$checksum_url" -o "$tmp/$asset.sha256"; then
    (cd "$tmp" && sha256sum -c "$asset.sha256")
  else
    echo "Checksum not found; continuing without verification."
  fi
else
  echo "sha256sum not found; continuing without checksum verification."
fi

tar -xzf "$tmp/$asset" -C "$tmp"
binary="$(find "$tmp" -type f -name herdlite -perm -u+x | head -n 1)"
if [ -z "$binary" ]; then
  binary="$(find "$tmp" -type f -name herdlite | head -n 1)"
fi
if [ -z "$binary" ]; then
  echo "install.sh: release archive did not contain a herdlite binary" >&2
  exit 1
fi

mkdir -p "$bin_dir"
cp "$binary" "$bin_dir/herdlite"
chmod 755 "$bin_dir/herdlite"

echo
echo "Installed Herdlite:"
echo "  $bin_dir/herdlite"

write_zsh_hook

missing_next=""
warn_missing sudo
warn_missing pacman
if [ -n "$missing_next" ]; then
  echo
  echo "Before running Herdlite setup, install or make available:"
  for item in $missing_next; do
    echo "  - $item"
  done
fi

echo
echo "Next step:"
echo "  herdlite install"

if [ "$run_install" -eq 1 ]; then
  echo
  echo "Running herdlite install..."
  "$bin_dir/herdlite" install
fi
