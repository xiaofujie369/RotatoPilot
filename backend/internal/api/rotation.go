package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/store"
)

func (a *API) changeIP(w http.ResponseWriter, r *http.Request) {
	machineID := chi.URLParam(r, "id")
	var in struct {
		ConfirmMachineID string `json:"confirmMachineId"`
		Reason           string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ConfirmMachineID != machineID {
		fail(w, 400, "type the exact machine ID to confirm this operation")
		return
	}
	if in.Reason == "" {
		in.Reason = "manual dashboard request"
	}
	result, err := a.rotate(r.Context(), machineID, "manual", in.Reason)
	if err != nil {
		status := 500
		if err == errRotationBusy {
			status = 409
		}
		fail(w, status, err.Error())
		return
	}
	write(w, 200, result)
}

var errRotationBusy = fmt.Errorf("rotation already in progress")

func (a *API) rotate(ctx context.Context, machineID, trigger, reason string) (map[string]any, error) {
	if _, loaded := a.rotations.LoadOrStore(machineID, true); loaded {
		return nil, errRotationBusy
	}
	defer a.rotations.Delete(machineID)
	m, err := scanMachine(a.Store.DB.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE id=?`, machineID))
	if err != nil {
		return nil, fmt.Errorf("machine not found")
	}
	if trigger == "auto" {
		if !a.Cfg.AutoRotateGlobal || !m.AutoRotateEnabled {
			return nil, fmt.Errorf("automatic rotation is disabled")
		}
		if m.CooldownUntil != "" {
			if until, e := time.Parse(time.RFC3339, m.CooldownUntil); e == nil && time.Now().Before(until) {
				return nil, fmt.Errorf("rotation cooldown is active")
			}
		}
		var count int
		_ = a.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM rotations WHERE machine_id=? AND status='completed' AND started_at>=?`, machineID, time.Now().UTC().Truncate(24*time.Hour).Format(time.RFC3339)).Scan(&count)
		if count >= a.Cfg.MaxRotationsPerDay {
			return nil, fmt.Errorf("daily rotation limit reached")
		}
		if a.Cfg.RequireConfirmation {
			score, _, e := a.checkMachineNow(ctx, machineID)
			if e != nil || score < 5 {
				return nil, fmt.Errorf("confirmation check did not meet failure threshold")
			}
		}
	}
	client, err := a.providerClient(ctx, m.ProviderID)
	if err != nil {
		return nil, err
	}
	oldIP := m.PublicIPv4
	if machines, e := client.ListMachines(ctx); e == nil {
		for _, pm := range machines {
			if pm.ID == machineID && pm.PublicIPv4 != "" {
				oldIP = pm.PublicIPv4
				break
			}
		}
	}
	started := store.Now()
	res, err := a.Store.DB.ExecContext(ctx, `INSERT INTO rotations(machine_id,old_ip,trigger_type,reason,status,started_at)VALUES(?,?,?,?,?,?)`, machineID, oldIP, trigger, reason, "running", started)
	if err != nil {
		return nil, err
	}
	rotationID, _ := res.LastInsertId()
	a.Store.Log("info", machineID, "rotate_ip", "IP rotation started", fmt.Sprintf(`{"rotationId":%d,"trigger":%q,"oldIp":%q}`, rotationID, trigger, oldIP))
	a.Hub.Broadcast("rotation.started", map[string]any{"rotationId": rotationID, "machineId": machineID, "oldIp": oldIP, "trigger": trigger})
	a.Telegram.Send("🔄 RotatoPilot IP rotation started\nMachine: " + machineID + "\nOld IP: " + oldIP)
	if err = client.ChangeIP(ctx, machineID); err != nil {
		a.failRotation(rotationID, machineID, err)
		return nil, err
	}
	wait := a.Cfg.ChangeIPWait
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	newIP := ""
	deadline := time.Now().Add(a.Cfg.ChangeIPPollTimeout)
	for err == nil && time.Now().Before(deadline) {
		machines, e := client.ListMachines(ctx)
		if e == nil {
			for _, pm := range machines {
				if pm.ID == machineID && pm.PublicIPv4 != "" && pm.PublicIPv4 != oldIP {
					newIP = pm.PublicIPv4
					break
				}
			}
		}
		if newIP != "" {
			break
		}
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	if err == nil && newIP == "" {
		err = fmt.Errorf("provider accepted change but IP did not change before timeout")
	}
	if err != nil {
		a.failRotation(rotationID, machineID, err)
		return nil, err
	}
	cooldown := time.Now().UTC().Add(a.Cfg.RotationCooldown).Format(time.RFC3339)
	_, _ = a.Store.DB.ExecContext(ctx, `UPDATE machines SET public_ipv4=?,health_status='unknown',failure_count=0,success_count=0,last_rotation_at=?,rotation_cooldown_until=?,updated_at=? WHERE id=?`, newIP, store.Now(), cooldown, store.Now(), machineID)
	dnsStatus := "disabled"
	if e := a.syncDNSForMachine(ctx, machineID, newIP, true); e != nil {
		dnsStatus = "failed"
		a.Store.Log("error", machineID, "dns_sync", "DNS sync after rotation failed", fmt.Sprintf(`{"error":%q}`, e.Error()))
	} else {
		var n int
		_ = a.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_records WHERE machine_id=? AND enabled=1 AND sync_after_rotation=1`, machineID).Scan(&n)
		if n > 0 {
			dnsStatus = "completed"
		}
	}
	postStatus := "not_run"
	if score, total, e := a.checkMachineNow(ctx, machineID); e == nil && total > 0 {
		if score == 0 {
			postStatus = "passed"
		} else {
			postStatus = "failed"
		}
	}
	finished := store.Now()
	_, _ = a.Store.DB.ExecContext(ctx, `UPDATE rotations SET new_ip=?,status='completed',dns_sync_status=?,post_check_status=?,finished_at=? WHERE id=?`, newIP, dnsStatus, postStatus, finished, rotationID)
	a.Store.Log("info", machineID, "rotate_ip", "IP rotation completed", fmt.Sprintf(`{"rotationId":%d,"oldIp":%q,"newIp":%q}`, rotationID, oldIP, newIP))
	result := map[string]any{"rotationId": rotationID, "machineId": machineID, "oldIp": oldIP, "newIp": newIP, "dnsSyncStatus": dnsStatus, "postCheckStatus": postStatus}
	a.Hub.Broadcast("rotation.completed", result)
	a.Telegram.Send("✅ RotatoPilot IP rotation completed\nMachine: " + machineID + "\nOld IP: " + oldIP + "\nNew IP: " + newIP + "\nDNS: " + dnsStatus)
	return result, nil
}
func (a *API) failRotation(id int64, machineID string, err error) {
	_, _ = a.Store.DB.Exec(`UPDATE rotations SET status='failed',error=?,finished_at=? WHERE id=?`, err.Error(), store.Now(), id)
	a.Store.Log("error", machineID, "rotate_ip", "IP rotation failed", fmt.Sprintf(`{"rotationId":%d,"error":%q}`, id, err.Error()))
	a.Hub.Broadcast("rotation.failed", map[string]any{"rotationId": id, "machineId": machineID, "error": err.Error()})
	a.Telegram.Send("❌ RotatoPilot IP rotation failed\nMachine: " + machineID + "\nError: " + err.Error())
}
func (a *API) rotationHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if n, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && n > 0 && n <= 500 {
		limit = n
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT id,machine_id,COALESCE(old_ip,''),COALESCE(new_ip,''),COALESCE(trigger_type,''),COALESCE(reason,''),COALESCE(status,''),COALESCE(dns_sync_status,''),COALESCE(post_check_status,''),COALESCE(error,''),COALESCE(started_at,''),COALESCE(finished_at,'') FROM rotations ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		fail(w, 500, "database error")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var machine, oldIP, newIP, trigger, reason, status, dns, post, er, started, finished string
		if rows.Scan(&id, &machine, &oldIP, &newIP, &trigger, &reason, &status, &dns, &post, &er, &started, &finished) == nil {
			out = append(out, map[string]any{"id": id, "machineId": machine, "oldIp": oldIP, "newIp": newIP, "triggerType": trigger, "reason": reason, "status": status, "dnsSyncStatus": dns, "postCheckStatus": post, "error": er, "startedAt": started, "finishedAt": finished})
		}
	}
	write(w, 200, out)
}
func (a *API) rotationByID(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	row := a.Store.DB.QueryRowContext(r.Context(), `SELECT id,machine_id,COALESCE(old_ip,''),COALESCE(new_ip,''),COALESCE(trigger_type,''),COALESCE(reason,''),COALESCE(status,''),COALESCE(dns_sync_status,''),COALESCE(post_check_status,''),COALESCE(error,''),COALESCE(started_at,''),COALESCE(finished_at,'') FROM rotations WHERE id=?`, id)
	var machine, oldIP, newIP, trigger, reason, status, dns, post, er, started, finished string
	if row.Scan(&id, &machine, &oldIP, &newIP, &trigger, &reason, &status, &dns, &post, &er, &started, &finished) != nil {
		fail(w, 404, "rotation not found")
		return
	}
	write(w, 200, map[string]any{"id": id, "machineId": machine, "oldIp": oldIP, "newIp": newIP, "triggerType": trigger, "reason": reason, "status": status, "dnsSyncStatus": dns, "postCheckStatus": post, "error": er, "startedAt": started, "finishedAt": finished})
}
func (a *API) retryRotationDNS(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var machine, newIP string
	if a.Store.DB.QueryRowContext(r.Context(), `SELECT machine_id,new_ip FROM rotations WHERE id=?`, id).Scan(&machine, &newIP) != nil {
		fail(w, 404, "rotation not found")
		return
	}
	if err := a.syncDNSForMachine(r.Context(), machine, newIP, false); err != nil {
		fail(w, 502, err.Error())
		return
	}
	_, _ = a.Store.DB.ExecContext(r.Context(), `UPDATE rotations SET dns_sync_status='completed' WHERE id=?`, id)
	write(w, 200, map[string]bool{"ok": true})
}
