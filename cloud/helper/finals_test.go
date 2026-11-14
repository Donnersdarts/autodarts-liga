package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func completeTestOverview() LeagueOverview {
	names := []string{"A", "B", "C", "D", "E", "F"}
	ov := LeagueOverview{LeagueName: "Testliga", SeasonID: "s1", SeasonName: "Saison Test"}
	for i, n := range names {
		ov.Players = append(ov.Players, PlayerOverview{Name: n, Wins: 10 - i, Average: 60 - float64(i)})
	}
	day := 1
	count := 0
	for round := 0; round < 2; round++ {
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				ov.Matches = append(ov.Matches, MatchOverview{MatchID: newUUID(), Matchday: day, Player1: names[i], Player2: names[j], Winner: names[i]})
				ov.DayCounts[day-1]++
				count++
				if count%3 == 0 {
					day++
				}
			}
		}
	}
	return ov
}

func TestRegularProgressRequiresExactSixPlayerDoubleRoundRobin(t *testing.T) {
	ov := completeTestOverview()
	p := regularProgress(ov)
	if !p.Complete {
		t.Fatalf("expected complete league, got %#v", p)
	}
	if p.CompletedMatches != 30 || p.MatchdaysComplete != 10 || p.PairingsComplete != 15 {
		t.Fatalf("unexpected progress: %#v", p)
	}
	ov.Matches = ov.Matches[:29]
	ov.DayCounts[9] = 2
	p = regularProgress(ov)
	if p.Complete {
		t.Fatal("29 matches must not unlock finals")
	}
}

func TestFinalDependencyOrderAllowsPostponements(t *testing.T) {
	ov := completeTestOverview()
	table := frozenTableFromOverview(ov)
	st := FinalState{FrozenTable: table, Fixtures: initialFinalFixtures(table)}
	for _, idx := range []int{0, 1, 2} {
		if st.Fixtures[idx].Status != "ready" {
			t.Fatalf("fixture %s must be initially playable: %#v", st.Fixtures[idx].ID, st.Fixtures[idx])
		}
	}
	if st.Fixtures[3].Status != "locked" || st.Fixtures[4].Status != "locked" {
		t.Fatal("third-place game and final must wait for both semifinals")
	}

	// Semi 2 may be played before semi 1; place 5/6 may remain postponed.
	st.Fixtures[2].Status = "completed"
	st.Fixtures[2].Winner = "B"
	st.Fixtures[2].Loser = "C"
	updateFinalAvailability(&st)
	if st.Fixtures[0].Status != "ready" || st.Fixtures[1].Status != "ready" {
		t.Fatal("place 5/6 and semi1 must remain independently playable")
	}
	if st.Fixtures[3].Status != "locked" || st.Fixtures[4].Status != "locked" {
		t.Fatal("one semifinal is not enough to unlock dependent games")
	}

	st.Fixtures[1].Status = "completed"
	st.Fixtures[1].Winner = "A"
	st.Fixtures[1].Loser = "D"
	updateFinalAvailability(&st)
	if st.Fixtures[3].Status != "ready" || st.Fixtures[4].Status != "ready" {
		t.Fatalf("third and final must both unlock after both semis: third=%#v final=%#v", st.Fixtures[3], st.Fixtures[4])
	}
	if st.Fixtures[3].Player1 != "D" || st.Fixtures[3].Player2 != "C" {
		t.Fatalf("third-place pairing wrong: %#v", st.Fixtures[3])
	}
	if st.Fixtures[4].Player1 != "A" || st.Fixtures[4].Player2 != "B" {
		t.Fatalf("final pairing wrong: %#v", st.Fixtures[4])
	}

	// Final may be played before third place and before place 5/6.
	st.Fixtures[4].Status = "completed"
	st.Fixtures[4].Winner = "A"
	st.Fixtures[4].Loser = "B"
	updateFinalAvailability(&st)
	if st.Fixtures[0].Status != "ready" || st.Fixtures[3].Status != "ready" {
		t.Fatal("postponed independent games must stay playable after the final")
	}
}

