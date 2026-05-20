#!/usr/bin/env sh
set -u

usage() {
  cat <<'EOF'
Usage: ./uninstall.sh [--help]

Interactively removes Herdlite-owned traces from the system.
For each action, the script prints the command, explains why it exists,
then asks y/n. Choosing n skips that action and continues to the next one.

It never deletes linked Laravel/project folders.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

TARGET_USER="${SUDO_USER:-$(id -un)}"
TARGET_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
if [ -z "$TARGET_HOME" ]; then
  echo "Could not resolve home directory for $TARGET_USER" >&2
  exit 1
fi

XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$TARGET_HOME/.config}"
XDG_DATA_HOME="${XDG_DATA_HOME:-$TARGET_HOME/.local/share}"
XDG_CACHE_HOME="${XDG_CACHE_HOME:-$TARGET_HOME/.cache}"

CONFIG_DIR="$XDG_CONFIG_HOME/herdlite"
DATA_DIR="$XDG_DATA_HOME/herdlite"
CACHE_DIR="$XDG_CACHE_HOME/herdlite"
RUNTIME_DIR="$DATA_DIR/runtime"
NGINX_DIR="$DATA_DIR/nginx"
POSTGRES_DIR="$DATA_DIR/postgres"
DESKTOP_FILE="$XDG_DATA_HOME/applications/herdlite-debug.desktop"
ZSHRC="$TARGET_HOME/.zshrc"

SYSTEM_CA="/etc/ca-certificates/trust-source/anchors/herdlite-local-ca.crt"
NM_CONF="/etc/NetworkManager/conf.d/herdlite.conf"
NM_DNSMASQ_CONF="/etc/NetworkManager/dnsmasq.d/herdlite-test.conf"
NSS_DB="$TARGET_HOME/.pki/nssdb"
NSS_NICKNAME="Herdlite Local Development CA"

say() {
  printf '%s\n' "$*"
}

ask() {
  prompt="$1"
  while :; do
    printf '%s [y/n] ' "$prompt"
    read answer || answer=n
    case "$answer" in
      y|Y|yes|YES) return 0 ;;
      n|N|no|NO|'') return 1 ;;
      *) say "Please answer y or n." ;;
    esac
  done
}

run_step() {
  title="$1"
  command="$2"
  reason="$3"
  shift 3

  say
  say "$title"
  say "Command: $command"
  say "Why: $reason"

  if ask "Run this command?"; then
    "$@"
    status=$?
    if [ "$status" -ne 0 ]; then
      say "Command failed with exit code $status. Continuing."
    fi
  else
    say "Skipped."
  fi
}

run_shell_step() {
  title="$1"
  command="$2"
  reason="$3"
  run_step "$title" "$command" "$reason" sh -c "$command"
}

remove_path_step() {
  path="$1"
  reason="$2"
  if [ -e "$path" ] || [ -L "$path" ]; then
    run_shell_step "Remove $path" "rm -rf '$path'" "$reason"
  else
    say
    say "Remove $path"
    say "Command: rm -rf '$path'"
    say "Why: $reason"
    say "Nothing to remove; path is missing."
  fi
}

stop_pid_step() {
  name="$1"
  pid_file="$2"
  signal="$3"
  reason="$4"

  if [ ! -f "$pid_file" ]; then
    say
    say "Stop $name"
    say "Command: kill -$signal <pid from $pid_file>"
    say "Why: $reason"
    say "Nothing to stop; pid file is missing."
    return
  fi

  pid="$(tr -d '[:space:]' < "$pid_file" 2>/dev/null || true)"
  case "$pid" in
    ''|*[!0-9]*)
      say
      say "Stop $name"
      say "Command: kill -$signal <pid from $pid_file>"
      say "Why: $reason"
      say "Skipping; pid file is invalid."
      return
      ;;
  esac

  if kill -0 "$pid" 2>/dev/null; then
    run_step "Stop $name" "kill -$signal $pid" "$reason" kill "-$signal" "$pid"
  else
    say
    say "Stop $name"
    say "Command: kill -$signal $pid"
    say "Why: $reason"
    say "Nothing to stop; process is not running."
  fi
}

stop_matching_processes_step() {
  name="$1"
  pattern="$2"
  signal="$3"
  reason="$4"

  if ! command -v pgrep >/dev/null 2>&1; then
    say
    say "Stop orphaned $name processes"
    say "Command: pgrep -u '$TARGET_USER' -f '$pattern' | xargs kill -$signal"
    say "Why: $reason"
    say "Skipping; pgrep is missing."
    return
  fi

  pids="$(pgrep -u "$TARGET_USER" -f "$pattern" 2>/dev/null || true)"
  if [ -z "$pids" ]; then
    say
    say "Stop orphaned $name processes"
    say "Command: pgrep -u '$TARGET_USER' -f '$pattern' | xargs kill -$signal"
    say "Why: $reason"
    say "Nothing to stop; no matching processes found."
    return
  fi

  for pid in $pids; do
    if [ "$pid" = "$$" ]; then
      continue
    fi
    run_step "Stop orphaned $name process $pid" "kill -$signal $pid" "$reason" kill "-$signal" "$pid"
  done
}

say "Herdlite interactive cleanup"
say "Target user: $TARGET_USER"
say "Home:        $TARGET_HOME"
say
say "This script asks before every action and continues after skipped or failed steps."
say "Project folders are never deleted."

