package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/xiaofujie369/RotatoPilot/backend/internal/dns"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/models"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/store"
)

const dnsProviderCols = `id,name,provider_type,COALESCE(token_encrypted,''),COALESCE(extra_config_json,'{}'),enabled`

func scanDNSProvider(row interface{ Scan(...any) error }) (models.DNSProvider, string, error) {
	var p models.DNSProvider
	var enc string
	var enabled int
	err := row.Scan(&p.ID, &p.Name, &p.ProviderType, &enc, &p.ExtraConfigJSON, &enabled)
	p.Enabled = enabled == 1
	if enc != "" {
		p.TokenMasked = "********"
	}
	return p, enc, err
}
func (a *API) dnsProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT `+dnsProviderCols+` FROM dns_providers ORDER BY id`)
	if err != nil {
		fail(w, 500, "database error")
		return
	}
	defer rows.Close()
	out := []models.DNSProvider{}
	for rows.Next() {
		p, _, e := scanDNSProvider(rows)
		if e == nil {
			out = append(out, p)
		}
	}
	write(w, 200, out)
}
func validDNSProvider(t string) bool {
	return t == "cloudflare" || t == "webhook" || t == "generic_webhook"
}
func (a *API) createDNSProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name, ProviderType, Token, ExtraConfigJSON string
		Enabled                                    *bool
	}
	if !decode(w, r, &in) {
		return
	}
	in.ProviderType = strings.ToLower(in.ProviderType)
	if in.Name == "" || !validDNSProvider(in.ProviderType) {
		fail(w, 400, "valid name and providerType are required")
		return
	}
	enc := ""
	var err error
	if in.Token != "" {
		enc, err = a.Vault.Encrypt(in.Token)
		if err != nil {
			fail(w, 500, "could not encrypt DNS token")
			return
		}
	}
	if in.ExtraConfigJSON == "" {
		in.ExtraConfigJSON = "{}"
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	now := store.Now()
	res, err := a.Store.DB.ExecContext(r.Context(), `INSERT INTO dns_providers(name,provider_type,token_encrypted,extra_config_json,enabled,created_at,updated_at)VALUES(?,?,?,?,?,?,?)`, in.Name, in.ProviderType, enc, in.ExtraConfigJSON, store.Bool(enabled), now, now)
	if err != nil {
		fail(w, 500, "could not create DNS provider")
		return
	}
	id, _ := res.LastInsertId()
	p, _, _ := scanDNSProvider(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+dnsProviderCols+` FROM dns_providers WHERE id=?`, id))
	write(w, 201, p)
}
func (a *API) updateDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	p, enc, err := scanDNSProvider(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+dnsProviderCols+` FROM dns_providers WHERE id=?`, id))
	if err != nil {
		fail(w, 404, "DNS provider not found")
		return
	}
	var in struct {
		Name, ProviderType, Token, ExtraConfigJSON string
		Enabled                                    *bool
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name != "" {
		p.Name = in.Name
	}
	if in.ProviderType != "" {
		p.ProviderType = strings.ToLower(in.ProviderType)
	}
	if !validDNSProvider(p.ProviderType) {
		fail(w, 400, "invalid providerType")
		return
	}
	if in.ExtraConfigJSON != "" {
		p.ExtraConfigJSON = in.ExtraConfigJSON
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.Token != "" {
		enc, err = a.Vault.Encrypt(in.Token)
		if err != nil {
			fail(w, 500, "could not encrypt token")
			return
		}
	}
	_, err = a.Store.DB.ExecContext(r.Context(), `UPDATE dns_providers SET name=?,provider_type=?,token_encrypted=?,extra_config_json=?,enabled=?,updated_at=? WHERE id=?`, p.Name, p.ProviderType, enc, p.ExtraConfigJSON, store.Bool(p.Enabled), store.Now(), id)
	if err != nil {
		fail(w, 500, "could not update DNS provider")
		return
	}
	p.TokenMasked = map[bool]string{true: "********"}[enc != ""]
	write(w, 200, p)
}
func (a *API) deleteDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var n int
	_ = a.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM dns_records WHERE dns_provider_id=?`, id).Scan(&n)
	if n > 0 {
		fail(w, 409, "DNS provider has records")
		return
	}
	a.Store.DB.ExecContext(r.Context(), `DELETE FROM dns_providers WHERE id=?`, id)
	w.WriteHeader(204)
}

