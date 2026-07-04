#!/bin/sh
set -eu

CONTROLLER="${CONTROLLER_URL:-}"
TOKEN="${AGENT_TOKEN:-}"
SERVER_ID="${SERVER_ID:-}"
IMAGE="${AGENT_IMAGE:-ghcr.io/xiaofujie369/rotatopilot-agent:latest}"
CONFIG_DIR="${AGENT_CONFIG_DIR:-/opt/rotatopilot-agent}"

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }

while [ "$#" -gt 0 ]; do
  case "$1" in
    --controller) [ "$#" -ge 2 ] || die "--controller requires a value"; CONTROLLER=$2; shift 2 ;;
    --agent-token) [ "$#" -ge 2 ] || die "--agent-token requires a value"; TOKEN=$2; shift 2 ;;
    --server-id) [ "$#" -ge 2 ] || die "--server-id requires a value"; SERVER_ID=$2; shift 2 ;;
    --image) [ "$#" -ge 2 ] || die "--image requires a value"; IMAGE=$2; shift 2 ;;
    *) die "Unknown option: $1" ;;
  esac
done

[ "$(uname -s 2>/dev/null || true)" = "Linux" ] || die "Only Linux hosts are supported"
[ "$(id -u)" -eq 0 ] || die "Run this installer as root (for example: sudo sh install-agent.sh ...)"
case "$(uname -m)" in x86_64|amd64) PLATFORM=amd64 ;; aarch64|arm64) PLATFORM=arm64 ;; *) die "Unsupported CPU architecture: $(uname -m)" ;; esac
[ -n "$CONTROLLER" ] && [ -n "$TOKEN" ] && [ -n "$SERVER_ID" ] || die "Usage: install-agent.sh --controller URL --agent-token TOKEN --server-id ID"
case "$CONTROLLER" in
  https://*) ;;
  http://127.0.0.1:*|http://localhost:*|http://\[::1\]:*) ;;
  http://*) [ "${ALLOW_INSECURE_HTTP:-false}" = "true" ] || die "HTTPS is required for a remote controller (set ALLOW_INSECURE_HTTP=true only on a trusted private network)" ;;
  *) die "Controller URL must begin with https://" ;;
esac
case "$CONTROLLER" in *[[:space:]]*|*"'"*) die "Controller URL contains unsupported characters" ;; esac
case "$TOKEN" in air_*) ;; *) die "Agent token must begin with air_" ;; esac
case "$TOKEN" in *[!A-Za-z0-9_-]*) die "Agent token contains unsupported characters" ;; esac
case "$SERVER_ID" in ''|*[!A-Za-z0-9._:-]*) die "Server ID contains unsupported characters" ;; esac
case "$CONFIG_DIR" in /*) ;; *) die "AGENT_CONFIG_DIR must be absolute" ;; esac

if ! command -v docker >/dev/null 2>&1; then
  command -v curl >/dev/null 2>&1 || {
    if command -v apt-get >/dev/null 2>&1; then apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates
    elif command -v dnf >/dev/null 2>&1; then dnf install -y curl ca-certificates
    elif command -v yum >/dev/null 2>&1; then yum install -y curl ca-certificates
    elif command -v apk >/dev/null 2>&1; then apk add --no-cache curl ca-certificates
    else die "curl is required to install Docker"; fi
  }
  info "Installing Docker Engine with Docker's official installer"
  tmp="$(mktemp -t rotatopilot-docker.XXXXXX)" || die "Cannot create a temporary file"
  trap 'rm -f "$tmp"' EXIT HUP INT TERM
  curl --proto '=https' --tlsv1.2 -fsSL https://get.docker.com -o "$tmp"
  sh "$tmp"
  rm -f "$tmp"; trap - EXIT HUP INT TERM
fi
if command -v systemctl >/dev/null 2>&1; then systemctl enable --now docker 2>/dev/null || true
elif command -v service >/dev/null 2>&1; then service docker start 2>/dev/null || true
fi
docker info >/dev/null 2>&1 || die "Docker daemon is not running"

umask 077
mkdir -p "$CONFIG_DIR"
chmod 700 "$CONFIG_DIR"
cat > "$CONFIG_DIR/.env" <<EOF
CONTROLLER_URL='$CONTROLLER'
AGENT_TOKEN='$TOKEN'
SERVER_ID='$SERVER_ID'
HEARTBEAT_INTERVAL_SECONDS='30'
TASK_POLL_INTERVAL_SECONDS='15'
EOF
chmod 600 "$CONFIG_DIR/.env"

info "Pulling agent image for $PLATFORM"
docker pull "$IMAGE"
docker rm -f rotatopilot-agent >/dev/null 2>&1 || true
docker run -d --name rotatopilot-agent --restart unless-stopped \
  --env-file "$CONFIG_DIR/.env" \
  --read-only --tmpfs /tmp:rw,noexec,nosuid,size=8m \
  --security-opt no-new-privileges:true --cap-drop ALL \
  "$IMAGE" >/dev/null

for _ in 1 2 3 4 5 6; do
  if [ "$(docker inspect -f '{{.State.Running}}' rotatopilot-agent 2>/dev/null || true)" = "true" ]; then
    info "RotatoPilot Agent is running for machine $SERVER_ID"
    exit 0
  fi
  sleep 2
done
docker logs --tail=50 rotatopilot-agent >&2 || true
die "Agent container did not remain running"
