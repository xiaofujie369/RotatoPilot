package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/models"
	probeutil "github.com/xiaofujie369/RotatoPilot/backend/internal/probe"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/security"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/store"
)

const probeCols = `id,machine_id,name,source,type,COALESCE(target_host,''),target_port,COALESCE(url,''),COALESCE(expected_status,''),timeout_ms,interval_seconds,failure_weight,enabled`

func scanProbe(row interface{ Scan(...any) error }) (models.Probe, error) {
	var p models.Probe
	var enabled int
	err := row.Scan(&p.ID, &p.MachineID, &p.Name, &p.Source, &p.Type, &p.TargetHost, &p.TargetPort, &p.URL, &p.ExpectedStatus, &p.TimeoutMS, &p.IntervalSeconds, &p.FailureWeight, &enabled)
	p.Enabled = enabled == 1
	return p, err
}
func (a *API) probes(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT `+probeCols+` FROM probe_configs ORDER BY machine_id,id`)
	if err != nil {
		fail(w, 500, "database error")
		return
	}
	defer rows.Close()
	out := []models.Probe{}
	for rows.Next() {
		p, e := scanProbe(rows)
		if e == nil {
			out = append(out, p)
		}
	}
	write(w, 200, out)
}
func validateProbe(p *models.Probe) error {
	p.Type = strings.ToLower(p.Type)
	p.Source = strings.ToLower(p.Source)
	if p.MachineID == "" || p.Name == "" {
		return fmt.Errorf("machineId and name are required")
	}
	if p.Source == "" {
		p.Source = "controller"
	}
	if p.Source != "controller" && p.Source != "agent" && p.Source != "external-agent" {
		return fmt.Errorf("invalid probe source")
	}
	if p.Type != "tcp" && p.Type != "http" && p.Type != "https" && p.Type != "icmp" {
		return fmt.Errorf("invalid probe type")
	}
	if (p.Type == "tcp" || p.Type == "icmp") && p.TargetPort == 0 {
		return fmt.Errorf("targetPort is required for this probe")
	}
	if p.TimeoutMS <= 0 {
		p.TimeoutMS = 5000
	}
	if p.IntervalSeconds <= 0 {
		p.IntervalSeconds = 300
	}
	if p.FailureWeight <= 0 {
		p.FailureWeight = 1
	}
	return nil
}
func (a *API) createProbe(w http.ResponseWriter, r *http.Request) {
	var p models.Probe
	if !decode(w, r, &p) {
		return
	}
	if err := validateProbe(&p); err != nil {
		fail(w, 400, err.Error())
		return
	}
	now := store.Now()
	res, err := a.Store.DB.ExecContext(r.Context(), `INSERT INTO probe_configs(machine_id,name,source,type,target_host,target_port,url,expected_status,timeout_ms,interval_seconds,failure_weight,enabled,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.MachineID, p.Name, p.Source, p.Type, p.TargetHost, p.TargetPort, p.URL, p.ExpectedStatus, p.TimeoutMS, p.IntervalSeconds, p.FailureWeight, store.Bool(p.Enabled), now, now)
	if err != nil {
		fail(w, 500, "could not create probe")
		return
	}
	p.ID, _ = res.LastInsertId()
	write(w, 201, p)
}
func (a *API) updateProbe(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	p, err := scanProbe(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+probeCols+` FROM probe_configs WHERE id=?`, id))
	if err != nil {
		fail(w, 404, "probe not found")
		return
	}
	var in models.Probe
	if !decode(w, r, &in) {
		return
	}
	in.ID = id
	if in.MachineID == "" {
		in.MachineID = p.MachineID
	}
	if in.Name == "" {
		in.Name = p.Name
	}
	if in.Source == "" {
		in.Source = p.Source
	}
	if in.Type == "" {
		in.Type = p.Type
	}
	if err := validateProbe(&in); err != nil {
		fail(w, 400, err.Error())
		return
	}
	_, err = a.Store.DB.ExecContext(r.Context(), `UPDATE probe_configs SET machine_id=?,name=?,source=?,type=?,target_host=?,target_port=?,url=?,expected_status=?,timeout_ms=?,interval_seconds=?,failure_weight=?,enabled=?,updated_at=? WHERE id=?`, in.MachineID, in.Name, in.Source, in.Type, in.TargetHost, in.TargetPort, in.URL, in.ExpectedStatus, in.TimeoutMS, in.IntervalSeconds, in.FailureWeight, store.Bool(in.Enabled), store.Now(), id)
	if err != nil {
		fail(w, 500, "could not update probe")
		return
	}
	write(w, 200, in)
}
func (a *API) deleteProbe(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	a.Store.DB.ExecContext(r.Context(), `DELETE FROM probe_configs WHERE id=?`, id)
	w.WriteHeader(204)
}
func (a *API) runProbe(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	p, err := scanProbe(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+probeCols+` FROM probe_configs WHERE id=?`, id))
	if err != nil {
		fail(w, 404, "probe not found")
		return
	}
	result := a.executeProbe(r.Context(), p)
	write(w, 200, result)
}
func (a *API) executeProbe(ctx context.Context, p models.Probe) probeutil.Result {
	var ip string
	_ = a.Store.DB.QueryRowContext(ctx, `SELECT COALESCE(public_ipv4,'') FROM machines WHERE id=?`, p.MachineID).Scan(&ip)
	if p.Source != "controller" {
		result := probeutil.Result{Success: false, Error: "agent probe queued", Target: p.TargetHost}
		var agentID string
		if a.Store.DB.QueryRowContext(ctx, `SELECT id FROM agents WHERE machine_id=? AND status='online' AND revoked=0 ORDER BY last_heartbeat_at DESC LIMIT 1`, p.MachineID).Scan(&agentID) == nil {
			taskID := security.NewID("task")
			payload := fmt.Sprintf(`{"targetHost":%q,"targetPort":%d,"url":%q,"timeoutMs":%d}`, p.TargetHost, p.TargetPort, p.URL, p.TimeoutMS)
			_, _ = a.Store.DB.ExecContext(ctx, `INSERT INTO agent_tasks(id,agent_id,machine_id,type,payload_json,status,created_at)VALUES(?,?,?,?,?,'pending',?)`, taskID, agentID, p.MachineID, p.Type+"_probe", payload, store.Now())
			result.Success = true
			result.Error = "queued as " + taskID
		}
		return result
	}
	result := probeutil.Run(ctx, p, ip)
	_, _ = a.Store.DB.ExecContext(ctx, `INSERT INTO probe_results(machine_id,probe_id,source,probe_type,target,success,latency_ms,error,checked_at)VALUES(?,?,?,?,?,?,?,?,?)`, p.MachineID, p.ID, p.Source, p.Type, result.Target, store.Bool(result.Success), result.LatencyMS, result.Error, store.Now())
	a.Hub.Broadcast("probe.result", map[string]any{"machineId": p.MachineID, "probeId": p.ID, "result": result})
	return result
}
func (a *API) checkMachine(w http.ResponseWriter, r *http.Request) {
	machineID := chi.URLParam(r, "id")
	score, total, err := a.checkMachineNow(r.Context(), machineID)
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	write(w, 200, map[string]any{"ok": score == 0, "failureScore": score, "probesRun": total})
}
func (a *API) checkMachineNow(ctx context.Context, machineID string) (int, int, error) {
	rows, err := a.Store.DB.QueryContext(ctx, `SELECT `+probeCols+` FROM probe_configs WHERE machine_id=? AND enabled=1`, machineID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	ps := []models.Probe{}
	for rows.Next() {
		p, e := scanProbe(rows)
		if e == nil {
			ps = append(ps, p)
		}
	}
	if len(ps) == 0 {
		return 0, 0, fmt.Errorf("no enabled probes configured")
	}
	score := 0
	for _, p := range ps {
		res := a.executeProbe(ctx, p)
		if !res.Success {
			score += p.FailureWeight
		}
	}
	var failure, success int
	_ = a.Store.DB.QueryRowContext(ctx, `SELECT failure_count,success_count FROM machines WHERE id=?`, machineID).Scan(&failure, &success)
	health := "healthy"
	if score >= 5 {
		failure++
		success = 0
		if failure >= a.Cfg.FailureThreshold {
			health = "suspect"
		}
	} else {
		success++
		if success >= a.Cfg.SuccessRecovery {
			failure = 0
		}
		if failure > 0 {
			health = "degraded"
		}
	}
	_, _ = a.Store.DB.ExecContext(ctx, `UPDATE machines SET health_status=?,failure_count=?,success_count=?,last_check_at=?,updated_at=? WHERE id=?`, health, failure, success, store.Now(), store.Now(), machineID)
	a.Hub.Broadcast("machine.health", map[string]any{"machineId": machineID, "healthStatus": health, "failureScore": score, "failureCount": failure})
	if health == "suspect" {
		a.Telegram.Send("⚠️ RotatoPilot machine suspect\nMachine: " + machineID + "\nFailure score: " + strconv.Itoa(score))
		if a.Cfg.AutoRotateGlobal {
			go a.rotate(context.Background(), machineID, "auto", "weighted probe threshold reached")
		}
	}
	return score, len(ps), nil
}
