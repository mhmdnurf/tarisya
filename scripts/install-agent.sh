#!/usr/bin/env bash
set -Eeuo pipefail

readonly REPOSITORY="mhmdnurf/tarisya"
readonly INSTALL_DIR="/usr/local/bin"
readonly CONFIG_DIR="/etc/tarisya"
readonly CONFIG_FILE="${CONFIG_DIR}/agent.env"
readonly DATA_DIR="/var/lib/tarisya-agent"
readonly SERVICE_NAME="tarisya-agent"
readonly SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

TEMP_DIR=""
ROLLBACK_DIR=""
UPGRADE=0
MUTATION_STARTED=0
INSTALL_SUCCEEDED=0
SERVICE_WAS_ACTIVE=0

log() {
  printf '%s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

validate_env_value() {
  local name="$1"
  local value="$2"
  case "$value" in
    *$'\n'* | *$'\r'* | *$'\t'* | *' '* | *'"'* | *"'"* | *'\'* | *'#'*)
      fail "$name contains characters that are unsafe in a systemd environment file"
      ;;
  esac
}

rollback_installation() {
  log "Agent installation failed; restoring the previous installation..."
  systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true

  if [ -f "${ROLLBACK_DIR}/tarisya-agent" ]; then
    install -m 0755 "${ROLLBACK_DIR}/tarisya-agent" "${INSTALL_DIR}/tarisya-agent"
  elif [ "$UPGRADE" -eq 0 ]; then
    rm -f "${INSTALL_DIR}/tarisya-agent"
  fi
  if [ -f "${ROLLBACK_DIR}/tarisya-agent.service" ]; then
    install -m 0644 "${ROLLBACK_DIR}/tarisya-agent.service" "$SERVICE_FILE"
  elif [ "$UPGRADE" -eq 0 ]; then
    rm -f "$SERVICE_FILE"
  fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  if [ "$SERVICE_WAS_ACTIVE" -eq 1 ]; then
    systemctl start "$SERVICE_NAME" >/dev/null 2>&1 || true
  fi
}

on_exit() {
  local exit_code=$?
  trap - EXIT
  if [ "$exit_code" -ne 0 ] && [ "$MUTATION_STARTED" -eq 1 ] && [ "$INSTALL_SUCCEEDED" -eq 0 ]; then
    rollback_installation
  fi
  if [ -n "$TEMP_DIR" ]; then
    rm -rf "$TEMP_DIR"
  fi
  exit "$exit_code"
}

trap on_exit EXIT
umask 077

[ "$#" -eq 0 ] || fail "this installer does not accept positional arguments"
[ "$(id -u)" -eq 0 ] || fail "this installer must run as root"
[ "$(uname -s)" = "Linux" ] || fail "this installer currently supports Linux only"
[ -d /run/systemd/system ] || fail "systemd is not running"

for command in awk chown cp curl getent groupadd id install mkdir mktemp rm sha256sum sleep systemctl tar useradd; do
  require_command "$command"
done

case "$(uname -m)" in
  x86_64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

CORE_URL="${TARISYA_CORE_URL:-}"
SERVER_ID="${TARISYA_SERVER_ID:-}"
API_KEY="${TARISYA_API_KEY:-}"
INTERVAL="${TARISYA_INTERVAL:-15s}"
HTTP_TIMEOUT="${TARISYA_HTTP_TIMEOUT:-10s}"
DISK_PATH="${TARISYA_DISK_PATH:-/}"

[ -n "$CORE_URL" ] || fail "TARISYA_CORE_URL is required"
[ -n "$SERVER_ID" ] || fail "TARISYA_SERVER_ID is required"
[ -n "$API_KEY" ] || fail "TARISYA_API_KEY is required"
case "$CORE_URL" in
  http://* | https://*) ;;
  *) fail "TARISYA_CORE_URL must start with http:// or https://" ;;
esac
for pair in \
  "TARISYA_CORE_URL:$CORE_URL" \
  "TARISYA_SERVER_ID:$SERVER_ID" \
  "TARISYA_API_KEY:$API_KEY" \
  "TARISYA_INTERVAL:$INTERVAL" \
  "TARISYA_HTTP_TIMEOUT:$HTTP_TIMEOUT" \
  "TARISYA_DISK_PATH:$DISK_PATH"; do
  validate_env_value "${pair%%:*}" "${pair#*:}"
done

if [ -n "${TARISYA_VERSION:-}" ]; then
  RELEASE_TAG="$TARISYA_VERSION"
  case "$RELEASE_TAG" in
    v*) ;;
    *) RELEASE_TAG="v${RELEASE_TAG}" ;;
  esac
else
  LATEST_URL="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPOSITORY}/releases/latest")"
  RELEASE_TAG="${LATEST_URL##*/}"
fi

