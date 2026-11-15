package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type AwardHistoryItem struct {
	SeasonID   string `json:"seasonId"`
	SeasonName string `json:"seasonName"`
	AwardID    string `json:"awardId"`
	Title      string `json:"title"`
	Emoji      string `json:"emoji"`
	Winner     string `json:"winner"`
	Value      string `json:"value,omitempty"`
	Negative   bool   `json:"negative,omitempty"`
}

type CareerPlayer struct {
	Name             string
	Seasons          int
	Titles           int
	RunnerUp         int
	Third            int
	Matches          int
	Wins             int
	Losses           int
	LegsFor          int
	LegsAgainst      int
	LegDiff          int
	Average          float64
	CheckoutPct      float64
	CheckoutHits     int
	CheckoutAttempts int
	OneEighties      int
	HighFinish       int
	Elo              float64
	MaxWinStreak     int
	MaxLossStreak    int
	Awards           []AwardHistoryItem
	Placements       []PlacementHistoryItem
}

type H2HMatch struct {
	SeasonID     string  `json:"seasonId"`
	SeasonName   string  `json:"seasonName"`
	Matchday     int     `json:"matchday"`
	Player1      string  `json:"player1"`
	Player2      string  `json:"player2"`
	Score1       int     `json:"score1"`
	Score2       int     `json:"score2"`
	Winner       string  `json:"winner"`
	Avg1         float64 `json:"avg1,omitempty"`
	Avg2         float64 `json:"avg2,omitempty"`
	OneEighties1 int     `json:"oneEighties1,omitempty"`
	OneEighties2 int     `json:"oneEighties2,omitempty"`
	Walkover     bool    `json:"walkover,omitempty"`
}

type LeagueRecord struct {
	Title  string
	Value  string
	Player string
	Detail string
}

type FormPlayer struct {
	Name       string
	Results    []string
	Wins       int
	Losses     int
	LegDiff    int
	Average    float64
	PowerScore int
}

type MatchdayFixtureView struct {
	Player1  string
	Player2  string
	Played   bool
	Walkover bool
	Score1   int
	Score2   int
	Winner   string
}

type MatchdayView struct {
	Day       int
	Completed int
	Total     int
	Fixtures  []MatchdayFixtureView
	Complete  bool
}

