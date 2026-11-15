package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoricalSeasonAddsPodiumAndSeasonWithoutFakeStats(t *testing.T) {
	hs := []HistoricalSeason{{ID: "historisch-saison-1", Name: "Saison 1 (historisch)", Placements: []string{"Maxi", "Flo", "Marco", "Juli", "Sven", "Hauke"}}}
	reg := SeasonRegistry{}
	h := buildLeagueHistory(reg, map[string]LeagueOverview{}, map[string]*FinalState{}, hs)
	maxi, ok := careerPlayer(h, "Maxi")
	if !ok || maxi.Titles != 1 || maxi.Seasons != 1 || maxi.Matches != 0 || maxi.Elo != 1500 {
		t.Fatalf("Maxi historical career wrong: %+v ok=%v", maxi, ok)
	}
	flo, _ := careerPlayer(h, "Flo")
	if flo.RunnerUp != 1 {
		t.Fatalf("Flo runner-up=%d", flo.RunnerUp)
	}
	marco, _ := careerPlayer(h, "Marco")
	if marco.Third != 1 {
		t.Fatalf("Marco third=%d", marco.Third)
	}
	hauke, _ := careerPlayer(h, "Hauke")
	if len(hauke.Placements) != 1 || hauke.Placements[0].Place != 6 {
		t.Fatalf("Hauke placements=%+v", hauke.Placements)
	}
	if len(h.H2HMatches) != 0 || len(h.AwardHistory) != 0 {
		t.Fatalf("historical data invented detailed stats")
	}
}

func TestLoadHistoricalSeasonFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Historie"), 0755); err != nil {
		t.Fatal(err)
	}
	f := HistoricalSeasonFile{Format: "autodarts-liga-historical-seasons", Version: 1, Seasons: []HistoricalSeason{{ID: "h1", Name: "Saison 1 (historisch)", Placements: []string{"Maxi", "Flo", "Marco", "Juli", "Sven", "Hauke"}}}}
	b, _ := json.Marshal(f)
	if err := os.WriteFile(filepath.Join(dir, "Historie", "historische-saisons.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
	got := loadHistoricalSeasons(dir)
	if len(got) != 1 || got[0].Placements[0] != "Maxi" {
		t.Fatalf("got=%+v", got)
	}
	var out strings.Builder
	writeHistoricalSeasonsHTML(&out, got)
	if !strings.Contains(out.String(), "Maxi") || !strings.Contains(out.String(), "Historische Saisons") {
		t.Fatalf("html=%s", out.String())
	}
}
