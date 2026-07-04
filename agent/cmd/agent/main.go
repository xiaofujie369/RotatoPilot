package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const version = "1.0.0"

type config struct {
	URL, Token, ServerID string
	Heartbeat, Poll      time.Duration
}
type client struct {
	cfg  config
	http *http.Client
}

func main() {
	cfg := config{URL: strings.TrimRight(os.Getenv("CONTROLLER_URL"), "/"), Token: os.Getenv("AGENT_TOKEN"), ServerID: os.Getenv("SERVER_ID"), Heartbeat: duration("HEARTBEAT_INTERVAL_SECONDS", 30), Poll: duration("TASK_POLL_INTERVAL_SECONDS", 15)}
	if cfg.URL == "" || cfg.Token == "" || cfg.ServerID == "" {
		slog.Error("CONTROLLER_URL, AGENT_TOKEN and SERVER_ID are required")
		os.Exit(1)
	}
	c := &client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	for {
		err := c.register(ctx)
		if err == nil {
			break
		}
		slog.Warn("registration failed", "error", err)
		select {
		case <-time.After(15 * time.Second):
		case <-ctx.Done():
			return
		}
	}
	slog.Info("agent registered", "serverId", cfg.ServerID, "version", version)
	go c.heartbeatLoop(ctx)
	c.taskLoop(ctx)
}
func duration(k string, d int) time.Duration {
	n, e := strconv.Atoi(os.Getenv(k))
	if e != nil || n < 1 {
		n = d
	}
	return time.Duration(n) * time.Second
}
func (c *client) call(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.URL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Agent "+c.cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("controller HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
func (c *client) register(ctx context.Context) error {
	host, _ := os.Hostname()
	v4, v6 := publicIPs(ctx)
	return c.call(ctx, http.MethodPost, "/api/agent/register", map[string]any{"serverId": c.cfg.ServerID, "hostname": host, "agentVersion": version, "os": osDescription(), "arch": runtime.GOARCH, "publicIPv4": v4, "publicIPv6": v6}, nil)
}
func (c *client) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.Heartbeat)
	defer ticker.Stop()
	for {
		c.heartbeat(ctx)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
func (c *client) heartbeat(ctx context.Context) {
	v4, v6 := publicIPs(ctx)
	load := []float64{}
	if b, e := os.ReadFile("/proc/loadavg"); e == nil {
		fields := strings.Fields(string(b))
		if len(fields) > 3 {
			fields = fields[:3]
		}
		for _, s := range fields {
			v, _ := strconv.ParseFloat(s, 64)
			load = append(load, v)
		}
	}
	body := map[string]any{"serverId": c.cfg.ServerID, "publicIPv4": v4, "publicIPv6": v6, "uptimeSeconds": uptime(), "loadAvg": load, "dockerRunning": dockerAvailable(), "timestamp": time.Now().UTC()}
	if err := c.call(ctx, http.MethodPost, "/api/agent/heartbeat", body, nil); err != nil {
		slog.Warn("heartbeat failed", "error", err)
	}
}
func (c *client) taskLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.Poll)
	defer ticker.Stop()
	for {
		c.pollTasks(ctx)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
func (c *client) pollTasks(ctx context.Context) {
	var response struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := c.call(ctx, http.MethodGet, "/api/agent/tasks", nil, &response); err != nil {
		slog.Warn("task poll failed", "error", err)
		return
	}
	for _, task := range response.Tasks {
		go c.execute(context.Background(), task)
	}
}
func (c *client) execute(ctx context.Context, t map[string]any) {
	id, _ := t["taskId"].(string)
	typ, _ := t["type"].(string)
	start := time.Now()
	err := errors.New("unsupported task type")
	timeout := time.Duration(number(t["timeoutMs"], 5000)) * time.Millisecond
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch typ {
	case "tcp_probe":
		host, _ := t["targetHost"].(string)
		port := number(t["targetPort"], 0)
		var conn net.Conn
		conn, err = (&net.Dialer{}).DialContext(taskCtx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if conn != nil {
			_ = conn.Close()
		}
	case "http_probe", "https_probe":
		target, _ := t["url"].(string)
		req, e := http.NewRequestWithContext(taskCtx, http.MethodGet, target, nil)
		if e != nil {
			err = e
		} else {
			resp, e := c.http.Do(req)
			err = e
			if resp != nil {
				_ = resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 400 {
					err = fmt.Errorf("HTTP %d", resp.StatusCode)
				}
			}
		}
	case "upgrade":
		err = errors.New("self-upgrade requires reinstall command")
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	result := map[string]any{"taskId": id, "success": err == nil, "latencyMs": time.Since(start).Milliseconds(), "error": message, "finishedAt": time.Now().UTC()}
	if e := c.call(ctx, http.MethodPost, "/api/agent/tasks/result", result, nil); e != nil {
		slog.Warn("task result failed", "taskId", id, "error", e)
	}
}
func number(v any, d int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return d
}
func publicIPs(ctx context.Context) (string, string) {
	return publicIP(ctx, "https://api.ipify.org"), publicIP(ctx, "https://api6.ipify.org")
}
func publicIP(ctx context.Context, u string) string {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}
func uptime() int64 {
	b, e := os.ReadFile("/proc/uptime")
	if e != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return int64(v)
}
func dockerAvailable() bool {
	if runtime.GOOS == "windows" {
		_, e := os.Stat(`//./pipe/docker_engine`)
		return e == nil
	}
	c, e := net.DialTimeout("unix", "/var/run/docker.sock", time.Second)
	if c != nil {
		_ = c.Close()
	}
	return e == nil
}

func osDescription() string {
	name := runtime.GOOS
	if raw, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				value := strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"'`)
				if value != "" {
					name = value
				}
				break
			}
		}
	}
	if kernel, err := exec.Command("uname", "-r").Output(); err == nil && strings.TrimSpace(string(kernel)) != "" {
		name += " · kernel " + strings.TrimSpace(string(kernel))
	}
	return name
}
