# RotatoPilot

RotatoPilot is a self-hosted controller and lightweight VPS agent for safely detecting connectivity failures, rotating provider-managed public IPs, and optionally synchronizing DNS. The controller owns all provider and DNS credentials; agents receive only a machine-scoped token.

> Automatic rotation and DNS synchronization are disabled by default. Test your probes and make a manual rotation before enabling automation.

## Architecture

```text
Browser ──HTTPS/WebSocket── Controller ──SQLite
                              ├── Provider panel API
                              ├── Cloudflare / DDNS webhook
                              └── Telegram
                                  ▲
                                  │ scoped HTTPS agent API
                              VPS Agent
```

The Go controller serves the React dashboard and API, schedules weighted probes, enforces safety rules, and records an audit trail. The Go agent reports host/network health and executes only predefined probe tasks. It never receives provider, DNS, Telegram, JWT, or encryption secrets.

## Quick start

Requirements: Docker Engine with Compose v2.

```bash
git clone https://github.com/xiaofujie369/RotatoPilot.git
cd RotatoPilot
cp .env.example .env
# Replace every change_me value in .env with a strong random secret.
docker compose up -d --build
```

Open `http://SERVER_IP:8080`. For a server install that generates secrets interactively, download the script first so it can be inspected before running as root:

```bash
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/xiaofujie369/RotatoPilot/main/install-controller.sh \
  -o /tmp/install-rotatopilot.sh \
  && sudo sh /tmp/install-rotatopilot.sh
```

Use a TLS reverse proxy (Caddy, Nginx, or a trusted load balancer) before exposing the dashboard publicly. Set `PUBLIC_URL` to the final HTTPS origin.

## Host compatibility

The controller and agent run in Alpine-based containers, keeping host dependencies deliberately small. The installers support Linux on `amd64` and `arm64`, including Debian 11/12/13, Ubuntu 20.04/22.04/24.04, their common derivatives, and vendor-customized images that retain a supported package manager and normal Docker kernel features. Secondary best-effort installation paths cover `dnf`, `yum`, `zypper`, and `apk` systems.

Both `systemd` and legacy `service` startup paths are handled. Docker Compose v2 is preferred, with legacy `docker-compose` accepted for older vendor images. The installer checks root privileges, architecture, Docker availability, ports, URLs, usernames, tokens, machine IDs, secret file modes, and final service health.

Minimal cloud images need a writable `/opt`, outbound HTTPS, and Docker-compatible cgroups, namespaces, and storage drivers. Restricted OpenVZ/LXC plans require the hosting vendor to enable nesting and container kernel features first.

## First-time setup

1. Sign in with `ADMIN_USERNAME` and `ADMIN_PASSWORD`.
2. Open **Providers**, add the panel URL and token, then use **Test**. Raw `Authorization` is the default; Bearer is opt-in.
3. Use **Sync** to import machines. Provider machine IDs remain strings throughout the system.
4. Open **Machines**, select **Agent**, and generate credentials. Copy the command immediately—the token is shown once and only its SHA-256 hash is stored.
5. Run the generated command as root on the target VPS. The Agents page will show registration and heartbeats.
6. Add controller TCP/HTTPS probes and test them. A typical starting set is TCP 443 (weight 2), HTTPS (weight 2), and a trusted external signal (weight 5).
7. Make one confirmed manual rotation. Verify history, post-change checks, and any DNS record.
8. Only then enable per-machine and global automatic rotation.

## Provider API

The bundled adapter targets Lightsail-like panels:

- `POST /lb/lightsail/page` to list machines
- `POST /lb/lightsail/changeIp` with `{"id":"machine-id"}` to rotate

List parsing tolerates common nested `data`, `result`, `list`, `records`, and `items` response shapes. After a rotation request, the controller polls the list endpoint until a different IP appears or the configured timeout expires.

## Probes and automatic rotation

Controller TCP, HTTP, and HTTPS probes are implemented. Agent and external-agent probe configurations queue scoped tasks. ICMP is represented in configuration but uses a TCP target in the unprivileged controller, avoiding raw-socket container privileges.

A machine becomes suspect after a failure score of at least 5 for `FAILURE_THRESHOLD` consecutive checks. Automatic rotation additionally requires:

- `AUTO_ROTATE_GLOBAL_ENABLED=true`;
- automatic rotation enabled for that machine;
- a failed confirmation pass (when enabled);
- expired cooldown;
- fewer than `MAX_ROTATIONS_PER_DAY` completed rotations;
- no rotation already running for the machine.

