#!/bin/bash
set -Eeuo pipefail

readonly REPOSITORY="mhmdnurf/tarisya"
readonly INSTALL_DIR="/usr/local/bin"
readonly BASE_DIR="/Library/Application Support/Tarisya"
readonly CONFIG_FILE="${BASE_DIR}/core.env"
readonly AGENT_CONFIG_FILE="${BASE_DIR}/agent.env"
readonly DATA_DIR="${BASE_DIR}/data"
readonly AGENT_DATA_DIR="${BASE_DIR}/agent-data"
readonly BACKUP_DIR="${BASE_DIR}/backups"
readonly LOG_DIR="/Library/Logs/Tarisya"
readonly CORE_LABEL="com.tarisya.core"
readonly AGENT_LABEL="com.tarisya.agent"
readonly CORE_PLIST="/Library/LaunchDaemons/${CORE_LABEL}.plist"
readonly AGENT_PLIST="/Library/LaunchDaemons/${AGENT_LABEL}.plist"
readonly CORE_RUNNER="${BASE_DIR}/run-core.sh"
readonly AGENT_RUNNER="${BASE_DIR}/run-agent.sh"

TEMP_DIR=""
ROLLBACK_DIR=""
MUTATION_STARTED=0
INSTALL_SUCCEEDED=0
CORE_WAS_LOADED=0
AGENT_WAS_LOADED=0
FRESH_INSTALL=0
INSTALL_LOCAL_AGENT=0

log() { printf '%s\n' "$*"; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }
require_command() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required"; }

prompt_value() {
  local variable_name="$1" prompt="$2" secret="${3:-false}" value
  eval "value=\${${variable_name}:-}"
  if [ -z "$value" ]; then
    [ -r /dev/tty ] || fail "$variable_name is required for non-interactive installation"
    if [ "$secret" = true ]; then
      read -r -s -p "$prompt" value </dev/tty
      printf '\n' >/dev/tty
    else
      read -r -p "$prompt" value </dev/tty
    fi
  fi
  printf -v "$variable_name" '%s' "$value"
}

write_env() {
  local key="$1" value="$2"
  printf '%s=%q\n' "$key" "$value"
}

is_loaded() { launchctl print "system/$1" >/dev/null 2>&1; }
stop_service() { launchctl bootout "system/$1" >/dev/null 2>&1 || true; }
start_service() {
  local label="$1" plist="$2"
  launchctl bootstrap system "$plist"
  launchctl enable "system/$label" >/dev/null 2>&1 || true
  launchctl kickstart -k "system/$label"
}

wait_for_core() {
  local health_url="$1" attempt=1
  while [ "$attempt" -le 30 ]; do
    if is_loaded "$CORE_LABEL" && curl -fsS "$health_url" >/dev/null 2>&1; then return 0; fi
    sleep 1
    attempt=$((attempt + 1))
  done
  return 1
}

install_plist() {
  local source="$1" destination="$2"
  sed -e "s/__TARISYA_USER__/${SERVICE_USER}/g" \
      -e "s/__TARISYA_GROUP__/${SERVICE_GROUP}/g" "$source" >"${TEMP_DIR}/service.plist"
  plutil -lint "${TEMP_DIR}/service.plist" >/dev/null
  install -m 0644 -o root -g wheel "${TEMP_DIR}/service.plist" "$destination"
}

rollback_installation() {
  log "Installation failed; restoring the previous macOS installation..."
  stop_service "$AGENT_LABEL"
  stop_service "$CORE_LABEL"
  for name in tarisya tarisya-core tarisya-agent; do
    if [ -f "${ROLLBACK_DIR}/${name}" ]; then
      install -m 0755 "${ROLLBACK_DIR}/${name}" "${INSTALL_DIR}/${name}"
    elif [ "$FRESH_INSTALL" -eq 1 ]; then
      rm -f "${INSTALL_DIR}/${name}"
    fi
  done
  for pair in "core.plist:$CORE_PLIST" "agent.plist:$AGENT_PLIST" \
    "core.env:$CONFIG_FILE" "agent.env:$AGENT_CONFIG_FILE" \
    "run-core.sh:$CORE_RUNNER" "run-agent.sh:$AGENT_RUNNER"; do
    source_file="${ROLLBACK_DIR}/${pair%%:*}"
    destination="${pair#*:}"
    if [ "$FRESH_INSTALL" -eq 1 ]; then rm -f "$destination"; elif [ -f "$source_file" ]; then cp -p "$source_file" "$destination"; fi
  done
  if [ "$CORE_WAS_LOADED" -eq 1 ] && [ -f "$CORE_PLIST" ]; then start_service "$CORE_LABEL" "$CORE_PLIST" >/dev/null 2>&1 || true; fi
  if [ "$AGENT_WAS_LOADED" -eq 1 ] && [ -f "$AGENT_PLIST" ]; then start_service "$AGENT_LABEL" "$AGENT_PLIST" >/dev/null 2>&1 || true; fi
}

