package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xiaofujie369/RotatoPilot/backend/internal/models"
)

type Result struct {
	Success   bool   `json:"success"`
	LatencyMS int64  `json:"latencyMs"`
	Error     string `json:"error,omitempty"`
	Target    string `json:"target"`
}

func Run(ctx context.Context, p models.Probe, fallbackIP string) Result {
	start := time.Now()
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	host := p.TargetHost
	if host == "" {
		host = fallbackIP
	}
	r := Result{}
	switch strings.ToLower(p.Type) {
	case "tcp", "icmp": // ICMP commonly requires privileges; TCP fallback is explicit in the result.
		if p.TargetPort == 0 {
			r.Error = "target port is required"
			break
		}
		r.Target = net.JoinHostPort(host, strconv.Itoa(p.TargetPort))
		d := net.Dialer{}
		c, err := d.DialContext(ctx, "tcp", r.Target)
		if err != nil {
			r.Error = err.Error()
		} else {
			r.Success = true
			_ = c.Close()
		}
	case "http", "https":
		u := p.URL
		if u == "" {
			scheme := strings.ToLower(p.Type)
			u = scheme + "://" + host
			if p.TargetPort > 0 {
				u += "/"
				parsed := net.JoinHostPort(host, strconv.Itoa(p.TargetPort))
				u = scheme + "://" + parsed
			}
		}
		r.Target = u
		tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
		client := &http.Client{Transport: tr}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err == nil {
			resp, e := client.Do(req)
			err = e
			if resp != nil {
				defer resp.Body.Close()
				expected := p.ExpectedStatus
				if expected == "" {
					expected = "200-399"
				}
				r.Success = statusMatches(resp.StatusCode, expected)
				if !r.Success {
					r.Error = fmt.Sprintf("HTTP %d, expected %s", resp.StatusCode, expected)
				}
			}
		}
		if err != nil {
			r.Error = err.Error()
		}
	default:
		r.Error = "unsupported probe type"
	}
	r.LatencyMS = time.Since(start).Milliseconds()
	return r
}
func statusMatches(code int, expected string) bool {
	expected = strings.TrimSpace(expected)
	if strings.Contains(expected, "-") {
		p := strings.SplitN(expected, "-", 2)
		a, _ := strconv.Atoi(p[0])
		b, _ := strconv.Atoi(p[1])
		return code >= a && code <= b
	}
	for _, s := range strings.Split(expected, ",") {
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		if code == n {
			return true
		}
	}
	return false
}
