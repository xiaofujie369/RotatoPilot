package api

import (
	"net/http"
	"strconv"
	"strings"
)

func (a *API) logs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if n, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && n > 0 && n <= 1000 {
		limit = n
	}
	q := `SELECT id,COALESCE(level,''),COALESCE(machine_id,''),COALESCE(job_type,''),COALESCE(message,''),COALESCE(meta_json,'{}'),COALESCE(created_at,'') FROM logs WHERE 1=1`
	args := []any{}
	if v := strings.TrimSpace(r.URL.Query().Get("machineId")); v != "" {
		q += ` AND machine_id=?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(r.URL.Query().Get("level")); v != "" {
		q += ` AND level=?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(r.URL.Query().Get("jobType")); v != "" {
		q += ` AND job_type=?`
		args = append(args, v)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := a.Store.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		fail(w, 500, "database error")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var level, machine, job, message, meta, created string
		if rows.Scan(&id, &level, &machine, &job, &message, &meta, &created) == nil {
			out = append(out, map[string]any{"id": id, "level": level, "machineId": machine, "jobType": job, "message": message, "metaJson": meta, "createdAt": created})
		}
	}
	write(w, 200, out)
}