on_exit() {
  local exit_code=$?
  trap - EXIT
  if [ "$exit_code" -ne 0 ] && [ "$MUTATION_STARTED" -eq 1 ] && [ "$INSTALL_SUCCEEDED" -eq 0 ]; then rollback_installation; fi
  [ -z "$TEMP_DIR" ] || rm -rf "$TEMP_DIR"
  exit "$exit_code"
}
trap on_exit EXIT
umask 077

[ "$#" -eq 0 ] || fail "this installer does not accept positional arguments"
[ "$(id -u)" -eq 0 ] || fail "run this installer with sudo"
[ "$(uname -s)" = Darwin ] || fail "this installer supports macOS only"

for command in awk chown cp curl date id install launchctl mkdir mktemp openssl plutil rm sed shasum sleep stat sw_vers tail tar tr; do require_command "$command"; done

MACOS_MAJOR="$(sw_vers -productVersion | awk -F. '{print $1}')"
[ "$MACOS_MAJOR" -ge 12 ] || fail "macOS 12 Monterey or newer is required"
case "$(uname -m)" in x86_64) ARCH=amd64 ;; arm64) ARCH=arm64 ;; *) fail "unsupported architecture: $(uname -m)" ;; esac

SERVICE_USER="${TARISYA_MACOS_USER:-${SUDO_USER:-}}"
if [ -z "$SERVICE_USER" ] || [ "$SERVICE_USER" = root ]; then SERVICE_USER="$(stat -f '%Su' /dev/console)"; fi
[ -n "$SERVICE_USER" ] && [ "$SERVICE_USER" != root ] && [ "$SERVICE_USER" != loginwindow ] || fail "could not determine the macOS user that should run Tarisya"
id "$SERVICE_USER" >/dev/null 2>&1 || fail "macOS user ${SERVICE_USER} does not exist"
SERVICE_GROUP="$(id -gn "$SERVICE_USER")"

