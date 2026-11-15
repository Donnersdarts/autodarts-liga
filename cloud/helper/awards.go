package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SeasonAward represents the current leader of one award. During the regular
// season these values are recalculated live after every league update. Once the
// regular season is complete they become the final regular-season award result.
// Final-day matches are deliberately excluded.
type SeasonAward struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Emoji    string `json:"emoji"`
	Winner   string `json:"winner"`
	Value    string `json:"value,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Negative bool   `json:"negative,omitempty"`
	Complete bool   `json:"complete"`
}

type scoringAwardCandidate struct {
	P     PlayerOverview
	Score int
}

type deciderAwardCandidate struct {
	Name   string
	Lost   int
	Played int
}

func scoringAwardPoints(p PlayerOverview) int {
	// Exclusive scoring classes used by the league: a 180 is not counted again
	// as 133+/114+/95+. The weighting rewards increasingly rare high scores.
	return p.Scores95Plus +
		2*p.Scores114Plus +
		3*p.Scores133Plus +
		5*p.Scores171 +
		6*p.Scores174 +
		7*p.Scores177 +
		8*p.OneEighties
}

func seasonAwardsForOverview(ov LeagueOverview) []SeasonAward {
	if len(ov.Players) == 0 {
		return nil
	}
	awards := make([]SeasonAward, 0, 5)

	// 1) Spieler der Saison = best-ranked player of the regular-season table.
	// The table itself is deterministically sorted by wins, leg difference,
	// average and finally name.
	best := ov.Players[0]
	awards = append(awards, SeasonAward{
		ID:       "player-of-season",
		Title:    "Spieler der Saison",
		Emoji:    "🏆",
		Winner:   best.Name,
		Value:    fmt.Sprintf("%d Siege · %+d Legs · Ø %.2f", best.Wins, best.LegDiff, best.Average),
		Detail:   "Bester Spieler der regulären Saison nach Tabellenwertung (Siege, Leg-Differenz, Average).",
		Complete: true,
	})

	// 2) Scoring-Maschine = weighted high-score performance over the season.
	scoring := make([]scoringAwardCandidate, 0, len(ov.Players))
	scoringComplete := true
	for _, p := range ov.Players {
		scoring = append(scoring, scoringAwardCandidate{P: p, Score: scoringAwardPoints(p)})
		if !p.ScoringComplete {
			scoringComplete = false
		}
	}
	sort.SliceStable(scoring, func(i, j int) bool {
		if scoring[i].Score != scoring[j].Score {
			return scoring[i].Score > scoring[j].Score
		}
		if scoring[i].P.OneEighties != scoring[j].P.OneEighties {
			return scoring[i].P.OneEighties > scoring[j].P.OneEighties
		}
		if scoring[i].P.Average != scoring[j].P.Average {
			return scoring[i].P.Average > scoring[j].P.Average
		}
		return strings.ToLower(scoring[i].P.Name) < strings.ToLower(scoring[j].P.Name)
	})
	sc := scoring[0]
	scoreDetail := "Gewichtung: 95+=1, 114+=2, 133+=3, 171=5, 174=6, 177=7, 180=8 Punkte."
	if !scoringComplete {
		scoreDetail += " Mindestens ein älteres Match enthält nicht alle neuen Scoringklassen; die Wertung basiert auf den vorhandenen Daten."
	}
	awards = append(awards, SeasonAward{
		ID:       "scoring-machine",
		Title:    "Scoring-Maschine",
		Emoji:    "🔥",
		Winner:   sc.P.Name,
		Value:    fmt.Sprintf("%d Scoring-Punkte · %d× 180", sc.Score, sc.P.OneEighties),
		Detail:   scoreDetail,
		Complete: scoringComplete,
	})

	// 3) Checkout-König = highest finish; tiebreak CO%, then checkouts hit.
	checkouts := append([]PlayerOverview(nil), ov.Players...)
	sort.SliceStable(checkouts, func(i, j int) bool {
		a, b := checkouts[i], checkouts[j]
		if a.HighFinish != b.HighFinish {
			return a.HighFinish > b.HighFinish
		}
		if a.CheckoutPct != b.CheckoutPct {
			return a.CheckoutPct > b.CheckoutPct
		}
		if a.CheckoutHits != b.CheckoutHits {
			return a.CheckoutHits > b.CheckoutHits
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	co := checkouts[0]
	awards = append(awards, SeasonAward{
		ID:       "checkout-king",
		Title:    "Checkout-König",
		Emoji:    "🎯",
		Winner:   co.Name,
		Value:    fmt.Sprintf("High Finish %d · CO %.1f %%", co.HighFinish, co.CheckoutPct),
		Detail:   "Höchstes Finish der regulären Saison. Bei Gleichstand entscheiden Checkoutquote und danach erfolgreiche Checkouts.",
		Complete: true,
	})

	// 4) Double Trouble = most misses at double = attempts - successful checkouts.
	doubles := append([]PlayerOverview(nil), ov.Players...)
	doubleComplete := true
	for _, p := range doubles {
		if !p.CheckoutDataComplete {
			doubleComplete = false
		}
	}
	sort.SliceStable(doubles, func(i, j int) bool {
		a, b := doubles[i], doubles[j]
		if a.DoubleMisses != b.DoubleMisses {
			return a.DoubleMisses > b.DoubleMisses
		}
		if a.CheckoutPct != b.CheckoutPct {
			return a.CheckoutPct < b.CheckoutPct // worse percentage wins this negative award
		}
		if a.CheckoutAttempts != b.CheckoutAttempts {
			return a.CheckoutAttempts > b.CheckoutAttempts
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	dt := doubles[0]
	dtValue := fmt.Sprintf("%d Fehlversuche · %d/%d Checkouts", dt.DoubleMisses, dt.CheckoutHits, dt.CheckoutAttempts)
	dtDetail := "Fehlversuche = Doppel-/Checkout-Versuche minus erfolgreiche Checkouts. Bei Gleichstand entscheidet die schlechtere Checkoutquote."
	if !doubleComplete {
		dtValue = "≈ " + dtValue
		dtDetail += " Mindestens ein älteres Match enthält keine explizite Versuchszahl; die Gesamtwertung kann deshalb aus vorhandener Checkoutquote rekonstruiert sein."
	}
	awards = append(awards, SeasonAward{
		ID:       "double-trouble",
		Title:    "Double Trouble Award",
		Emoji:    "💀",
		Winner:   dt.Name,
		Value:    dtValue,
		Detail:   dtDetail,
		Negative: true,
		Complete: doubleComplete,
	})

	// 5) Nervenflatter = most regular-season matches lost exactly 3:4.
	deciders := map[string]*deciderAwardCandidate{}
	ensure := func(name string) *deciderAwardCandidate {
		key := keyName(name)
		if deciders[key] == nil {
			deciders[key] = &deciderAwardCandidate{Name: name}
		}
		return deciders[key]
	}
	for _, p := range ov.Players {
		ensure(p.Name)
	}
	for _, m := range ov.Matches {
		if m.Walkover || !((m.Score1 == 4 && m.Score2 == 3) || (m.Score1 == 3 && m.Score2 == 4)) {
			continue
		}
		a, b := ensure(m.Player1), ensure(m.Player2)
		a.Played++
		b.Played++
		if m.Score1 == 3 {
			a.Lost++
		} else {
			b.Lost++
		}
	}
	nervous := make([]deciderAwardCandidate, 0, len(deciders))
	for _, c := range deciders {
		nervous = append(nervous, *c)
	}
	sort.SliceStable(nervous, func(i, j int) bool {
		a, b := nervous[i], nervous[j]
		if a.Lost != b.Lost {
			return a.Lost > b.Lost
		}
		// If losses tie, the higher loss share in all deciders is the more
		// fitting "Nervenflatter" result.
		left, right := a.Lost*b.Played, b.Lost*a.Played
		if a.Played > 0 && b.Played > 0 && left != right {
			return left > right
		}
		if a.Played != b.Played {
			return a.Played > b.Played
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	nv := nervous[0]
	nervousWinner := nv.Name
	nervousValue := fmt.Sprintf("%d verlorene Decider · %d gespielt", nv.Lost, nv.Played)
	if nv.Lost == 0 {
		nervousWinner = "Niemand"
		nervousValue = "0 verlorene 3:4-Decider"
	}
	awards = append(awards, SeasonAward{
		ID:       "nerves",
		Title:    "Nervenflatter Award",
		Emoji:    "😰",
		Winner:   nervousWinner,
		Value:    nervousValue,
		Detail:   "Gezählt werden ausschließlich Niederlagen mit 3:4 in der regulären Saison. Kampflose Spiele zählen nicht als Decider.",
		Negative: true,
		Complete: true,
	})

	return awards
}

func seasonAwardsEqual(a, b []SeasonAward) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func refreshSeasonAwards(st *FinalState, ov LeagueOverview) {
	if st == nil {
		return
	}
	next := seasonAwardsForOverview(ov)
	if len(next) == 0 {
		st.SeasonAwards = nil
		st.AwardsComputedAt = ""
		return
	}
	if !seasonAwardsEqual(st.SeasonAwards, next) || st.AwardsComputedAt == "" {
		st.SeasonAwards = next
		st.AwardsComputedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
}

func writeSeasonAwardsHTML(b *strings.Builder, st *FinalState) {
	if st == nil {
		return
	}
	played, expected := st.Progress.CompletedMatches, st.Progress.ExpectedMatches
	if expected <= 0 {
		expected = 30
	}
	if len(st.SeasonAwards) == 0 {
		b.WriteString(`<div class="panel season-awards"><h2>🏅 Saison-Awards · ` + htmlE(st.SeasonName) + `</h2><div class="season-awards-note">Live-Stand: Noch keine regulären Ligaspiele mit auswertbaren Daten vorhanden.</div></div>`)
		return
	}
	phase := "LIVE · aktueller Stand"
	note := fmt.Sprintf("Stand nach %d/%d regulären Ligaspielen. Die Gewinner können sich nach jedem weiteren Ligaspiel ändern. Finalspiele zählen nicht.", played, expected)
	if st.Progress.Complete {
		phase = "ENDSTAND · reguläre Saison"
		note = "Die reguläre Saison ist vollständig. Diese Award-Wertung ist damit final; der Finalspieltag verändert sie nicht."
	}
	b.WriteString(`<div class="panel season-awards"><h2>🏅 Saison-Awards · ` + htmlE(st.SeasonName) + ` <span class="award-phase">` + htmlE(phase) + `</span></h2><div class="season-awards-note">` + htmlE(note) + `</div><div class="season-awards-grid">`)
	for _, a := range st.SeasonAwards {
		cls := "season-award"
		if a.Negative {
			cls += " negative"
		}
		if !a.Complete {
			cls += " incomplete"
		}
		fmt.Fprintf(b, `<div class="%s"><div class="season-award-icon">%s</div><div class="season-award-copy"><span>%s</span><strong>%s</strong><b>%s</b><small>%s%s</small></div></div>`, cls, htmlE(a.Emoji), htmlE(a.Title), htmlE(a.Winner), htmlE(a.Value), htmlE(a.Detail), map[bool]string{true: "", false: " · Datenhinweis beachten"}[a.Complete])
	}
	b.WriteString(`</div></div>`)
}
