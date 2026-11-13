package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const uuidTest = "01a0b094-d47e-762e-8c8e-4e5fd92d4e14"

func matchFixture(a, b any) json.RawMessage {
	p := func(name string, val any) map[string]any {
		m := map[string]any{"average": 43.45}
		if val != nil {
			m["scores95Plus"] = val
		}
		m["scores114Plus"] = float64(0)
		return map[string]any{"name": name, "autodartsName": name, "wins": 2, "metrics": m}
	}
	payload := map[string]any{"matchId": uuidTest, "usageMode": "league", "matchday": 5, "score": []int{3, 1}, "players": []any{p("SVEN", a), p("HAUKE", b)}}
	raw, _ := json.Marshal(payload)
	return raw
}
func metricFrom(t *testing.T, b json.RawMessage, index int, key string) (any, bool) {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	ps := v["players"].([]any)
	m := ps[index].(map[string]any)["metrics"].(map[string]any)
	val, ok := m[key]
	return val, ok
}
func TestMergeZeroIsValidAndMissingDoesNotOverwrite(t *testing.T) {
	old := matchFixture(float64(0), float64(5))
	incoming := matchFixture(nil, nil)
	res, err := mergeProtectedMatch(old, incoming)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{0, 5} {
		got, ok := metricFrom(t, res, i, "scores95Plus")
		if !ok || got != want {
			t.Fatalf("player %d got %v, %v", i, got, ok)
		}
	}
}
func TestMergeEnrichMissing(t *testing.T) {
	old := matchFixture(nil, nil)
	incoming := matchFixture(float64(0), float64(3))
	res, err := mergeProtectedMatch(old, incoming)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{0, 3} {
		got, ok := metricFrom(t, res, i, "scores95Plus")
		if !ok || got != want {
			t.Fatalf("player %d got %v, %v", i, got, ok)
		}
	}
}
func TestRejectContradictoryScoring(t *testing.T) {
	_, err := mergeProtectedMatch(matchFixture(float64(0), float64(5)), matchFixture(float64(2), float64(5)))
	if err == nil || !strings.Contains(err.Error(), "Scoring-Konflikt") {
		t.Fatal(err)
	}
}
func TestRejectSwappedPlayers(t *testing.T) {
	old := matchFixture(float64(0), float64(5))
	var v map[string]any
	_ = json.Unmarshal(matchFixture(nil, nil), &v)
	ps := v["players"].([]any)
	ps[0], ps[1] = ps[1], ps[0]
	raw, _ := json.Marshal(v)
	_, err := mergeProtectedMatch(old, raw)
	if err == nil {
		t.Fatal("swapped players accepted")
	}
}
func TestRejectChangedScore(t *testing.T) {
	var v map[string]any
	_ = json.Unmarshal(matchFixture(nil, nil), &v)
	v["score"] = []int{2, 2}
	raw, _ := json.Marshal(v)
	_, err := mergeProtectedMatch(matchFixture(float64(0), float64(1)), raw)
	if err == nil {
		t.Fatal("changed score accepted")
	}
}
func TestInvalidScoringCount(t *testing.T) {
	for _, invalid := range []any{-1, 1.5, "0", true} {
		if err := validateNewMatch(matchFixture(invalid, nil)); err == nil {
			t.Fatalf("invalid value %v accepted", invalid)
		}
	}
	if err := validateNewMatch(matchFixture(float64(0), nil)); err != nil {
		t.Fatal(err)
	}
}
func TestRealWriteProtectsExistingJSONAndBackups(t *testing.T) {
	dir := t.TempDir()
	srv := &Server{config: Config{BasePath: dir}}
	mf := func(m json.RawMessage) MatchFile {
		return MatchFile{Format: "autodarts-match-analytics-league-match", Version: 1, League: LeagueInfo{Name: "Eigene Liga"}, Match: m}
	}
	action, err := srv.writeMatchFile(mf(matchFixture(float64(0), float64(7))))
	if err != nil || action != "created" {
		t.Fatalf("create=%s err=%v", action, err)
	}
	path := filepath.Join(dir, "Matches", uuidTest+".json")
	original, _ := os.ReadFile(path)
	// Old local history has no statistics. Guard must avoid write entirely.
	action, err = srv.writeMatchFile(mf(matchFixture(nil, nil)))
	if err != nil || action != "unchanged" {
		t.Fatalf("protect=%s err=%v", action, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("existing cloud file was overwritten by stale local history")
	}
	// Conflict is rejected without modifying the file.
	action, err = srv.writeMatchFile(mf(matchFixture(float64(9), float64(7))))
	if err == nil {
		t.Fatalf("conflict accepted: %s", action)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("conflict modified the file")
	}
	// Enrichment should persist an immutable backup first.
	var enhanced map[string]any
	_ = json.Unmarshal(matchFixture(float64(0), float64(7)), &enhanced)
	players := enhanced["players"].([]any)
	players[0].(map[string]any)["metrics"].(map[string]any)["scores171"] = float64(1)
	richer, _ := json.Marshal(enhanced)
	action, err = srv.writeMatchFile(mf(richer))
	if err != nil || action != "updated" {
		t.Fatalf("enrich=%s err=%v", action, err)
	}
	backups, err := os.ReadDir(filepath.Join(dir, "Backup-Matches-v0.9.4"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup count %d, err=%v", len(backups), err)
	}
	savedBackup, _ := os.ReadFile(filepath.Join(dir, "Backup-Matches-v0.9.4", backups[0].Name()))
	if !bytes.Equal(original, savedBackup) {
		t.Fatal("backup not byte-identical")
	}
	now, _ := os.ReadFile(path)
	var result MatchFile
	_ = json.Unmarshal(now, &result)
	got, _ := metricFrom(t, result.Match, 0, "scores171")
	if got != float64(1) {
		t.Fatalf("enrichment missing: %v", got)
	}
}

func TestFrozenSeasonAllowsIdenticalSyncButRejectsRegularChanges(t *testing.T) {
	dir := t.TempDir()
	srv := &Server{config: Config{BasePath: dir}}
	mf := func(m json.RawMessage) MatchFile {
		return MatchFile{Format: "autodarts-match-analytics-league-match", Version: 1, League: LeagueInfo{Name: "Eigene Liga"}, Match: m}
	}
	originalRaw := matchFixture(float64(0), float64(7))
	action, err := srv.writeMatchFile(mf(originalRaw))
	if err != nil || action != "created" {
		t.Fatalf("create=%s err=%v", action, err)
	}
	path := filepath.Join(dir, "Matches", uuidTest+".json")
	var stored MatchFile
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.League.SeasonID == "" {
		t.Fatal("season id missing")
	}
	if err := srv.saveFinalState(&FinalState{Format: "autodarts-match-analytics-final", Version: 1, LeagueName: "Eigene Liga", SeasonID: stored.League.SeasonID, SeasonName: stored.League.SeasonName, Available: true, RegularLocked: true, Status: "ready"}); err != nil {
		t.Fatal(err)
	}

	// A stale/second PC may re-send the exact same regular match after the freeze.
	action, err = srv.writeMatchFile(mf(originalRaw))
	if err != nil || action != "unchanged" {
		t.Fatalf("identical sync after freeze=%s err=%v", action, err)
	}

	// Enrichment or any other actual modification must be rejected after freeze.
	var richer map[string]any
	_ = json.Unmarshal(originalRaw, &richer)
	players := richer["players"].([]any)
	players[0].(map[string]any)["metrics"].(map[string]any)["scores171"] = float64(1)
	richerRaw, _ := json.Marshal(richer)
	if _, err = srv.writeMatchFile(mf(richerRaw)); err == nil || !strings.Contains(err.Error(), "eingefroren") {
		t.Fatalf("regular enrichment after freeze accepted: %v", err)
	}

	// A completely new regular match is blocked as well.
	var newer map[string]any
	_ = json.Unmarshal(originalRaw, &newer)
	newer["matchId"] = newUUID()
	newRaw, _ := json.Marshal(newer)
	if _, err = srv.writeMatchFile(mf(newRaw)); err == nil || !strings.Contains(err.Error(), "eingefroren") {
		t.Fatalf("new regular match after freeze accepted: %v", err)
	}
}
