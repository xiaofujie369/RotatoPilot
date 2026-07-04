#!/bin/sh
set -eu

INSTALL_DIR="${INSTALL_DIR:-/opt/rotatopilot}"
IMAGE="${CONTROLLER_IMAGE:-ghcr.io/xiaofujie369/rotatopilot-controller:latest}"
ENV_FILE="$INSTALL_DIR/.env"
COMPOSE_FILE="$INSTALL_DIR/docker-compose.yml"

die() { printf '错误：%s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }
warn() { printf '警告：%s\n' "$*" >&2; }

[ "$(uname -s 2>/dev/null || true)" = "Linux" ] || die "仅支持 Linux 系统"
[ "$(id -u)" -eq 0 ] || die "请使用 root 权限运行（例如 sudo sh install-controller.sh）"
case "$(uname -m)" in
  x86_64|amd64) PLATFORM=amd64 ;;
  aarch64|arm64) PLATFORM=arm64 ;;
  *) die "不支持的 CPU 架构：$(uname -m)，仅支持 amd64、arm64" ;;
esac
case "$INSTALL_DIR" in /*) ;; *) die "INSTALL_DIR 必须是绝对路径" ;; esac
[ "$INSTALL_DIR" != "/" ] || die "INSTALL_DIR 不能是根目录"

OS_NAME="Linux"
if [ -r /etc/os-release ]; then
  OS_NAME="$(awk -F= '$1=="PRETTY_NAME"{gsub(/^\"|\"$/,"",$2); print $2; exit}' /etc/os-release 2>/dev/null || true)"
  [ -n "$OS_NAME" ] || OS_NAME="Linux"
fi
info "检测到 $OS_NAME（$PLATFORM）"

install_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
  elif command -v dnf >/dev/null 2>&1; then dnf install -y "$@"
  elif command -v yum >/dev/null 2>&1; then yum install -y "$@"
  elif command -v zypper >/dev/null 2>&1; then zypper --non-interactive install "$@"
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache "$@"
  else die "找不到受支持的包管理器，请手动安装：$*"; fi
}

command -v curl >/dev/null 2>&1 || { info "安装 curl 与 CA 证书"; install_packages curl ca-certificates; }

start_docker() {
  if command -v systemctl >/dev/null 2>&1; then systemctl enable --now docker 2>/dev/null || true
  elif command -v service >/dev/null 2>&1; then service docker start 2>/dev/null || true
  fi
  docker info >/dev/null 2>&1 || die "Docker 守护进程没有运行"
}

if ! command -v docker >/dev/null 2>&1; then
  info "使用 Docker 官方安装器安装 Docker Engine"
  tmp="$(mktemp -t rotatopilot-docker.XXXXXX)" || die "无法创建临时文件"
  trap 'rm -f "$tmp"' EXIT HUP INT TERM
  curl --proto '=https' --tlsv1.2 -fsSL https://get.docker.com -o "$tmp"
  sh "$tmp"
  rm -f "$tmp"; trap - EXIT HUP INT TERM
fi
start_docker

if ! docker compose version >/dev/null 2>&1 && ! command -v docker-compose >/dev/null 2>&1; then
  info "安装 Docker Compose"
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y docker-compose-plugin 2>/dev/null || DEBIAN_FRONTEND=noninteractive apt-get install -y docker-compose
  elif command -v dnf >/dev/null 2>&1; then dnf install -y docker-compose-plugin
  elif command -v yum >/dev/null 2>&1; then yum install -y docker-compose-plugin
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache docker-cli-compose
  else die "需要 Docker Compose v2"; fi
fi

compose() {
  if docker compose version >/dev/null 2>&1; then docker compose "$@"; else docker-compose "$@"; fi
}
compose version >/dev/null 2>&1 || die "Docker Compose 不可用"

read_env_value() {
  key=$1
  [ -r "$ENV_FILE" ] || return 0
  value="$(awk -v key="$key" 'index($0,key"=")==1 {print substr($0,length(key)+2); exit}' "$ENV_FILE")"
  case "$value" in "'"*"'") value=${value#\'}; value=${value%\'} ;; esac
  printf '%s' "$value"
}

EXISTING=false
if [ -f "$ENV_FILE" ]; then
  EXISTING=true
  info "检测到现有安装，将保留数据库、管理员凭据和加密密钥"
fi

PORT="${HOST_PORT:-$(read_env_value HOST_PORT)}"; PORT="${PORT:-$(read_env_value APP_PORT)}"; PORT="${PORT:-8080}"
USER_NAME="${ADMIN_USERNAME:-$(read_env_value ADMIN_USERNAME)}"; USER_NAME="${USER_NAME:-admin}"
PASS="${ADMIN_PASSWORD:-$(read_env_value ADMIN_PASSWORD)}"
JWT="${JWT_SECRET:-$(read_env_value JWT_SECRET)}"
ENCRYPTION="${APP_ENCRYPTION_KEY:-$(read_env_value APP_ENCRYPTION_KEY)}"
OLD_PUBLIC_URL="$(read_env_value PUBLIC_URL)"
TELEGRAM_BOT_TOKEN_CURRENT="$(read_env_value TELEGRAM_BOT_TOKEN)"
TELEGRAM_CHAT_ID_CURRENT="$(read_env_value TELEGRAM_CHAT_ID)"
DOMAIN="${PUBLIC_DOMAIN:-}"
if [ -z "$DOMAIN" ] && [ -n "$OLD_PUBLIC_URL" ]; then
  DOMAIN=${OLD_PUBLIC_URL#https://}; DOMAIN=${DOMAIN#http://}; DOMAIN=${DOMAIN%%:*}; DOMAIN=${DOMAIN%%/*}
fi

if [ -t 0 ]; then
  printf "控制台端口 [%s]: " "$PORT"; read -r answer; PORT="${answer:-$PORT}"
  if [ "$EXISTING" = false ]; then
    printf "管理员用户名 [%s]: " "$USER_NAME"; read -r answer; USER_NAME="${answer:-$USER_NAME}"
    if [ -z "$PASS" ]; then printf "管理员密码（至少 12 位）: "; stty -echo; read -r PASS; stty echo; printf '\n'; fi
  fi
  if [ -n "$DOMAIN" ]; then printf "控制台域名 [%s]: " "$DOMAIN"; else printf "控制台域名（例如 panel.example.com）: "; fi
  read -r answer; DOMAIN="${answer:-$DOMAIN}"
else
  [ -n "$PASS" ] || die "非交互安装必须设置 ADMIN_PASSWORD"
  [ -n "$DOMAIN" ] || die "非交互安装必须设置 PUBLIC_DOMAIN"
fi

DOMAIN=${DOMAIN#https://}; DOMAIN=${DOMAIN#http://}; DOMAIN=${DOMAIN%.}
case "$PORT" in ''|*[!0-9]*) die "APP_PORT 必须是数字" ;; esac
if [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then die "APP_PORT 必须在 1 到 65535 之间"; fi
case "$USER_NAME" in ''|*[!A-Za-z0-9_.-]*) die "管理员用户名包含不支持的字符" ;; esac
[ "${#PASS}" -ge 12 ] || die "管理员密码至少需要 12 个字符"
case "$PASS" in *"'"*) die "管理员密码不能包含单引号" ;; esac
PASS_SINGLE_LINE="$(printf '%s' "$PASS" | tr -d '\r\n')"
[ "$PASS" = "$PASS_SINGLE_LINE" ] || die "管理员密码不能包含换行"
case "$DOMAIN" in
  ''|*/*|*:*|*[!A-Za-z0-9.-]*|.*|*.) die "请输入纯域名，不要包含协议、端口或路径" ;;
  *.*) ;;
  *) die "域名格式无效，例如 panel.example.com" ;;
