package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/xiaofujie369/RotatoPilot/backend/internal/security"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/store"
)

type agentKey struct{}
type agentIdentity struct{ ID, MachineID string }

func (a *API) agentAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Agent ") {
			fail(w, 401, "agent authentication required")
			return
		}
		hash := security.HashToken(strings.TrimPrefix(h, "Agent "))
		var x agentIdentity
		var revoked int
		err := a.Store.DB.QueryRowContext(r.Context(), `SELECT id,machine_id,revoked FROM agents WHERE token_hash=?`, hash).Scan(&x.ID, &x.MachineID, &revoked)
		if err != nil || revoked == 1 {
			fail(w, 401, "invalid or revoked agent token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), agentKey{}, x)))
	})
}
func identity(r *http.Request) agentIdentity { return r.Context().Value(agentKey{}).(agentIdentity) }
func (a *API) agentRegister(w http.ResponseWriter, r *http.Request) {
	x := identity(r)
	var in struct{ ServerID, Hostname, AgentVersion, OS, Arch, PublicIPv4, PublicIPv6 string }
	if !decode(w, r, &in) {
		return
	}
	if in.ServerID != x.MachineID {
		fail(w, 403, "token is not scoped to this machine")
		return
	}
	now := store.Now()
	_, err := a.Store.DB.ExecContext(r.Context(), `UPDATE agents SET hostname=?,agent_version=?,os=?,arch=?,public_ipv4=?,public_ipv6=?,status='online',last_heartbeat_at=?,updated_at=? WHERE id=?`, in.Hostname, in.AgentVersion, in.OS, in.Arch, in.PublicIPv4, in.PublicIPv6, now, now, x.ID)
	if err != nil {
		fail(w, 500, "registration failed")
		return
	}
	_, _ = a.Store.DB.ExecContext(r.Context(), `UPDATE machines SET public_ipv4=COALESCE(NULLIF(?,''),public_ipv4),public_ipv6=COALESCE(NULLIF(?,''),public_ipv6),updated_at=? WHERE id=?`, in.PublicIPv4, in.PublicIPv6, now, x.MachineID)
	a.Store.Log("info", x.MachineID, "agent", "Agent registered", fmt.Sprintf(`{"agentId":%q}`, x.ID))
	a.Hub.Broadcast("agent.online", map[string]string{"agentId": x.ID, "machineId": x.MachineID})
	a.Telegram.Send("✅ RotatoPilot agent online\nMachine: " + x.MachineID)
	write(w, 200, map[string]any{"ok": true, "agentId": x.ID, "config": map[string]int{"heartbeatIntervalSeconds": 30, "taskPollIntervalSeconds": 15}})
}
func (a *API) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	x := identity(r)
	var in struct {
		ServerID, PublicIPv4, PublicIPv6, Timestamp string
		UptimeSeconds                               int64
		LoadAvg                                     []float64
		DockerRunning                               bool
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ServerID != x.MachineID {
		fail(w, 403, "token is not scoped to this machine")
		return
	}
	now := store.Now()
	_, err := a.Store.DB.ExecContext(r.Context(), `UPDATE agents SET public_ipv4=?,public_ipv6=?,status='online',last_heartbeat_at=?,updated_at=? WHERE id=?`, in.PublicIPv4, in.PublicIPv6, now, now, x.ID)
	if err != nil {
		fail(w, 500, "heartbeat failed")
		return
	}
	_, _ = a.Store.DB.ExecContext(r.Context(), `UPDATE machines SET public_ipv4=COALESCE(NULLIF(?,''),public_ipv4),public_ipv6=COALESCE(NULLIF(?,''),public_ipv6),updated_at=? WHERE id=?`, in.PublicIPv4, in.PublicIPv6, now, x.MachineID)
	a.Hub.Broadcast("agent.heartbeat", map[string]any{"agentId": x.ID, "machineId": x.MachineID, "publicIPv4": in.PublicIPv4, "dockerRunning": in.DockerRunning})
	write(w, 200, map[string]bool{"ok": true})
}
func (a *API) agentTasks(w http.ResponseWriter, r *http.Request) {
	x := identity(r)
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT id,type,COALESCE(payload_json,'{}') FROM agent_tasks WHERE agent_id=? AND status='pending' ORDER BY created_at LIMIT 10`, x.ID)
	if err != nil {
		fail(w, 500, "could not load tasks")
		return
	}
	defer rows.Close()
	tasks := []map[string]any{}
	ids := []string{}
	for rows.Next() {
		var id, typ, payload string
		if rows.Scan(&id, &typ, &payload) == nil {
			var p map[string]any
			_ = json.Unmarshal([]byte(payload), &p)
			if p == nil {
				p = map[string]any{}
			}
			p["taskId"] = id
			p["type"] = typ
			tasks = append(tasks, p)
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		_, _ = a.Store.DB.ExecContext(r.Context(), `UPDATE agent_tasks SET status='running',started_at=? WHERE id=? AND status='pending'`, store.Now(), id)
	}
	write(w, 200, map[string]any{"tasks": tasks})
}
func (a *API) agentTaskResult(w http.ResponseWriter, r *http.Request) {
	x := identity(r)
	var in struct {
		TaskID            string `json:"taskId"`
		Success           bool   `json:"success"`
		LatencyMS         int64  `json:"latencyMs"`
		Error, FinishedAt string
	}
	if !decode(w, r, &in) {
		return
	}
	result, _ := json.Marshal(in)
	res, err := a.Store.DB.ExecContext(r.Context(), `UPDATE agent_tasks SET status=?,result_json=?,error=?,finished_at=? WHERE id=? AND agent_id=?`, map[bool]string{true: "completed", false: "failed"}[in.Success], string(result), in.Error, store.Now(), in.TaskID, x.ID)
	if err != nil {
		fail(w, 500, "could not save result")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "task not found")
		return
	}
	a.Hub.Broadcast("agent.task.result", map[string]any{"taskId": in.TaskID, "machineId": x.MachineID, "success": in.Success})
	write(w, 200, map[string]bool{"ok": true})
}
