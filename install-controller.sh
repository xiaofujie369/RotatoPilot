#!/bin/sh
set -eu

INSTALL_DIR="${INSTALL_DIR:-/opt/rotatopilot}"
IMAGE="${CONTROLLER_IMAGE:-ghcr.io/xiaofujie369/rotatopilot-controller:latest}"

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }

[ "$(uname -s 2>/dev/null || true)" = "Linux" ] || die "Only Linux hosts are supported"
[ "$(id -u)" -eq 0 ] || die "Run this installer as root (for example: sudo sh install-controller.sh)"
case "$(uname -m)" in
  x86_64|amd64) PLATFORM=amd64 ;;
  aarch64|arm64) PLATFORM=arm64 ;;
  *) die "Unsupported CPU architecture: $(uname -m). Supported: amd64, arm64" ;;
esac
case "$INSTALL_DIR" in /*) ;; *) die "INSTALL_DIR must be an absolute path" ;; esac
[ "$INSTALL_DIR" != "/" ] || die "INSTALL_DIR cannot be /"

OS_NAME="Linux"
if [ -r /etc/os-release ]; then
  OS_NAME="$(awk -F= '$1=="PRETTY_NAME"{gsub(/^\"|\"$/,"",$2); print $2; exit}' /etc/os-release 2>/dev/null || true)"
  [ -n "$OS_NAME" ] || OS_NAME="Linux"
fi
info "Detected $OS_NAME ($PLATFORM)"

install_curl() {
  if command -v curl >/dev/null 2>&1; then return; fi
  info "Installing curl and CA certificates"
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates
  elif command -v dnf >/dev/null 2>&1; then dnf install -y curl ca-certificates
  elif command -v yum >/dev/null 2>&1; then yum install -y curl ca-certificates
  elif command -v zypper >/dev/null 2>&1; then zypper --non-interactive install curl ca-certificates
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache curl ca-certificates
  else die "curl is missing and no supported package manager was found"; fi
}

start_docker() {
  if command -v systemctl >/dev/null 2>&1; then systemctl enable --now docker 2>/dev/null || true
  elif command -v service >/dev/null 2>&1; then service docker start 2>/dev/null || true
  fi
  docker info >/dev/null 2>&1 || die "Docker daemon is not running"
}

install_docker() {
  command -v docker >/dev/null 2>&1 && return
  install_curl
  info "Installing Docker Engine with Docker's official installer"
  tmp="$(mktemp -t rotatopilot-docker.XXXXXX)" || die "Cannot create a temporary file"
  trap 'rm -f "$tmp"' EXIT HUP INT TERM
  curl --proto '=https' --tlsv1.2 -fsSL https://get.docker.com -o "$tmp"
  sh "$tmp"
  rm -f "$tmp"; trap - EXIT HUP INT TERM
}

install_compose() {
  docker compose version >/dev/null 2>&1 && return
  command -v docker-compose >/dev/null 2>&1 && return
  info "Installing Docker Compose"
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y docker-compose-plugin 2>/dev/null || DEBIAN_FRONTEND=noninteractive apt-get install -y docker-compose
  elif command -v dnf >/dev/null 2>&1; then dnf install -y docker-compose-plugin
  elif command -v yum >/dev/null 2>&1; then yum install -y docker-compose-plugin
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache docker-cli-compose
  else die "Docker Compose v2 is required"; fi
}

compose() {
  if docker compose version >/dev/null 2>&1; then docker compose "$@"; else docker-compose "$@"; fi
}

install_curl
install_docker
start_docker
install_compose
compose version >/dev/null 2>&1 || die "Docker Compose is unavailable"

PORT="${APP_PORT:-}"
USER_NAME="${ADMIN_USERNAME:-}"
PASS="${ADMIN_PASSWORD:-}"
URL="${PUBLIC_URL:-}"
if [ -t 0 ]; then
  [ -n "$PORT" ] || { printf "Dashboard port [8080]: "; read -r PORT; PORT="${PORT:-8080}"; }
  [ -n "$USER_NAME" ] || { printf "Admin username [admin]: "; read -r USER_NAME; USER_NAME="${USER_NAME:-admin}"; }
  if [ -z "$PASS" ]; then printf "Admin password: "; stty -echo; read -r PASS; stty echo; printf '\n'; fi
  [ -n "$URL" ] || { printf "Public URL [http://127.0.0.1:%s]: " "$PORT"; read -r URL; URL="${URL:-http://127.0.0.1:$PORT}"; }
else
  PORT="${PORT:-8080}"; USER_NAME="${USER_NAME:-admin}"; URL="${URL:-http://127.0.0.1:$PORT}"
  [ -n "$PASS" ] || die "Set ADMIN_PASSWORD for non-interactive installation"
fi

case "$PORT" in ''|*[!0-9]*) die "APP_PORT must be numeric" ;; esac
[ "$PORT" -ge 1 ] 2>/dev/null && [ "$PORT" -le 65535 ] 2>/dev/null || die "APP_PORT must be between 1 and 65535"
case "$USER_NAME" in ''|*[!A-Za-z0-9_.-]*) die "ADMIN_USERNAME contains unsupported characters" ;; esac
[ "${#PASS}" -ge 12 ] || die "ADMIN_PASSWORD must contain at least 12 characters"
case "$PASS" in *"'"*|*"
"*|*""*) die "ADMIN_PASSWORD cannot contain quotes or line breaks" ;; esac
case "$URL" in http://*|https://*) ;; *) die "PUBLIC_URL must begin with http:// or https://" ;; esac
case "$URL" in *[[:space:]]*|*"'"*) die "PUBLIC_URL contains unsupported characters" ;; esac

random_hex() { od -An -N "$1" -tx1 /dev/urandom | tr -d ' \n'; }
JWT="$(random_hex 32)"; ENCRYPTION="$(random_hex 32)"
umask 077
mkdir -p "$INSTALL_DIR"
chmod 700 "$INSTALL_DIR"
cat > "$INSTALL_DIR/.env" <<EOF
APP_ENV=production
APP_PORT='$PORT'
PUBLIC_URL='$URL'
ADMIN_USERNAME='$USER_NAME'
ADMIN_PASSWORD='$PASS'
JWT_SECRET='$JWT'
APP_ENCRYPTION_KEY='$ENCRYPTION'
DB_PATH='/app/data/app.db'
AUTO_ROTATE_GLOBAL_ENABLED='false'
CHECK_INTERVAL_SECONDS='300'
FAILURE_THRESHOLD='3'
SUCCESS_RECOVERY_THRESHOLD='2'
ROTATION_COOLDOWN_MINUTES='30'
CHANGE_IP_WAIT_SECONDS='180'
CHANGE_IP_POLL_TIMEOUT_SECONDS='600'
MAX_ROTATIONS_PER_DAY='10'
REQUIRE_CONFIRMATION_CHECK='true'
TELEGRAM_ENABLED='false'
TELEGRAM_BOT_TOKEN=''
TELEGRAM_CHAT_ID=''
LOG_LEVEL='info'
EOF
chmod 600 "$INSTALL_DIR/.env"
cat > "$INSTALL_DIR/docker-compose.yml" <<EOF
services:
  controller:
    image: $IMAGE
    container_name: rotatopilot-controller
    restart: unless-stopped
    env_file: [.env]
    ports: ["$PORT:8080"]
    volumes: ["rotatopilot-data:/app/data"]
    read_only: true
    tmpfs: ["/tmp:rw,noexec,nosuid,size=16m"]
    security_opt: ["no-new-privileges:true"]
    cap_drop: [ALL]
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
volumes:
  rotatopilot-data:
EOF
chmod 600 "$INSTALL_DIR/docker-compose.yml"

cd "$INSTALL_DIR"
info "Pulling controller image"
compose pull
compose up -d
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
    info "RotatoPilot is healthy at $URL"
    [ "${URL#http://}" = "$URL" ] || printf 'WARNING: Configure HTTPS before exposing the dashboard publicly.\n' >&2
    exit 0
  fi
  sleep 2
done
compose logs --tail=50 controller >&2 || true
die "Controller did not become healthy"