esac
DOMAIN="$(printf '%s' "$DOMAIN" | tr '[:upper:]' '[:lower:]')"
PUBLIC_URL="https://$DOMAIN"

random_hex() { od -An -N "$1" -tx1 /dev/urandom | tr -d ' \n'; }
[ -n "$JWT" ] || JWT="$(random_hex 32)"
[ -n "$ENCRYPTION" ] || ENCRYPTION="$(random_hex 32)"

port_busy() {
  port=$1
  if command -v ss >/dev/null 2>&1; then ss -ltnH "sport = :$port" 2>/dev/null | grep -q .
  elif command -v netstat >/dev/null 2>&1; then netstat -lnt 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]$port$"
  else docker ps --format '{{.Ports}}' 2>/dev/null | grep -Eq "[:.]$port->"; fi
}

TLS_MODE="${TLS_MODE:-$(read_env_value TLS_MODE)}"; TLS_MODE="${TLS_MODE:-auto}"
if [ "$TLS_MODE" = auto ]; then
  if port_busy 443 || port_busy 80; then TLS_MODE=cloudflare; else TLS_MODE=caddy; fi
fi
case "$TLS_MODE" in caddy|cloudflare|none) ;; *) die "TLS_MODE 仅支持 auto、caddy、cloudflare、none" ;; esac

