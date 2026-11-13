package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// GitHub credentials NEVER go to the addon or this configuration.
// The endpoint is an optional external trigger gateway, not a direct GitHub API URL.
type publishTriggerConfig struct {
	Endpoint     string `json:"endpoint"`
	TriggerKey   string `json:"triggerKey"`
	DelaySeconds int    `json:"delaySeconds,omitempty"`
}

func (s *Server) triggerConfigPath() string {
	return filepath.Join(filepath.Dir(s.configPath), "publish-trigger.json")
}

func (s *Server) loadPublishTriggerConfig() (publishTriggerConfig, error) {
	var cfg publishTriggerConfig
	path := s.triggerConfigPath()
	stat, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if runtime.GOOS != "windows" && stat.Mode().Perm()&0077 != 0 {
		return cfg, fmt.Errorf("%s: Zugriffsrechte zu offen; chmod 600 verwenden", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Endpoint == "" && cfg.TriggerKey == "" {
		return cfg, nil
	}
	target, err := url.Parse(cfg.Endpoint)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil || target.Fragment != "" {
		return cfg, errors.New("Trigger-URL muss eine gueltige HTTPS-Adresse ohne Zugangsdaten sein")
	}
	if len(cfg.TriggerKey) < 32 || strings.ContainsAny(cfg.TriggerKey, "\r\n") {
		return cfg, errors.New("Trigger-Key fehlt oder ist zu kurz (mindestens 32 Zeichen)")
	}
	if cfg.DelaySeconds == 0 {
		cfg.DelaySeconds = 45
	}
	if cfg.DelaySeconds < 5 || cfg.DelaySeconds > 300 {
		return cfg, errors.New("delaySeconds muss zwischen 5 und 300 liegen")
	}
	return cfg, nil
}

// Coalesce multiple successful writes into one eventual publish request.
// A delay allows rclone's write-back cache to send OneDrive changes first.
func (s *Server) queuePublishTrigger() {
	cfg, err := s.loadPublishTriggerConfig()
	if err != nil {
		log.Printf("Cloud-Trigger-Konfiguration: %v", err)
		return
	}
	if cfg.Endpoint == "" {
		return
	}
	s.triggerMu.Lock()
	if s.triggerPending {
		s.triggerMu.Unlock()
		return
	}
	s.triggerPending = true
	s.triggerMu.Unlock()
	time.AfterFunc(time.Duration(cfg.DelaySeconds)*time.Second, func() {
		defer func() { s.triggerMu.Lock(); s.triggerPending = false; s.triggerMu.Unlock() }()
		if err := sendPublishTrigger(cfg, nil); err != nil {
			log.Printf("Cloud-Trigger fehlgeschlagen, 5-Minuten-Backup bleibt aktiv: %v", err)
		}
	})
}

func sendPublishTrigger(cfg publishTriggerConfig, client *http.Client) error {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequest(http.MethodPost, cfg.Endpoint, bytes.NewBufferString(`{"event":"match_uploaded"}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Liga-Trigger-Key", cfg.TriggerKey)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil
	} // another player just triggered this publish
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("Gateway HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
