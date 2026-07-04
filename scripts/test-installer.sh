#!/bin/sh
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
MOCKS="$TMP/mocks"
REAL_DOCKER="$(command -v docker 2>/dev/null || true)"
mkdir -p "$MOCKS"

cat > "$MOCKS/id" <<'EOF'
#!/bin/sh
[ "${1:-}" = "-u" ] && { echo 0; exit 0; }
exec /usr/bin/id "$@"
EOF
cat > "$MOCKS/uname" <<'EOF'
#!/bin/sh
case "${1:-}" in -s) echo Linux ;; -m) echo x86_64 ;; *) echo Linux ;; esac
EOF
cat > "$MOCKS/docker" <<'EOF'
#!/bin/sh
case "${1:-} ${2:-}" in
  "info "|"compose version"|"compose pull"|"compose up"|"exec rotatopilot-controller") exit 0 ;;
  "ps --format") exit 0 ;;
esac
exit 0
EOF
cat > "$MOCKS/curl" <<'EOF'
#!/bin/sh
exit 0
EOF
cat > "$MOCKS/ss" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod +x "$MOCKS"/*

run_install() {
  mode=$1 dir=$2 domain=$3
  PATH="$MOCKS:$PATH" \
    INSTALL_DIR="$dir" \
    ADMIN_PASSWORD='test-password-123' \
    PUBLIC_DOMAIN="$domain" \
    TLS_MODE="$mode" \
    CLOUDFLARE_TUNNEL_TOKEN='test-tunnel-token' \
    sh "$ROOT/install-controller.sh" >/dev/null
}

NONE_DIR="$TMP/none"
run_install none "$NONE_DIR" panel.example.com
grep -q "PUBLIC_URL='https://panel.example.com'" "$NONE_DIR/.env"
grep -q "APP_PORT='8080'" "$NONE_DIR/.env"
grep -q "HOST_PORT='8080'" "$NONE_DIR/.env"
grep -q 'ports: \["8080:8080"\]' "$NONE_DIR/docker-compose.yml"
key_before="$(awk -F= '$1=="APP_ENCRYPTION_KEY"{print $2}' "$NONE_DIR/.env")"
run_install none "$NONE_DIR" new.example.com
key_after="$(awk -F= '$1=="APP_ENCRYPTION_KEY"{print $2}' "$NONE_DIR/.env")"
[ "$key_before" = "$key_after" ]
ls "$NONE_DIR"/.env.backup-* >/dev/null

CADDY_DIR="$TMP/caddy"
run_install caddy "$CADDY_DIR" caddy.example.com
grep -q 'image: caddy:2-alpine' "$CADDY_DIR/docker-compose.yml"
grep -q 'ports: \["80:80", "443:443", "443:443/udp"\]' "$CADDY_DIR/docker-compose.yml"
grep -q 'reverse_proxy controller:8080' "$CADDY_DIR/Caddyfile"

TUNNEL_DIR="$TMP/tunnel"
run_install cloudflare "$TUNNEL_DIR" tunnel.example.com
grep -q 'image: cloudflare/cloudflared:latest' "$TUNNEL_DIR/docker-compose.yml"
grep -q 'TUNNEL_TOKEN' "$TUNNEL_DIR/docker-compose.yml"
if grep -q '443:443' "$TUNNEL_DIR/docker-compose.yml"; then
  printf 'Cloudflare Tunnel mode unexpectedly exposes port 443\n' >&2
  exit 1
fi

if [ -n "$REAL_DOCKER" ] && "$REAL_DOCKER" compose version >/dev/null 2>&1; then
  "$REAL_DOCKER" compose -f "$NONE_DIR/docker-compose.yml" --env-file "$NONE_DIR/.env" config -q
  "$REAL_DOCKER" compose -f "$CADDY_DIR/docker-compose.yml" --env-file "$CADDY_DIR/.env" config -q
  "$REAL_DOCKER" compose -f "$TUNNEL_DIR/docker-compose.yml" --env-file "$TUNNEL_DIR/.env" config -q
fi

printf 'installer generation tests passed\n'