if [ -t 0 ]; then
  printf '\nHTTPS 反向代理模式：\n'
  printf '  1) Caddy 自动证书（占用 80/443）\n'
  printf '  2) Cloudflare Tunnel 自动证书（不占用本机 80/443，适合 Xray）\n'
  printf '  3) 不配置 HTTPS，仅开放 %s 端口\n' "$PORT"
  case "$TLS_MODE" in caddy) default_mode=1 ;; cloudflare) default_mode=2 ;; *) default_mode=3 ;; esac
  printf "请选择 [%s]: " "$default_mode"; read -r answer; answer="${answer:-$default_mode}"
  case "$answer" in 1) TLS_MODE=caddy ;; 2) TLS_MODE=cloudflare ;; 3) TLS_MODE=none ;; *) die "无效选项" ;; esac
fi

CF_TUNNEL_TOKEN="${CLOUDFLARE_TUNNEL_TOKEN:-$(read_env_value CLOUDFLARE_TUNNEL_TOKEN)}"
CF_API_TOKEN="${CLOUDFLARE_API_TOKEN:-}"
CF_ZONE_ID="${CLOUDFLARE_ZONE_ID:-}"

if [ "$TLS_MODE" = caddy ] && { port_busy 443 || port_busy 80; }; then
  die "80 或 443 已被占用。Xray 环境请选择 Cloudflare Tunnel 模式，它不会占用本机 443"
fi

cf_request() {
  method=$1; endpoint=$2; body=${3:-}
  if [ -n "$body" ]; then
    curl --proto '=https' --tlsv1.2 -fsS -X "$method" \
      -H "Authorization: Bearer $CF_API_TOKEN" -H 'Content-Type: application/json' \
      --data "$body" "https://api.cloudflare.com/client/v4$endpoint"
  else
    curl --proto '=https' --tlsv1.2 -fsS -X "$method" \
      -H "Authorization: Bearer $CF_API_TOKEN" -H 'Content-Type: application/json' \
      "https://api.cloudflare.com/client/v4$endpoint"
  fi
}

