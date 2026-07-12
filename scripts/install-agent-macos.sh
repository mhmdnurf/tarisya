#!/bin/bash
set -Eeuo pipefail

readonly REPOSITORY="mhmdnurf/tarisya"
readonly INSTALL_DIR="/usr/local/bin"
readonly BASE_DIR="/Library/Application Support/Tarisya"
readonly CONFIG_FILE="${BASE_DIR}/agent.env"
readonly DATA_DIR="${BASE_DIR}/agent-data"
readonly LOG_DIR="/Library/Logs/Tarisya"
readonly LABEL="com.tarisya.agent"
readonly PLIST_FILE="/Library/LaunchDaemons/${LABEL}.plist"
readonly RUNNER="${BASE_DIR}/run-agent.sh"

TEMP_DIR=""
ROLLBACK_DIR=""
MUTATION_STARTED=0
INSTALL_SUCCEEDED=0
SERVICE_WAS_LOADED=0
FRESH_INSTALL=0

log() { printf '%s\n' "$*"; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }
require_command() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required"; }
write_env() { printf '%s=%q\n' "$1" "$2"; }
is_loaded() { launchctl print "system/$LABEL" >/dev/null 2>&1; }
stop_service() {
  local attempt=1
  launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  while [ "$attempt" -le 10 ] && is_loaded; do
    sleep 1
    attempt=$((attempt + 1))
  done
  if is_loaded; then
    printf 'error: launchd did not stop %s\n' "$LABEL" >&2
    return 1
  fi
}
start_service() {
  launchctl bootstrap system "$PLIST_FILE"
  launchctl enable "system/$LABEL" >/dev/null 2>&1 || true
  launchctl kickstart -k "system/$LABEL"
}

validate_value() {
  local name="$1" value="$2"
  case "$value" in *$'\n'*|*$'\r'*) fail "$name must not contain newlines" ;; esac
}

rollback_installation() {
  log "Agent installation failed; restoring the previous macOS installation..."
  stop_service || true
  if [ -f "${ROLLBACK_DIR}/tarisya-agent" ]; then install -m 0755 "${ROLLBACK_DIR}/tarisya-agent" "${INSTALL_DIR}/tarisya-agent"; elif [ "$FRESH_INSTALL" -eq 1 ]; then rm -f "${INSTALL_DIR}/tarisya-agent"; fi
  for pair in "agent.plist:$PLIST_FILE" "agent.env:$CONFIG_FILE" "run-agent.sh:$RUNNER"; do
    source_file="${ROLLBACK_DIR}/${pair%%:*}"; destination="${pair#*:}"
    if [ "$FRESH_INSTALL" -eq 1 ]; then rm -f "$destination"; elif [ -f "$source_file" ]; then cp -p "$source_file" "$destination"; fi
  done
  if [ "$SERVICE_WAS_LOADED" -eq 1 ] && [ -f "$PLIST_FILE" ]; then start_service >/dev/null 2>&1 || true; fi
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
for command in awk chown cp curl id install launchctl mkdir mktemp plutil rm sed shasum sleep stat sw_vers tar; do require_command "$command"; done
MACOS_MAJOR="$(sw_vers -productVersion | awk -F. '{print $1}')"
[ "$MACOS_MAJOR" -ge 12 ] || fail "macOS 12 Monterey or newer is required"
case "$(uname -m)" in x86_64) ARCH=amd64 ;; arm64) ARCH=arm64 ;; *) fail "unsupported architecture: $(uname -m)" ;; esac

SERVICE_USER="${TARISYA_MACOS_USER:-${SUDO_USER:-}}"
if [ -z "$SERVICE_USER" ] || [ "$SERVICE_USER" = root ]; then SERVICE_USER="$(stat -f '%Su' /dev/console)"; fi
[ -n "$SERVICE_USER" ] && [ "$SERVICE_USER" != root ] && [ "$SERVICE_USER" != loginwindow ] || fail "could not determine the macOS user that should run Tarisya"
id "$SERVICE_USER" >/dev/null 2>&1 || fail "macOS user ${SERVICE_USER} does not exist"
SERVICE_GROUP="$(id -gn "$SERVICE_USER")"

CORE_URL="${TARISYA_CORE_URL:-}"; SERVER_ID="${TARISYA_SERVER_ID:-}"; API_KEY="${TARISYA_API_KEY:-}"
INTERVAL="${TARISYA_INTERVAL:-15s}"; HTTP_TIMEOUT="${TARISYA_HTTP_TIMEOUT:-10s}"; DISK_PATH="${TARISYA_DISK_PATH:-/}"
[ -n "$CORE_URL" ] || fail "TARISYA_CORE_URL is required"; [ -n "$SERVER_ID" ] || fail "TARISYA_SERVER_ID is required"; [ -n "$API_KEY" ] || fail "TARISYA_API_KEY is required"
case "$CORE_URL" in http://*|https://*) ;; *) fail "TARISYA_CORE_URL must start with http:// or https://" ;; esac
for pair in "TARISYA_CORE_URL:$CORE_URL" "TARISYA_SERVER_ID:$SERVER_ID" "TARISYA_API_KEY:$API_KEY" "TARISYA_INTERVAL:$INTERVAL" "TARISYA_HTTP_TIMEOUT:$HTTP_TIMEOUT" "TARISYA_DISK_PATH:$DISK_PATH"; do validate_value "${pair%%:*}" "${pair#*:}"; done
curl -fsS "${CORE_URL%/}/health" >/dev/null || fail "Tarisya Core health check failed at ${CORE_URL%/}/health"

