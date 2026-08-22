package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
)

//go:embed web/templates/*.html
var templateFS embed.FS

//go:embed web/static/*
var staticFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"statusLabel": func(running bool) string {
		if running {
			return "Running"
		}
		return "Stopped"
	},
}).ParseFS(templateFS, "web/templates/*.html"))

// App wires the config store and proxy manager into HTTP handlers.
type App struct {
	store   *ConfigStore
	manager *Manager
}

func NewApp(store *ConfigStore, manager *Manager) *App {
	return &App{store: store, manager: manager}
}

// ruleView is what templates render — a Rule plus its live status.
type ruleView struct {
	Rule
	Running bool
	Stats   Stats
}

func (a *App) ruleViews() []ruleView {
	cfg := a.store.Snapshot()
	views := make([]ruleView, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		views = append(views, ruleView{
			Rule:    r,
			Running: a.manager.IsRunning(r.ID),
			Stats:   a.manager.StatsFor(r.ID),
		})
	}
	return views
}

// pageData is what templates receive: rules plus a precomputed running count
// (Go's html/template has no arithmetic helpers, so we count here).
type pageData struct {
	Rules        []ruleView
	RunningCount int
}

func (a *App) pageData() pageData {
	views := a.ruleViews()
	running := 0
	for _, v := range views {
		if v.Running {
			running++
		}
	}
	return pageData{Rules: views, RunningCount: running}
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))

	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/partials/rules", a.handleRulesPartial)
	mux.HandleFunc("/rules", a.handleCreateRule) // POST
	mux.HandleFunc("/rules/toggle", a.handleToggleRule)
	mux.HandleFunc("/rules/delete", a.handleDeleteRule)

	return mux
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := a.pageData()
	if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (a *App) handleRulesPartial(w http.ResponseWriter, r *http.Request) {
	data := a.pageData()
	if err := tmpl.ExecuteTemplate(w, "rules_table.html", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (a *App) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	listenPort, err := strconv.Atoi(r.FormValue("listen_port"))
	if err != nil {
		http.Error(w, "invalid listen_port", http.StatusBadRequest)
		return
	}
	targetPort, err := strconv.Atoi(r.FormValue("target_port"))
	if err != nil {
		http.Error(w, "invalid target_port", http.StatusBadRequest)
		return
	}

	rule := Rule{
		ID:         newID(),
		Name:       r.FormValue("name"),
		Protocol:   r.FormValue("protocol"),
		ListenAddr: r.FormValue("listen_addr"),
		ListenPort: listenPort,
		TargetAddr: r.FormValue("target_addr"),
		TargetPort: targetPort,
		Enabled:    true,
	}
	if rule.ListenAddr == "" {
		rule.ListenAddr = "0.0.0.0"
	}
	if rule.Protocol == "" {
		rule.Protocol = "tcp"
	}
	if rule.Name == "" {
		rule.Name = fmt.Sprintf("%s:%d -> %s:%d", rule.ListenAddr, rule.ListenPort, rule.TargetAddr, rule.TargetPort)
	}

	if err := a.store.AddRule(rule); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.manager.Start(rule); err != nil {
		log.Printf("failed to start rule %s: %v", rule.ID, err)
	}

	a.handleRulesPartial(w, r)
}

func (a *App) handleToggleRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.FormValue("id")

	var updated Rule
	updated, err := a.store.UpdateRule(id, func(rule *Rule) {
		rule.Enabled = !rule.Enabled
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if updated.Enabled {
		if err := a.manager.Start(updated); err != nil {
			log.Printf("failed to start rule %s: %v", updated.ID, err)
		}
	} else {
		a.manager.Stop(updated.ID)
	}

	a.handleRulesPartial(w, r)
}

func (a *App) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.FormValue("id")
	a.manager.Stop(id)
	if err := a.store.DeleteRule(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.handleRulesPartial(w, r)
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
