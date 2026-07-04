package provider

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

type Client struct {
	baseURL, token, tokenType string
	http                      *http.Client
}

func New(baseURL, token, tokenType string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid provider URL")
	}
	if u.Scheme != "https" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("provider URL must use HTTPS unless it is loopback")
		}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, tokenType: tokenType, http: &http.Client{Timeout: 20 * time.Second}}, nil
}
func (c *Client) post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	auth := c.token
	if c.tokenType == "bearer" {
		auth = "Bearer " + c.token
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	return raw, nil
}
func (c *Client) ListMachines(ctx context.Context) ([]models.ProviderMachine, error) {
	raw, err := c.post(ctx, "/lb/lightsail/page", map[string]any{"offset": 0, "limit": 100, "keyword": "", "remark": "", "groupName": ""})
	if err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("invalid provider JSON: %w", err)
	}
	list := findList(root)
	if list == nil {
		return nil, fmt.Errorf("provider response contains no machine list")
	}
	out := make([]models.ProviderMachine, 0, len(list))
	for _, v := range list {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		b, _ := json.Marshal(m)
		id := str(m, "id", "machineId", "instanceId")
		if id == "" {
			continue
		}
		out = append(out, models.ProviderMachine{ID: id, Name: str(m, "name", "instanceName"), Remark: str(m, "remark", "description"), Region: str(m, "region", "regionName"), Status: str(m, "status", "state"), PublicIPv4: str(m, "publicIpAddress", "publicIPv4", "ip"), PublicIPv6: str(m, "ipv6Address", "publicIPv6"), TrafficText: str(m, "traffic", "trafficText"), ExpireTime: str(m, "expireTime", "expiration"), Raw: b})
	}
	return out, nil
}
func (c *Client) ChangeIP(ctx context.Context, machineID string) error {
	_, err := c.post(ctx, "/lb/lightsail/changeIp", map[string]string{"id": machineID})
	return err
}
func findList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		for _, k := range []string{"list", "records", "items", "data", "result"} {
			if child, ok := x[k]; ok {
				if a := findList(child); a != nil {
					return a
				}
			}
		}
	}
	return nil
}
func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch x := v.(type) {
			case string:
				return x
			case json.Number:
				return x.String()
			case float64:
				return fmt.Sprintf("%.0f", x)
			default:
				return fmt.Sprint(x)
			}
		}
	}
	return ""
}