func TestFinalPlacementsUseActualPlace56Result(t *testing.T) {
	st := FinalState{Status: "completed", Fixtures: make([]FinalFixture, 5)}
	st.Fixtures[0] = FinalFixture{Winner: "F", Loser: "E", Status: "completed"}
	st.Fixtures[3] = FinalFixture{Winner: "C", Loser: "D", Status: "completed"}
	st.Fixtures[4] = FinalFixture{Winner: "A", Loser: "B", Status: "completed"}
	got := finalPlacements(&st)
	want := []string{"A", "B", "C", "D", "F", "E"}
	if len(got) != len(want) {
		t.Fatalf("placements length %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("place %d: got %s want %s", i+1, got[i], want[i])
		}
	}
}

func completeMetrics(avg float64) map[string]interface{} {
	return map[string]interface{}{
		"average": avg, "checkoutHits": 1.0, "checkoutAttempts": 2.0,
		"scores57Plus": 4.0, "scores95Plus": 2.0, "scores114Plus": 1.0,
		"scores133Plus": 1.0, "scores171": 0.0, "scores174": 0.0,
		"scores177": 0.0, "oneEighties": 1.0, "highestCheckout": 120.0,
		"dartsThrown": 45.0,
	}
}

func TestWalkoverCountsResultButDoesNotPolluteDartStats(t *testing.T) {
	real := OverviewMatch{
		MatchID: newUUID(), Matchday: 1, UsageMode: "league", Winner: 0, Score: []int{3, 1},
		Players: []OverviewPlayer{
			{Name: "A", AutodartsName: "A", Wins: 3, Metrics: completeMetrics(60)},
			{Name: "B", AutodartsName: "B", Wins: 1, Metrics: completeMetrics(50)},
		},
	}
	wo := OverviewMatch{
		MatchID: newUUID(), Matchday: 2, UsageMode: "league", Winner: 1, Score: []int{0, 0}, Walkover: true,
		Players: []OverviewPlayer{{Name: "A", AutodartsName: "A"}, {Name: "B", AutodartsName: "B", Wins: 1}},
	}
	ov := makeLeagueOverview("Test", "s1", "Saison", true, []OverviewMatch{real, wo})
	if len(ov.Players) != 2 {
		t.Fatalf("players=%d", len(ov.Players))
	}
	var a, b *PlayerOverview
	for i := range ov.Players {
		if ov.Players[i].Name == "A" {
			a = &ov.Players[i]
		}
		if ov.Players[i].Name == "B" {
			b = &ov.Players[i]
		}
	}
	if a == nil || b == nil {
		t.Fatal("players missing")
	}
	if a.Matches != 2 || a.Wins != 1 || a.Losses != 1 || b.Matches != 2 || b.Wins != 1 || b.Losses != 1 {
		t.Fatalf("walkover W/L incorrect: A=%#v B=%#v", *a, *b)
	}
	if a.Average != 60 || b.Average != 50 || a.AverageCount != 1 || b.AverageCount != 1 {
		t.Fatalf("walkover changed average: A=%v/%d B=%v/%d", a.Average, a.AverageCount, b.Average, b.AverageCount)
	}
	if a.OneEighties != 1 || b.OneEighties != 1 || a.HighFinish != 120 || b.HighFinish != 120 {
		t.Fatalf("walkover changed dart stats: A=%#v B=%#v", *a, *b)
	}
	if !a.ScoringComplete || !b.ScoringComplete {
		t.Fatal("walkover must not make real scoring statistics incomplete")
	}
}