configure_cloudflare_tunnel() {
  command -v jq >/dev/null 2>&1 || { info "安装 jq 以配置 Cloudflare Tunnel"; install_packages jq; }
  if [ -z "$CF_API_TOKEN" ] && [ -t 0 ]; then
    printf "Cloudflare API Token（需要 Tunnel 编辑、Zone 读取、DNS 编辑权限）: "; stty -echo; read -r CF_API_TOKEN; stty echo; printf '\n'
  fi
  if [ -z "$CF_ZONE_ID" ] && [ -t 0 ]; then printf "Cloudflare Zone ID: "; read -r CF_ZONE_ID; fi
  [ -n "$CF_API_TOKEN" ] || die "Cloudflare Tunnel 模式需要 CLOUDFLARE_API_TOKEN"
  [ -n "$CF_ZONE_ID" ] || die "Cloudflare Tunnel 模式需要 CLOUDFLARE_ZONE_ID"
  case "$CF_ZONE_ID" in *[!A-Fa-f0-9]*|'') die "Cloudflare Zone ID 格式无效" ;; esac

  info "验证 Cloudflare Zone 与 API Token"
  zone_json="$(cf_request GET "/zones/$CF_ZONE_ID")" || die "无法访问 Cloudflare Zone，请检查 Token 权限"
  printf '%s' "$zone_json" | jq -e '.success == true' >/dev/null || die "Cloudflare Zone 验证失败"
  ACCOUNT_ID="$(printf '%s' "$zone_json" | jq -r '.result.account.id // empty')"
  [ -n "$ACCOUNT_ID" ] || die "无法从 Zone 获取 Cloudflare Account ID"

  TUNNEL_NAME="rotatopilot-$(printf '%s' "$DOMAIN" | tr '.' '-' | cut -c1-70)"
  tunnels="$(cf_request GET "/accounts/$ACCOUNT_ID/cfd_tunnel?name=$TUNNEL_NAME&is_deleted=false")"
  TUNNEL_ID="$(printf '%s' "$tunnels" | jq -r '.result[0].id // empty')"
  if [ -z "$TUNNEL_ID" ]; then
    info "创建 Cloudflare Tunnel：$TUNNEL_NAME"
    secret="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
    body="$(jq -cn --arg name "$TUNNEL_NAME" --arg secret "$secret" '{name:$name,config_src:"cloudflare",tunnel_secret:$secret}')"
    created="$(cf_request POST "/accounts/$ACCOUNT_ID/cfd_tunnel" "$body")"
    TUNNEL_ID="$(printf '%s' "$created" | jq -r '.result.id // empty')"
    [ -n "$TUNNEL_ID" ] || die "创建 Cloudflare Tunnel 失败：$(printf '%s' "$created" | jq -r '.errors[0].message // "unknown error"')"
  else
    info "复用现有 Cloudflare Tunnel：$TUNNEL_NAME"
  fi

  info "配置 Tunnel 域名反向代理"
  config_body="$(jq -cn --arg host "$DOMAIN" '{config:{ingress:[{hostname:$host,service:"http://controller:8080"},{service:"http_status:404"}]}}')"
  config_result="$(cf_request PUT "/accounts/$ACCOUNT_ID/cfd_tunnel/$TUNNEL_ID/configurations" "$config_body")"
  printf '%s' "$config_result" | jq -e '.success == true' >/dev/null || die "配置 Tunnel ingress 失败"

  info "更新 Cloudflare DNS 记录"
  records="$(cf_request GET "/zones/$CF_ZONE_ID/dns_records?name=$DOMAIN")"
  printf '%s' "$records" | jq -r '.result[] | select(.type=="A" or .type=="AAAA" or .type=="CNAME") | .id' | while IFS= read -r record_id; do
    [ -n "$record_id" ] && cf_request DELETE "/zones/$CF_ZONE_ID/dns_records/$record_id" >/dev/null
  done
  dns_body="$(jq -cn --arg name "$DOMAIN" --arg content "$TUNNEL_ID.cfargotunnel.com" '{type:"CNAME",name:$name,content:$content,ttl:1,proxied:true,comment:"Managed by RotatoPilot installer"}')"
  dns_result="$(cf_request POST "/zones/$CF_ZONE_ID/dns_records" "$dns_body")"
  printf '%s' "$dns_result" | jq -e '.success == true' >/dev/null || die "创建 Tunnel DNS 记录失败"

  token_result="$(cf_request GET "/accounts/$ACCOUNT_ID/cfd_tunnel/$TUNNEL_ID/token")"
  CF_TUNNEL_TOKEN="$(printf '%s' "$token_result" | jq -r '.result // empty')"
  [ -n "$CF_TUNNEL_TOKEN" ] || die "无法获取 Tunnel Token"
}

