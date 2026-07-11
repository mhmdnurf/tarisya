#!/usr/bin/env bash
set -Eeuo pipefail

readonly REPOSITORY="mhmdnurf/tarisya"
readonly INSTALL_DIR="/usr/local/bin"
readonly CONFIG_DIR="/etc/tarisya"
readonly CONFIG_FILE="${CONFIG_DIR}/core.env"
readonly AGENT_CONFIG_FILE="${CONFIG_DIR}/agent.env"
readonly DATA_DIR="/var/lib/tarisya"
readonly AGENT_DATA_DIR="/var/lib/tarisya-agent"
readonly BACKUP_DIR="/var/backups/tarisya"
readonly SERVICE_NAME="tarisya-core"
readonly SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
readonly AGENT_SERVICE_NAME="tarisya-agent"
readonly AGENT_SERVICE_FILE="/etc/systemd/system/${AGENT_SERVICE_NAME}.service"

TEMP_DIR=""
ROLLBACK_DIR=""
UPGRADE=0
MUTATION_STARTED=0
INSTALL_SUCCEEDED=0
SERVICE_WAS_ACTIVE=0
AGENT_SERVICE_WAS_ACTIVE=0
INSTALL_LOCAL_AGENT=0

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

prompt_value() {
  local variable_name="$1"
  local prompt="$2"
  local secret="${3:-false}"
  local value="${!variable_name:-}"
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

append_systemd_environment() {
  local key="$1"
  local value="$2"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s="%s"\n' "$key" "$value"
}

wait_for_core() {
  local health_url="$1"
  local attempt=1
  while [ "$attempt" -le 30 ]; do
    if systemctl is-active --quiet "$SERVICE_NAME" && curl -fsS "$health_url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    attempt=$((attempt + 1))
  done
  return 1
}

rollback_installation() {
  log "Installation failed; restoring the previous binaries and services..."
  systemctl stop "$AGENT_SERVICE_NAME" >/dev/null 2>&1 || true
  systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true

  for name in tarisya tarisya-core tarisya-agent; do
    if [ -f "${ROLLBACK_DIR}/${name}" ]; then
      install -m 0755 "${ROLLBACK_DIR}/${name}" "${INSTALL_DIR}/${name}"
    else
      rm -f "${INSTALL_DIR}/${name}"
    fi
  done
  if [ -f "${ROLLBACK_DIR}/tarisya-core.service" ]; then
    install -m 0644 "${ROLLBACK_DIR}/tarisya-core.service" "$SERVICE_FILE"
  else
    rm -f "$SERVICE_FILE"
  fi
  if [ -f "${ROLLBACK_DIR}/tarisya-agent.service" ]; then
    install -m 0644 "${ROLLBACK_DIR}/tarisya-agent.service" "$AGENT_SERVICE_FILE"
  else
    rm -f "$AGENT_SERVICE_FILE"
  fi
  if [ -f "${ROLLBACK_DIR}/core.env" ]; then
    install -m 0640 -o root -g tarisya "${ROLLBACK_DIR}/core.env" "$CONFIG_FILE"
  elif [ "$UPGRADE" -eq 0 ]; then
    rm -f "$CONFIG_FILE"
  fi
  if [ -f "${ROLLBACK_DIR}/agent.env" ]; then
    install -m 0640 -o root -g tarisya "${ROLLBACK_DIR}/agent.env" "$AGENT_CONFIG_FILE"
  else
    rm -f "$AGENT_CONFIG_FILE"
  fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  if [ "$SERVICE_WAS_ACTIVE" -eq 1 ]; then
    systemctl start "$SERVICE_NAME" >/dev/null 2>&1 || true
  fi
  if [ "$AGENT_SERVICE_WAS_ACTIVE" -eq 1 ]; then
    systemctl start "$AGENT_SERVICE_NAME" >/dev/null 2>&1 || true
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

for command in awk chown cp curl date getent grep groupadd id install journalctl mkdir mktemp od rm sha256sum sleep systemctl tar tr useradd; do
  require_command "$command"
done

case "$(uname -m)" in
  x86_64)
    ARCH="amd64"
    ;;
  aarch64 | arm64)
    ARCH="arm64"
    ;;
  *)
    fail "unsupported architecture: $(uname -m)"
    ;;
esac

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

log "Downloading Tarisya ${RELEASE_TAG} for linux/${ARCH}..."
curl -fsSLo "${TEMP_DIR}/${ARCHIVE}" "${DOWNLOAD_URL}/${ARCHIVE}"
curl -fsSLo "${TEMP_DIR}/checksums.txt" "${DOWNLOAD_URL}/checksums.txt"

