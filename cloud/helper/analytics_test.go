package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fp(v float64) *float64 { return &v }

func TestLeagueHistoryBuildsCareerHeadToHeadAndRecords(t *testing.T) {
	ov1 := LeagueOverview{LeagueName: "Liga", SeasonID: "s1", SeasonName: "Saison 1", Players: []PlayerOverview{
		{Name: "A", Matches: 2, Wins: 2, LegsFor: 8, LegsAgainst: 4, LegDiff: 4, Average: 60, AverageCount: 2, CheckoutHits: 8, CheckoutAttempts: 20, Scores171: 3, OneEighties: 2, HighFinish: 120},
		{Name: "B", Matches: 2, Losses: 2, LegsFor: 4, LegsAgainst: 8, LegDiff: -4, Average: 55, AverageCount: 2, CheckoutHits: 4, CheckoutAttempts: 15, OneEighties: 1, HighFinish: 100},
	}, Matches: []MatchOverview{
		{MatchID: "m1", Matchday: 1, Player1: "A", Player2: "B", Score1: 4, Score2: 3, Winner: "A", Avg1: fp(61), Avg2: fp(56), OneEighties1: 2, HF1: 120},
		{MatchID: "m2", Matchday: 6, Player1: "B", Player2: "A", Score1: 1, Score2: 4, Winner: "A", Avg1: fp(54), Avg2: fp(59), OneEighties2: 1, HF2: 80},
	}}
	st := &FinalState{SeasonID: "s1", SeasonName: "Saison 1", Status: "completed", Progress: LeagueProgress{Complete: true}, Fixtures: []FinalFixture{
		{ID: "place56", Winner: "E", Loser: "F", Status: "completed"}, {ID: "semi1", Winner: "A", Loser: "D", Status: "completed"}, {ID: "semi2", Winner: "B", Loser: "C", Status: "completed"}, {ID: "third", Winner: "C", Loser: "D", Status: "completed"}, {ID: "final", Winner: "A", Loser: "B", Status: "completed"},
	}, SeasonAwards: []SeasonAward{{ID: "player-of-season", Title: "Spieler der Saison", Emoji: "🏆", Winner: "A", Value: "Test", Complete: true}}}
	reg := SeasonRegistry{CurrentSeasonID: "s1", Seasons: []SeasonInfo{{ID: "s1", Name: "Saison 1", Closed: true}}}
	h := buildLeagueHistory(reg, map[string]LeagueOverview{"s1": ov1}, map[string]*FinalState{"s1": st}, nil)
	a, ok := careerPlayer(h, "A")
	if !ok {
		t.Fatal("A missing")
	}
	if a.Titles != 1 || a.Wins != 2 || a.Scores171 != 3 || a.OneEighties != 2 {
		t.Fatalf("bad career: %+v", a)
	}
	if len(h.H2HMatches) != 2 {
		t.Fatalf("h2h=%d", len(h.H2HMatches))
	}
	if len(h.AwardHistory) != 1 {
		t.Fatalf("awards=%d", len(h.AwardHistory))
	}
	found := false
	for _, r := range h.Records {
		if r.Title == "Höchster Match-Average" && r.Player == "A" && strings.Contains(r.Value, "61") {
			found = true
		}
	}
	if !found {
		t.Fatalf("record missing: %+v", h.Records)
	}
}

func TestCurrentMatchdayUsesFirstIncompleteDay(t *testing.T) {
	rows, _ := generateSeasonSchedule("s1", []string{"A", "B", "C", "D", "E", "F"})
	ov := LeagueOverview{SeasonID: "s1"}
	// complete all day 1 fixtures
	for i, r := range rows {
		if r.Day == 1 {
			ov.Matches = append(ov.Matches, MatchOverview{MatchID: string(rune('a' + i)), Matchday: 1, Player1: r.Player1, Player2: r.Player2, Score1: 4, Score2: 2, Winner: r.Player1})
		}
	}
	v := currentMatchdayView(ov, rows)
	if v.Day != 2 || v.Completed != 0 || v.Total != 3 {
		t.Fatalf("bad current day: %+v", v)
	}
}