if [ "$TLS_MODE" = cloudflare ] && [ -z "$CF_TUNNEL_TOKEN" ]; then configure_cloudflare_tunnel; fi

umask 077
mkdir -p "$INSTALL_DIR"
chmod 700 "$INSTALL_DIR"
if [ "$EXISTING" = true ]; then
  stamp="$(date +%Y%m%d%H%M%S)"
  cp "$ENV_FILE" "$ENV_FILE.backup-$stamp"
  [ ! -f "$COMPOSE_FILE" ] || cp "$COMPOSE_FILE" "$COMPOSE_FILE.backup-$stamp"
fi

cat > "$ENV_FILE" <<EOF
APP_ENV='production'
APP_PORT='8080'
HOST_PORT='$PORT'
PUBLIC_URL='$PUBLIC_URL'
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
TELEGRAM_BOT_TOKEN='$TELEGRAM_BOT_TOKEN_CURRENT'
TELEGRAM_CHAT_ID='$TELEGRAM_CHAT_ID_CURRENT'
LOG_LEVEL='info'
TLS_MODE='$TLS_MODE'
CLOUDFLARE_TUNNEL_TOKEN='$CF_TUNNEL_TOKEN'
EOF
chmod 600 "$ENV_FILE"

cat > "$COMPOSE_FILE" <<EOF
services:
  controller:
    image: $IMAGE
    container_name: rotatopilot-controller
    restart: unless-stopped
    env_file: [.env]
    expose: ["8080"]
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
EOF

if [ "$TLS_MODE" = caddy ]; then
  cat > "$INSTALL_DIR/Caddyfile" <<EOF
$DOMAIN {
  encode zstd gzip
  reverse_proxy controller:8080
  header {
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
    -Server
  }
}
EOF
  cat >> "$COMPOSE_FILE" <<'EOF'
  caddy:
    image: caddy:2-alpine
    container_name: rotatopilot-caddy
    restart: unless-stopped
    depends_on: [controller]
    ports: ["80:80", "443:443", "443:443/udp"]
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    security_opt: ["no-new-privileges:true"]
    cap_drop: [ALL]
    cap_add: [NET_BIND_SERVICE]
EOF
elif [ "$TLS_MODE" = cloudflare ]; then
  cat >> "$COMPOSE_FILE" <<'EOF'
  cloudflared:
    image: cloudflare/cloudflared:latest
    container_name: rotatopilot-cloudflared
    restart: unless-stopped
    depends_on: [controller]
    environment:
      TUNNEL_TOKEN: "${CLOUDFLARE_TUNNEL_TOKEN}"
    command: tunnel --no-autoupdate run
    read_only: true
    security_opt: ["no-new-privileges:true"]
    cap_drop: [ALL]
EOF
else
  cat >> "$COMPOSE_FILE" <<EOF
    ports: ["$PORT:8080"]
EOF
fi

cat >> "$COMPOSE_FILE" <<'EOF'
volumes:
  rotatopilot-data:
  caddy-data:
  caddy-config:
EOF
chmod 600 "$COMPOSE_FILE"

cd "$INSTALL_DIR"
info "拉取并启动 RotatoPilot"
compose pull
compose up -d --remove-orphans

healthy=false
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
  if docker exec rotatopilot-controller wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1; then healthy=true; break; fi
  sleep 2
done
[ "$healthy" = true ] || { compose logs --tail=80 >&2 || true; die "控制器没有通过健康检查"; }

if [ "$TLS_MODE" != none ]; then
  info "等待域名 HTTPS 生效"
  external=false
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
    if curl --proto '=https' --tlsv1.2 -fsS "$PUBLIC_URL/healthz" >/dev/null 2>&1; then external=true; break; fi
    sleep 5
  done
  [ "$external" = true ] || warn "服务已启动，但域名证书或 DNS 尚未生效，请稍后访问并检查容器日志"
fi

info "RotatoPilot 升级/安装完成"
printf '访问地址：%s\n' "$PUBLIC_URL"
printf '反向代理模式：%s\n' "$TLS_MODE"