func TestFinalWalkoverIsExcludedFromFinalDartStats(t *testing.T) {
	table := []FrozenStanding{{Rank: 1, Name: "A"}, {Rank: 2, Name: "B"}, {Rank: 3, Name: "C"}, {Rank: 4, Name: "D"}, {Rank: 5, Name: "E"}, {Rank: 6, Name: "F"}}
	st := &FinalState{LeagueName: "Test", SeasonID: "s", SeasonName: "S", FrozenTable: table, Fixtures: initialFinalFixtures(table)}
	actual := OverviewMatch{MatchID: newUUID(), Winner: 0, Score: []int{3, 1}, UsageMode: "final", FinalGameID: "place56", Players: []OverviewPlayer{{Name: "E", RealName: "E", Metrics: completeMetrics(61)}, {Name: "F", RealName: "F", Metrics: completeMetrics(52)}}}
	actualRaw, _ := json.Marshal(actual)
	woRaw := walkoverRaw(SeasonInfo{ID: "s", Name: "S"}, "final", "semi1", 0, "A", "D", "A")
	files := map[string]FinalMatchFile{
		"place56": {Format: "autodarts-match-analytics-final-match", SeasonID: "s", GameID: "place56", Match: actualRaw},
		"semi1":   {Format: "autodarts-match-analytics-final-match", SeasonID: "s", GameID: "semi1", Walkover: true, Match: woRaw},
	}
	srv := &Server{}
	stats, games := srv.finalStatsForState(st, files)
	if games != 1 {
		t.Fatalf("final stat games=%d want 1", games)
	}
	if len(stats) != 2 {
		t.Fatalf("stats players=%d want 2", len(stats))
	}
	for _, p := range stats {
		if p.Name == "A" || p.Name == "D" {
			t.Fatalf("walkover player leaked into dart stats: %#v", p)
		}
	}
}

func TestLockedFinalRendersGraphicalBracket(t *testing.T) {
	st := previewFinalState(completeTestOverview())
	var b strings.Builder
	writeFinalSectionHTML(&b, st)
	h := b.String()
	for _, want := range []string{`class="final-bracket"`, "Platz 1", "Platz 4", "Verlierer HF1", "Verlierer HF2", "Sieger HF1", "Sieger HF2"} {
		if !strings.Contains(h, want) {
			t.Fatalf("locked final HTML missing %q", want)
		}
	}
	if strings.Contains(h, `class="final-order"`) {
		t.Fatal("locked final must render graphical bracket, not the old vertical list")
	}
}

func makeFinalTestRaw(p1, p2 string, winner int) json.RawMessage {
	m := OverviewMatch{
		MatchID: newUUID(), Winner: winner, Score: []int{3, 1}, UsageMode: "final",
		Players: []OverviewPlayer{{Name: p1, AutodartsName: p1}, {Name: p2, AutodartsName: p2}},
	}
	b, _ := json.Marshal(m)
	return b
}

