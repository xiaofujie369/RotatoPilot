package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/xiaofujie369/RotatoPilot/backend/internal/models"
)

func (a *API) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	slog.Info("probe scheduler started", "interval", a.Cfg.CheckInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.schedulerTick(ctx)
		}
	}
}
func (a *API) schedulerTick(ctx context.Context) {
	rows, err := a.Store.DB.QueryContext(ctx, `SELECT `+probeCols+` FROM probe_configs p WHERE p.enabled=1 AND p.source='controller' AND NOT EXISTS(SELECT 1 FROM probe_results r WHERE r.probe_id=p.id AND r.checked_at > datetime('now','-'||p.interval_seconds||' seconds'))`)
	if err != nil {
		return
	}
	ps := []models.Probe{}
	for rows.Next() {
		p, e := scanProbe(rows)
		if e == nil {
			ps = append(ps, p)
		}
	}
	rows.Close()
	seen := map[string]bool{}
	for _, p := range ps {
		if seen[p.MachineID] {
			continue
		}
		seen[p.MachineID] = true
		go func(id string) {
			if _, _, e := a.checkMachineNow(context.Background(), id); e != nil {
				slog.Warn("scheduled probe failed", "machineId", id, "error", e)
			}
		}(p.MachineID)
	}
}
