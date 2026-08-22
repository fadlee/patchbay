package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Rule represents a single port forwarding rule.
type Rule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`    // "tcp", "udp", or "tcp+udp"
	ListenAddr string `json:"listen_addr"` // e.g. "0.0.0.0"
	ListenPort int    `json:"listen_port"`
	TargetAddr string `json:"target_addr"`
	TargetPort int    `json:"target_port"`
	Enabled    bool   `json:"enabled"`
}

// Config is the full persisted application state.
type Config struct {
	AdminPort int    `json:"admin_port"`
	Rules     []Rule `json:"rules"`
}

const configFileName = "portforward-config.json"

// ConfigStore handles thread-safe loading/saving of Config to disk.
type ConfigStore struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

// migrateConfig copies a valid source config to dst if dst does not exist.
// If the source is absent the call is a no-op. If the source exists but is
// unreadable or contains invalid JSON the call fails without creating dst,
// so a broken adjacent config never silently overwrites a fresh destination.
func migrateConfig(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil // destination already exists — never overwrite
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat config destination: %w", err)
	}
	srcData, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil // no adjacent source to migrate
	}
	if err != nil {
		return fmt.Errorf("read source config for migration: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(srcData, &cfg); err != nil {
		return fmt.Errorf("invalid source config, refusing to migrate: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return os.WriteFile(dst, srcData, 0644)
}

// NewConfigStore loads config from disk, creating a default one if absent.
func NewConfigStore(path string) (*ConfigStore, error) {
	path, err := prepareConfigPath(path)
	if err != nil {
		return nil, err
	}
	cs := &ConfigStore{path: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cs.cfg = Config{AdminPort: 8787, Rules: []Rule{}}
		if saveErr := cs.saveLocked(); saveErr != nil {
			return nil, saveErr
		}
		return cs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.AdminPort == 0 {
		cfg.AdminPort = 8787
	}
	cs.cfg = cfg
	return cs, nil
}

func (cs *ConfigStore) saveLocked() error {
	data, err := json.MarshalIndent(cs.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := cs.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, cs.path)
}

// Snapshot returns a copy of the current config.
func (cs *ConfigStore) Snapshot() Config {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cp := cs.cfg
	cp.Rules = append([]Rule{}, cs.cfg.Rules...)
	return cp
}

// AddRule appends a new rule and persists it.
func (cs *ConfigStore) AddRule(r Rule) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.cfg.Rules = append(cs.cfg.Rules, r)
	return cs.saveLocked()
}

// UpdateRule replaces a rule by ID and persists it.
func (cs *ConfigStore) UpdateRule(id string, mutate func(*Rule)) (Rule, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i := range cs.cfg.Rules {
		if cs.cfg.Rules[i].ID == id {
			mutate(&cs.cfg.Rules[i])
			if err := cs.saveLocked(); err != nil {
				return Rule{}, err
			}
			return cs.cfg.Rules[i], nil
		}
	}
	return Rule{}, fmt.Errorf("rule not found: %s", id)
}

// DeleteRule removes a rule by ID and persists it.
func (cs *ConfigStore) DeleteRule(id string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := cs.cfg.Rules[:0]
	for _, r := range cs.cfg.Rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	cs.cfg.Rules = out
	return cs.saveLocked()
}
