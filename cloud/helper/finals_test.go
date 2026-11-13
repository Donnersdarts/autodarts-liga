package main

import (
	"encoding/json"
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

func TestFinalStrictOrderAndPairings(t *testing.T) {
	ov := completeTestOverview()
	table := frozenTableFromOverview(ov)
	st := FinalState{FrozenTable: table, Fixtures: initialFinalFixtures(table)}
	if st.Fixtures[0].Status != "ready" || st.Fixtures[1].Status != "locked" {
		t.Fatalf("wrong initial order: %#v", st.Fixtures)
	}
	st.Fixtures[0].Status = "completed"
	st.Fixtures[0].Winner = "E"
	st.Fixtures[0].Loser = "F"
	updateFinalAvailability(&st)
	if st.Fixtures[1].Status != "ready" || st.Fixtures[2].Status != "locked" {
		t.Fatal("semi1 must follow place 5/6")
	}
	st.Fixtures[1].Status = "completed"
	st.Fixtures[1].Winner = "A"
	st.Fixtures[1].Loser = "D"
	updateFinalAvailability(&st)
	if st.Fixtures[2].Status != "ready" {
		t.Fatal("semi2 must follow semi1")
	}
	st.Fixtures[2].Status = "completed"
	st.Fixtures[2].Winner = "B"
	st.Fixtures[2].Loser = "C"
	updateFinalAvailability(&st)
	if st.Fixtures[3].Status != "ready" || st.Fixtures[3].Player1 != "D" || st.Fixtures[3].Player2 != "C" {
		t.Fatalf("third-place game wrong: %#v", st.Fixtures[3])
	}
	if st.Fixtures[4].Player1 != "A" || st.Fixtures[4].Player2 != "B" || st.Fixtures[4].Status != "locked" {
		t.Fatalf("final pairing/order wrong: %#v", st.Fixtures[4])
	}
	st.Fixtures[3].Status = "completed"
	st.Fixtures[3].Winner = "C"
	st.Fixtures[3].Loser = "D"
	updateFinalAvailability(&st)
	if st.Fixtures[4].Status != "ready" {
		t.Fatal("final must unlock only after third-place game")
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
