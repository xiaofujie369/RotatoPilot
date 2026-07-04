package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xiaofujie369/RotatoPilot/backend/internal/models"
)

func Sync(ctx context.Context, provider models.DNSProvider, token string, record models.DNSRecord, ip string) error {
	switch provider.ProviderType {
	case "cloudflare":
		return cloudflare(ctx, token, record, ip)
	case "webhook", "generic_webhook":
		return webhook(ctx, token, provider.ExtraConfigJSON, record, ip)
	default:
		return fmt.Errorf("unsupported DNS provider %q", provider.ProviderType)
	}
}
func request(ctx context.Context, method, endpoint, token string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("DNS API returned HTTP %d", resp.StatusCode)
	}
	return raw, nil
}
func cloudflare(ctx context.Context, token string, r models.DNSRecord, ip string) error {
	base := "https://api.cloudflare.com/client/v4/zones/" + url.PathEscape(r.ZoneID) + "/dns_records"
	raw, err := request(ctx, http.MethodGet, base+"?type="+url.QueryEscape(r.RecordType)+"&name="+url.QueryEscape(r.RecordName), token, nil)
	if err != nil {
		return err
	}
	var result struct {
		Success bool `json:"success"`
		Result  []struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success || len(result.Result) == 0 {
		return fmt.Errorf("Cloudflare DNS record not found")
	}
	body := map[string]any{"type": r.RecordType, "name": r.RecordName, "content": ip, "ttl": r.TTL, "proxied": r.Proxied}
	_, err = request(ctx, http.MethodPut, base+"/"+url.PathEscape(result.Result[0].ID), token, body)
	return err
}
func webhook(ctx context.Context, token, extra string, r models.DNSRecord, ip string) error {
	var cfg struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	if json.Unmarshal([]byte(extra), &cfg) != nil || cfg.URL == "" {
		return fmt.Errorf("webhook URL is not configured")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid webhook URL")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("webhook URL must use HTTPS unless it is loopback")
		}
	}
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodPost
	}
	_, err = request(ctx, method, cfg.URL, token, map[string]any{"recordName": r.RecordName, "recordType": r.RecordType, "ip": ip, "machineId": r.MachineID})
	return err
}