func TestFinalFileFlowAllowsFlexibleOrderAndClosesOnlyAtFiveOfFive(t *testing.T) {
	table := []FrozenStanding{{Rank: 1, Name: "A"}, {Rank: 2, Name: "B"}, {Rank: 3, Name: "C"}, {Rank: 4, Name: "D"}, {Rank: 5, Name: "E"}, {Rank: 6, Name: "F"}}
	srv := &Server{config: Config{BasePath: t.TempDir()}}
	season := SeasonInfo{ID: "s", Name: "S"}
	st := &FinalState{LeagueName: "Test", SeasonID: "s", SeasonName: "S", Status: "ready", Available: true, RegularLocked: true, FrozenTable: table, Fixtures: initialFinalFixtures(table)}
	if err := srv.saveFinalState(st); err != nil {
		t.Fatal(err)
	}

	// Semi 2 first.
	if _, err := srv.writeFinalMatchFile(st, season, "semi2", makeFinalTestRaw("B", "C", 0), false); err != nil {
		t.Fatalf("semi2 first failed: %v", err)
	}
	if fixtureByID(st, "semi1").Status != "ready" || fixtureByID(st, "place56").Status != "ready" {
		t.Fatal("semi1/place56 must remain playable after semi2")
	}
	if fixtureByID(st, "final").Status != "locked" {
		t.Fatal("final must still wait for semi1")
	}

	// Semi 1 next; third and final both unlock.
	if _, err := srv.writeFinalMatchFile(st, season, "semi1", makeFinalTestRaw("A", "D", 0), false); err != nil {
		t.Fatalf("semi1 failed: %v", err)
	}
	if fixtureByID(st, "third").Status != "ready" || fixtureByID(st, "final").Status != "ready" {
		t.Fatal("third and final must both be playable after both semis")
	}

	// Final before third and before place 5/6.
	if _, err := srv.writeFinalMatchFile(st, season, "final", makeFinalTestRaw("A", "B", 0), false); err != nil {
		t.Fatalf("final before third/place56 failed: %v", err)
	}
	if st.Champion != "A" || st.Status != "running" {
		t.Fatalf("final winner should be visible but season still running: status=%s champion=%s", st.Status, st.Champion)
	}
	if fixtureByID(st, "third").Status != "ready" || fixtureByID(st, "place56").Status != "ready" {
		t.Fatal("remaining independent games must stay playable")
	}

	if _, err := srv.writeFinalMatchFile(st, season, "third", makeFinalTestRaw("D", "C", 1), false); err != nil {
		t.Fatalf("third failed: %v", err)
	}
	if st.Status != "running" {
		t.Fatalf("season must stay running until place56 is done, got %s", st.Status)
	}
	if _, err := srv.writeFinalMatchFile(st, season, "place56", makeFinalTestRaw("E", "F", 1), false); err != nil {
		t.Fatalf("place56 last failed: %v", err)
	}
	if st.Status != "completed" || st.Champion != "A" || st.CompletedAt == "" {
		t.Fatalf("season did not close at 5/5: %#v", st)
	}
	got := finalPlacements(st)
	want := []string{"A", "B", "C", "D", "F", "E"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("placement %d got=%v want=%v", i+1, got, want)
		}
	}
}

func TestFinalHTMLShowsPartialChampionAndMultiplePlayableGames(t *testing.T) {
	st := &FinalState{
		Available: true, RegularLocked: true, Status: "running", Champion: "A", SeasonName: "Saison Test",
		Progress: LeagueProgress{CompletedMatches: 30, ExpectedMatches: 30, MatchdaysComplete: 10, PairingsComplete: 15, PairingsExpected: 15},
		Fixtures: []FinalFixture{
			{ID: "place56", Order: 1, Label: "Spiel um Platz 5/6", Player1: "E", Player2: "F", Status: "ready"},
			{ID: "semi1", Order: 2, Label: "Halbfinale 1", Player1: "A", Player2: "D", Status: "completed", Winner: "A", Loser: "D", Score: []int{3, 1}},
			{ID: "semi2", Order: 3, Label: "Halbfinale 2", Player1: "B", Player2: "C", Status: "completed", Winner: "B", Loser: "C", Score: []int{3, 2}},
			{ID: "third", Order: 4, Label: "Spiel um Platz 3", Player1: "D", Player2: "C", Status: "ready"},
			{ID: "final", Order: 5, Label: "Finale", Player1: "A", Player2: "B", Status: "completed", Winner: "A", Loser: "B", Score: []int{3, 1}},
		},
	}
	var b strings.Builder
	writeFinalSectionHTML(&b, st)
	html := b.String()
	for _, want := range []string{"FINALSIEGER", "Saisonabschluss wartet", "3/5 Finalspiele", "Spielbar · Reihenfolge frei wählbar", "Flexible Reihenfolge"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML missing %q", want)
		}
	}
	if strings.Contains(html, "LIGAMEISTER") {
		t.Fatal("running season must not be labelled LIGAMEISTER")
	}
}