func TestSeasonScheduleVariesBySeasonButStaysValid(t *testing.T) {
	players := []string{"A", "B", "C", "D", "E", "F"}
	a, err := generateSeasonSchedule("saison-2027-2028", players)
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateSeasonSchedule("saison-2028-2029", players)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSeasonSchedule(a, "saison-2027-2028", players); err != nil {
		t.Fatal(err)
	}
	if err := validateSeasonSchedule(b, "saison-2028-2029", players); err != nil {
		t.Fatal(err)
	}
	same := true
	for i := 0; i < 3; i++ {
		if pairKey(a[i].Player1, a[i].Player2) != pairKey(b[i].Player1, b[i].Player2) {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different season IDs unexpectedly produced identical first matchday")
	}
}

func TestOverviewRendersAnalyticsAndSummaryLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	ov := LeagueOverview{LeagueName: "Liga", SeasonID: "s1", SeasonName: "Saison 1", IsCurrent: true, Players: []PlayerOverview{{Name: "A"}, {Name: "B"}}}
	st := &FinalState{SeasonID: "s1", SeasonName: "Saison 1", Status: "completed", Champion: "A", Progress: LeagueProgress{Complete: true}, Fixtures: []FinalFixture{{Winner: "E", Loser: "F", Status: "completed"}, {Winner: "A", Loser: "D", Status: "completed"}, {Winner: "B", Loser: "C", Status: "completed"}, {Winner: "C", Loser: "D", Status: "completed"}, {Winner: "A", Loser: "B", Status: "completed"}}}
	h := LeagueHistory{Players: []CareerPlayer{{Name: "A", Titles: 1, Elo: 1510, Scores171: 3, OneEighties: 2}, {Name: "B", Elo: 1490}}, Records: []LeagueRecord{{Title: "Testrekord", Value: "1", Player: "A"}}, Form: []FormPlayer{{Name: "A", Results: []string{"W"}, Wins: 1}}}
	if err := writeHTMLOverview(path, ov, []SeasonInfo{{ID: "s1", Name: "Saison 1"}}, "s1", st, OverviewExtras{History: h}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"Hall of Fame", "Head-to-Head", "Rekordbuch", "Spielerprofile", "Form-Ranking", "abschluss-s1.html", "data-tab=\"spielplan\"", "data-tab=\"statistiken\"", "data-tab=\"historie\"", "data-tab=\"finale\"", "LIGA_SPIELPLAN_SLOT", "171er", ">3</b><span>171er</span>", ">2</b><span>180er</span>"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(s, "System-Gesundheit") {
		t.Fatal("System-Gesundheit darf auf der Homepage nicht mehr sichtbar sein")
	}
}

func TestSeasonSummaryContainsFinalRoundAndSeasonHighlights(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abschluss-s1.html")
	ov := LeagueOverview{
		LeagueName: "Liga", SeasonID: "s1", SeasonName: "Saison 1",
		Matches: make([]MatchOverview, 30),
		Players: []PlayerOverview{
			{Name: "A", Average: 61.2, HighFinish: 121, OneEighties: 4},
			{Name: "B", Average: 58.4, HighFinish: 145, OneEighties: 2},
		},
	}
	st := &FinalState{SeasonID: "s1", SeasonName: "Saison 1", Status: "completed", Champion: "A", Fixtures: []FinalFixture{
		{Order: 1, Label: "Platz 5/6", Player1: "E", Player2: "F", Winner: "E", Score: []int{4, 2}, Status: "completed"},
		{Order: 2, Label: "Halbfinale 1", Player1: "A", Player2: "D", Winner: "A", Score: []int{4, 1}, Status: "completed"},
		{Order: 3, Label: "Halbfinale 2", Player1: "B", Player2: "C", Winner: "B", Score: []int{4, 3}, Status: "completed"},
		{Order: 4, Label: "Spiel um Platz 3", Player1: "C", Player2: "D", Winner: "C", Score: []int{4, 2}, Status: "completed"},
		{Order: 5, Label: "Finale", Player1: "A", Player2: "B", Winner: "A", Score: []int{4, 3}, Status: "completed"},
	}}
	if err := writeSeasonSummaryHTML(path, ov, st, LeagueHistory{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"Finalrunde", "Halbfinale 1", "Finale", "4:3", "Bester Saison-Average", "61.20", "Höchstes Saison-Finish", "145", "Meiste 180er", "4"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary missing %q", want)
		}
	}
}
