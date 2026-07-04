# Codex Task

Please implement this repository strictly according to `DESIGN.md`.

Priority order:

1. Build the Controller backend, database, authentication, provider panel configuration, encrypted token storage, machine sync, manual IP rotation, logs, and rotation history.
2. Build the Agent token generation flow and dashboard-generated one-line Agent install command.
3. Build the Agent service with registration, heartbeat, local probes, task polling, and result reporting.
4. Build probe configuration, scheduler, failure scoring, cooldown, daily limits, and safe automatic IP rotation.
5. Build optional DNS/DDNS sync, starting with Cloudflare and generic webhook.
6. Build WebSocket real-time updates and professional React dashboard.
7. Add Docker Compose, install-controller.sh, install-agent.sh, README, and production-safe defaults.

Important constraints:

- Never hardcode real tokens.
- Never log full tokens.
- Machine IDs must be strings.
- Agent must not receive provider panel token by default.
- Auto rotation and DNS sync must be disabled by default.
- The provider Authorization header uses the raw token by default, not Bearer.
- Implement clear error handling, structured logs, and audit records.