EXPECTED_CHECKSUM="$(awk -v archive="$ARCHIVE" '$2 == archive { print $1; exit }' "${TEMP_DIR}/checksums.txt")"
[ -n "$EXPECTED_CHECKSUM" ] || fail "${ARCHIVE} is missing from checksums.txt"
(
  cd "$TEMP_DIR"
  printf '%s  %s\n' "$EXPECTED_CHECKSUM" "$ARCHIVE" | sha256sum --check --status
) || fail "release checksum verification failed"

tar -xzf "${TEMP_DIR}/${ARCHIVE}" -C "$TEMP_DIR"
for binary in tarisya tarisya-core tarisya-agent; do
  [ -f "${TEMP_DIR}/${binary}" ] || fail "release archive is missing ${binary}"
done
[ -f "${TEMP_DIR}/packaging/systemd/tarisya-core.service" ] || fail "release archive is missing the systemd service"
[ -f "${TEMP_DIR}/packaging/systemd/tarisya-agent.service" ] || fail "release archive is missing the agent systemd service"

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
install -d -m 0750 -o tarisya -g tarisya "$AGENT_DATA_DIR"
install -d -m 0750 -o tarisya -g tarisya "$BACKUP_DIR"

if [ ! -f "$CONFIG_FILE" ]; then
  JWT_SECRET="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
  COOKIE_SECURE="${TARISYA_COOKIE_SECURE:-false}"
  PUBLIC_CORE_URL="${TARISYA_PUBLIC_CORE_URL:-http://localhost:8081}"
  ALLOWED_ORIGINS="${TARISYA_ALLOWED_ORIGINS:-http://localhost:3000,http://localhost:5173}"
  case "$COOKIE_SECURE" in
    true | false) ;;
    *) fail "TARISYA_COOKIE_SECURE must be true or false" ;;
  esac
  {
    printf 'TARISYA_CORE_ADDRESS=:8081\n'
    printf 'TARISYA_PUBLIC_CORE_URL=%s\n' "$PUBLIC_CORE_URL"
    printf 'TARISYA_DATABASE_URL=file:/var/lib/tarisya/tarisya.db\n'
    printf 'TARISYA_JWT_SECRET=%s\n' "$JWT_SECRET"
    printf 'TARISYA_ALLOWED_ORIGINS=%s\n' "$ALLOWED_ORIGINS"
    printf 'TARISYA_COOKIE_SECURE=%s\n' "$COOKIE_SECURE"
    printf 'TARISYA_RETENTION_RAW=7d\n'
    printf 'TARISYA_RETENTION_5M=30d\n'
    printf 'TARISYA_RETENTION_AGGREGATED=90d\n'
    printf 'TARISYA_MAX_DATABASE_SIZE=5GB\n'
    printf 'TARISYA_DATABASE_WARNING_THRESHOLD=0.8\n'
  } >"${TEMP_DIR}/core.env"
  install -m 0640 -o root -g tarisya "${TEMP_DIR}/core.env" "$CONFIG_FILE"
  log "Created ${CONFIG_FILE}"
else
  log "Preserving existing ${CONFIG_FILE}"
fi

if [ -e "${INSTALL_DIR}/tarisya-core" ] || [ -e "$SERVICE_FILE" ]; then
  UPGRADE=1
fi
if systemctl is-active --quiet "$SERVICE_NAME"; then
  SERVICE_WAS_ACTIVE=1
fi
if systemctl is-active --quiet "$AGENT_SERVICE_NAME"; then
  AGENT_SERVICE_WAS_ACTIVE=1
fi

if [ "$UPGRADE" -eq 1 ]; then
  for name in tarisya tarisya-core tarisya-agent; do
    if [ -f "${INSTALL_DIR}/${name}" ]; then
      cp -p "${INSTALL_DIR}/${name}" "${ROLLBACK_DIR}/${name}"
    fi
  done
  if [ -f "$SERVICE_FILE" ]; then
    cp -p "$SERVICE_FILE" "${ROLLBACK_DIR}/tarisya-core.service"
  fi
  if [ -f "$AGENT_SERVICE_FILE" ]; then
    cp -p "$AGENT_SERVICE_FILE" "${ROLLBACK_DIR}/tarisya-agent.service"
  fi
  if [ -f "$CONFIG_FILE" ]; then
    cp -p "$CONFIG_FILE" "${ROLLBACK_DIR}/core.env"
  fi
  if [ -f "$AGENT_CONFIG_FILE" ]; then
    cp -p "$AGENT_CONFIG_FILE" "${ROLLBACK_DIR}/agent.env"
  fi

  DATABASE_URL="$(awk -F= '$1 == "TARISYA_DATABASE_URL" { sub(/^[^=]*=/, ""); print; exit }' "$CONFIG_FILE")"
  DATABASE_URL="${DATABASE_URL#\"}"
  DATABASE_URL="${DATABASE_URL%\"}"
  [ -n "$DATABASE_URL" ] || DATABASE_URL="file:/var/lib/tarisya/tarisya.db"
  UPGRADE_BACKUP="${BACKUP_DIR}/tarisya-before-${RELEASE_TAG}-$(date -u +%Y%m%dT%H%M%SZ).db"
  log "Creating pre-upgrade backup..."
  "${TEMP_DIR}/tarisya" backup --database "$DATABASE_URL" --output "$UPGRADE_BACKUP"
  chown tarisya:tarisya "$UPGRADE_BACKUP" "${UPGRADE_BACKUP}.sha256"