Manual rotation requires typing the exact machine ID in the dashboard.

## DNS / DDNS

DNS is optional and records start disabled. Supported providers:

- **Cloudflare:** use a least-privilege token limited to DNS edit for one zone, then set Zone ID, full record name, type, TTL, and proxy status. Proxying defaults off.
- **Generic webhook:** provider config is JSON such as `{"url":"https://ddns.example/hook","method":"POST"}`. The controller sends record name/type, machine ID, and IP as JSON.

Enable both the record and **Sync after rotation** only after a successful manual sync.

## Telegram

Set these values and restart the controller:

```env
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=your_bot_token
TELEGRAM_CHAT_ID=your_chat_id
```

Notifications cover agent registration, suspect machines, rotation start/completion/failure, and DNS outcomes. Tokens are never added to audit metadata.

## Agent installation

The dashboard generates the canonical command:

```bash
bash <(curl -fsSL https://rotator.example.com/install-agent.sh) \
  --controller https://rotator.example.com \
  --agent-token air_REDACTED \
  --server-id 1783916711346432
```

The installer writes `/opt/rotatopilot-agent/.env` with mode `0600`, replaces an older container, and starts the agent with a read-only filesystem, no Linux capabilities, and `no-new-privileges`. Mounting `/var/run/docker.sock` is optional and intentionally absent from the generated command.

## Development

Go 1.25 and Node.js 22 or newer are recommended.

```bash
go test ./...
cd frontend && npm install && npm run build
go run ./backend/cmd/controller
```

For frontend development, run `npm run dev`; Vite proxies API and WebSocket traffic to port 8080. Runtime data defaults to `./data/app.db`.

## Upgrade

Back up the `rotatopilot-data` Docker volume and `.env`, pull the new image/source, and run `docker compose up -d`. Migrations are additive and run at controller startup. Upgrade an agent by rerunning its generated installation command with a newly generated token if the old credential was revoked.

## Troubleshooting

- **Agent cannot register:** confirm the controller URL is reachable from the VPS, HTTPS has a valid certificate, the machine ID exactly matches, and the token has not been revoked. Check `docker logs rotatopilot-agent`.
- **Provider token invalid:** confirm the authorization scheme. The default sends the token raw, not as `Bearer`. Use **Test** and inspect the redacted controller audit entry.
- **Machine ID not found:** synchronize the provider again and compare IDs as strings; do not round them in a spreadsheet or JavaScript number.
- **IP does not change:** the provider may have accepted the job but not completed before `CHANGE_IP_POLL_TIMEOUT_SECONDS`. Review rotation history and provider limits.
- **DNS sync failed:** verify zone ID, full record name, record type, and least-privilege token scope. A record must already exist in Cloudflare.
- **WebSocket disconnected:** verify the reverse proxy forwards Upgrade/Connection headers on `/ws`; the dashboard reconnects every three seconds.
- **Container does not start:** run `docker compose logs controller`, verify directory permissions, environment values, port availability, and health at `/healthz`.
- **SQLite locked:** keep only one controller replica on SQLite. WAL and a busy timeout are enabled; use PostgreSQL before introducing multiple writers.

## Security

- Never commit `.env`, agent credentials, database files, or logs.
- Provider and DNS tokens are encrypted with AES-256-GCM using `APP_ENCRYPTION_KEY` and are never returned by APIs after saving.
- Agent tokens are scoped to one machine, displayed once, and stored as hashes.
- Admin sessions use signed, HTTP-only, SameSite cookies. Use HTTPS so the cookie is marked Secure.
- Rotate any credential that may have leaked. Changing `APP_ENCRYPTION_KEY` without re-encrypting stored values makes them unreadable.
- Use a strong admin password and random 32-byte-or-longer JWT/encryption secrets.
- The controller container drops all capabilities and runs as an unprivileged user.
- Controller data uses a named volume initialized for the unprivileged container user, avoiding host-distribution UID/GID mismatches.
- Production startup rejects placeholder JWT and encryption secrets. Remote provider, webhook, and agent-controller traffic require HTTPS by default.
- CI runs Go tests and vetting, `govulncheck`, TypeScript compilation, `npm audit`, and ShellCheck. Published images include SBOM and provenance attestations.

API health and version endpoints are available at `/healthz` and `/api/version`.
