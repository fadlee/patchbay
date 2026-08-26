package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)
//go:embed web/templates/*.html
var templateFS embed.FS
//go:embed web/static/*
var staticFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "web/templates/*.html"))

// App wires the config store, proxy manager, traffic logger, and SSE hub into HTTP handlers.
type App struct {
	store   *ConfigStore
	manager *Manager
	logger  *TrafficLogger
	hub     *SSEHub
	updater *Updater
}

func NewApp(store *ConfigStore, manager *Manager, logger *TrafficLogger, hub *SSEHub) *App {
	return &App{
		store:   store,
		manager: manager,
		logger:  logger,
		hub:     hub,
		updater: NewUpdater(""),
	}
}

func (a *App) SetUpdater(u *Updater) {
	a.updater = u
}

// ruleView is what JSON API / templates serialize — a Rule plus its live status.
type ruleView struct {
	Rule
	Running         bool   `json:"running"`
	Stats           Stats  `json:"stats"`
	FirewallWarning string `json:"firewall_warning,omitempty"`
}

func (a *App) ruleViews() []ruleView {
	cfg := a.store.Snapshot()
	views := make([]ruleView, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		views = append(views, ruleView{
			Rule:            r,
			Running:         a.manager.IsRunning(r.ID),
			Stats:           a.manager.StatsFor(r.ID),
			FirewallWarning: a.manager.FirewallWarning(r.ID),
		})
	}
	return views
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	staticRoot, err := fs.Sub(staticFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/static/", http.FileServer(http.FS(staticRoot)))
	mux.HandleFunc("/", a.handleIndex)

	// Realtime SSE stream
	if a.hub != nil {
		mux.HandleFunc("/api/events", a.hub.Handler())
	}

	// JSON REST APIs
	mux.HandleFunc("/api/rules", a.handleAPIRules)
	mux.HandleFunc("/api/rules/", a.handleAPIRuleOperations)
	mux.HandleFunc("/api/logs/summary", a.handleAPILogsSummary)
	mux.HandleFunc("/api/logs/clear", a.handleAPILogsClear)
	mux.HandleFunc("/api/logs", a.handleAPILogs)
	mux.HandleFunc("/api/config", a.handleAPIConfig)
	mux.HandleFunc("/api/config/logging", a.handleAPIToggleLogging)
	mux.HandleFunc("/api/update/check", a.handleAPIUpdateCheck)
	mux.HandleFunc("/api/update/apply", a.handleAPIUpdateApply)
	return a.wrapAuth(mux)
}

func (a *App) wrapAuth(next http.Handler) http.Handler {
	authUser := os.Getenv("PATCHBAY_AUTH_USER")
	authPass := os.Getenv("PATCHBAY_AUTH_PASS")
	if authUser == "" && authPass == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(authUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(authPass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Patchbay Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "index.html", nil); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (a *App) handleAPIRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"rules": a.ruleViews(),
		})

	case http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			Protocol   string `json:"protocol"`
			ListenAddr string `json:"listen_addr"`
			ListenPort int    `json:"listen_port"`
			TargetAddr string `json:"target_addr"`
			TargetPort int    `json:"target_port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Fallback to Form
			_ = r.ParseForm()
			req.Name = r.FormValue("name")
			req.Protocol = r.FormValue("protocol")
			req.ListenAddr = r.FormValue("listen_addr")
			req.ListenPort, _ = strconv.Atoi(r.FormValue("listen_port"))
			req.TargetAddr = r.FormValue("target_addr")
			req.TargetPort, _ = strconv.Atoi(r.FormValue("target_port"))
		}

		if req.ListenPort <= 0 || req.ListenPort > 65535 || req.TargetPort <= 0 || req.TargetPort > 65535 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ports"})
			return
		}
		if req.ListenAddr == "" {
			req.ListenAddr = "0.0.0.0"
		}
		if req.Protocol == "" {
			req.Protocol = "tcp"
		}
		if req.Name == "" {
			req.Name = fmt.Sprintf("%s:%d -> %s:%d", req.ListenAddr, req.ListenPort, req.TargetAddr, req.TargetPort)
		}

		rule := Rule{
			ID:         newID(),
			Name:       req.Name,
			Protocol:   req.Protocol,
			ListenAddr: req.ListenAddr,
			ListenPort: req.ListenPort,
			TargetAddr: req.TargetAddr,
			TargetPort: req.TargetPort,
			Enabled:    true,
		}

		if err := a.store.AddRule(rule); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := a.manager.Start(rule); err != nil {
			log.Printf("failed to start rule %s: %v", rule.ID, err)
		}
		writeJSON(w, http.StatusCreated, rule)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleAPIRuleOperations(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/rules/")
	parts := strings.Split(path, "/")
	id := parts[0]
	if id == "" {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 2 && parts[1] == "toggle" && r.Method == http.MethodPost {
		rule, err := a.store.UpdateRule(id, func(r *Rule) {
			r.Enabled = !r.Enabled
		})
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		if rule.Enabled {
			if err := a.manager.Start(rule); err != nil {
				log.Printf("failed to start rule %s: %v", id, err)
			}
		} else {
			a.manager.Stop(id)
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": rule.Enabled, "running": a.manager.IsRunning(id)})
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Name       string `json:"name"`
			Protocol   string `json:"protocol"`
			ListenAddr string `json:"listen_addr"`
			ListenPort int    `json:"listen_port"`
			TargetAddr string `json:"target_addr"`
			TargetPort int    `json:"target_port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request body"})
			return
		}
		rule, err := a.store.UpdateRule(id, func(r *Rule) {
			if req.Name != "" {
				r.Name = req.Name
			}
			if req.Protocol != "" {
				r.Protocol = req.Protocol
			}
			if req.ListenAddr != "" {
				r.ListenAddr = req.ListenAddr
			}
			if req.ListenPort > 0 {
				r.ListenPort = req.ListenPort
			}
			if req.TargetAddr != "" {
				r.TargetAddr = req.TargetAddr
			}
			if req.TargetPort > 0 {
				r.TargetPort = req.TargetPort
			}
		})
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		if rule.Enabled {
			a.manager.Stop(id)
			_ = a.manager.Start(rule)
		}
		writeJSON(w, http.StatusOK, rule)

	case http.MethodDelete:
		a.manager.Stop(id)
		if err := a.store.DeleteRule(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleAPILogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	ruleID := r.URL.Query().Get("rule_id")

	var logs []LogEntry
	if a.logger != nil {
		logs = a.logger.Recent(limit, ruleID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (a *App) handleAPILogsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.logger != nil {
		a.logger.Clear()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (a *App) handleAPILogsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	since := time.Now().Add(-24 * time.Hour)
	if h := r.URL.Query().Get("hours"); h != "" {
		if val, err := strconv.Atoi(h); err == nil && val > 0 {
			since = time.Now().Add(-time.Duration(val) * time.Hour)
		}
	}
	var summary TrafficSummary
	if a.logger != nil {
		summary = a.logger.Summary(since)
	}
	writeJSON(w, http.StatusOK, summary)
}

func (a *App) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := a.store.Snapshot()
	loggingEnabled := cfg.IsLoggingEnabled()
	if a.logger != nil {
		loggingEnabled = a.logger.IsEnabled()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"admin_port":      cfg.AdminPort,
		"logging_enabled": loggingEnabled,
	})
}

func (a *App) handleAPIToggleLogging(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	cfg := a.store.Snapshot()
	newVal := !cfg.IsLoggingEnabled()
	if req.Enabled != nil {
		newVal = *req.Enabled
	}

	if err := a.store.SetLoggingEnabled(newVal); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if a.logger != nil {
		a.logger.SetEnabled(newVal)
	}
	writeJSON(w, http.StatusOK, map[string]any{"logging_enabled": newVal})
}
func (a *App) handleAPIUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	updater := a.updater
	if updater == nil {
		updater = NewUpdater("")
	}
	info, err := updater.Check(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (a *App) handleAPIUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AssetURL string `json:"asset_url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	updater := a.updater
	if updater == nil {
		updater = NewUpdater("")
	}

	assetURL := req.AssetURL
	if assetURL == "" {
		info, err := updater.Check(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "check update: " + err.Error()})
			return
		}
		if !info.UpdateAvail || info.AssetURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no suitable update asset found"})
			return
		}
		assetURL = info.AssetURL
	}

	// Download asset
	tmpFile, err := updater.DownloadAsset(r.Context(), assetURL, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "download update: " + err.Error()})
		return
	}

	if err := LaunchInstaller(tmpFile); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "launch installer: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "installing",
		"message": "Installer launched. Application will exit to apply update.",
	})

	// Allow response to be flushed before exiting
	go func() {
		time.Sleep(1 * time.Second)
		os.Exit(0)
	}()
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