stop_pid_step "Herdlite daemon" "$RUNTIME_DIR/daemon.pid" TERM "Stops the background API/mail/debug daemon before deleting its files."
stop_matching_processes_step "Herdlite daemon" "herdlite daemon run" TERM "Stops Herdlite daemon processes that survived after their pid file was deleted."
stop_pid_step "Herdlite nginx" "$NGINX_DIR/nginx.pid" QUIT "Stops the user-run Nginx instance that serves Herdlite .test sites."

if [ -d "$POSTGRES_DIR/data" ] && command -v pg_ctl >/dev/null 2>&1; then
  run_step "Stop Herdlite PostgreSQL" "pg_ctl -D '$POSTGRES_DIR/data' stop -m fast" "Stops the Herdlite-managed PostgreSQL server before deleting its data directory." pg_ctl -D "$POSTGRES_DIR/data" stop -m fast
else
  say
  say "Stop Herdlite PostgreSQL"
  say "Command: pg_ctl -D '$POSTGRES_DIR/data' stop -m fast"
  say "Why: Stops the Herdlite-managed PostgreSQL server before deleting its data directory."
  say "Nothing to stop; data directory or pg_ctl is missing."
fi

remove_path_step "$DESKTOP_FILE" "Removes the desktop launcher installed by 'herdlite app install'."
remove_path_step "$CONFIG_DIR" "Removes Herdlite config, state database, and shell integration target files."
remove_path_step "$DATA_DIR" "Removes Herdlite binaries, runtimes, logs, certs, mail, Nginx config, PostgreSQL data, and shims."
remove_path_step "$CACHE_DIR" "Removes Herdlite build and download cache."

if [ "$(id -u)" -ne 0 ]; then
  say
  say "System integration cleanup"
  say "Command: sudo ./clean-herdlite.sh"
  say "Why: Removing system CA and NetworkManager dnsmasq config requires root."
  say "Skipping system-owned files because this script is not running as root."
else
  remove_path_step "$SYSTEM_CA" "Removes Herdlite's local CA from the system trust anchor directory."
  remove_path_step "$NM_CONF" "Removes Herdlite's NetworkManager setting that enables dnsmasq."
  remove_path_step "$NM_DNSMASQ_CONF" "Removes Herdlite's dnsmasq .test host configuration."

  if command -v trust >/dev/null 2>&1; then
    run_step "Refresh system trust store" "trust extract-compat" "Rebuilds trust-store compatibility files after removing the Herdlite CA." trust extract-compat
  else
    say
    say "Refresh system trust store"
    say "Command: trust extract-compat"
    say "Why: Rebuilds trust-store compatibility files after removing the Herdlite CA."
    say "Skipping; trust command is missing."
  fi

  if command -v systemctl >/dev/null 2>&1; then
    run_step "Reload NetworkManager" "systemctl reload NetworkManager" "Applies removal of Herdlite DNS configuration." systemctl reload NetworkManager
  else
    say
    say "Reload NetworkManager"
    say "Command: systemctl reload NetworkManager"
    say "Why: Applies removal of Herdlite DNS configuration."
    say "Skipping; systemctl is missing."
  fi
fi

if [ -d "$NSS_DB" ] && command -v certutil >/dev/null 2>&1; then
  if [ "$(id -u)" -eq 0 ] && [ "$TARGET_USER" != "root" ]; then
    run_step "Remove browser NSS CA trust" "sudo -u '$TARGET_USER' certutil -D -d 'sql:$NSS_DB' -n '$NSS_NICKNAME'" "Removes Herdlite's CA from the target user's browser NSS database." sudo -u "$TARGET_USER" certutil -D -d "sql:$NSS_DB" -n "$NSS_NICKNAME"
  else
    run_step "Remove browser NSS CA trust" "certutil -D -d 'sql:$NSS_DB' -n '$NSS_NICKNAME'" "Removes Herdlite's CA from the browser NSS database." certutil -D -d "sql:$NSS_DB" -n "$NSS_NICKNAME"
  fi
else
  say
  say "Remove browser NSS CA trust"
  say "Command: certutil -D -d 'sql:$NSS_DB' -n '$NSS_NICKNAME'"
  say "Why: Removes Herdlite's CA from the browser NSS database."
  say "Skipping; NSS DB or certutil is missing."
fi

if [ -f "$ZSHRC" ]; then
    run_shell_step "Comment Herdlite lines in .zshrc" "sed -i '/herdlite/s/^/# Disabled by uninstall.sh: /' '$ZSHRC'" "Disables any remaining shell hook that sources Herdlite integration."
else
  say
  say "Comment Herdlite lines in .zshrc"
  say "Command: sed -i '/herdlite/s/^/# Disabled by uninstall.sh: /' '$ZSHRC'"
  say "Why: Disables any remaining shell hook that sources Herdlite integration."
  say "Skipping; .zshrc is missing."
fi

say
say "Cleanup prompts complete. Project folders were not touched."
say
say "IMPORTANT: System packages were not removed."
say "Packages such as nginx, PostgreSQL, dnsmasq, PHP build dependencies, and NSS tools may still be installed."
say "Remove OS packages manually with your package manager only if you no longer need them."
