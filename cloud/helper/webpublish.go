package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type WebPublishConfig struct {
	Enabled   bool   `json:"enabled"`
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	Token     string `json:"token"`
	PublicURL string `json:"publicUrl,omitempty"`
}

type WebPublishStatus struct {
	Enabled         bool   `json:"enabled"`
	Configured      bool   `json:"configured"`
	Owner           string `json:"owner,omitempty"`
	Repo            string `json:"repo,omitempty"`
	Branch          string `json:"branch,omitempty"`
	PublicURL       string `json:"publicUrl,omitempty"`
	LastPublishedAt string `json:"lastPublishedAt,omitempty"`
	LastError       string `json:"lastError,omitempty"`
}

type githubContentMeta struct {
	SHA string `json:"sha"`
}

func (s *Server) loadWebPublishConfig() (WebPublishConfig, error) {
	var cfg WebPublishConfig
	if strings.TrimSpace(s.webConfigPath) == "" {
		return cfg, errors.New("Web-Publish-Konfigurationspfad fehlt")
	}
	b, err := os.ReadFile(s.webConfigPath)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	cfg.Owner = strings.TrimSpace(cfg.Owner)
	cfg.Repo = strings.TrimSpace(cfg.Repo)
	cfg.Branch = strings.TrimSpace(cfg.Branch)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.PublicURL = strings.TrimSpace(cfg.PublicURL)
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	return cfg, nil
}

func defaultPagesURL(owner, repo string) string {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" {
		return ""
	}
	if strings.EqualFold(repo, owner+".github.io") {
		return "https://" + owner + ".github.io/"
	}
	return "https://" + owner + ".github.io/" + repo + "/"
}

func (s *Server) refreshWebPublishStatus() {
	cfg, err := s.loadWebPublishConfig()
	st := WebPublishStatus{}
	if err == nil {
		st.Enabled = cfg.Enabled
		st.Configured = cfg.Owner != "" && cfg.Repo != "" && cfg.Token != ""
		st.Owner, st.Repo, st.Branch = cfg.Owner, cfg.Repo, cfg.Branch
		st.PublicURL = cfg.PublicURL
		if st.PublicURL == "" {
			st.PublicURL = defaultPagesURL(cfg.Owner, cfg.Repo)
		}
	}
	s.webStatusMu.Lock()
	if s.webStatus.LastPublishedAt != "" {
		st.LastPublishedAt = s.webStatus.LastPublishedAt
	}
	if err != nil && !os.IsNotExist(err) {
		st.LastError = err.Error()
	} else if s.webStatus.LastError != "" {
		st.LastError = s.webStatus.LastError
	}
	s.webStatus = st
	s.webStatusMu.Unlock()
}

func (s *Server) setWebPublishResult(cfg WebPublishConfig, published bool, err error) {
	s.webStatusMu.Lock()
	defer s.webStatusMu.Unlock()
	s.webStatus.Enabled = cfg.Enabled
	s.webStatus.Configured = cfg.Owner != "" && cfg.Repo != "" && cfg.Token != ""
	s.webStatus.Owner, s.webStatus.Repo, s.webStatus.Branch = cfg.Owner, cfg.Repo, cfg.Branch
	s.webStatus.PublicURL = cfg.PublicURL
	if s.webStatus.PublicURL == "" {
		s.webStatus.PublicURL = defaultPagesURL(cfg.Owner, cfg.Repo)
	}
	if published {
		s.webStatus.LastPublishedAt = time.Now().Format(time.RFC3339)
		s.webStatus.LastError = ""
	} else if err != nil {
		s.webStatus.LastError = err.Error()
	}
}

func (s *Server) currentWebStatus() WebPublishStatus {
	s.webStatusMu.RLock()
	defer s.webStatusMu.RUnlock()
	return s.webStatus
}

func githubAPIURL(cfg WebPublishConfig, path string) string {
	return "https://api.github.com/repos/" + url.PathEscape(cfg.Owner) + "/" + url.PathEscape(cfg.Repo) + "/contents/" + strings.TrimPrefix(path, "/")
}

func githubRequest(cfg WebPublishConfig, method, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "Autodarts-Liga-Sync-Helper/0.9.3")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	return client.Do(req)
}

func githubExistingSHA(cfg WebPublishConfig, path string) (string, error) {
	endpoint := githubAPIURL(cfg, path) + "?ref=" + url.QueryEscape(cfg.Branch)
	resp, err := githubRequest(cfg, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("GitHub GET %s: HTTP %d %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var meta githubContentMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", err
	}
	return meta.SHA, nil
}

func githubPutFile(cfg WebPublishConfig, path string, content []byte, message string) error {
	sha, err := githubExistingSHA(cfg, path)
	if err != nil {
		return err
	}
	payload := map[string]interface{}{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  cfg.Branch,
	}
	if sha != "" {
		payload["sha"] = sha
	}
	body, _ := json.Marshal(payload)
	resp, err := githubRequest(cfg, http.MethodPut, githubAPIURL(cfg, path), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("GitHub PUT %s: HTTP %d %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (s *Server) publishOverviewHTML(htmlPath string) error {
	s.webPublishMu.Lock()
	defer s.webPublishMu.Unlock()

	cfg, err := s.loadWebPublishConfig()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		s.setWebPublishResult(cfg, false, err)
		return err
	}
	if !cfg.Enabled {
		s.setWebPublishResult(cfg, false, nil)
		return nil
	}
	if cfg.Owner == "" || cfg.Repo == "" || cfg.Token == "" {
		err := errors.New("GitHub-Webseite ist aktiviert, aber Owner, Repository oder Token fehlen")
		s.setWebPublishResult(cfg, false, err)
		return err
	}
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	b, err := os.ReadFile(htmlPath)
	if err != nil {
		s.setWebPublishResult(cfg, false, err)
		return err
	}
	if err := githubPutFile(cfg, "index.html", b, "Autodarts Liga: Übersicht aktualisiert"); err != nil {
		s.setWebPublishResult(cfg, false, err)
		return err
	}
	// Archivierte Saisonseiten ebenfalls veröffentlichen, damit der Saisonwähler
	// auf GitHub Pages ohne Add-on funktioniert.
	dir := filepath.Dir(htmlPath)
	if entries, readErr := os.ReadDir(dir); readErr == nil {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasPrefix(name, "season-") || !strings.HasSuffix(strings.ToLower(name), ".html") {
				continue
			}
			content, readErr := os.ReadFile(filepath.Join(dir, name))
			if readErr != nil {
				continue
			}
			if err := githubPutFile(cfg, name, content, "Autodarts Liga: Saisonarchiv aktualisiert"); err != nil {
				s.setWebPublishResult(cfg, false, err)
				return err
			}
		}
	}
	// Disable Jekyll processing for a plain static dashboard.
	if err := githubPutFile(cfg, ".nojekyll", []byte{}, "Autodarts Liga: GitHub Pages konfigurieren"); err != nil {
		s.setWebPublishResult(cfg, false, err)
		return err
	}
	s.setWebPublishResult(cfg, true, nil)
	return nil
}

func (s *Server) handleWebStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.refreshWebPublishStatus()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "web": s.currentWebStatus()})
}

func (s *Server) handleWebPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	leagues := s.knownLeagueNames()
	if len(leagues) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "Noch keine Liga-Daten vorhanden"})
		return
	}
	// The default overview file is regenerated for the first/active league name found.
	s.generateOverviewForLeague(leagues[0])
	status := s.currentWebStatus()
	if status.LastError != "" {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "error": status.LastError, "web": status})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "web": status})
}
