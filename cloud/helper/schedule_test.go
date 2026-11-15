package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateSeasonScheduleSixPlayers(t *testing.T) {
	players := []string{"A", "B", "C", "D", "E", "F"}
	rows, err := generateSeasonSchedule("saison-test", players)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 30 {
		t.Fatalf("expected 30 rows, got %d", len(rows))
	}
	if err := validateSeasonSchedule(rows, "saison-test", players); err != nil {
		t.Fatal(err)
	}
	days := map[int]int{}
	seen := map[string]int{}
	for _, r := range rows {
		days[r.Day]++
		seen[pairKey(r.Player1, r.Player2)]++
	}
	if len(days) != 10 {
		t.Fatalf("expected 10 days, got %d", len(days))
	}
	for d := 1; d <= 10; d++ {
		if days[d] != 3 {
			t.Fatalf("day %d has %d matches", d, days[d])
		}
	}
	if len(seen) != 15 {
		t.Fatalf("expected 15 pairings, got %d", len(seen))
	}
	for k, n := range seen {
		if n != 2 {
			t.Fatalf("pair %q has %d matches", k, n)
		}
	}
	// Rückrunde must reverse every first-leg fixture exactly five days later.
	byDay := map[int][]ScheduleRow{}
	for _, r := range rows {
		byDay[r.Day] = append(byDay[r.Day], r)
	}
	for d := 1; d <= 5; d++ {
		for _, a := range byDay[d] {
			found := false
			for _, b := range byDay[d+5] {
				if keyName(a.Player1) == keyName(b.Player2) && keyName(a.Player2) == keyName(b.Player1) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("no reversed return leg for day %d %s-%s", d, a.Player1, a.Player2)
			}
		}
	}
}

func TestGenerateSeasonScheduleRejectsWrongPlayerCount(t *testing.T) {
	if _, err := generateSeasonSchedule("s", []string{"A", "B", "C", "D", "E"}); err == nil {
		t.Fatal("expected error for five players")
	}
}

func TestInstallSeasonSchedulePreservesOlderSeason(t *testing.T) {
	dir := t.TempDir()
	s := &Server{config: Config{BasePath: dir}}
	old := []ScheduleRow{{Index: 0, Season: "old", Day: 1, Player1: "Alt A", Player2: "Alt B"}}
	b, err := scheduleCSVBytes(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Spielplan.csv"), b, 0644); err != nil {
		t.Fatal(err)
	}
	season := SeasonInfo{ID: "new-season", Name: "New"}
	summary, rollback, err := s.installSeasonSchedule(season, []string{"A", "B", "C", "D", "E", "F"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Matches != 30 || summary.Matchdays != 10 {
		t.Fatalf("bad summary: %+v", summary)
	}
	rows, err := readScheduleCSV(filepath.Join(dir, "Spielplan.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 31 {
		t.Fatalf("expected 31 rows, got %d", len(rows))
	}
	if rows[0].Season != "old" {
		t.Fatalf("old season lost: %+v", rows[0])
	}
	archive := filepath.Join(dir, "Spielplaene", "Spielplan_new-season.csv")
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	rollback()
	rows, err = readScheduleCSV(filepath.Join(dir, "Spielplan.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Season != "old" {
		t.Fatalf("rollback failed: %+v", rows)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("archive not rolled back")
	}
}

func TestSeasonCreateAutomaticallyBuildsSchedule(t *testing.T) {
	dir := t.TempDir()
	s := &Server{config: Config{BasePath: dir}}
	old := SeasonInfo{ID: "old-season", Name: "Saison alt", CreatedAt: "2026-01-01T00:00:00Z", CompletedAt: "2026-06-01T00:00:00Z", Champion: "A", Closed: true, Active: true}
	reg := SeasonRegistry{Format: "autodarts-match-analytics-seasons", Version: 1, LeagueName: "Testliga", CurrentSeasonID: old.ID, Seasons: []SeasonInfo{old}}
	if err := s.saveSeasonRegistryLocked(reg); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.finalsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	st := FinalState{Format: "autodarts-match-analytics-final", Version: 1, LeagueName: "Testliga", SeasonID: old.ID, SeasonName: old.Name, Status: "completed", Available: true, RegularLocked: true, Champion: "A", Fixtures: []FinalFixture{
		{ID: "place56", Status: "completed", Winner: "E", Loser: "F"},
		{ID: "semi1", Status: "completed", Winner: "A", Loser: "D"},
		{ID: "semi2", Status: "completed", Winner: "B", Loser: "C"},
		{ID: "third", Status: "completed", Winner: "C", Loser: "D"},
		{ID: "final", Status: "completed", Winner: "A", Loser: "B"},
	}}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(s.finalStatePath(old.ID), raw, 0644); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"league":{"name":"Testliga"},"name":"Saison neu"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/seasons/create", body)
	rec := httptest.NewRecorder()
	s.handleSeasonCreate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK              bool            `json:"ok"`
		CurrentSeasonID string          `json:"currentSeasonId"`
		Schedule        ScheduleSummary `json:"schedule"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.CurrentSeasonID == "" {
		t.Fatalf("bad response: %s", rec.Body.String())
	}
	if resp.Schedule.Matches != 30 || resp.Schedule.Matchdays != 10 {
		t.Fatalf("bad schedule summary: %+v", resp.Schedule)
	}
	rows, err := readScheduleCSV(filepath.Join(dir, "Spielplan.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 30 {
		t.Fatalf("expected 30 schedule rows, got %d", len(rows))
	}
	if err := validateSeasonSchedule(rows, resp.CurrentSeasonID, []string{"A", "B", "C", "D", "E", "F"}); err != nil {
		t.Fatal(err)
	}
	// handleSeasonCreate regenerates the overview asynchronously. Give that short-lived
	// writer time to finish before TempDir cleanup removes the test directory.
	time.Sleep(150 * time.Millisecond)
}

func TestParticipantsForNextSeasonRequiresClosedSeason(t *testing.T) {
	dir := t.TempDir()
	s := &Server{config: Config{BasePath: dir}}
	_, err := s.participantsForNextSeason("Testliga", SeasonInfo{ID: "running", Name: "Saison läuft", Closed: false})
	if err == nil {
		t.Fatal("expected unfinished season to block automatic season switch")
	}
}
