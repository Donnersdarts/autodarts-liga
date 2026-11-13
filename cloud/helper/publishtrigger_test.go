package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTriggerDisabledByDefault(t *testing.T) {
	s := &Server{configPath: filepath.Join(t.TempDir(), "config.json")}
	c, err := s.loadPublishTriggerConfig()
	if err != nil || c.Endpoint != "" {
		t.Fatalf("%+v %v", c, err)
	}
	s.queuePublishTrigger()
	if s.triggerPending {
		t.Fatal("disabled trigger queued")
	}
}
func TestTriggerKeyAndHTTP(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("X-Liga-Trigger-Key") != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Error("bad request headers")
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "match_uploaded") {
			t.Error("bad body")
		}
		w.WriteHeader(202)
	}))
	defer server.Close()
	cfg := publishTriggerConfig{Endpoint: server.URL, TriggerKey: strings.Repeat("a", 32)}
	if err := sendPublishTrigger(cfg, server.Client()); err != nil {
		t.Fatal(err)
	}
}
func TestTriggerDoesNotAcceptInsecureURL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "publish-trigger.json")
	if err := os.WriteFile(p, []byte(`{"endpoint":"http://example.com","triggerKey":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{configPath: filepath.Join(dir, "config.json")}
	if _, err := s.loadPublishTriggerConfig(); err == nil {
		t.Fatal("accepted insecure URL")
	}
}
func TestTriggerRateLimitedGatewayIsNonfatal(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) }))
	defer srv.Close()
	if err := sendPublishTrigger(publishTriggerConfig{Endpoint: srv.URL, TriggerKey: strings.Repeat("x", 40)}, srv.Client()); err != nil {
		t.Fatal(err)
	}
}