type HealthCheck struct {
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type LeagueHealth struct {
	OK     bool          `json:"ok"`
	Checks []HealthCheck `json:"checks"`
}

type LeagueHistory struct {
	Players      []CareerPlayer
	H2HMatches   []H2HMatch
	Records      []LeagueRecord
	AwardHistory []AwardHistoryItem
	Form         []FormPlayer
	Historical   []HistoricalSeason
}

type OverviewExtras struct {
	History  LeagueHistory
	Schedule []ScheduleRow
	Health   LeagueHealth
}

func playerByName(ov LeagueOverview, name string) (PlayerOverview, bool) {
	k := keyName(name)
	for _, p := range ov.Players {
		if keyName(p.Name) == k {
			return p, true
		}
	}
	return PlayerOverview{}, false
}

func matchChronoLess(a, b MatchOverview) bool {
	if a.Matchday != b.Matchday {
		return a.Matchday < b.Matchday
	}
	if !a.Date.Equal(b.Date) {
		if a.Date.IsZero() {
			return false
		}
		if b.Date.IsZero() {
			return true
		}
		return a.Date.Before(b.Date)
	}
	return a.MatchID < b.MatchID
}

func buildLeagueHistory(reg SeasonRegistry, overviews map[string]LeagueOverview, finals map[string]*FinalState, historical []HistoricalSeason) LeagueHistory {
	type careerAcc struct {
		p           CareerPlayer
		seasons     map[string]bool
		avgWeighted float64
		avgCount    int
		winStreak   int
		lossStreak  int
	}
	careers := map[string]*careerAcc{}
	get := func(name string) *careerAcc {
		k := keyName(name)
		if k == "" {
			return nil
		}
		a := careers[k]
		if a == nil {
			a = &careerAcc{p: CareerPlayer{Name: cleanPlayerName(name), Elo: 1500}, seasons: map[string]bool{}}
			careers[k] = a
		}
		return a
	}

	var h2h []H2HMatch
	var awardHistory []AwardHistoryItem

	// Historische Saisons enthalten bewusst nur die sicher bekannten Abschlussplaetze.
	// Sie zaehlen fuer Saisonanzahl und Podiumsplatzierungen, aber niemals fuer
	// Matchstatistiken, Elo, Rekorde oder Awards.
	for _, hs := range historical {
		if validateHistoricalSeason(hs) != nil {
			continue
		}
		for i, name := range hs.Placements {
			a := get(name)
			if a == nil {
				continue
			}
			a.seasons[hs.ID] = true
			a.p.Placements = append(a.p.Placements, PlacementHistoryItem{SeasonID: hs.ID, SeasonName: hs.Name, Place: i + 1, Historical: true})
			switch i {
			case 0:
				a.p.Titles++
			case 1:
				a.p.RunnerUp++
			case 2:
				a.p.Third++
			}
		}
	}
	maxAvg := struct {
		name, season string
		value        float64
		day          int
	}{}
	maxFinish := struct {
		name, season string
		value        int
		day          int
	}{}
	max180 := struct {
		name, season string
		value        int
		day          int
	}{}
	fastestLeg := struct {
		name, season string
		darts        int
		day          int
	}{darts: 999}
	deciderLost := map[string]int{}

	// Keep registry order: season progression, then matchday progression.
	for _, season := range reg.Seasons {
		ov, ok := overviews[season.ID]
		if !ok {
			continue
		}
		matches := append([]MatchOverview(nil), ov.Matches...)
		sort.SliceStable(matches, func(i, j int) bool { return matchChronoLess(matches[i], matches[j]) })
		for _, p := range ov.Players {
			a := get(p.Name)
			if a == nil {
				continue
			}
			a.seasons[season.ID] = true
			a.p.Matches += p.Matches
			a.p.Wins += p.Wins
			a.p.Losses += p.Losses
			a.p.LegsFor += p.LegsFor
			a.p.LegsAgainst += p.LegsAgainst
			a.p.CheckoutHits += p.CheckoutHits
			a.p.CheckoutAttempts += p.CheckoutAttempts
			a.p.OneEighties += p.OneEighties
			if p.HighFinish > a.p.HighFinish {
				a.p.HighFinish = p.HighFinish
			}
			if p.AverageCount > 0 {
				a.avgWeighted += p.Average * float64(p.AverageCount)
				a.avgCount += p.AverageCount
			}
		}
		for _, m := range matches {
			a1, a2 := get(m.Player1), get(m.Player2)
			if a1 == nil || a2 == nil {
				continue
			}
			if !m.Walkover {
				if m.Avg1 != nil && *m.Avg1 > maxAvg.value {
					maxAvg = struct {
						name, season string
						value        float64
						day          int
					}{m.Player1, season.Name, *m.Avg1, m.Matchday}
				}
				if m.Avg2 != nil && *m.Avg2 > maxAvg.value {
					maxAvg = struct {
						name, season string
						value        float64
						day          int
					}{m.Player2, season.Name, *m.Avg2, m.Matchday}
				}
				if m.HF1 > maxFinish.value {
					maxFinish = struct {
						name, season string
						value        int
						day          int
					}{m.Player1, season.Name, m.HF1, m.Matchday}
				}
				if m.HF2 > maxFinish.value {
					maxFinish = struct {
						name, season string
						value        int
						day          int
					}{m.Player2, season.Name, m.HF2, m.Matchday}
				}
				if m.OneEighties1 > max180.value {
					max180 = struct {
						name, season string
						value        int
						day          int
					}{m.Player1, season.Name, m.OneEighties1, m.Matchday}
				}
				if m.OneEighties2 > max180.value {
					max180 = struct {
						name, season string
						value        int
						day          int
					}{m.Player2, season.Name, m.OneEighties2, m.Matchday}
				}
				for _, x := range validWebHighlights(m.Highlights1).ShortLegs {
					if x.Darts > 0 && x.Darts < fastestLeg.darts {
						fastestLeg = struct {
							name, season string
							darts, day   int
						}{m.Player1, season.Name, x.Darts, m.Matchday}
					}
				}
				for _, x := range validWebHighlights(m.Highlights2).ShortLegs {
					if x.Darts > 0 && x.Darts < fastestLeg.darts {
						fastestLeg = struct {
							name, season string
							darts, day   int
						}{m.Player2, season.Name, x.Darts, m.Matchday}
					}
				}
			}
			h := H2HMatch{SeasonID: season.ID, SeasonName: season.Name, Matchday: m.Matchday, Player1: m.Player1, Player2: m.Player2, Score1: m.Score1, Score2: m.Score2, Winner: m.Winner, OneEighties1: m.OneEighties1, OneEighties2: m.OneEighties2, Walkover: m.Walkover}
			if m.Avg1 != nil {
				h.Avg1 = *m.Avg1
			}
			if m.Avg2 != nil {
				h.Avg2 = *m.Avg2
			}
			h2h = append(h2h, h)

			if !m.Walkover && ((m.Score1 == 4 && m.Score2 == 3) || (m.Score1 == 3 && m.Score2 == 4)) {
				if m.Score1 == 3 {
					deciderLost[keyName(m.Player1)]++
				} else {
					deciderLost[keyName(m.Player2)]++
				}
			}
			// Career streaks include official wins/losses, including WO, because standings do too.
			if namesMatch(m.Winner, m.Player1) {
				a1.winStreak++
				a1.lossStreak = 0
				a2.lossStreak++
				a2.winStreak = 0
			} else if namesMatch(m.Winner, m.Player2) {
				a2.winStreak++
				a2.lossStreak = 0
				a1.lossStreak++
				a1.winStreak = 0
			}
			if a1.winStreak > a1.p.MaxWinStreak {
				a1.p.MaxWinStreak = a1.winStreak
			}
			if a2.winStreak > a2.p.MaxWinStreak {
				a2.p.MaxWinStreak = a2.winStreak
			}
			if a1.lossStreak > a1.p.MaxLossStreak {
				a1.p.MaxLossStreak = a1.lossStreak
			}
			if a2.lossStreak > a2.p.MaxLossStreak {
				a2.p.MaxLossStreak = a2.lossStreak
			}

			// Elo uses played dart matches only; walkovers should not alter playing strength.
			if !m.Walkover && m.Winner != "" {
				ea := 1.0 / (1.0 + math.Pow(10, (a2.p.Elo-a1.p.Elo)/400.0))
				sa := 0.0
				if namesMatch(m.Winner, m.Player1) {
					sa = 1
				}
				const k = 32.0
				a1.p.Elo += k * (sa - ea)
				a2.p.Elo += k * ((1 - sa) - (1 - ea))
			}
		}
		if st := finals[season.ID]; st != nil {
			places := finalPlacements(st)
			if len(places) == 6 {
				for i, name := range places {
					if a := get(name); a != nil {
						a.p.Placements = append(a.p.Placements, PlacementHistoryItem{SeasonID: season.ID, SeasonName: season.Name, Place: i + 1})
					}
				}
				if a := get(places[0]); a != nil {
					a.p.Titles++
				}
				if a := get(places[1]); a != nil {
					a.p.RunnerUp++
				}
				if a := get(places[2]); a != nil {
					a.p.Third++
				}
			}
			// Award history becomes permanent once regular season is complete.
			if st.Progress.Complete {
				for _, aw := range st.SeasonAwards {
					item := AwardHistoryItem{SeasonID: season.ID, SeasonName: season.Name, AwardID: aw.ID, Title: aw.Title, Emoji: aw.Emoji, Winner: aw.Winner, Value: aw.Value, Negative: aw.Negative}
					awardHistory = append(awardHistory, item)
					if !strings.EqualFold(strings.TrimSpace(aw.Winner), "Niemand") {
						if a := get(aw.Winner); a != nil {
							a.p.Awards = append(a.p.Awards, item)
						}
					}
				}
			}
		}
	}

	players := make([]CareerPlayer, 0, len(careers))
	for _, a := range careers {
		a.p.Seasons = len(a.seasons)
		a.p.LegDiff = a.p.LegsFor - a.p.LegsAgainst
		if a.avgCount > 0 {
			a.p.Average = a.avgWeighted / float64(a.avgCount)
		}
		if a.p.CheckoutAttempts > 0 {
			a.p.CheckoutPct = float64(a.p.CheckoutHits) / float64(a.p.CheckoutAttempts) * 100
		}
		players = append(players, a.p)
	}
	sort.SliceStable(players, func(i, j int) bool {
		if players[i].Titles != players[j].Titles {
			return players[i].Titles > players[j].Titles
		}
		if players[i].Wins != players[j].Wins {
			return players[i].Wins > players[j].Wins
		}
		if players[i].Elo != players[j].Elo {
			return players[i].Elo > players[j].Elo
		}
		return strings.ToLower(players[i].Name) < strings.ToLower(players[j].Name)
	})

	var records []LeagueRecord
	if len(players) > 0 {
		add := func(title, value, player, detail string) {
			records = append(records, LeagueRecord{Title: title, Value: value, Player: player, Detail: detail})
		}
		titleLeader := players[0]
		for _, p := range players {
			if p.Titles > titleLeader.Titles {
				titleLeader = p
			}
		}
		add("Meiste Meisterschaften", strconv.Itoa(titleLeader.Titles), titleLeader.Name, "saisonübergreifend")
		if maxAvg.value > 0 {
			add("Höchster Match-Average", fmt.Sprintf("%.2f", maxAvg.value), maxAvg.name, fmt.Sprintf("%s · ST %d", maxAvg.season, maxAvg.day))
		}
		if maxFinish.value > 0 {
			add("Höchstes Finish", strconv.Itoa(maxFinish.value), maxFinish.name, fmt.Sprintf("%s · ST %d", maxFinish.season, maxFinish.day))
		}
		if max180.value > 0 {
			add("Meiste 180er in einem Match", strconv.Itoa(max180.value), max180.name, fmt.Sprintf("%s · ST %d", max180.season, max180.day))
		}
		if fastestLeg.darts < 999 {
			add("Schnellstes Leg", fmt.Sprintf("%d Darts", fastestLeg.darts), fastestLeg.name, fmt.Sprintf("%s · ST %d", fastestLeg.season, fastestLeg.day))
		}
		w := players[0]
		l := players[0]
		for _, p := range players {
			if p.MaxWinStreak > w.MaxWinStreak {
				w = p
			}
			if p.MaxLossStreak > l.MaxLossStreak {
				l = p
			}
		}
		add("Längste Siegesserie", strconv.Itoa(w.MaxWinStreak), w.Name, "reguläre Ligaspiele")
		add("Längste Niederlagenserie", strconv.Itoa(l.MaxLossStreak), l.Name, "reguläre Ligaspiele")
		decName, decN := "", -1
		for _, p := range players {
			if n := deciderLost[keyName(p.Name)]; n > decN {
				decName, decN = p.Name, n
			}
		}
		if decN >= 0 {
			add("Meiste verlorene Decider", strconv.Itoa(decN), decName, "3:4-Niederlagen")
		}
	}

	history := LeagueHistory{Players: players, H2HMatches: h2h, Records: records, AwardHistory: awardHistory, Historical: append([]HistoricalSeason(nil), historical...)}
	if cur, ok := overviews[reg.CurrentSeasonID]; ok {
		history.Form = buildFormRanking(cur)
	}
	return history
}

func buildFormRanking(ov LeagueOverview) []FormPlayer {
	by := map[string][]MatchOverview{}
	names := map[string]string{}
	matches := append([]MatchOverview(nil), ov.Matches...)
	sort.SliceStable(matches, func(i, j int) bool { return matchChronoLess(matches[i], matches[j]) })
	for _, m := range matches {
		by[keyName(m.Player1)] = append(by[keyName(m.Player1)], m)
		names[keyName(m.Player1)] = m.Player1
		by[keyName(m.Player2)] = append(by[keyName(m.Player2)], m)
		names[keyName(m.Player2)] = m.Player2
	}
	out := make([]FormPlayer, 0, len(by))
	for k, ms := range by {
		if len(ms) > 5 {
			ms = ms[len(ms)-5:]
		}
		f := FormPlayer{Name: names[k]}
		avgSum, avgN := 0.0, 0
		for _, m := range ms {
			self1 := keyName(m.Player1) == k
			sf, sa := m.Score1, m.Score2
			av := m.Avg1
			if !self1 {
				sf, sa = m.Score2, m.Score1
				av = m.Avg2
			}
			f.LegDiff += sf - sa
			if namesMatch(m.Winner, f.Name) {
				f.Results = append(f.Results, "W")
				f.Wins++
			} else {
				f.Results = append(f.Results, "L")
				f.Losses++
			}
			if av != nil {
				avgSum += *av
				avgN++
			}
		}
		if avgN > 0 {
			f.Average = avgSum / float64(avgN)
		}
		f.PowerScore = f.Wins*10 + f.LegDiff
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Wins != out[j].Wins {
			return out[i].Wins > out[j].Wins
		}
		if out[i].LegDiff != out[j].LegDiff {
			return out[i].LegDiff > out[j].LegDiff
		}
		if out[i].Average != out[j].Average {
			return out[i].Average > out[j].Average
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func scheduleForSeason(rows []ScheduleRow, seasonID string) []ScheduleRow {
	out := []ScheduleRow{}
	for _, r := range rows {
		if strings.EqualFold(strings.TrimSpace(r.Season), strings.TrimSpace(seasonID)) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		return out[i].Index < out[j].Index
	})
	return out
}

func matchForFixture(ov LeagueOverview, day int, a, b string) (MatchOverview, bool) {
	want := pairKey(a, b)
	for _, m := range ov.Matches {
		if m.Matchday == day && pairKey(m.Player1, m.Player2) == want {
			return m, true
		}
	}
	return MatchOverview{}, false
}

func currentMatchdayView(ov LeagueOverview, schedule []ScheduleRow) MatchdayView {
	rows := scheduleForSeason(schedule, ov.SeasonID)
	if len(rows) == 0 {
		return MatchdayView{}
	}
	selected := 0
	for d := 1; d <= expectedMatchdays; d++ {
		total, done := 0, 0
		for _, r := range rows {
			if r.Day == d {
				total++
				if _, ok := matchForFixture(ov, d, r.Player1, r.Player2); ok {
					done++
				}
			}
		}
		if total > 0 && done < total {
			selected = d
			break
		}
	}
	if selected == 0 {
		selected = expectedMatchdays
	}
	v := MatchdayView{Day: selected}
	for _, r := range rows {
		if r.Day != selected {
			continue
		}
		v.Total++
		fv := MatchdayFixtureView{Player1: r.Player1, Player2: r.Player2}
		if m, ok := matchForFixture(ov, selected, r.Player1, r.Player2); ok {
			fv.Played = true
			fv.Walkover = m.Walkover
			fv.Winner = m.Winner
			if keyName(m.Player1) == keyName(r.Player1) {
				fv.Score1, fv.Score2 = m.Score1, m.Score2
			} else {
				fv.Score1, fv.Score2 = m.Score2, m.Score1
			}
			v.Completed++
		}
		v.Fixtures = append(v.Fixtures, fv)
	}
	v.Complete = v.Total > 0 && v.Completed == v.Total
	return v
}

func buildLeagueHealth(s *Server, ov LeagueOverview, schedule []ScheduleRow, st *FinalState) LeagueHealth {
	checks := []HealthCheck{}
	add := func(label string, ok bool, detail string) {
		checks = append(checks, HealthCheck{Label: label, OK: ok, Detail: detail})
	}
	base := s.basePath()
	sync := inspectSyncPath(base)
	add("Liga-Datenordner", sync.SyncReady, sync.Status)
	rows := scheduleForSeason(schedule, ov.SeasonID)
	playerNames := make([]string, 0, len(ov.Players))
	for _, p := range ov.Players {
		playerNames = append(playerNames, p.Name)
	}
	if len(playerNames) != expectedLeaguePlayers && len(rows) > 0 {
		seen := map[string]string{}
		for _, r := range rows {
			seen[keyName(r.Player1)] = r.Player1
			seen[keyName(r.Player2)] = r.Player2
		}
		playerNames = playerNames[:0]
		for _, name := range seen {
			playerNames = append(playerNames, name)
		}
		sort.Strings(playerNames)
	}
	schedOK := false
	schedDetail := "Kein saisonbezogener Spielplan gefunden"
	if len(rows) > 0 {
		if len(playerNames) == expectedLeaguePlayers {
			if err := validateSeasonSchedule(rows, ov.SeasonID, playerNames); err == nil {
				schedOK = true
				schedDetail = "30 Spiele · 10 Spieltage · jede Paarung zweimal"
			} else {
				schedDetail = err.Error()
			}
		} else {
			schedDetail = fmt.Sprintf("%d statt 6 Spieler aus Matchdaten erkannt", len(playerNames))
		}
	}
	add("Spielplan", schedOK, schedDetail)
	progress := regularProgress(ov)
	structureOK := progress.PlayerCount <= expectedLeaguePlayers && progress.CompletedMatches <= expectedLeagueMatches
	add("Ligadaten", structureOK, fmt.Sprintf("%d/30 Spiele · %d/6 Spieler", progress.CompletedMatches, progress.PlayerCount))
	unreadable := 0
	if base != "" {
		entries, _ := os.ReadDir(filepath.Join(base, "Matches"))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(base, "Matches", e.Name()))
			if err != nil {
				unreadable++
				continue
			}
			var mf MatchFile
			if json.Unmarshal(b, &mf) != nil {
				unreadable++
			}
		}
	}
	add("Match-Dateien", unreadable == 0, func() string {
		if unreadable == 0 {
			return "Alle JSON-Dateien lesbar"
		}
		return fmt.Sprintf("%d Datei(en) nicht lesbar", unreadable)
	}())
	finalOK := true
	finalDetail := "Finalspieltag wartet auf vollständige Saison"
	if st != nil {
		finalDetail = fmt.Sprintf("Status %s · %d/5 Finalspiele", st.Status, st.FinalStatsGames)
		if st.Status == "completed" && len(finalPlacements(st)) != 6 {
			finalOK = false
			finalDetail = "Abgeschlossen, aber Platzierungen unvollständig"
		}
	}
	add("Finalrunde", finalOK, finalDetail)
	ok := true
	for _, c := range checks {
		if !c.OK {
			ok = false
		}
	}
	return LeagueHealth{OK: ok, Checks: checks}
}

func writeAnalyticsCSS(b *strings.Builder) {
	b.WriteString(`<style id="liga-analytics-css">.analytics-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.analytics-card{border:1px solid #2b4156;background:#0d1826;border-radius:12px;padding:13px}.analytics-card h3{margin:0 0 8px;font-size:15px}.hall-table td:first-child{font-weight:800}.medal{font-size:18px}.record-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:9px}.record{border:1px solid #2b4156;background:#0d1826;border-radius:11px;padding:11px}.record small{color:#8da7bd;display:block}.record strong{font-size:20px;display:block;margin:4px 0}.profile-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.profile{border:1px solid #2b4156;background:#0d1826;border-radius:12px}.profile summary{cursor:pointer;padding:12px 14px;font-weight:850}.profile-body{padding:0 14px 14px}.profile-kpis{display:grid;grid-template-columns:repeat(3,1fr);gap:7px}.profile-kpi{background:#122235;border-radius:8px;padding:8px;text-align:center}.profile-kpi b{display:block;font-size:16px}.award-chip{display:inline-block;border:1px solid #405871;border-radius:999px;padding:4px 7px;margin:3px 3px 0 0;font-size:10px}.form-list{display:grid;gap:8px}.form-row{display:grid;grid-template-columns:28px minmax(90px,1fr) 150px 70px;gap:9px;align-items:center}.form-balls{display:flex;gap:4px}.form-ball{display:grid;place-items:center;width:24px;height:24px;border-radius:50%;font-size:10px;font-weight:900;background:#23384b}.form-ball.w{background:#164b38;color:#75efb8}.form-ball.l{background:#502631;color:#ffabb8}.matchday-live{border-color:#3b6f98}.fixture-live{display:grid;grid-template-columns:1fr 70px 1fr;gap:8px;padding:10px 0;border-top:1px solid #253b50;align-items:center}.fixture-live span:first-child{text-align:right}.fixture-live .fx-score{text-align:center;font-weight:900}.health-head{display:flex;gap:10px;align-items:center}.health-pill{border-radius:999px;padding:5px 9px;font-weight:900;font-size:11px}.health-pill.ok{background:#123d2d;color:#72eeb4}.health-pill.bad{background:#4a222b;color:#ffabb8}.health-list{display:grid;gap:7px;margin-top:10px}.health-row{display:grid;grid-template-columns:22px 150px 1fr;gap:8px;font-size:12px}.h2h-controls{display:flex;flex-wrap:wrap;gap:8px;margin:10px 0}.h2h-controls select{background:#0d1826;color:#eaf5ff;border:1px solid #35536e;border-radius:8px;padding:8px}.h2h-result{border:1px solid #2b4156;border-radius:12px;padding:12px;background:#0d1826}.share-link{display:inline-block;margin-top:8px;color:#8ec4ff;text-decoration:none;font-weight:800}@media(max-width:780px){.analytics-grid,.profile-grid{grid-template-columns:1fr}.record-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.form-row{grid-template-columns:26px 1fr}.form-balls,.form-row>span:last-child{grid-column:2}.health-row{grid-template-columns:22px 1fr}.health-row span:last-child{grid-column:2}.fixture-live{grid-template-columns:1fr 52px 1fr}}@media(max-width:430px){.record-grid{grid-template-columns:1fr}.profile-kpis{grid-template-columns:repeat(2,1fr)}}</style>`)
}

func writeCurrentMatchdayHTML(b *strings.Builder, ov LeagueOverview, schedule []ScheduleRow) {
	v := currentMatchdayView(ov, schedule)
	if v.Day == 0 {
		return
	}
	b.WriteString(`<div class="panel matchday-live"><h2>🎮 Aktueller Spieltag</h2>`)
	fmt.Fprintf(b, `<div class="muted">Spieltag %d · %d/%d Spiele abgeschlossen</div>`, v.Day, v.Completed, v.Total)
	for _, f := range v.Fixtures {
		score := "offen"
		if f.Played {
			if f.Walkover {
				score = "WO"
			} else {
				score = fmt.Sprintf("%d : %d", f.Score1, f.Score2)
			}
		}
		fmt.Fprintf(b, `<div class="fixture-live"><span><b>%s</b></span><span class="fx-score">%s</span><span><b>%s</b></span></div>`, htmlE(f.Player1), htmlE(score), htmlE(f.Player2))
	}
	b.WriteString(`</div>`)
}

func writeFormHTML(b *strings.Builder, history LeagueHistory) {
	b.WriteString(`<div class="panel"><h2>📈 Form-Ranking</h2><div class="muted">Letzte maximal 5 Ligaspiele · sortiert nach Siegen, Leg-Differenz und Average.</div>`)
	if len(history.Form) == 0 {
		b.WriteString(`<p class="muted">Noch keine Ligaspiele für eine Formauswertung vorhanden.</p></div>`)
		return
	}
	b.WriteString(`<div class="form-list" style="margin-top:12px">`)
	for i, f := range history.Form {
		fmt.Fprintf(b, `<div class="form-row"><b>%d.</b><strong>%s</strong><div class="form-balls">`, i+1, htmlE(f.Name))
		for _, r := range f.Results {
			cl := "l"
			if r == "W" {
				cl = "w"
			}
			fmt.Fprintf(b, `<span class="form-ball %s">%s</span>`, cl, r)
		}
		fmt.Fprintf(b, `</div><span class="muted">%dS/%dN · %+d Legs</span></div>`, f.Wins, f.Losses, f.LegDiff)
	}
	b.WriteString(`</div></div>`)
}

func writeHallOfFameHTML(b *strings.Builder, history LeagueHistory) {
	b.WriteString(`<div class="panel" id="hall-of-fame"><h2>🏛️ Hall of Fame · Ewige Bestenliste</h2>`)
	if len(history.Players) == 0 {
		b.WriteString(`<p class="muted">Die Hall of Fame füllt sich mit den ersten Ligaspielen.</p></div>`)
		return
	}
	b.WriteString(`<table class="hall-table"><thead><tr><th>#</th><th>Spieler</th><th>Saisons</th><th>🏆</th><th>🥈</th><th>🥉</th><th>Sp.</th><th>S</th><th>Ø</th><th>180</th><th>Elo</th></tr></thead><tbody>`)
	for i, p := range history.Players {
		fmt.Fprintf(b, `<tr><td>%d</td><td>%s</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%.2f</td><td>%d</td><td>%.0f</td></tr>`, i+1, htmlE(p.Name), p.Seasons, p.Titles, p.RunnerUp, p.Third, p.Matches, p.Wins, p.Average, p.OneEighties, p.Elo)
	}
	b.WriteString(`</tbody></table></div>`)
}

func writeRecordsHTML(b *strings.Builder, history LeagueHistory) {
	b.WriteString(`<div class="panel"><h2>📚 Rekordbuch</h2>`)
	if len(history.Records) == 0 {
		b.WriteString(`<p class="muted">Noch keine Rekorde vorhanden.</p></div>`)
		return
	}
	b.WriteString(`<div class="record-grid">`)
	for _, r := range history.Records {
		fmt.Fprintf(b, `<div class="record"><small>%s</small><strong>%s</strong><b>%s</b><small>%s</small></div>`, htmlE(r.Title), htmlE(r.Value), htmlE(r.Player), htmlE(r.Detail))
	}
	b.WriteString(`</div></div>`)
}

func writeProfilesHTML(b *strings.Builder, history LeagueHistory, current LeagueOverview) {
	b.WriteString(`<div class="panel"><h2>👤 Spielerprofile</h2>`)
	if len(history.Players) == 0 {
		b.WriteString(`<p class="muted">Spielerprofile werden mit den ersten Ligaspielen aufgebaut.</p></div>`)
		return
	}
	b.WriteString(`<div class="profile-grid">`)
	for _, p := range history.Players {
		cur, _ := playerByName(current, p.Name)
		fmt.Fprintf(b, `<details class="profile"><summary>%s · Elo %.0f</summary><div class="profile-body"><div class="profile-kpis"><div class="profile-kpi"><b>%d</b><span>Saisons</span></div><div class="profile-kpi"><b>%d</b><span>Titel</span></div><div class="profile-kpi"><b>%d/%d</b><span>Siege/Spiele</span></div><div class="profile-kpi"><b>%.2f</b><span>Karriere-AVG</span></div><div class="profile-kpi"><b>%d</b><span>180er</span></div><div class="profile-kpi"><b>%d</b><span>High Finish</span></div><div class="profile-kpi"><b>%d</b><span>max. Siegesserie</span></div></div>`, htmlE(p.Name), p.Elo, p.Seasons, p.Titles, p.Wins, p.Matches, p.Average, p.OneEighties, p.HighFinish, p.MaxWinStreak)
		if cur.Name != "" {
			fmt.Fprintf(b, `<p class="muted">Aktuelle Saison: %d Siege · %+d Legs · Ø %.2f · CO %.1f %%</p>`, cur.Wins, cur.LegDiff, cur.Average, cur.CheckoutPct)
		}
		if len(p.Placements) > 0 {
			items := append([]PlacementHistoryItem(nil), p.Placements...)
			sort.SliceStable(items, func(i, j int) bool { return items[i].SeasonName < items[j].SeasonName })
			b.WriteString(`<div><b>Platzierungen:</b><br>`)
			for _, ph := range items {
				suffix := ""
				if ph.Historical {
					suffix = " · historisch"
				}
				fmt.Fprintf(b, `<span class="award-chip">%s · Platz %d%s</span>`, htmlE(ph.SeasonName), ph.Place, htmlE(suffix))
			}
			b.WriteString(`</div>`)
		}
		if len(p.Awards) > 0 {
			b.WriteString(`<div><b>Awards:</b><br>`)
			for _, a := range p.Awards {
				fmt.Fprintf(b, `<span class="award-chip">%s %s · %s</span>`, htmlE(a.Emoji), htmlE(a.Title), htmlE(a.SeasonName))
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div></details>`)
	}
	b.WriteString(`</div></div>`)
}

func writeAwardHistoryHTML(b *strings.Builder, history LeagueHistory) {
	if len(history.AwardHistory) == 0 {
		return
	}
	b.WriteString(`<div class="panel"><h2>🏅 Award-Historie</h2><table><thead><tr><th>Saison</th><th>Award</th><th>Gewinner</th><th>Wert</th></tr></thead><tbody>`)
	items := append([]AwardHistoryItem(nil), history.AwardHistory...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SeasonName != items[j].SeasonName {
			return items[i].SeasonName > items[j].SeasonName
		}
		return items[i].Title < items[j].Title
	})
	for _, a := range items {
		fmt.Fprintf(b, `<tr><td>%s</td><td>%s %s</td><td><b>%s</b></td><td>%s</td></tr>`, htmlE(a.SeasonName), htmlE(a.Emoji), htmlE(a.Title), htmlE(a.Winner), htmlE(a.Value))
	}
	b.WriteString(`</tbody></table></div>`)
}

func writeHeadToHeadHTML(b *strings.Builder, history LeagueHistory, seasons []SeasonInfo, currentSeasonID string) {
	b.WriteString(`<div class="panel" id="head-to-head"><h2>⚔️ Head-to-Head</h2>`)
	if len(history.Players) < 2 {
		b.WriteString(`<p class="muted">Mindestens zwei Spieler mit Ligadaten werden benötigt.</p></div>`)
		return
	}
	data, _ := json.Marshal(history.H2HMatches)
	b.WriteString(`<div class="h2h-controls"><select id="h2hA">`)
	for _, p := range history.Players {
		fmt.Fprintf(b, `<option value="%s">%s</option>`, htmlE(p.Name), htmlE(p.Name))
	}
	b.WriteString(`</select><span>gegen</span><select id="h2hB">`)
	for i, p := range history.Players {
		sel := ""
		if i == 1 {
			sel = " selected"
		}
		fmt.Fprintf(b, `<option value="%s"%s>%s</option>`, htmlE(p.Name), sel, htmlE(p.Name))
	}
	b.WriteString(`</select><select id="h2hSeason"><option value="">Alle Saisons</option>`)
	for _, s := range seasons {
		sel := ""
		if s.ID == currentSeasonID {
			sel = " selected"
		}
		fmt.Fprintf(b, `<option value="%s"%s>%s</option>`, htmlE(s.ID), sel, htmlE(s.Name))
	}
	b.WriteString(`</select></div><div id="h2hResult" class="h2h-result"></div><script>(function(){const data=` + string(data) + `;const a=document.getElementById('h2hA'),b=document.getElementById('h2hB'),s=document.getElementById('h2hSeason'),out=document.getElementById('h2hResult');if(!a||!b||!out)return;const n=x=>String(x||'').trim().toLocaleLowerCase('de');function render(){const A=a.value,B=b.value,S=s?s.value:'';if(n(A)===n(B)){out.textContent='Bitte zwei unterschiedliche Spieler auswählen.';return}let rows=data.filter(x=>(!S||x.seasonId===S)&&((n(x.player1)===n(A)&&n(x.player2)===n(B))||(n(x.player1)===n(B)&&n(x.player2)===n(A))));let wa=0,wb=0,la=0,lb=0,eA=0,eB=0,avA=0,avB=0,avNA=0,avNB=0;rows.forEach(x=>{const rev=n(x.player1)!==n(A);const sa=rev?x.score2:x.score1,sb=rev?x.score1:x.score2;la+=sa;lb+=sb;if(n(x.winner)===n(A))wa++;else if(n(x.winner)===n(B))wb++;eA+=rev?x.oneEighties2:x.oneEighties1;eB+=rev?x.oneEighties1:x.oneEighties2;const aa=rev?x.avg2:x.avg1,ab=rev?x.avg1:x.avg2;if(aa){avA+=aa;avNA++}if(ab){avB+=ab;avNB++}});const avg=(v,c)=>c?(v/c).toFixed(2):'–';out.textContent=A+' vs. '+B+' · '+rows.length+' Spiele · Siege '+wa+':'+wb+' · Legs '+la+':'+lb+' · Ø AVG '+avg(avA,avNA)+' : '+avg(avB,avNB)+' · 180er '+eA+':'+eB}a.addEventListener('change',render);b.addEventListener('change',render);if(s)s.addEventListener('change',render);render()})();</script></div>`)
}

func writeHealthHTML(b *strings.Builder, h LeagueHealth) {
	cl := "bad"
	txt := "Prüfung erforderlich"
	if h.OK {
		cl = "ok"
		txt = "Liga-System OK"
	}
	b.WriteString(`<div class="panel"><div class="health-head"><h2 style="margin:0">🛡️ System-Gesundheit</h2><span class="health-pill ` + cl + `">` + htmlE(txt) + `</span></div><div class="health-list">`)
	for _, c := range h.Checks {
		ico := "⚠"
		if c.OK {
			ico = "✓"
		}
		fmt.Fprintf(b, `<div class="health-row"><b>%s</b><strong>%s</strong><span class="muted">%s</span></div>`, ico, htmlE(c.Label), htmlE(c.Detail))
	}
	b.WriteString(`</div></div>`)
}

func writeSeasonSummaryHTML(path string, ov LeagueOverview, st *FinalState, history LeagueHistory) error {
	if st == nil || st.Status != "completed" {
		return nil
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Saisonabschluss · ` + htmlE(ov.SeasonName) + `</title><style>body{font-family:system-ui;background:#08111b;color:#edf6ff;margin:0}.wrap{max-width:980px;margin:auto;padding:28px 18px}.hero{border:1px solid #d5a93c;background:linear-gradient(135deg,#38270d,#102033);border-radius:18px;padding:24px;text-align:center}.hero h1{margin:6px}.panel{margin-top:16px;border:1px solid #29435b;background:#0c1724;border-radius:14px;padding:16px}table{width:100%;border-collapse:collapse}td,th{padding:8px;border-bottom:1px solid #263b50;text-align:left}.award-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:10px}.award{border:1px solid #31485e;border-radius:10px;padding:10px}@media(max-width:650px){.award-grid{grid-template-columns:1fr}}</style></head><body><div class="wrap">`)
	fmt.Fprintf(&b, `<div class="hero"><div style="font-size:36px">🏆</div><h1>%s</h1><div>Saisonmeister</div><h2>%s</h2></div>`, htmlE(ov.SeasonName), htmlE(st.Champion))
	places := finalPlacements(st)
	if len(places) == 6 {
		b.WriteString(`<div class="panel"><h2>Abschlusstabelle</h2><table><tbody>`)
		for i, p := range places {
			fmt.Fprintf(&b, `<tr><td>%d.</td><td><b>%s</b></td></tr>`, i+1, htmlE(p))
		}
		b.WriteString(`</tbody></table></div>`)
	}
	b.WriteString(`<div class="panel"><h2>Finalrunde</h2><table><thead><tr><th>Spiel</th><th>Begegnung</th><th>Ergebnis</th><th>Sieger</th></tr></thead><tbody>`)
	for _, f := range st.Fixtures {
		result := "–"
		if f.Walkover {
			result = "WO"
		} else if len(f.Score) >= 2 {
			result = fmt.Sprintf("%d:%d", f.Score[0], f.Score[1])
		}
		fmt.Fprintf(&b, `<tr><td>%d. %s</td><td><b>%s</b> – <b>%s</b></td><td>%s</td><td>%s</td></tr>`, f.Order, htmlE(f.Label), htmlE(f.Player1), htmlE(f.Player2), htmlE(result), htmlE(f.Winner))
	}
	b.WriteString(`</tbody></table></div>`)
	b.WriteString(`<div class="panel"><h2>Saison-Awards</h2><div class="award-grid">`)
	for _, a := range st.SeasonAwards {
		fmt.Fprintf(&b, `<div class="award"><b>%s %s</b><div style="font-size:20px;margin:5px 0">%s</div><small>%s</small></div>`, htmlE(a.Emoji), htmlE(a.Title), htmlE(a.Winner), htmlE(a.Value))
	}
	b.WriteString(`</div></div>`)
	b.WriteString(`<div class="panel"><h2>Saison in Zahlen</h2>`)
	fmt.Fprintf(&b, `<p>%d Ligaspiele · %d Spieler · Finalrunde mit 5 Spielen</p>`, len(ov.Matches), len(ov.Players))
	if len(ov.Players) > 0 {
		bestAvg, bestHF, best180 := ov.Players[0], ov.Players[0], ov.Players[0]
		for _, p := range ov.Players[1:] {
			if p.Average > bestAvg.Average {
				bestAvg = p
			}
			if p.HighFinish > bestHF.HighFinish {
				bestHF = p
			}
			if p.OneEighties > best180.OneEighties {
				best180 = p
			}
		}
		b.WriteString(`<ul>`)
		fmt.Fprintf(&b, `<li><b>Bester Saison-Average:</b> %.2f · %s</li>`, bestAvg.Average, htmlE(bestAvg.Name))
		fmt.Fprintf(&b, `<li><b>Höchstes Saison-Finish:</b> %d · %s</li>`, bestHF.HighFinish, htmlE(bestHF.Name))
		fmt.Fprintf(&b, `<li><b>Meiste 180er:</b> %d · %s</li>`, best180.OneEighties, htmlE(best180.Name))
		b.WriteString(`</ul>`)
	}
	b.WriteString(`</div>`)
	if len(history.Records) > 0 {
		b.WriteString(`<div class="panel"><h2>Ewige Rekorde nach dieser Saison</h2><ul>`)
		for _, r := range history.Records {
			fmt.Fprintf(&b, `<li><b>%s:</b> %s · %s</li>`, htmlE(r.Title), htmlE(r.Value), htmlE(r.Player))
		}
		b.WriteString(`</ul></div>`)
	}
	b.WriteString(`</div></body></html>`)
	return writeAtomic(path, []byte(b.String()), 0644)
}

// API response used by the add-on health box.
func (s *Server) handleLeagueHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		methodNotAllowed(w)
		return
	}
	league := strings.TrimSpace(r.URL.Query().Get("league"))
	reg, err := s.ensureSeasonRegistry(league)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	season, _ := currentSeason(reg)
	raw, err := s.collectOverviewMatches(league, season.ID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ov := makeLeagueOverview(league, season.ID, season.Name, true, raw)
	rows, _ := readScheduleCSV(filepath.Join(s.basePath(), "Spielplan.csv"))
	st, _ := s.ensureFinalStateForOverview(ov)
	h := buildLeagueHealth(s, ov, rows, st)
	writeJSON(w, 200, map[string]any{"ok": true, "health": h})
}

// Used in tests and diagnostics.
func careerPlayer(history LeagueHistory, name string) (CareerPlayer, bool) {
	k := keyName(name)
	for _, p := range history.Players {
		if keyName(p.Name) == k {
			return p, true
		}
	}
	return CareerPlayer{}, false
}

// Ensure imported time stays useful if future record ordering changes; currently
// season registry order is authoritative for historical progression.
var _ = time.Time{}