if [ -n "${TARISYA_VERSION:-}" ]; then RELEASE_TAG="$TARISYA_VERSION"; case "$RELEASE_TAG" in v*) ;; *) RELEASE_TAG="v${RELEASE_TAG}" ;; esac
else RELEASE_TAG="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPOSITORY}/releases/latest")"; RELEASE_TAG="${RELEASE_TAG##*/}"; fi
if [[ ! "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then fail "invalid release version: ${RELEASE_TAG}"; fi

VERSION="${RELEASE_TAG#v}"
ARCHIVE="tarisya_${VERSION}_darwin_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPOSITORY}/releases/download/${RELEASE_TAG}"
TEMP_DIR="$(mktemp -d)"
ROLLBACK_DIR="${TEMP_DIR}/rollback"
mkdir -p "$ROLLBACK_DIR"

log "Downloading Tarisya ${RELEASE_TAG} for darwin/${ARCH}..."
curl -fsSLo "${TEMP_DIR}/${ARCHIVE}" "${DOWNLOAD_URL}/${ARCHIVE}"
curl -fsSLo "${TEMP_DIR}/checksums.txt" "${DOWNLOAD_URL}/checksums.txt"
EXPECTED="$(awk -v archive="$ARCHIVE" '$2 == archive {print $1; exit}' "${TEMP_DIR}/checksums.txt")"
[ -n "$EXPECTED" ] || fail "${ARCHIVE} is missing from checksums.txt"
ACTUAL="$(shasum -a 256 "${TEMP_DIR}/${ARCHIVE}" | awk '{print $1}')"
[ "$ACTUAL" = "$EXPECTED" ] || fail "release checksum verification failed"
tar -xzf "${TEMP_DIR}/${ARCHIVE}" -C "$TEMP_DIR"
for file in tarisya tarisya-core tarisya-agent packaging/launchd/com.tarisya.core.plist packaging/launchd/com.tarisya.agent.plist packaging/launchd/run-core.sh packaging/launchd/run-agent.sh; do [ -f "${TEMP_DIR}/${file}" ] || fail "release archive is missing ${file}"; done

install -d -m 0755 "$INSTALL_DIR"
install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$BASE_DIR" "$DATA_DIR" "$AGENT_DATA_DIR" "$BACKUP_DIR" "$LOG_DIR"

if [ ! -f "$CONFIG_FILE" ]; then
  FRESH_INSTALL=1
  ADMIN_NAME="${TARISYA_ADMIN_NAME:-}"; ADMIN_EMAIL="${TARISYA_ADMIN_EMAIL:-}"; ADMIN_PASSWORD="${TARISYA_ADMIN_PASSWORD:-}"
  prompt_value ADMIN_NAME "Administrator name: "
  prompt_value ADMIN_EMAIL "Administrator email: "
  prompt_value ADMIN_PASSWORD "Administrator password: " true
  if [ -z "${TARISYA_ADMIN_PASSWORD:-}" ]; then ADMIN_PASSWORD_CONFIRMATION=""; prompt_value ADMIN_PASSWORD_CONFIRMATION "Confirm password: " true; [ "$ADMIN_PASSWORD" = "$ADMIN_PASSWORD_CONFIRMATION" ] || fail "administrator passwords do not match"; fi
  case "$ADMIN_EMAIL" in *@*.*) ;; *) fail "administrator email is invalid" ;; esac
  [ "${#ADMIN_PASSWORD}" -ge 8 ] && [ "${#ADMIN_PASSWORD}" -le 128 ] || fail "administrator password must contain 8 to 128 characters"
  for value in "$ADMIN_NAME" "$ADMIN_EMAIL" "$ADMIN_PASSWORD"; do case "$value" in *$'\n'*|*$'\r'*) fail "administrator values must not contain newlines" ;; esac; done

  INSTALL_LOCAL_AGENT_VALUE="${TARISYA_INSTALL_LOCAL_AGENT:-}"
  if [ -z "$INSTALL_LOCAL_AGENT_VALUE" ]; then [ -r /dev/tty ] || fail "TARISYA_INSTALL_LOCAL_AGENT is required for non-interactive installation"; read -r -p "Install the local monitoring agent? [Y/n]: " INSTALL_LOCAL_AGENT_VALUE </dev/tty; INSTALL_LOCAL_AGENT_VALUE="${INSTALL_LOCAL_AGENT_VALUE:-y}"; fi
  case "$(printf '%s' "$INSTALL_LOCAL_AGENT_VALUE" | tr '[:upper:]' '[:lower:]')" in y|yes|true|1) INSTALL_LOCAL_AGENT=1 ;; n|no|false|0) INSTALL_LOCAL_AGENT=0 ;; *) fail "TARISYA_INSTALL_LOCAL_AGENT must be true or false" ;; esac

  LOCAL_SERVER_ID=""; LOCAL_API_KEY=""
  if [ "$INSTALL_LOCAL_AGENT" -eq 1 ]; then LOCAL_SERVER_ID="srv_$(openssl rand -hex 10)"; LOCAL_API_KEY="tar_$(openssl rand -hex 32)"; fi
  {
    write_env TARISYA_CORE_ADDRESS "127.0.0.1:8081"
    write_env TARISYA_PUBLIC_CORE_URL "${TARISYA_PUBLIC_CORE_URL:-http://localhost:8081}"
    write_env TARISYA_DATABASE_URL "file:${DATA_DIR}/tarisya.db"
    write_env TARISYA_JWT_SECRET "$(openssl rand -hex 32)"
    write_env TARISYA_ALLOWED_ORIGINS "${TARISYA_ALLOWED_ORIGINS:-http://localhost:8081,http://127.0.0.1:8081}"
    write_env TARISYA_COOKIE_SECURE "${TARISYA_COOKIE_SECURE:-false}"
    write_env TARISYA_RETENTION_RAW 7d; write_env TARISYA_RETENTION_5M 30d; write_env TARISYA_RETENTION_AGGREGATED 90d
    write_env TARISYA_MAX_DATABASE_SIZE 5GB; write_env TARISYA_DATABASE_WARNING_THRESHOLD 0.8
    write_env TARISYA_BOOTSTRAP_USER_NAME "$ADMIN_NAME"; write_env TARISYA_BOOTSTRAP_USER_EMAIL "$ADMIN_EMAIL"; write_env TARISYA_BOOTSTRAP_USER_PASSWORD "$ADMIN_PASSWORD"
    if [ "$INSTALL_LOCAL_AGENT" -eq 1 ]; then write_env TARISYA_BOOTSTRAP_SERVER_ID "$LOCAL_SERVER_ID"; write_env TARISYA_BOOTSTRAP_API_KEY "$LOCAL_API_KEY"; fi
  } >"${TEMP_DIR}/core.env"
  install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "${TEMP_DIR}/core.env" "$CONFIG_FILE"
  if [ "$INSTALL_LOCAL_AGENT" -eq 1 ]; then
    { write_env TARISYA_SERVER_ID "$LOCAL_SERVER_ID"; write_env TARISYA_API_KEY "$LOCAL_API_KEY"; write_env TARISYA_CORE_URL http://127.0.0.1:8081; write_env TARISYA_INTERVAL 15s; write_env TARISYA_HTTP_TIMEOUT 10s; write_env TARISYA_DISK_PATH /; } >"${TEMP_DIR}/agent.env"
    install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "${TEMP_DIR}/agent.env" "$AGENT_CONFIG_FILE"
  fi
  unset ADMIN_PASSWORD ADMIN_PASSWORD_CONFIRMATION LOCAL_API_KEY TARISYA_ADMIN_PASSWORD
else
  log "Preserving existing ${CONFIG_FILE}"
fi

is_loaded "$CORE_LABEL" && CORE_WAS_LOADED=1
is_loaded "$AGENT_LABEL" && AGENT_WAS_LOADED=1
for pair in "tarisya:${INSTALL_DIR}/tarisya" "tarisya-core:${INSTALL_DIR}/tarisya-core" "tarisya-agent:${INSTALL_DIR}/tarisya-agent" "core.plist:$CORE_PLIST" "agent.plist:$AGENT_PLIST" "core.env:$CONFIG_FILE" "agent.env:$AGENT_CONFIG_FILE" "run-core.sh:$CORE_RUNNER" "run-agent.sh:$AGENT_RUNNER"; do [ ! -f "${pair#*:}" ] || cp -p "${pair#*:}" "${ROLLBACK_DIR}/${pair%%:*}"; done

if [ "$FRESH_INSTALL" -eq 0 ]; then
  DATABASE_URL="$(/bin/bash -c 'source "$1"; printf %s "$TARISYA_DATABASE_URL"' _ "$CONFIG_FILE")"
  UPGRADE_BACKUP="${BACKUP_DIR}/tarisya-before-${RELEASE_TAG}-$(date -u +%Y%m%dT%H%M%SZ).db"
  log "Creating pre-upgrade backup..."
  "${TEMP_DIR}/tarisya" backup --database "$DATABASE_URL" --output "$UPGRADE_BACKUP"
  chown "$SERVICE_USER:$SERVICE_GROUP" "$UPGRADE_BACKUP" "${UPGRADE_BACKUP}.sha256"
fi

MUTATION_STARTED=1
stop_service "$AGENT_LABEL"; stop_service "$CORE_LABEL"
install -m 0755 "${TEMP_DIR}/tarisya" "${INSTALL_DIR}/tarisya"
install -m 0755 "${TEMP_DIR}/tarisya-core" "${INSTALL_DIR}/tarisya-core"
install -m 0755 "${TEMP_DIR}/tarisya-agent" "${INSTALL_DIR}/tarisya-agent"
install -m 0755 -o root -g wheel "${TEMP_DIR}/packaging/launchd/run-core.sh" "$CORE_RUNNER"
install -m 0755 -o root -g wheel "${TEMP_DIR}/packaging/launchd/run-agent.sh" "$AGENT_RUNNER"
install_plist "${TEMP_DIR}/packaging/launchd/com.tarisya.core.plist" "$CORE_PLIST"
[ ! -f "$AGENT_CONFIG_FILE" ] || install_plist "${TEMP_DIR}/packaging/launchd/com.tarisya.agent.plist" "$AGENT_PLIST"

start_service "$CORE_LABEL" "$CORE_PLIST"
HEALTH_URL="${TARISYA_HEALTH_URL:-http://127.0.0.1:8081/health}"
if ! wait_for_core "$HEALTH_URL"; then tail -n 50 "${LOG_DIR}/core-error.log" >&2 2>/dev/null || true; fail "Tarisya Core failed its health check at ${HEALTH_URL}"; fi

if [ "$FRESH_INSTALL" -eq 1 ]; then
  awk '!/^TARISYA_BOOTSTRAP_(SERVER_ID|API_KEY|USER_NAME|USER_EMAIL|USER_PASSWORD)=/' "$CONFIG_FILE" >"${TEMP_DIR}/core-without-bootstrap.env"
  install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "${TEMP_DIR}/core-without-bootstrap.env" "$CONFIG_FILE"
  stop_service "$CORE_LABEL"; start_service "$CORE_LABEL" "$CORE_PLIST"
  wait_for_core "$HEALTH_URL" || fail "Tarisya Core failed after removing bootstrap credentials"
fi

if [ -f "$AGENT_CONFIG_FILE" ]; then start_service "$AGENT_LABEL" "$AGENT_PLIST"; sleep 2; is_loaded "$AGENT_LABEL" || fail "Tarisya Agent failed to start"; fi
INSTALL_SUCCEEDED=1

log ""; log "Tarisya ${RELEASE_TAG} installed successfully on macOS"
log ""; log "Core service: running"; log "Console: ${HEALTH_URL%/health}"; [ ! -f "$AGENT_CONFIG_FILE" ] || log "Local agent: running"
log "Configuration: ${CONFIG_FILE}"; log "Database: ${DATA_DIR}/tarisya.db"; log "Backups: ${BACKUP_DIR}"; log "Logs: ${LOG_DIR}"
log ""; log "Commands:"; log "  tarisya doctor --config '${CONFIG_FILE}'"; log "  sudo launchctl print system/${CORE_LABEL}"; log "  tail -f '${LOG_DIR}/core.log'"
