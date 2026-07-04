package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/xiaofujie369/RotatoPilot/backend/internal/config"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/models"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/notify"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/provider"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/realtime"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/security"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/store"
)

const Version = "1.0.0"

type API struct {
	Cfg       config.Config
	Store     *store.Store
	Vault     *security.Vault
	Hub       *realtime.Hub
	Telegram  notify.Telegram
	rotations sync.Map
}

func New(cfg config.Config, st *store.Store) *API {
	return &API{Cfg: cfg, Store: st, Vault: security.NewVault(cfg.EncryptionKey), Hub: realtime.New(), Telegram: notify.Telegram{Enabled: cfg.TelegramEnabled, Token: cfg.TelegramBotToken, ChatID: cfg.TelegramChatID}}
}

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(a.recoverer, a.securityHeaders, a.accessLog)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Store.DB.PingContext(r.Context()); err != nil {
			fail(w, 503, "database unavailable")
			return
		}
		write(w, 200, map[string]any{"ok": true})
	})
	r.Get("/api/version", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"version": Version}) })
	r.Get("/install-agent.sh", func(w http.ResponseWriter, r *http.Request) {
		path := os.Getenv("INSTALL_AGENT_SCRIPT")
		if path == "" {
			path = "./install-agent.sh"
		}
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		http.ServeFile(w, r, path)
	})
	r.Post("/api/auth/login", a.login)
	r.Post("/api/auth/logout", a.logout)
	r.Route("/api/agent", func(r chi.Router) {
		r.Use(a.agentAuth)
		r.Post("/register", a.agentRegister)
		r.Post("/heartbeat", a.agentHeartbeat)
		r.Get("/tasks", a.agentTasks)
		r.Post("/tasks/result", a.agentTaskResult)
	})
	r.Group(func(r chi.Router) {
		r.Use(a.adminAuth)
		r.Get("/api/auth/me", a.me)
		r.Post("/api/auth/password", a.changePassword)
		r.Get("/ws", a.ws)
		r.Route("/api/providers", func(r chi.Router) {
			r.Get("/", a.providers)
			r.Post("/", a.createProvider)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", a.providerByID)
				r.Patch("/", a.updateProvider)
				r.Delete("/", a.deleteProvider)
				r.Post("/test", a.testProvider)
				r.Post("/sync-machines", a.syncProvider)
			})
		})
		r.Route("/api/machines", func(r chi.Router) {
			r.Get("/", a.machines)
			r.Post("/", a.createMachine)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", a.machineByID)
				r.Patch("/", a.updateMachine)
				r.Delete("/", a.deleteMachine)
				r.Post("/check", a.checkMachine)
				r.Post("/change-ip", a.changeIP)
				r.Post("/sync-dns", a.syncMachineDNS)
				r.Post("/generate-agent-token", a.generateAgentToken)
			})
		})
		r.Get("/api/agents", a.agents)
		r.Post("/api/agents/{id}/revoke", a.revokeAgent)
		r.Post("/api/agents/{id}/upgrade-task", a.upgradeAgent)
		r.Route("/api/probes", func(r chi.Router) {
			r.Get("/", a.probes)
			r.Post("/", a.createProbe)
			r.Patch("/{id}", a.updateProbe)
			r.Delete("/{id}", a.deleteProbe)
			r.Post("/{id}/run", a.runProbe)
		})
		r.Route("/api/dns-providers", func(r chi.Router) {
			r.Get("/", a.dnsProviders)
			r.Post("/", a.createDNSProvider)
			r.Patch("/{id}", a.updateDNSProvider)
			r.Delete("/{id}", a.deleteDNSProvider)
		})
		r.Route("/api/dns-records", func(r chi.Router) {
			r.Get("/", a.dnsRecords)
			r.Post("/", a.createDNSRecord)
			r.Patch("/{id}", a.updateDNSRecord)
			r.Delete("/{id}", a.deleteDNSRecord)
			r.Post("/{id}/sync", a.syncDNSRecord)
		})
		r.Get("/api/rotations", a.rotationHistory)
		r.Get("/api/rotations/{id}", a.rotationByID)
		r.Post("/api/rotations/{id}/retry-dns", a.retryRotationDNS)
		r.Get("/api/logs", a.logs)
		r.Get("/api/overview", a.overview)
	})
	r.Handle("/*", a.frontend())
	return r
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]any{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "invalid request: "+err.Error())
		return false
	}
	return true
}
func idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		fail(w, 400, "invalid id")
		return 0, false
	}
	return id, true
}
func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if x := recover(); x != nil {
				slog.Error("request panic", "error", x)
				fail(w, 500, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; style-src 'self' 'unsafe-inline'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}
func (a *API) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if !decode(w, r, &in) {
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(in.Username), []byte(a.Cfg.AdminUsername)) == 1
	passwordValue, mustChange := a.adminPassword()
	passOK := false
	if strings.HasPrefix(passwordValue, "$2") {
		passOK = bcrypt.CompareHashAndPassword([]byte(passwordValue), []byte(in.Password)) == nil
	} else {
		passOK = subtle.ConstantTimeCompare([]byte(in.Password), []byte(passwordValue)) == 1
	}
	if !userOK || !passOK {
		a.Store.Log("warn", "", "auth", "Failed login", `{"reason":"invalid_credentials"}`)
		fail(w, 401, "invalid credentials")
		return
	}
	claims := jwt.MapClaims{"sub": in.Username, "exp": time.Now().Add(12 * time.Hour).Unix(), "iat": time.Now().Unix()}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.Cfg.JWTSecret))
	http.SetCookie(w, &http.Cookie{Name: "rotato_session", Value: token, HttpOnly: true, Secure: strings.HasPrefix(a.Cfg.PublicURL, "https://"), SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: 43200})
	write(w, 200, map[string]any{"ok": true, "username": in.Username, "mustChangePassword": mustChange})
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "rotato_session", Value: "", HttpOnly: true, Path: "/", MaxAge: -1})
	write(w, 200, map[string]bool{"ok": true})
}
func (a *API) tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, e := r.Cookie("rotato_session"); e == nil {
		return c.Value
	}
	return ""
}
func (a *API) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, err := jwt.Parse(a.tokenFromRequest(r), func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("invalid signing method")
			}
			return []byte(a.Cfg.JWTSecret), nil
		})
		if err != nil || !t.Valid {
			fail(w, 401, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) adminPassword() (string, bool) {
	var passwordHash string
	if a.Store.DB.QueryRow(`SELECT value FROM app_settings WHERE key='admin_password_hash'`).Scan(&passwordHash) == nil && passwordHash != "" {
		return passwordHash, false
	}
	return a.Cfg.AdminPassword, strings.HasPrefix(a.Cfg.AdminPassword, "change_me")
}
func (a *API) me(w http.ResponseWriter, r *http.Request) {
	_, mustChange := a.adminPassword()
	write(w, 200, map[string]any{"username": a.Cfg.AdminUsername, "mustChangePassword": mustChange})
}
func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ CurrentPassword, NewPassword string }
	if !decode(w, r, &in) {
		return
	}
	if len(in.NewPassword) < 12 {
		fail(w, 400, "new password must contain at least 12 characters")
		return
	}
	current, _ := a.adminPassword()
	valid := false
	if strings.HasPrefix(current, "$2") {
		valid = bcrypt.CompareHashAndPassword([]byte(current), []byte(in.CurrentPassword)) == nil
	} else {
		valid = subtle.ConstantTimeCompare([]byte(current), []byte(in.CurrentPassword)) == 1
	}
	if !valid {
		fail(w, 403, "current password is incorrect")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		fail(w, 500, "could not secure new password")
		return
	}
	_, err = a.Store.DB.ExecContext(r.Context(), `INSERT INTO app_settings(key,value,updated_at) VALUES('admin_password_hash',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, string(hash), store.Now())
	if err != nil {
		fail(w, 500, "could not save new password")
		return
	}
	a.Store.Log("info", "", "auth", "Administrator password changed", `{}`)
	write(w, 200, map[string]bool{"ok": true})
}

func (a *API) frontend() http.Handler {
	dir := os.Getenv("STATIC_DIR")
	if dir == "" {
		dir = "./frontend/dist"
	}
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		index := filepath.Join(dir, "index.html")
		_, err := os.Stat(index)
		if err == nil {
			http.ServeFile(w, r, index)
			return
		}
		if errors.Is(err, fs.ErrNotExist) {
			fail(w, 404, "frontend is not built")
			return
		}
		fail(w, 500, "frontend unavailable")
	})
}

func (a *API) ws(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	a.Hub.Add(c)
	defer a.Hub.Remove(c)
	ctx := r.Context()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return
		}
	}
}

func scanProvider(row interface{ Scan(...any) error }) (models.Provider, string, error) {
	var p models.Provider
	var enc string
	var enabled int
	err := row.Scan(&p.ID, &p.Name, &p.APIBaseURL, &enc, &p.TokenType, &enabled, &p.LastTestStatus, &p.LastTestError, &p.LastTestAt, &p.CreatedAt, &p.UpdatedAt)
	p.Enabled = enabled == 1
	if enc != "" {
		p.TokenMasked = "********"
	}
	return p, enc, err
}

const providerCols = `id,name,api_base_url,token_encrypted,token_type,enabled,COALESCE(last_test_status,''),COALESCE(last_test_error,''),COALESCE(last_test_at,''),created_at,updated_at`

func (a *API) providers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT `+providerCols+` FROM provider_panels ORDER BY id`)
	if err != nil {
		fail(w, 500, "database error")
		return
	}
	defer rows.Close()
	out := []models.Provider{}
	for rows.Next() {
		p, _, e := scanProvider(rows)
		if e == nil {
			out = append(out, p)
		}
	}
	write(w, 200, out)
}
func (a *API) createProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name, APIBaseURL, Token, TokenType string
		Enabled                            *bool
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name == "" || in.APIBaseURL == "" || in.Token == "" {
		fail(w, 400, "name, apiBaseUrl and token are required")
		return
	}
	if _, err := provider.New(in.APIBaseURL, in.Token, in.TokenType); err != nil {
		fail(w, 400, err.Error())
		return
	}
	enc, err := a.Vault.Encrypt(in.Token)
	if err != nil {
		fail(w, 500, "could not encrypt token")
		return
	}
	tt := in.TokenType
	if tt == "" {
		tt = "raw_authorization"
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	now := store.Now()
	res, err := a.Store.DB.ExecContext(r.Context(), `INSERT INTO provider_panels(name,api_base_url,token_encrypted,token_type,enabled,created_at,updated_at)VALUES(?,?,?,?,?,?,?)`, in.Name, in.APIBaseURL, enc, tt, store.Bool(enabled), now, now)
	if err != nil {
		fail(w, 500, "could not save provider")
		return
	}
	id, _ := res.LastInsertId()
	a.Store.Log("info", "", "provider", "Provider created", fmt.Sprintf(`{"providerId":%d}`, id))
	a.providerResponse(w, r, id, 201)
}
func (a *API) providerResponse(w http.ResponseWriter, r *http.Request, id int64, status int) {
	p, _, err := scanProvider(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+providerCols+` FROM provider_panels WHERE id=?`, id))
	if err != nil {
		fail(w, 404, "provider not found")
		return
	}
	write(w, status, p)
}
func (a *API) providerByID(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if ok {
		a.providerResponse(w, r, id, 200)
	}
}
func (a *API) updateProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	p, enc, err := scanProvider(a.Store.DB.QueryRowContext(r.Context(), `SELECT `+providerCols+` FROM provider_panels WHERE id=?`, id))
	if err != nil {
		fail(w, 404, "provider not found")
		return
	}
	var in struct {
		Name, APIBaseURL, Token, TokenType string
		Enabled                            *bool
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name != "" {
		p.Name = in.Name
	}
	if in.APIBaseURL != "" {
		p.APIBaseURL = in.APIBaseURL
	}
	if in.TokenType != "" {
		p.TokenType = in.TokenType
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
	if _, err = provider.New(p.APIBaseURL, "validation", p.TokenType); err != nil {
		fail(w, 400, err.Error())
		return
	}
	_, err = a.Store.DB.ExecContext(r.Context(), `UPDATE provider_panels SET name=?,api_base_url=?,token_encrypted=?,token_type=?,enabled=?,updated_at=? WHERE id=?`, p.Name, p.APIBaseURL, enc, p.TokenType, store.Bool(p.Enabled), store.Now(), id)
	if err != nil {
		fail(w, 500, "could not update provider")
		return
	}
	a.providerResponse(w, r, id, 200)
}
func (a *API) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var n int
	_ = a.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM machines WHERE provider_id=?`, id).Scan(&n)
	if n > 0 {
		fail(w, 409, "provider has machines and cannot be deleted")
		return
	}
	a.Store.DB.ExecContext(r.Context(), `DELETE FROM provider_panels WHERE id=?`, id)
	w.WriteHeader(204)
}
func (a *API) providerClient(ctx context.Context, id int64) (*provider.Client, error) {
	p, enc, err := scanProvider(a.Store.DB.QueryRowContext(ctx, `SELECT `+providerCols+` FROM provider_panels WHERE id=?`, id))
	if err != nil {
		return nil, fmt.Errorf("provider not found")
	}
	token, err := a.Vault.Decrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("provider token cannot be decrypted")
	}
	return provider.New(p.APIBaseURL, token, p.TokenType)
}
func (a *API) testProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	client, err := a.providerClient(r.Context(), id)
	count := 0
	if err == nil {
		var ms []models.ProviderMachine
		ms, err = client.ListMachines(r.Context())
		count = len(ms)
	}
	status := "ok"
	msg := ""
	if err != nil {
		status = "error"
		msg = err.Error()
	}
	a.Store.DB.Exec(`UPDATE provider_panels SET last_test_status=?,last_test_error=?,last_test_at=?,updated_at=? WHERE id=?`, status, msg, store.Now(), store.Now(), id)
	if err != nil {
		fail(w, 502, msg)
		return
	}
	write(w, 200, map[string]any{"ok": true, "machineCount": count})
}
func (a *API) syncProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	client, err := a.providerClient(r.Context(), id)
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	ms, err := client.ListMachines(r.Context())
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	tx, _ := a.Store.DB.BeginTx(r.Context(), nil)
	now := store.Now()
	for _, m := range ms {
		raw, _ := json.Marshal(m.Raw)
		_, err = tx.Exec(`INSERT INTO machines(id,provider_id,name,remark,region,status,public_ipv4,public_ipv6,traffic_text,expire_time,raw_provider_json,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET provider_id=excluded.provider_id,name=COALESCE(NULLIF(excluded.name,''),machines.name),remark=excluded.remark,region=excluded.region,status=excluded.status,public_ipv4=excluded.public_ipv4,public_ipv6=excluded.public_ipv6,traffic_text=excluded.traffic_text,expire_time=excluded.expire_time,raw_provider_json=excluded.raw_provider_json,updated_at=excluded.updated_at`, m.ID, id, m.Name, m.Remark, m.Region, m.Status, m.PublicIPv4, m.PublicIPv6, m.TrafficText, m.ExpireTime, string(raw), now, now)
		if err != nil {
			_ = tx.Rollback()
			fail(w, 500, "machine sync failed")
			return
		}
	}
	_ = tx.Commit()
	a.Store.Log("info", "", "provider_sync", "Machines synchronized", fmt.Sprintf(`{"providerId":%d,"count":%d}`, id, len(ms)))
	a.Hub.Broadcast("machines.synced", map[string]any{"providerId": id, "count": len(ms)})
	write(w, 200, map[string]any{"ok": true, "count": len(ms), "machines": ms})
}