if [[ ! "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  fail "invalid release version: ${RELEASE_TAG}"
fi

VERSION="${RELEASE_TAG#v}"
ARCHIVE="tarisya_${VERSION}_linux_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPOSITORY}/releases/download/${RELEASE_TAG}"
TEMP_DIR="$(mktemp -d)"
ROLLBACK_DIR="${TEMP_DIR}/rollback"
mkdir -p "$ROLLBACK_DIR"

log "Downloading Tarisya Agent ${RELEASE_TAG} for linux/${ARCH}..."
curl -fsSLo "${TEMP_DIR}/${ARCHIVE}" "${DOWNLOAD_URL}/${ARCHIVE}"
curl -fsSLo "${TEMP_DIR}/checksums.txt" "${DOWNLOAD_URL}/checksums.txt"

EXPECTED_CHECKSUM="$(awk -v archive="$ARCHIVE" '$2 == archive { print $1; exit }' "${TEMP_DIR}/checksums.txt")"
[ -n "$EXPECTED_CHECKSUM" ] || fail "${ARCHIVE} is missing from checksums.txt"
(
  cd "$TEMP_DIR"
  printf '%s  %s\n' "$EXPECTED_CHECKSUM" "$ARCHIVE" | sha256sum --check --status
) || fail "release checksum verification failed"

tar -xzf "${TEMP_DIR}/${ARCHIVE}" -C "$TEMP_DIR"
[ -f "${TEMP_DIR}/tarisya-agent" ] || fail "release archive is missing tarisya-agent"
[ -f "${TEMP_DIR}/packaging/systemd/tarisya-agent.service" ] || fail "release archive is missing the agent systemd service"

HEALTH_URL="${CORE_URL%/}/health"
curl -fsS "$HEALTH_URL" >/dev/null || fail "Tarisya Core health check failed at ${HEALTH_URL}"

if ! getent group tarisya >/dev/null 2>&1; then
  groupadd --system tarisya
fi
if ! id -u tarisya >/dev/null 2>&1; then
  NOLOGIN_SHELL="$(command -v nologin || true)"
  [ -n "$NOLOGIN_SHELL" ] || NOLOGIN_SHELL="/usr/sbin/nologin"
  useradd --system --gid tarisya --home-dir "$DATA_DIR" --shell "$NOLOGIN_SHELL" tarisya
fi

install -d -m 0750 -o root -g tarisya "$CONFIG_DIR"
install -d -m 0750 -o tarisya -g tarisya "$DATA_DIR"

if [ ! -f "$CONFIG_FILE" ]; then
  {
    printf 'TARISYA_SERVER_ID=%s\n' "$SERVER_ID"
    printf 'TARISYA_API_KEY=%s\n' "$API_KEY"
    printf 'TARISYA_CORE_URL=%s\n' "${CORE_URL%/}"
    printf 'TARISYA_INTERVAL=%s\n' "$INTERVAL"
    printf 'TARISYA_HTTP_TIMEOUT=%s\n' "$HTTP_TIMEOUT"
    printf 'TARISYA_DISK_PATH=%s\n' "$DISK_PATH"
  } >"${TEMP_DIR}/agent.env"
  install -m 0640 -o root -g tarisya "${TEMP_DIR}/agent.env" "$CONFIG_FILE"
  log "Created ${CONFIG_FILE}"
else
  log "Preserving existing ${CONFIG_FILE}"
fi

if [ -e "${INSTALL_DIR}/tarisya-agent" ] || [ -e "$SERVICE_FILE" ]; then
  UPGRADE=1
fi
if systemctl is-active --quiet "$SERVICE_NAME"; then
  SERVICE_WAS_ACTIVE=1
fi
if [ -f "${INSTALL_DIR}/tarisya-agent" ]; then
  cp -p "${INSTALL_DIR}/tarisya-agent" "${ROLLBACK_DIR}/tarisya-agent"
fi
if [ -f "$SERVICE_FILE" ]; then
  cp -p "$SERVICE_FILE" "${ROLLBACK_DIR}/tarisya-agent.service"
fi

MUTATION_STARTED=1
if [ "$SERVICE_WAS_ACTIVE" -eq 1 ]; then
  systemctl stop "$SERVICE_NAME"
fi

install -m 0755 "${TEMP_DIR}/tarisya-agent" "${INSTALL_DIR}/tarisya-agent"
install -m 0644 "${TEMP_DIR}/packaging/systemd/tarisya-agent.service" "$SERVICE_FILE"

systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null
systemctl start "$SERVICE_NAME"

ATTEMPT=1
while [ "$ATTEMPT" -le 10 ]; do
  if systemctl is-active --quiet "$SERVICE_NAME"; then
    INSTALL_SUCCEEDED=1
    break
  fi
  sleep 1
  ATTEMPT=$((ATTEMPT + 1))
done

if [ "$INSTALL_SUCCEEDED" -ne 1 ]; then
  systemctl status "$SERVICE_NAME" --no-pager >&2 || true
  fail "Tarisya Agent failed to start"
fi

log ""
log "Tarisya Agent ${RELEASE_TAG} installed successfully"
log ""
log "Agent service: running"
log "Core address: ${CORE_URL%/}"
log "Configuration: ${CONFIG_FILE}"
log ""
log "Commands:"
log "  systemctl status ${SERVICE_NAME}"
log "  journalctl -u ${SERVICE_NAME}"