fi

if [ ! -f "$AGENT_CONFIG_FILE" ]; then
  INSTALL_LOCAL_AGENT_VALUE="${TARISYA_INSTALL_LOCAL_AGENT:-}"
  if [ -z "$INSTALL_LOCAL_AGENT_VALUE" ]; then
    [ -r /dev/tty ] || fail "TARISYA_INSTALL_LOCAL_AGENT is required for non-interactive installation"
    read -r -p "Install the local monitoring agent? [Y/n]: " INSTALL_LOCAL_AGENT_VALUE </dev/tty
    INSTALL_LOCAL_AGENT_VALUE="${INSTALL_LOCAL_AGENT_VALUE:-y}"
  fi
  case "${INSTALL_LOCAL_AGENT_VALUE,,}" in
    y | yes | true | 1) INSTALL_LOCAL_AGENT=1 ;;
    n | no | false | 0) INSTALL_LOCAL_AGENT=0 ;;
    *) fail "TARISYA_INSTALL_LOCAL_AGENT must be true or false" ;;
  esac
fi

if [ "$INSTALL_LOCAL_AGENT" -eq 1 ]; then
  ADMIN_NAME="${TARISYA_ADMIN_NAME:-}"
  ADMIN_EMAIL="${TARISYA_ADMIN_EMAIL:-}"
  ADMIN_PASSWORD="${TARISYA_ADMIN_PASSWORD:-}"
  prompt_value ADMIN_NAME "Administrator name: "
  prompt_value ADMIN_EMAIL "Administrator email: "
  prompt_value ADMIN_PASSWORD "Administrator password: " true
  if [ -z "${TARISYA_ADMIN_PASSWORD:-}" ]; then
    ADMIN_PASSWORD_CONFIRMATION=""
    prompt_value ADMIN_PASSWORD_CONFIRMATION "Confirm password: " true
    [ "$ADMIN_PASSWORD" = "$ADMIN_PASSWORD_CONFIRMATION" ] || fail "administrator passwords do not match"
  fi
  [ -n "$ADMIN_NAME" ] || fail "administrator name is required"
  case "$ADMIN_EMAIL" in
    *@*.*) ;;
    *) fail "administrator email is invalid" ;;
  esac
  [ "${#ADMIN_PASSWORD}" -ge 8 ] && [ "${#ADMIN_PASSWORD}" -le 128 ] || fail "administrator password must contain 8 to 128 characters"
  for value in "$ADMIN_NAME" "$ADMIN_EMAIL" "$ADMIN_PASSWORD"; do
    case "$value" in
      *$'\n'* | *$'\r'*) fail "administrator values must not contain newlines" ;;
    esac
  done

  LOCAL_SERVER_ID="srv_$(od -An -N10 -tx1 /dev/urandom | tr -d ' \n')"
  LOCAL_API_KEY="tar_$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"

  cp "$CONFIG_FILE" "${TEMP_DIR}/core.env"
  {
    append_systemd_environment TARISYA_BOOTSTRAP_SERVER_ID "$LOCAL_SERVER_ID"
    append_systemd_environment TARISYA_BOOTSTRAP_API_KEY "$LOCAL_API_KEY"
    append_systemd_environment TARISYA_BOOTSTRAP_USER_NAME "$ADMIN_NAME"
    append_systemd_environment TARISYA_BOOTSTRAP_USER_EMAIL "$ADMIN_EMAIL"
    append_systemd_environment TARISYA_BOOTSTRAP_USER_PASSWORD "$ADMIN_PASSWORD"
  } >>"${TEMP_DIR}/core.env"
  install -m 0640 -o root -g tarisya "${TEMP_DIR}/core.env" "$CONFIG_FILE"

  {
    printf 'TARISYA_SERVER_ID=%s\n' "$LOCAL_SERVER_ID"
    printf 'TARISYA_API_KEY=%s\n' "$LOCAL_API_KEY"
    printf 'TARISYA_CORE_URL=http://127.0.0.1:8081\n'
    printf 'TARISYA_INTERVAL=15s\n'
    printf 'TARISYA_HTTP_TIMEOUT=10s\n'
    printf 'TARISYA_DISK_PATH=/\n'
  } >"${TEMP_DIR}/agent.env"
  install -m 0640 -o root -g tarisya "${TEMP_DIR}/agent.env" "$AGENT_CONFIG_FILE"
  unset ADMIN_PASSWORD ADMIN_PASSWORD_CONFIRMATION LOCAL_API_KEY TARISYA_ADMIN_PASSWORD