const dnsRecordCols = `id,machine_id,dns_provider_id,record_name,record_type,COALESCE(zone_id,''),proxied,ttl,enabled,sync_after_rotation,COALESCE(last_ip,''),COALESCE(last_sync_status,''),COALESCE(last_sync_error,''),COALESCE(last_sync_at,'')`

func scanDNSRecord(row interface{ Scan(...any) error }) (models.DNSRecord, error) {
	var x models.DNSRecord
	var proxied, enabled, sync int
	err := row.Scan(&x.ID, &x.MachineID, &x.DNSProviderID, &x.RecordName, &x.RecordType, &x.ZoneID, &proxied, &x.TTL, &enabled, &sync, &x.LastIP, &x.LastSyncStatus, &x.LastSyncError, &x.LastSyncAt)
	x.Proxied = proxied == 1
	x.Enabled = enabled == 1
	x.SyncAfterRotation = sync == 1
	return x, err
}
func (a *API) dnsRecords(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT `+dnsRecordCols+` FROM dns_records ORDER BY id`)
	if err != nil {
		fail(w, 500, "database error")
		return
	}
	defer rows.Close()
	out := []models.DNSRecord{}
	for rows.Next() {
		x, e := scanDNSRecord(rows)
		if e == nil {
			out = append(out, x)
		}
	}
	write(w, 200, out)
}
func validateDNSRecord(x *models.DNSRecord) error {
	x.RecordType = strings.ToUpper(x.RecordType)
	if x.MachineID == "" || x.DNSProviderID == 0 || x.RecordName == "" {
		return fmt.Errorf("machineId, dnsProviderId and recordName are required")
	}
	if x.RecordType == "" {
		x.RecordType = "A"
	}
	if x.RecordType != "A" && x.RecordType != "AAAA" {
		return fmt.Errorf("recordType must be A or AAAA")
	}
	if x.TTL == 0 {
		x.TTL = 120
	}
	return nil
}
func (a *API) createDNSRecord(w http.ResponseWriter, r *http.Request) {
	var x models.DNSRecord
	if !decode(w, r, &x) {
		return
	}
	if err := validateDNSRecord(&x); err != nil {
		fail(w, 400, err.Error())
		return
	}
	now := store.Now()
	res, err := a.Store.DB.ExecContext(r.Context(), `INSERT INTO dns_records(machine_id,dns_provider_id,record_name,record_type,zone_id,proxied,ttl,enabled,sync_after_rotation,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?)`, x.MachineID, x.DNSProviderID, x.RecordName, x.RecordType, x.ZoneID, store.Bool(x.Proxied), x.TTL, store.Bool(x.Enabled), store.Bool(x.SyncAfterRotation), now, now)
	if err != nil {
		fail(w, 500, "could not create DNS record")
		return
	}
	x.ID, _ = res.LastInsertId()
	write(w, 201, x)
}
func (a *API) updateDNSRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var x models.DNSRecord
	if !decode(w, r, &x) {
		return
	}
	x.ID = id
	if err := validateDNSRecord(&x); err != nil {
		fail(w, 400, err.Error())
		return
	}
	_, err := a.Store.DB.ExecContext(r.Context(), `UPDATE dns_records SET machine_id=?,dns_provider_id=?,record_name=?,record_type=?,zone_id=?,proxied=?,ttl=?,enabled=?,sync_after_rotation=?,updated_at=? WHERE id=?`, x.MachineID, x.DNSProviderID, x.RecordName, x.RecordType, x.ZoneID, store.Bool(x.Proxied), x.TTL, store.Bool(x.Enabled), store.Bool(x.SyncAfterRotation), store.Now(), id)
	if err != nil {
		fail(w, 500, "could not update DNS record")
		return
	}
	write(w, 200, x)
}
func (a *API) deleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	a.Store.DB.ExecContext(r.Context(), `DELETE FROM dns_records WHERE id=?`, id)
	w.WriteHeader(204)
}
func (a *API) syncDNSRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	x, err := scanDNSRecord(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+dnsRecordCols+` FROM dns_records WHERE id=?`, id))
	if err != nil {
		fail(w, 404, "DNS record not found")
		return
	}
	var ip string
	field := "public_ipv4"
	if x.RecordType == "AAAA" {
		field = "public_ipv6"
	}
	if a.Store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(`+field+`,'') FROM machines WHERE id=?`, x.MachineID).Scan(&ip) != nil || ip == "" {
		fail(w, 409, "machine has no matching IP address")
		return
	}
	if err = a.syncOneDNS(r.Context(), x, ip); err != nil {
		fail(w, 502, err.Error())
		return
	}
	write(w, 200, map[string]any{"ok": true, "ip": ip})
}
func (a *API) syncMachineDNS(w http.ResponseWriter, r *http.Request) {
	machineID := chiMachineID(r)
	var ip string
	if a.Store.DB.QueryRowContext(r.Context(), `SELECT COALESCE(public_ipv4,'') FROM machines WHERE id=?`, machineID).Scan(&ip) != nil {
		fail(w, 404, "machine not found")
		return
	}
	if err := a.syncDNSForMachine(r.Context(), machineID, ip, false); err != nil {
		fail(w, 502, err.Error())
		return
	}
	write(w, 200, map[string]bool{"ok": true})
}
func (a *API) syncDNSForMachine(ctx context.Context, machineID, ipv4 string, afterRotation bool) error {
	q := `SELECT ` + dnsRecordCols + ` FROM dns_records WHERE machine_id=? AND enabled=1`
	if afterRotation {
		q += ` AND sync_after_rotation=1`
	}
	rows, err := a.Store.DB.QueryContext(ctx, q, machineID)
	if err != nil {
		return err
	}
	records := []models.DNSRecord{}
	for rows.Next() {
		x, e := scanDNSRecord(rows)
		if e == nil {
			records = append(records, x)
		}
	}
	rows.Close()
	for _, x := range records {
		ip := ipv4
		if x.RecordType == "AAAA" {
			_ = a.Store.DB.QueryRowContext(ctx, `SELECT COALESCE(public_ipv6,'') FROM machines WHERE id=?`, machineID).Scan(&ip)
		}
		if ip == "" {
			continue
		}
		if err := a.syncOneDNS(ctx, x, ip); err != nil {
			return err
		}
	}
	return nil
}
func (a *API) syncOneDNS(ctx context.Context, x models.DNSRecord, ip string) error {
	p, enc, err := scanDNSProvider(a.Store.DB.QueryRowContext(ctx, `SELECT `+dnsProviderCols+` FROM dns_providers WHERE id=? AND enabled=1`, x.DNSProviderID))
	if err != nil {
		return fmt.Errorf("DNS provider unavailable")
	}
	token := ""
	if enc != "" {
		token, err = a.Vault.Decrypt(enc)
		if err != nil {
			return fmt.Errorf("DNS token cannot be decrypted")
		}
	}
	err = dns.Sync(ctx, p, token, x, ip)
	status := "completed"
	msg := ""
	if err != nil {
		status = "failed"
		msg = err.Error()
	}
	_, _ = a.Store.DB.ExecContext(ctx, `UPDATE dns_records SET last_ip=?,last_sync_status=?,last_sync_error=?,last_sync_at=?,updated_at=? WHERE id=?`, ip, status, msg, store.Now(), store.Now(), x.ID)
	a.Hub.Broadcast("dns.sync", map[string]any{"recordId": x.ID, "machineId": x.MachineID, "status": status, "ip": ip})
	return err
}
