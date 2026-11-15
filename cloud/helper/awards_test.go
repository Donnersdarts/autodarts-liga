package main

import (
	"os"
	"strings"
	"testing"
)

func awardByID(t *testing.T, awards []SeasonAward, id string) SeasonAward {
	t.Helper()
	for _, a := range awards {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("award %s not found: %#v", id, awards)
	return SeasonAward{}
}

func TestSeasonAwardsRegularSeasonOnly(t *testing.T) {
	ov := LeagueOverview{
		LeagueName: "Eigene Liga",
		SeasonID:   "saison-test",
		SeasonName: "Saison Test",
		Players: []PlayerOverview{
			{Name: "A", Wins: 8, LegDiff: 12, Average: 61.2, CheckoutPct: 35, CheckoutHits: 20, CheckoutAttempts: 60, DoubleMisses: 40, CheckoutDataComplete: true, Scores95Plus: 20, Scores114Plus: 9, Scores133Plus: 5, Scores171: 2, Scores174: 1, Scores177: 0, OneEighties: 2, HighFinish: 121, ScoringComplete: true},
			{Name: "B", Wins: 7, LegDiff: 9, Average: 64.1, CheckoutPct: 28, CheckoutHits: 19, CheckoutAttempts: 75, DoubleMisses: 56, CheckoutDataComplete: true, Scores95Plus: 24, Scores114Plus: 10, Scores133Plus: 7, Scores171: 2, Scores174: 2, Scores177: 1, OneEighties: 4, HighFinish: 118, ScoringComplete: true},
			{Name: "C", Wins: 6, LegDiff: 3, Average: 58.0, CheckoutPct: 31, CheckoutHits: 17, CheckoutAttempts: 52, DoubleMisses: 35, CheckoutDataComplete: true, Scores95Plus: 18, Scores114Plus: 8, Scores133Plus: 4, Scores171: 1, Scores174: 0, Scores177: 0, OneEighties: 1, HighFinish: 130, ScoringComplete: true},
			{Name: "D", Wins: 5, LegDiff: 0, Average: 56.0, CheckoutPct: 30, CheckoutHits: 16, CheckoutAttempts: 50, DoubleMisses: 34, CheckoutDataComplete: true, Scores95Plus: 15, Scores114Plus: 7, Scores133Plus: 3, Scores171: 0, Scores174: 0, Scores177: 0, OneEighties: 0, HighFinish: 100, ScoringComplete: true},
			{Name: "E", Wins: 3, LegDiff: -8, Average: 52.0, CheckoutPct: 25, CheckoutHits: 12, CheckoutAttempts: 48, DoubleMisses: 36, CheckoutDataComplete: true, Scores95Plus: 12, Scores114Plus: 5, Scores133Plus: 2, Scores171: 0, Scores174: 0, Scores177: 0, OneEighties: 0, HighFinish: 86, ScoringComplete: true},
			{Name: "F", Wins: 1, LegDiff: -16, Average: 49.0, CheckoutPct: 22, CheckoutHits: 9, CheckoutAttempts: 42, DoubleMisses: 33, CheckoutDataComplete: true, Scores95Plus: 9, Scores114Plus: 3, Scores133Plus: 1, Scores171: 0, Scores174: 0, Scores177: 0, OneEighties: 0, HighFinish: 72, ScoringComplete: true},
		},
		Matches: []MatchOverview{
			{Player1: "A", Player2: "B", Score1: 4, Score2: 3, Winner: "A"},
			{Player1: "B", Player2: "C", Score1: 3, Score2: 4, Winner: "C"},
			{Player1: "B", Player2: "D", Score1: 3, Score2: 4, Winner: "D"},
			{Player1: "E", Player2: "B", Score1: 4, Score2: 3, Winner: "E"},
			{Player1: "B", Player2: "F", Score1: 4, Score2: 3, Winner: "B"},
			{Player1: "A", Player2: "F", Score1: 0, Score2: 0, Winner: "A", Walkover: true},
		},
	}

	awards := seasonAwardsForOverview(ov)
	if len(awards) != 5 {
		t.Fatalf("awards=%d want 5: %#v", len(awards), awards)
	}
	if a := awardByID(t, awards, "player-of-season"); a.Winner != "A" {
		t.Fatalf("player of season=%s want A", a.Winner)
	}
	if a := awardByID(t, awards, "scoring-machine"); a.Winner != "B" {
		t.Fatalf("scoring machine=%s want B", a.Winner)
	}
	if a := awardByID(t, awards, "checkout-king"); a.Winner != "C" {
		t.Fatalf("checkout king=%s want C", a.Winner)
	}
	if a := awardByID(t, awards, "double-trouble"); a.Winner != "B" || !a.Complete {
		t.Fatalf("double trouble=%#v want B complete", a)
	}
	if a := awardByID(t, awards, "nerves"); a.Winner != "B" {
		t.Fatalf("nerves=%#v want B", a)
	}
}

func TestDoubleTroubleMarksIncompleteLegacyData(t *testing.T) {
	ov := LeagueOverview{Players: []PlayerOverview{
		{Name: "A", CheckoutAttempts: 50, CheckoutHits: 10, DoubleMisses: 40, CheckoutPct: 20, CheckoutDataComplete: false, ScoringComplete: true},
		{Name: "B", CheckoutAttempts: 40, CheckoutHits: 10, DoubleMisses: 30, CheckoutPct: 25, CheckoutDataComplete: true, ScoringComplete: true},
	}}
	awards := seasonAwardsForOverview(ov)
	a := awardByID(t, awards, "double-trouble")
	if a.Complete {
		t.Fatalf("legacy/incomplete checkout data must be flagged: %#v", a)
	}
}

func TestNervenflatterOnlyCountsThreeFourLosses(t *testing.T) {
	ov := LeagueOverview{
		Players: []PlayerOverview{{Name: "A", ScoringComplete: true, CheckoutDataComplete: true}, {Name: "B", ScoringComplete: true, CheckoutDataComplete: true}},
		Matches: []MatchOverview{
			{Player1: "A", Player2: "B", Score1: 3, Score2: 4, Winner: "B"},
			{Player1: "A", Player2: "B", Score1: 2, Score2: 4, Winner: "B"},
			{Player1: "A", Player2: "B", Score1: 0, Score2: 0, Winner: "B", Walkover: true},
		},
	}
	a := awardByID(t, seasonAwardsForOverview(ov), "nerves")
	if a.Winner != "A" || a.Value == "" {
		t.Fatalf("nerves=%#v want A with one 3:4 loss", a)
	}
}

func TestCompletedSeasonPersistsFiveAwardsAndHTML(t *testing.T) {
	ov := LeagueOverview{
		SeasonName: "Saison 2026/2027",
		Players: []PlayerOverview{
			{Name: "A", Wins: 10, LegDiff: 20, Average: 60, CheckoutPct: 30, CheckoutHits: 20, CheckoutAttempts: 60, DoubleMisses: 40, CheckoutDataComplete: true, ScoringComplete: true, HighFinish: 120},
			{Name: "B", Wins: 8, LegDiff: 10, Average: 58, CheckoutPct: 25, CheckoutHits: 15, CheckoutAttempts: 70, DoubleMisses: 55, CheckoutDataComplete: true, ScoringComplete: true, HighFinish: 100},
		},
		Matches: []MatchOverview{{Player1: "A", Player2: "B", Score1: 4, Score2: 3, Winner: "A"}},
	}
	st := &FinalState{Status: "completed", SeasonName: ov.SeasonName}
	refreshSeasonAwards(st, ov)
	if len(st.SeasonAwards) != 5 || st.AwardsComputedAt == "" {
		t.Fatalf("awards not persisted: %#v", st)
	}
	var b strings.Builder
	writeSeasonAwardsHTML(&b, st)
	html := b.String()
	for _, needle := range []string{"Saison-Awards", "Double Trouble Award", "Nervenflatter Award"} {
		if !strings.Contains(html, needle) {
			t.Fatalf("award HTML missing %q: %s", needle, html)
		}
	}
}

func TestAwardsAreLiveBeforeSeasonCompletion(t *testing.T) {
	ov := LeagueOverview{Players: []PlayerOverview{{Name: "A", Wins: 1, LegDiff: 2, Average: 60, ScoringComplete: true, CheckoutDataComplete: true}}}
	st := &FinalState{Status: "locked", SeasonName: "Saison", Progress: LeagueProgress{CompletedMatches: 1, ExpectedMatches: 30}}
	refreshSeasonAwards(st, ov)
	if len(st.SeasonAwards) != 5 || st.AwardsComputedAt == "" {
		t.Fatalf("live awards missing before season completion: %#v", st)
	}
	var b strings.Builder
	writeSeasonAwardsHTML(&b, st)
	if !strings.Contains(b.String(), "LIVE · aktueller Stand") || !strings.Contains(b.String(), "1/30") {
		t.Fatalf("live award HTML missing live/progress label: %s", b.String())
	}
}

func TestAwardsBecomeRegularSeasonEndState(t *testing.T) {
	ov := LeagueOverview{Players: []PlayerOverview{{Name: "A", Wins: 10, LegDiff: 20, Average: 60, ScoringComplete: true, CheckoutDataComplete: true}}}
	st := &FinalState{Status: "ready", SeasonName: "Saison", Progress: LeagueProgress{Complete: true, CompletedMatches: 30, ExpectedMatches: 30}}
	refreshSeasonAwards(st, ov)
	var b strings.Builder
	writeSeasonAwardsHTML(&b, st)
	if !strings.Contains(b.String(), "ENDSTAND · reguläre Saison") {
		t.Fatalf("regular-season end-state label missing: %s", b.String())
	}
}

func TestPreviewFinalStateContainsLiveAwards(t *testing.T) {
	ov := LeagueOverview{
		LeagueName: "Eigene Liga", SeasonID: "saison-live", SeasonName: "Saison Live",
		Players: []PlayerOverview{
			{Name: "A", Wins: 2, LegDiff: 4, Average: 61, CheckoutHits: 5, CheckoutAttempts: 15, DoubleMisses: 10, CheckoutDataComplete: true, ScoringComplete: true, HighFinish: 100},
			{Name: "B", Wins: 1, LegDiff: -1, Average: 57, CheckoutHits: 4, CheckoutAttempts: 18, DoubleMisses: 14, CheckoutDataComplete: true, ScoringComplete: true, HighFinish: 80},
		},
		Matches: []MatchOverview{
			{Player1: "A", Player2: "B", Score1: 4, Score2: 2, Winner: "A", Matchday: 1},
		},
	}
	st := previewFinalState(ov)
	if st.Available || st.RegularLocked {
		t.Fatalf("preview must stay locked before regular season completion: %#v", st)
	}
	if len(st.SeasonAwards) != 5 {
		t.Fatalf("preview live awards=%d want 5: %#v", len(st.SeasonAwards), st.SeasonAwards)
	}
	if st.AwardsComputedAt == "" {
		t.Fatal("preview live awards need computed timestamp")
	}
}

func TestHomepageShowsLiveAwardsBeforeFinals(t *testing.T) {
	ov := LeagueOverview{
		LeagueName: "Eigene Liga", SeasonID: "saison-live", SeasonName: "Saison Live",
		Players: []PlayerOverview{
			{Name: "A", Wins: 2, LegDiff: 4, Average: 61, CheckoutHits: 5, CheckoutAttempts: 15, DoubleMisses: 10, CheckoutDataComplete: true, ScoringComplete: true, HighFinish: 100},
			{Name: "B", Wins: 1, LegDiff: -1, Average: 57, CheckoutHits: 4, CheckoutAttempts: 18, DoubleMisses: 14, CheckoutDataComplete: true, ScoringComplete: true, HighFinish: 80},
		},
		Matches: []MatchOverview{{Player1: "A", Player2: "B", Score1: 4, Score2: 2, Winner: "A", Matchday: 1}},
	}
	st := previewFinalState(ov)
	path := t.TempDir() + "/index.html"
	if err := writeHTMLOverview(path, ov, nil, ov.SeasonID, st); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, needle := range []string{"Saison-Awards", "LIVE · aktueller Stand", "Stand nach", "Finalspieltag"} {
		if !strings.Contains(html, needle) {
			t.Fatalf("homepage missing %q: %s", needle, html)
		}
	}
}
