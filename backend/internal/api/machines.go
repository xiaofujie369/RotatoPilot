package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/models"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/security"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/store"
)

const machineCols = `id,provider_id,COALESCE(name,''),COALESCE(remark,''),COALESCE(region,''),COALESCE(status,''),COALESCE(public_ipv4,''),COALESCE(public_ipv6,''),expected_port,auto_rotate_enabled,health_status,failure_count,success_count,COALESCE(last_check_at,''),COALESCE(last_rotation_at,''),COALESCE(rotation_cooldown_until,''),COALESCE(traffic_text,''),COALESCE(expire_time,''),COALESCE(raw_provider_json,'')`

func scanMachine(row interface{ Scan(...any) error }) (models.Machine, error) {
	var m models.Machine
	var auto int
	err := row.Scan(&m.ID, &m.ProviderID, &m.Name, &m.Remark, &m.Region, &m.Status, &m.PublicIPv4, &m.PublicIPv6, &m.ExpectedPort, &auto, &m.HealthStatus, &m.FailureCount, &m.SuccessCount, &m.LastCheckAt, &m.LastRotationAt, &m.CooldownUntil, &m.TrafficText, &m.ExpireTime, &m.RawProviderJSON)
	m.AutoRotateEnabled = auto == 1
	return m, err
}
func (a *API) machines(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT `+machineCols+` FROM machines ORDER BY name,id`)
	if err != nil {
		fail(w, 500, "database error")
		return
	}
	defer rows.Close()
	out := []models.Machine{}
	for rows.Next() {
		m, e := scanMachine(rows)
		if e == nil {
			out = append(out, m)
		}
	}
	write(w, 200, out)
}
func (a *API) machineResponse(w http.ResponseWriter, r *http.Request, id string, status int) {
	m, err := scanMachine(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+machineCols+` FROM machines WHERE id=?`, id))
	if err != nil {
		fail(w, 404, "machine not found")
		return
	}
	write(w, status, m)
}
func (a *API) machineByID(w http.ResponseWriter, r *http.Request) {
	a.machineResponse(w, r, chiMachineID(r), 200)
}
func chiMachineID(r *http.Request) string { return chi.URLParam(r, "id") }
func (a *API) createMachine(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID                   string `json:"id"`
		ProviderID           int64  `json:"providerId"`
		Name, Remark, Region string
		ExpectedPort         int  `json:"expectedPort"`
		AutoRotateEnabled    bool `json:"autoRotateEnabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ID == "" || in.ProviderID == 0 {
		fail(w, 400, "id and providerId are required")
		return
	}
	if in.ExpectedPort == 0 {
		in.ExpectedPort = 443
	}
	now := store.Now()
	_, err := a.Store.DB.ExecContext(r.Context(), `INSERT INTO machines(id,provider_id,name,remark,region,expected_port,auto_rotate_enabled,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?)`, in.ID, in.ProviderID, in.Name, in.Remark, in.Region, in.ExpectedPort, store.Bool(in.AutoRotateEnabled), now, now)
	if err != nil {
		fail(w, 409, "machine could not be created")
		return
	}
	a.Store.Log("info", in.ID, "machine", "Machine created", `{}`)
	a.machineResponse(w, r, in.ID, 201)
}
func (a *API) updateMachine(w http.ResponseWriter, r *http.Request) {
	id := chiMachineID(r)
	m, err := scanMachine(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+machineCols+` FROM machines WHERE id=?`, id))
	if err != nil {
		fail(w, 404, "machine not found")
		return
	}
	var in struct {
		Name, Remark, Region string
		ExpectedPort         int   `json:"expectedPort"`
		AutoRotateEnabled    *bool `json:"autoRotateEnabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name != "" {
		m.Name = in.Name
	}
	m.Remark = in.Remark
	if in.Region != "" {
		m.Region = in.Region
	}
	if in.ExpectedPort > 0 {
		m.ExpectedPort = in.ExpectedPort
	}
	if in.AutoRotateEnabled != nil {
		m.AutoRotateEnabled = *in.AutoRotateEnabled
	}
	_, err = a.Store.DB.ExecContext(r.Context(), `UPDATE machines SET name=?,remark=?,region=?,expected_port=?,auto_rotate_enabled=?,updated_at=? WHERE id=?`, m.Name, m.Remark, m.Region, m.ExpectedPort, store.Bool(m.AutoRotateEnabled), store.Now(), id)
	if err != nil {
		fail(w, 500, "could not update machine")
		return
	}
	a.machineResponse(w, r, id, 200)
}
func (a *API) deleteMachine(w http.ResponseWriter, r *http.Request) {
	id := chiMachineID(r)
	_, err := a.Store.DB.ExecContext(r.Context(), `DELETE FROM machines WHERE id=?`, id)
	if err != nil {
		fail(w, 500, "could not delete machine")
		return
	}
	w.WriteHeader(204)
}
func (a *API) generateAgentToken(w http.ResponseWriter, r *http.Request) {
	machineID := chiMachineID(r)
	var exists int
	if a.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM machines WHERE id=?`, machineID).Scan(&exists); exists == 0 {
		fail(w, 404, "machine not found")
		return
	}
	token, err := security.NewAgentToken()
	if err != nil {
		fail(w, 500, "could not generate token")
		return
	}
	agentID := security.NewID("agt")
	now := store.Now()
	_, err = a.Store.DB.ExecContext(r.Context(), `INSERT INTO agents(id,machine_id,token_hash,token_prefix,status,created_at,updated_at)VALUES(?,?,?,?,?,?,?)`, agentID, machineID, security.HashToken(token), security.TokenPrefix(token), "pending", now, now)
	if err != nil {
		fail(w, 500, "could not create agent token")
		return
	}
	install := fmt.Sprintf(`bash <(curl -fsSL %s) --controller %s --agent-token %s --server-id %s`, shellQuote(a.Cfg.PublicURL+"/install-agent.sh"), shellQuote(a.Cfg.PublicURL), shellQuote(token), shellQuote(machineID))
	docker := fmt.Sprintf(`docker run -d --name rotatopilot-agent --restart unless-stopped --read-only --tmpfs /tmp:rw,noexec,nosuid,size=8m --security-opt no-new-privileges:true --cap-drop ALL -e CONTROLLER_URL=%s -e AGENT_TOKEN=%s -e SERVER_ID=%s ghcr.io/xiaofujie369/rotatopilot-agent:latest`, shellQuote(a.Cfg.PublicURL), shellQuote(token), shellQuote(machineID))
	a.Store.Log("info", machineID, "agent", "Agent token generated", fmt.Sprintf(`{"agentId":%q,"tokenPrefix":%q}`, agentID, security.TokenPrefix(token)))
	write(w, 201, map[string]any{"agentId": agentID, "token": token, "installCommand": install, "dockerCommand": docker, "shownOnce": true})
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'" }
func (a *API) agents(w http.ResponseWriter, r *http.Request) {
	_, _ = a.Store.DB.ExecContext(r.Context(), `UPDATE agents SET status='offline' WHERE status='online' AND last_heartbeat_at < ?`, time.Now().UTC().Add(-2*time.Minute).Format(time.RFC3339))
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT id,machine_id,token_prefix,COALESCE(name,''),COALESCE(hostname,''),COALESCE(agent_version,''),COALESCE(os,''),COALESCE(arch,''),COALESCE(public_ipv4,''),COALESCE(public_ipv6,''),status,COALESCE(last_heartbeat_at,''),revoked,created_at FROM agents ORDER BY created_at DESC`)
	if err != nil {
		fail(w, 500, "database error")
		return
	}
	defer rows.Close()
	out := []models.Agent{}
	for rows.Next() {
		var x models.Agent
		var rev int
		if rows.Scan(&x.ID, &x.MachineID, &x.TokenPrefix, &x.Name, &x.Hostname, &x.AgentVersion, &x.OS, &x.Arch, &x.PublicIPv4, &x.PublicIPv6, &x.Status, &x.LastHeartbeatAt, &rev, &x.CreatedAt) == nil {
			x.Revoked = rev == 1
			out = append(out, x)
		}
	}
	write(w, 200, out)
}
func (a *API) revokeAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := a.Store.DB.ExecContext(r.Context(), `UPDATE agents SET revoked=1,status='revoked',updated_at=? WHERE id=?`, store.Now(), id)
	if err != nil {
		fail(w, 500, "could not revoke agent")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "agent not found")
		return
	}
	a.Store.Log("warn", "", "agent", "Agent token revoked", fmt.Sprintf(`{"agentId":%q}`, id))
	write(w, 200, map[string]bool{"ok": true})
}
func (a *API) upgradeAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var machine string
	if a.Store.DB.QueryRowContext(r.Context(), `SELECT machine_id FROM agents WHERE id=? AND revoked=0`, id).Scan(&machine) != nil {
		fail(w, 404, "agent not found")
		return
	}
	taskID := security.NewID("task")
	payload := `{"image":"ghcr.io/xiaofujie369/rotatopilot-agent:latest"}`
	_, err := a.Store.DB.ExecContext(r.Context(), `INSERT INTO agent_tasks(id,agent_id,machine_id,type,payload_json,status,created_at)VALUES(?,?,?,?,?,'pending',?)`, taskID, id, machine, "upgrade", payload, store.Now())
	if err != nil {
		fail(w, 500, "could not create task")
		return
	}
	write(w, 201, map[string]string{"taskId": taskID})
}

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	for name, q := range map[string]string{"machines": "SELECT COUNT(*) FROM machines", "agentsOnline": "SELECT COUNT(*) FROM agents WHERE status='online' AND revoked=0", "unhealthy": "SELECT COUNT(*) FROM machines WHERE health_status IN ('suspect','unhealthy')", "rotationsToday": "SELECT COUNT(*) FROM rotations WHERE started_at>=datetime('now','start of day')"} {
		var n int
		_ = a.Store.DB.QueryRowContext(r.Context(), q).Scan(&n)
		out[name] = n
	}
	write(w, 200, out)
}