fi

MUTATION_STARTED=1
if [ "$AGENT_SERVICE_WAS_ACTIVE" -eq 1 ]; then
  systemctl stop "$AGENT_SERVICE_NAME"
fi
if [ "$SERVICE_WAS_ACTIVE" -eq 1 ]; then
  systemctl stop "$SERVICE_NAME"
fi

install -m 0755 "${TEMP_DIR}/tarisya" "${INSTALL_DIR}/tarisya"
install -m 0755 "${TEMP_DIR}/tarisya-core" "${INSTALL_DIR}/tarisya-core"
install -m 0755 "${TEMP_DIR}/tarisya-agent" "${INSTALL_DIR}/tarisya-agent"
install -m 0644 "${TEMP_DIR}/packaging/systemd/tarisya-core.service" "$SERVICE_FILE"
if [ -f "$AGENT_CONFIG_FILE" ]; then
  install -m 0644 "${TEMP_DIR}/packaging/systemd/tarisya-agent.service" "$AGENT_SERVICE_FILE"
fi

systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null
systemctl start "$SERVICE_NAME"

HEALTH_URL="${TARISYA_HEALTH_URL:-http://127.0.0.1:8081/health}"
if ! wait_for_core "$HEALTH_URL"; then
  systemctl status "$SERVICE_NAME" --no-pager >&2 || true
  fail "Tarisya Core failed its health check at ${HEALTH_URL}"
fi

if [ "$INSTALL_LOCAL_AGENT" -eq 1 ]; then
  awk '!/^TARISYA_BOOTSTRAP_(SERVER_ID|API_KEY|USER_NAME|USER_EMAIL|USER_PASSWORD)=/' \
    "$CONFIG_FILE" >"${TEMP_DIR}/core-without-bootstrap.env"
  install -m 0640 -o root -g tarisya "${TEMP_DIR}/core-without-bootstrap.env" "$CONFIG_FILE"
  systemctl restart "$SERVICE_NAME"
  if ! wait_for_core "$HEALTH_URL"; then
    systemctl status "$SERVICE_NAME" --no-pager >&2 || true
    fail "Tarisya Core failed after removing bootstrap credentials"
  fi
fi

if [ -f "$AGENT_CONFIG_FILE" ]; then
  systemctl enable "$AGENT_SERVICE_NAME" >/dev/null
  AGENT_START_TIME="$(date '+%Y-%m-%d %H:%M:%S')"
  systemctl start "$AGENT_SERVICE_NAME"
  AGENT_READY=0
  ATTEMPT=1
  while [ "$ATTEMPT" -le 30 ]; do
    if systemctl is-active --quiet "$AGENT_SERVICE_NAME" && \
      journalctl -u "$AGENT_SERVICE_NAME" --since "$AGENT_START_TIME" --no-pager -o cat 2>/dev/null | grep -F "metrics sent" >/dev/null; then
      AGENT_READY=1
      break
    fi
    sleep 1
    ATTEMPT=$((ATTEMPT + 1))
  done
  if [ "$AGENT_READY" -ne 1 ]; then
    systemctl status "$AGENT_SERVICE_NAME" --no-pager >&2 || true
    journalctl -u "$AGENT_SERVICE_NAME" --since "$AGENT_START_TIME" --no-pager -n 50 >&2 || true
    fail "Tarisya Agent did not submit metrics successfully"
  fi
fi

INSTALL_SUCCEEDED=1

log ""
log "Tarisya ${RELEASE_TAG} installed successfully"
log ""
log "Core service: running"
log "Core address: ${HEALTH_URL%/health}"
if [ -f "$AGENT_CONFIG_FILE" ]; then
  log "Local agent: running"
fi
log "Configuration: ${CONFIG_FILE}"
log "Database: ${DATA_DIR}/tarisya.db"
log "Backups: ${BACKUP_DIR}"
log ""
log "Commands:"
log "  tarisya doctor"
log "  sudo systemctl status ${SERVICE_NAME}"
log "  sudo journalctl -u ${SERVICE_NAME}"
if [ -f "$AGENT_CONFIG_FILE" ]; then
  log "  sudo systemctl status ${AGENT_SERVICE_NAME}"
  log "  sudo journalctl -u ${AGENT_SERVICE_NAME}"
fi
log "  sudo tarisya database check --database file:${DATA_DIR}/tarisya.db"
log "  sudo tarisya backup --database file:${DATA_DIR}/tarisya.db --output ${BACKUP_DIR}/manual.db"