if [ -n "${TARISYA_VERSION:-}" ]; then RELEASE_TAG="$TARISYA_VERSION"; case "$RELEASE_TAG" in v*) ;; *) RELEASE_TAG="v${RELEASE_TAG}" ;; esac
else RELEASE_TAG="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPOSITORY}/releases/latest")"; RELEASE_TAG="${RELEASE_TAG##*/}"; fi
if [[ ! "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then fail "invalid release version: ${RELEASE_TAG}"; fi
VERSION="${RELEASE_TAG#v}"; ARCHIVE="tarisya_${VERSION}_darwin_${ARCH}.tar.gz"; DOWNLOAD_URL="https://github.com/${REPOSITORY}/releases/download/${RELEASE_TAG}"
TEMP_DIR="$(mktemp -d)"; ROLLBACK_DIR="${TEMP_DIR}/rollback"; mkdir -p "$ROLLBACK_DIR"

log "Downloading Tarisya Agent ${RELEASE_TAG} for darwin/${ARCH}..."
curl -fsSLo "${TEMP_DIR}/${ARCHIVE}" "${DOWNLOAD_URL}/${ARCHIVE}"; curl -fsSLo "${TEMP_DIR}/checksums.txt" "${DOWNLOAD_URL}/checksums.txt"
EXPECTED="$(awk -v archive="$ARCHIVE" '$2 == archive {print $1; exit}' "${TEMP_DIR}/checksums.txt")"; [ -n "$EXPECTED" ] || fail "${ARCHIVE} is missing from checksums.txt"
ACTUAL="$(shasum -a 256 "${TEMP_DIR}/${ARCHIVE}" | awk '{print $1}')"; [ "$ACTUAL" = "$EXPECTED" ] || fail "release checksum verification failed"
tar -xzf "${TEMP_DIR}/${ARCHIVE}" -C "$TEMP_DIR"
for file in tarisya-agent packaging/launchd/com.tarisya.agent.plist packaging/launchd/run-agent.sh; do [ -f "${TEMP_DIR}/${file}" ] || fail "release archive is missing ${file}"; done

install -d -m 0755 "$INSTALL_DIR"
install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$BASE_DIR" "$DATA_DIR" "$LOG_DIR"
if [ ! -f "$CONFIG_FILE" ]; then
  FRESH_INSTALL=1
  { write_env TARISYA_SERVER_ID "$SERVER_ID"; write_env TARISYA_API_KEY "$API_KEY"; write_env TARISYA_CORE_URL "${CORE_URL%/}"; write_env TARISYA_INTERVAL "$INTERVAL"; write_env TARISYA_HTTP_TIMEOUT "$HTTP_TIMEOUT"; write_env TARISYA_DISK_PATH "$DISK_PATH"; } >"${TEMP_DIR}/agent.env"
  install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "${TEMP_DIR}/agent.env" "$CONFIG_FILE"
else log "Preserving existing ${CONFIG_FILE}"; fi

is_loaded && SERVICE_WAS_LOADED=1
[ ! -f "${INSTALL_DIR}/tarisya-agent" ] || cp -p "${INSTALL_DIR}/tarisya-agent" "${ROLLBACK_DIR}/tarisya-agent"
[ ! -f "$PLIST_FILE" ] || cp -p "$PLIST_FILE" "${ROLLBACK_DIR}/agent.plist"
[ ! -f "$CONFIG_FILE" ] || cp -p "$CONFIG_FILE" "${ROLLBACK_DIR}/agent.env"
[ ! -f "$RUNNER" ] || cp -p "$RUNNER" "${ROLLBACK_DIR}/run-agent.sh"

MUTATION_STARTED=1; stop_service
install -m 0755 "${TEMP_DIR}/tarisya-agent" "${INSTALL_DIR}/tarisya-agent"
install -m 0755 -o root -g wheel "${TEMP_DIR}/packaging/launchd/run-agent.sh" "$RUNNER"
sed -e "s/__TARISYA_USER__/${SERVICE_USER}/g" -e "s/__TARISYA_GROUP__/${SERVICE_GROUP}/g" "${TEMP_DIR}/packaging/launchd/com.tarisya.agent.plist" >"${TEMP_DIR}/agent.plist"
plutil -lint "${TEMP_DIR}/agent.plist" >/dev/null
install -m 0644 -o root -g wheel "${TEMP_DIR}/agent.plist" "$PLIST_FILE"
start_service; sleep 2; is_loaded || fail "Tarisya Agent failed to start"
INSTALL_SUCCEEDED=1

log ""; log "Tarisya Agent ${RELEASE_TAG} installed successfully on macOS"; log ""; log "Agent service: running"; log "Core address: ${CORE_URL%/}"; log "Configuration: ${CONFIG_FILE}"; log "Logs: ${LOG_DIR}/agent.log"
log ""; log "Commands:"; log "  sudo launchctl print system/${LABEL}"; log "  tail -f '${LOG_DIR}/agent.log'"
