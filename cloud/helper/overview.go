package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type HighlightItem struct {
	Score int `json:"score,omitempty"`
	Darts int `json:"darts,omitempty"`
	Leg   int `json:"leg"`
}

type PlayerHighlights struct {
	HighScores    []HighlightItem `json:"highScores,omitempty"`
	HighCheckouts []HighlightItem `json:"highCheckouts,omitempty"`
	ShortLegs     []HighlightItem `json:"shortLegs,omitempty"`
}

type OverviewPlayer struct {
	Name          string                 `json:"name"`
	AutodartsName string                 `json:"autodartsName"`
	RealName      string                 `json:"realName"`
	Wins          int                    `json:"wins"`
	Metrics       map[string]interface{} `json:"metrics"`
	Highlights    PlayerHighlights       `json:"highlights"`
}

type OverviewMatch struct {
	MatchID    string           `json:"matchId"`
	SeasonID   string           `json:"seasonId,omitempty"`
	SeasonName string           `json:"seasonName,omitempty"`
	SavedAt    string           `json:"savedAt"`
	MatchDate  string           `json:"matchDate"`
	Variant    string           `json:"variant"`
	Winner     int              `json:"winner"`
	Score      []int            `json:"score"`
	Players    []OverviewPlayer `json:"players"`
	Matchday   int              `json:"matchday"`
	UsageMode  string           `json:"usageMode"`
}

type PlayerOverview struct {
	Name             string
	Aliases          []string
	Matches          int
	Wins             int
	Losses           int
	LegsFor          int
	LegsAgainst      int
	LegDiff          int
	Average          float64
	AverageCount     int
	CheckoutPct      float64
	CheckoutHits     int
	CheckoutAttempts int
	Scores95Plus     int
	Scores114Plus    int
	Scores133Plus    int
	Scores171        int
	Scores174        int
	Scores177        int
	OneEighties      int
	ScoringComplete  bool
	HighFinish       int
	Darts            int
}

type MatchOverview struct {
	MatchID           string
	Matchday          int
	Date              time.Time
	DateRaw           string
	Variant           string
	Player1           string
	Player2           string
	Alias1            string
	Alias2            string
	Score1            int
	Score2            int
	Winner            string
	Avg1              *float64
	Avg2              *float64
	First91           *float64
	First92           *float64
	Checkout1         *float64
	Checkout2         *float64
	CheckoutHits1     int
	CheckoutHits2     int
	CheckoutAttempts1 int
	CheckoutAttempts2 int
	Scores57P1        int
	Scores57P2        int
	Scores95P1        int
	Scores95P2        int
	Scores114P1       int
	Scores114P2       int
	Scores133P1       int
	Scores133P2       int
	Scores171P1       int
	Scores171P2       int
	Scores174P1       int
	Scores174P2       int
	Scores177P1       int
	Scores177P2       int
	OneEighties1      int
	OneEighties2      int
	HF1               int
	HF2               int
	Darts1            int
	Darts2            int
	Highlights1       PlayerHighlights
	Highlights2       PlayerHighlights
	ScoringComplete1  bool
	ScoringComplete2  bool
}

type LeagueOverview struct {
	LeagueName string
	SeasonID   string
	SeasonName string
	IsCurrent  bool
	Generated  time.Time
	Matches    []MatchOverview
	Players    []PlayerOverview
	DayCounts  [10]int
}

func cleanPlayerName(v string) string { return strings.Join(strings.Fields(strings.TrimSpace(v)), " ") }
func canonicalPlayer(p OverviewPlayer, fallback string) (name, alias string) {
	alias = cleanPlayerName(p.AutodartsName)
	if alias == "" {
		alias = cleanPlayerName(p.Name)
	}
	if alias == "" {
		alias = fallback
	}
	name = cleanPlayerName(p.RealName)
	if name == "" {
		name = alias
	}
	return name, alias
}
func keyName(v string) string { return strings.ToLower(cleanPlayerName(v)) }

func metricFloat(metrics map[string]interface{}, key string) (float64, bool) {
	if metrics == nil {
		return 0, false
	}
	v, ok := metrics[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case json.Number:
		x, err := n.Float64()
		return x, err == nil
	case string:
		x, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(n), ",", "."), 64)
		return x, err == nil
	default:
		return 0, false
	}
}
func metricInt(metrics map[string]interface{}, key string) int {
	if n, ok := metricFloat(metrics, key); ok {
		return int(math.Round(n))
	}
	return 0
}

func scoreOrDash(complete bool, value int) string {
	if complete {
		return strconv.Itoa(value)
	}
	return "–"
}
func scoreCell(complete bool, value int) xCell {
	if complete {
		return xn(value, 4)
	}
	return xs("–", 4)
}

func parseAnyTime(vals ...string) time.Time {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (s *Server) overviewDir() string {
	base := s.basePath()
	if base == "" {
		return ""
	}
	if strings.EqualFold(filepath.Base(filepath.Clean(base)), "Liga-Daten") {
		return filepath.Dir(filepath.Clean(base))
	}
	return filepath.Clean(base)
}

func safeOverviewPart(v string) string {
	v = cleanPlayerName(v)
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-_äöüÄÖÜß", r) {
			b.WriteRune(r)
		} else if r == ' ' {
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "Liga"
	}
	if len(out) > 70 {
		out = out[:70]
	}
	return out
}

func (s *Server) collectOverviewMatches(league, seasonID string) ([]OverviewMatch, error) {
	base := s.basePath()
	if base == "" {
		return nil, errorsNew("Liga-Datenordner ist noch nicht eingerichtet")
	}
	dir := filepath.Join(base, "Matches")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []OverviewMatch{}, nil
		}
		return nil, err
	}
	leagueNorm := strings.ToLower(strings.TrimSpace(league))
	deleted := s.readDeletedIDs(league)
	type candidate struct {
		Match OverviewMatch
		TS    time.Time
	}
	byID := map[string]candidate{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var mf MatchFile
		if json.Unmarshal(b, &mf) != nil || mf.Format != "autodarts-match-analytics-league-match" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(mf.League.Name)) != leagueNorm {
			continue
		}
		var m OverviewMatch
		if json.Unmarshal(mf.Match, &m) != nil || !looksLikeUUID(m.MatchID) || m.UsageMode != "league" || m.Matchday < 1 || m.Matchday > 10 {
			continue
		}
		if strings.TrimSpace(m.SeasonID) != strings.TrimSpace(seasonID) {
			continue
		}
		key := strings.ToLower(m.MatchID)
		if deleted[key] {
			continue
		}
		ts := newestTimestamp(mf.UpdatedAt, m.SavedAt, m.MatchDate)
		if prev, ok := byID[key]; !ok || ts.After(prev.TS) {
			byID[key] = candidate{Match: m, TS: ts}
		}
	}
	out := make([]OverviewMatch, 0, len(byID))
	for _, c := range byID {
		out = append(out, c.Match)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Matchday != out[j].Matchday {
			return out[i].Matchday < out[j].Matchday
		}
		ti, tj := parseAnyTime(out[i].MatchDate, out[i].SavedAt), parseAnyTime(out[j].MatchDate, out[j].SavedAt)
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return out[i].MatchID < out[j].MatchID
	})
	return out, nil
}

func errorsNew(s string) error { return fmt.Errorf("%s", s) }

func (s *Server) knownLeagueNames() []string {
	set := map[string]string{}
	base := s.basePath()
	if base == "" {
		return nil
	}
	for _, dirName := range []string{"Matches", "Deleted"} {
		entries, _ := os.ReadDir(filepath.Join(base, dirName))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(base, dirName, e.Name()))
			if err != nil {
				continue
			}
			if dirName == "Matches" {
				var mf MatchFile
				if json.Unmarshal(b, &mf) == nil && strings.TrimSpace(mf.League.Name) != "" {
					set[strings.ToLower(strings.TrimSpace(mf.League.Name))] = strings.TrimSpace(mf.League.Name)
				}
			} else {
				var dm DeleteMarker
				if json.Unmarshal(b, &dm) == nil && strings.TrimSpace(dm.League.Name) != "" {
					set[strings.ToLower(strings.TrimSpace(dm.League.Name))] = strings.TrimSpace(dm.League.Name)
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for _, v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func makeLeagueOverview(league, seasonID, seasonName string, isCurrent bool, raw []OverviewMatch) LeagueOverview {
	ov := LeagueOverview{LeagueName: league, SeasonID: seasonID, SeasonName: seasonName, IsCurrent: isCurrent, Generated: time.Now()}
	type acc struct {
		P        PlayerOverview
		aliasSet map[string]bool
		avgSum   float64
	}
	players := map[string]*acc{}
	for _, m := range raw {
		mo := MatchOverview{MatchID: m.MatchID, Matchday: m.Matchday, Date: parseAnyTime(m.MatchDate, m.SavedAt), DateRaw: m.MatchDate, Variant: m.Variant}
		if m.Matchday >= 1 && m.Matchday <= 10 {
			ov.DayCounts[m.Matchday-1]++
		}
		names := make([]string, len(m.Players))
		aliases := make([]string, len(m.Players))
		for i, p := range m.Players {
			name, alias := canonicalPlayer(p, fmt.Sprintf("Spieler %d", i+1))
			names[i], aliases[i] = name, alias
			key := keyName(name)
			a := players[key]
			if a == nil {
				a = &acc{P: PlayerOverview{Name: name}, aliasSet: map[string]bool{}}
				players[key] = a
			}
			if alias != "" && keyName(alias) != key {
				a.aliasSet[alias] = true
			}
			a.P.Matches++
			if m.Winner == i {
				a.P.Wins++
			} else if m.Winner >= 0 {
				a.P.Losses++
			}
			if i < len(m.Score) {
				a.P.LegsFor += m.Score[i]
			}
			for j, v := range m.Score {
				if j != i {
					a.P.LegsAgainst += v
				}
			}
			if v, ok := metricFloat(p.Metrics, "average"); ok {
				a.avgSum += v
				a.P.AverageCount++
			}
			// Liga-Checkoutquote als Gesamtquote: erfolgreiche Checkouts / alle Checkout-Versuche.
			hits, hitsOK := metricFloat(p.Metrics, "checkoutHits")
			if !hitsOK {
				hits = float64(p.Wins) // gewonnene Legs entsprechen erfolgreichen Checkouts
			}
			attempts, attemptsOK := metricFloat(p.Metrics, "checkoutAttempts")
			if !attemptsOK {
				// Fallback fuer aeltere Matchdateien ohne explizite Versuchszahl.
				if pct, ok := metricFloat(p.Metrics, "checkoutPct"); ok {
					if math.Abs(pct) <= 1 {
						pct *= 100
					}
					if pct > 0 {
						attempts = math.Round(hits * 100 / pct)
						attemptsOK = true
					}
				}
			}
			if attemptsOK && attempts >= 0 {
				if hits < 0 {
					hits = 0
				}
				if attempts < hits {
					attempts = hits
				}
				a.P.CheckoutHits += int(math.Round(hits))
				a.P.CheckoutAttempts += int(math.Round(attempts))
			}
			_, has95 := metricFloat(p.Metrics, "scores95Plus")
			_, has114 := metricFloat(p.Metrics, "scores114Plus")
			_, has133 := metricFloat(p.Metrics, "scores133Plus")
			_, has171 := metricFloat(p.Metrics, "scores171")
			_, has174 := metricFloat(p.Metrics, "scores174")
			_, has177 := metricFloat(p.Metrics, "scores177")
			_, has180 := metricFloat(p.Metrics, "oneEighties")
			if a.P.Matches == 1 {
				a.P.ScoringComplete = true
			}
			if !has95 || !has114 || !has133 || !has171 || !has174 || !has177 || !has180 {
				a.P.ScoringComplete = false
			}
			a.P.Scores95Plus += metricInt(p.Metrics, "scores95Plus")
			a.P.Scores114Plus += metricInt(p.Metrics, "scores114Plus")
			a.P.Scores133Plus += metricInt(p.Metrics, "scores133Plus")
			a.P.Scores171 += metricInt(p.Metrics, "scores171")
			a.P.Scores174 += metricInt(p.Metrics, "scores174")
			a.P.Scores177 += metricInt(p.Metrics, "scores177")
			a.P.OneEighties += metricInt(p.Metrics, "oneEighties")
			a.P.Darts += metricInt(p.Metrics, "dartsThrown")
			if hf := metricInt(p.Metrics, "highestCheckout"); hf > a.P.HighFinish {
				a.P.HighFinish = hf
			}
		}
		if len(names) > 0 {
			mo.Player1, mo.Alias1 = names[0], aliases[0]
			if len(m.Score) > 0 {
				mo.Score1 = m.Score[0]
			}
			if len(m.Players) > 0 {
				if v, ok := metricFloat(m.Players[0].Metrics, "average"); ok {
					mo.Avg1 = &v
				}
				if v, ok := metricFloat(m.Players[0].Metrics, "first9"); ok {
					mo.First91 = &v
				}
				if v, ok := metricFloat(m.Players[0].Metrics, "checkoutPct"); ok {
					mo.Checkout1 = &v
				}
				mo.CheckoutHits1 = metricInt(m.Players[0].Metrics, "checkoutHits")
				if mo.CheckoutHits1 == 0 && m.Players[0].Wins > 0 {
					mo.CheckoutHits1 = m.Players[0].Wins
				}
				mo.CheckoutAttempts1 = metricInt(m.Players[0].Metrics, "checkoutAttempts")
				_, c57 := metricFloat(m.Players[0].Metrics, "scores57Plus")
				_, c95 := metricFloat(m.Players[0].Metrics, "scores95Plus")
				_, c114 := metricFloat(m.Players[0].Metrics, "scores114Plus")
				_, c133 := metricFloat(m.Players[0].Metrics, "scores133Plus")
				_, c171 := metricFloat(m.Players[0].Metrics, "scores171")
				_, c174 := metricFloat(m.Players[0].Metrics, "scores174")
				_, c177 := metricFloat(m.Players[0].Metrics, "scores177")
				_, c180 := metricFloat(m.Players[0].Metrics, "oneEighties")
				mo.ScoringComplete1 = c57 && c95 && c114 && c133 && c171 && c174 && c177 && c180
				mo.Scores57P1 = metricInt(m.Players[0].Metrics, "scores57Plus")
				mo.Scores95P1 = metricInt(m.Players[0].Metrics, "scores95Plus")
				mo.Scores114P1 = metricInt(m.Players[0].Metrics, "scores114Plus")
				mo.Scores133P1 = metricInt(m.Players[0].Metrics, "scores133Plus")
				mo.Scores171P1 = metricInt(m.Players[0].Metrics, "scores171")
				mo.Scores174P1 = metricInt(m.Players[0].Metrics, "scores174")
				mo.Scores177P1 = metricInt(m.Players[0].Metrics, "scores177")
				mo.OneEighties1 = metricInt(m.Players[0].Metrics, "oneEighties")
				mo.HF1 = metricInt(m.Players[0].Metrics, "highestCheckout")
				mo.Darts1 = metricInt(m.Players[0].Metrics, "dartsThrown")
				mo.Highlights1 = m.Players[0].Highlights
			}
		}
		if len(names) > 1 {
			mo.Player2, mo.Alias2 = names[1], aliases[1]
			if len(m.Score) > 1 {
				mo.Score2 = m.Score[1]
			}
			if len(m.Players) > 1 {
				if v, ok := metricFloat(m.Players[1].Metrics, "average"); ok {
					mo.Avg2 = &v
				}
				if v, ok := metricFloat(m.Players[1].Metrics, "first9"); ok {
					mo.First92 = &v
				}
				if v, ok := metricFloat(m.Players[1].Metrics, "checkoutPct"); ok {
					mo.Checkout2 = &v
				}
				mo.CheckoutHits2 = metricInt(m.Players[1].Metrics, "checkoutHits")
				if mo.CheckoutHits2 == 0 && m.Players[1].Wins > 0 {
					mo.CheckoutHits2 = m.Players[1].Wins
				}
				mo.CheckoutAttempts2 = metricInt(m.Players[1].Metrics, "checkoutAttempts")
				_, c57 := metricFloat(m.Players[1].Metrics, "scores57Plus")
				_, c95 := metricFloat(m.Players[1].Metrics, "scores95Plus")
				_, c114 := metricFloat(m.Players[1].Metrics, "scores114Plus")
				_, c133 := metricFloat(m.Players[1].Metrics, "scores133Plus")
				_, c171 := metricFloat(m.Players[1].Metrics, "scores171")
				_, c174 := metricFloat(m.Players[1].Metrics, "scores174")
				_, c177 := metricFloat(m.Players[1].Metrics, "scores177")
				_, c180 := metricFloat(m.Players[1].Metrics, "oneEighties")
				mo.ScoringComplete2 = c57 && c95 && c114 && c133 && c171 && c174 && c177 && c180
				mo.Scores57P2 = metricInt(m.Players[1].Metrics, "scores57Plus")
				mo.Scores95P2 = metricInt(m.Players[1].Metrics, "scores95Plus")
				mo.Scores114P2 = metricInt(m.Players[1].Metrics, "scores114Plus")
				mo.Scores133P2 = metricInt(m.Players[1].Metrics, "scores133Plus")
				mo.Scores171P2 = metricInt(m.Players[1].Metrics, "scores171")
				mo.Scores174P2 = metricInt(m.Players[1].Metrics, "scores174")
				mo.Scores177P2 = metricInt(m.Players[1].Metrics, "scores177")
				mo.OneEighties2 = metricInt(m.Players[1].Metrics, "oneEighties")
				mo.HF2 = metricInt(m.Players[1].Metrics, "highestCheckout")
				mo.Darts2 = metricInt(m.Players[1].Metrics, "dartsThrown")
				mo.Highlights2 = m.Players[1].Highlights
			}
		}
		if m.Winner >= 0 && m.Winner < len(names) {
			mo.Winner = names[m.Winner]
		}
		ov.Matches = append(ov.Matches, mo)
	}
	for _, a := range players {
		a.P.LegDiff = a.P.LegsFor - a.P.LegsAgainst
		if a.P.AverageCount > 0 {
			a.P.Average = a.avgSum / float64(a.P.AverageCount)
		}
		if a.P.CheckoutAttempts > 0 {
			a.P.CheckoutPct = float64(a.P.CheckoutHits) / float64(a.P.CheckoutAttempts) * 100
		}
		for alias := range a.aliasSet {
			a.P.Aliases = append(a.P.Aliases, alias)
		}
		sort.Strings(a.P.Aliases)
		ov.Players = append(ov.Players, a.P)
	}
	sort.Slice(ov.Players, func(i, j int) bool {
		a, b := ov.Players[i], ov.Players[j]
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		if a.LegDiff != b.LegDiff {
			return a.LegDiff > b.LegDiff
		}
		if a.Average != b.Average {
			return a.Average > b.Average
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return ov
}

func (s *Server) generateOverviewForLeague(league string) {
	league = strings.TrimSpace(league)
	if league == "" {
		return
	}
	s.overviewMu.Lock()
	defer s.overviewMu.Unlock()
	reg, err := s.ensureSeasonRegistry(league)
	if err != nil {
		log.Printf("Liga-Saisons: %v", err)
		return
	}
	dir := s.overviewDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	var currentHTML, currentXLSX string
	for _, season := range reg.Seasons {
		raw, err := s.collectOverviewMatches(league, season.ID)
		if err != nil {
			log.Printf("Liga-Übersicht %s: %v", season.Name, err)
			continue
		}
		ov := makeLeagueOverview(league, season.ID, season.Name, season.ID == reg.CurrentSeasonID, raw)
		htmlPath := filepath.Join(dir, "season-"+safeOverviewPart(season.ID)+".html")
		xlsxPath := filepath.Join(dir, "Liga-Übersicht_"+safeOverviewPart(season.Name)+".xlsx")
		if err := writeHTMLOverview(htmlPath, ov, reg.Seasons, reg.CurrentSeasonID); err != nil {
			log.Printf("HTML-Übersicht %s: %v", season.Name, err)
		}
		if err := writeXLSXOverview(xlsxPath, ov); err != nil {
			log.Printf("Excel-Übersicht %s: %v", season.Name, err)
		}
		if season.ID == reg.CurrentSeasonID {
			currentHTML, currentXLSX = htmlPath, xlsxPath
		}
	}
	if currentHTML != "" {
		_ = copyFileAtomic(currentHTML, filepath.Join(dir, "Liga-Übersicht.html"))
	}
	if currentXLSX != "" {
		_ = copyFileAtomic(currentXLSX, filepath.Join(dir, "Liga-Übersicht.xlsx"))
	}
	readme := filepath.Join(dir, "LIGA_UEBERSICHT_LESEN.txt")
	_ = os.WriteFile(readme, []byte("AUTODARTS LIGA – SAISONÜBERSICHT OHNE ADD-ON\n\nLiga-Übersicht.xlsx  -> aktive Saison in OneDrive / Excel Online / Excel\nLiga-Übersicht.html  -> aktive Saison im Browser\nseason-*.html        -> archivierte Saisons\n\nBeim Saisonwechsel bleiben alle alten Matchdaten erhalten. Die Webansicht bietet einen Saisonwähler.\nIst GitHub Pages eingerichtet, werden aktive und archivierte Saisonseiten automatisch veröffentlicht.\n"), 0o644)
	if currentHTML != "" {
		if err := s.publishOverviewHTML(filepath.Join(dir, "Liga-Übersicht.html")); err != nil {
			log.Printf("GitHub-Pages-Veröffentlichung: %v", err)
		}
	}
}
func copyFileAtomic(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeAtomic(dst, b, 0o644)
}
func writeAtomic(path string, b []byte, mode os.FileMode) error {
	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Server) overviewSignature() string {
	base := s.basePath()
	if base == "" {
		return ""
	}
	var parts []string
	for _, d := range []string{"Matches", "Deleted"} {
		entries, _ := os.ReadDir(filepath.Join(base, d))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				continue
			}
			if st, err := e.Info(); err == nil {
				parts = append(parts, d+"/"+e.Name()+":"+strconv.FormatInt(st.Size(), 10)+":"+strconv.FormatInt(st.ModTime().UnixNano(), 10))
			}
		}
	}
	if st, err := os.Stat(filepath.Join(base, "Seasons.json")); err == nil {
		parts = append(parts, "Seasons.json:"+strconv.FormatInt(st.Size(), 10)+":"+strconv.FormatInt(st.ModTime().UnixNano(), 10))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}
func (s *Server) overviewLoop() {
	var last string
	timer := time.NewTimer(5 * time.Second)
	<-timer.C
	for {
		base := s.basePath()
		if base != "" && inspectSyncPath(base).SyncReady {
			sig := s.overviewSignature()
			if sig != last {
				for _, league := range s.knownLeagueNames() {
					s.generateOverviewForLeague(league)
				}
				last = sig
			}
		}
		time.Sleep(30 * time.Second)
	}
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if r.Method == http.MethodPost {
		for _, league := range s.knownLeagueNames() {
			s.generateOverviewForLeague(league)
		}
	}
	dir := s.overviewDir()
	htmlPath, xlsxPath := "", ""
	if dir != "" {
		htmlPath = filepath.Join(dir, "Liga-Übersicht.html")
		xlsxPath = filepath.Join(dir, "Liga-Übersicht.xlsx")
	}
	hInfo, hErr := os.Stat(htmlPath)
	xInfo, xErr := os.Stat(xlsxPath)
	updated := ""
	if hErr == nil {
		updated = hInfo.ModTime().Format(time.RFC3339)
	}
	if xErr == nil && (updated == "" || xInfo.ModTime().After(hInfo.ModTime())) {
		updated = xInfo.ModTime().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "htmlPath": htmlPath, "xlsxPath": xlsxPath, "htmlExists": hErr == nil, "xlsxExists": xErr == nil, "updatedAt": updated})
}

func formatDateDE(t time.Time) string {
	if t.IsZero() {
		return "–"
	}
	return t.Local().Format("02.01.2006 15:04")
}
func fmtFloatPtr(p *float64) string {
	if p == nil {
		return "–"
	}
	return strings.ReplaceAll(fmt.Sprintf("%.2f", *p), ".", ",")
}
func fmtCheckoutPct(v float64, attempts int) string {
	if attempts <= 0 {
		return "–"
	}
	return strings.ReplaceAll(fmt.Sprintf("%.1f %%", v), ".", ",")
}
func htmlE(v string) string { return htmlEscape(v) }

func validWebHighlights(h PlayerHighlights) PlayerHighlights {
	out := PlayerHighlights{}
	for _, x := range h.HighScores {
		if x.Score > 170 && x.Leg > 0 {
			out.HighScores = append(out.HighScores, x)
		}
	}
	for _, x := range h.HighCheckouts {
		if x.Score > 100 && x.Leg > 0 {
			out.HighCheckouts = append(out.HighCheckouts, x)
		}
	}
	for _, x := range h.ShortLegs {
		if x.Darts > 0 && x.Darts <= 24 && x.Leg > 0 {
			out.ShortLegs = append(out.ShortLegs, x)
		}
	}
	return out
}

func hasWebHighlights(h PlayerHighlights) bool {
	h = validWebHighlights(h)
	return len(h.HighScores) > 0 || len(h.HighCheckouts) > 0 || len(h.ShortLegs) > 0
}

func writeHighlightPlayerHTML(b *strings.Builder, name string, h PlayerHighlights) {
	h = validWebHighlights(h)
	fmt.Fprintf(b, `<div class="highlight-player"><div class="highlight-player-name">%s</div>`, htmlE(name))
	if len(h.HighScores) > 0 {
		b.WriteString(`<div class="highlight-line"><span class="highlight-kind">Score &gt; 170</span>`)
		for _, x := range h.HighScores {
			fmt.Fprintf(b, `<span class="highlight-pill score">%d · Leg %d</span>`, x.Score, x.Leg)
		}
		b.WriteString(`</div>`)
	}
	if len(h.HighCheckouts) > 0 {
		b.WriteString(`<div class="highlight-line"><span class="highlight-kind">Checkout &gt; 100</span>`)
		for _, x := range h.HighCheckouts {
			fmt.Fprintf(b, `<span class="highlight-pill finish">%d · Leg %d</span>`, x.Score, x.Leg)
		}
		b.WriteString(`</div>`)
	}
	if len(h.ShortLegs) > 0 {
		b.WriteString(`<div class="highlight-line"><span class="highlight-kind">Short Leg</span>`)
		for _, x := range h.ShortLegs {
			fmt.Fprintf(b, `<span class="highlight-pill short">%d Darts · Leg %d</span>`, x.Darts, x.Leg)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
}

type SeasonHighlightItem struct {
	Value     int
	Leg       int
	Matchday  int
	Opponent  string
	MatchDate time.Time
}

type SeasonPlayerHighlights struct {
	Name          string
	HighScores    []SeasonHighlightItem
	HighCheckouts []SeasonHighlightItem
	ShortLegs     []SeasonHighlightItem
}

func collectSeasonHighlights(ov LeagueOverview) []SeasonPlayerHighlights {
	byKey := map[string]*SeasonPlayerHighlights{}
	for _, p := range ov.Players {
		key := keyName(p.Name)
		if key == "" {
			continue
		}
		byKey[key] = &SeasonPlayerHighlights{Name: p.Name}
	}
	add := func(name, opponent string, day int, date time.Time, h PlayerHighlights) {
		key := keyName(name)
		if key == "" {
			return
		}
		sp := byKey[key]
		if sp == nil {
			sp = &SeasonPlayerHighlights{Name: cleanPlayerName(name)}
			byKey[key] = sp
		}
		h = validWebHighlights(h)
		for _, x := range h.HighScores {
			sp.HighScores = append(sp.HighScores, SeasonHighlightItem{Value: x.Score, Leg: x.Leg, Matchday: day, Opponent: opponent, MatchDate: date})
		}
		for _, x := range h.HighCheckouts {
			sp.HighCheckouts = append(sp.HighCheckouts, SeasonHighlightItem{Value: x.Score, Leg: x.Leg, Matchday: day, Opponent: opponent, MatchDate: date})
		}
		for _, x := range h.ShortLegs {
			sp.ShortLegs = append(sp.ShortLegs, SeasonHighlightItem{Value: x.Darts, Leg: x.Leg, Matchday: day, Opponent: opponent, MatchDate: date})
		}
	}
	for _, m := range ov.Matches {
		add(m.Player1, m.Player2, m.Matchday, m.Date, m.Highlights1)
		add(m.Player2, m.Player1, m.Matchday, m.Date, m.Highlights2)
	}
	out := make([]SeasonPlayerHighlights, 0, len(byKey))
	for _, sp := range byKey {
		sort.SliceStable(sp.HighScores, func(i, j int) bool {
			if sp.HighScores[i].Value != sp.HighScores[j].Value {
				return sp.HighScores[i].Value > sp.HighScores[j].Value
			}
			return sp.HighScores[i].MatchDate.Before(sp.HighScores[j].MatchDate)
		})
		sort.SliceStable(sp.HighCheckouts, func(i, j int) bool {
			if sp.HighCheckouts[i].Value != sp.HighCheckouts[j].Value {
				return sp.HighCheckouts[i].Value > sp.HighCheckouts[j].Value
			}
			return sp.HighCheckouts[i].MatchDate.Before(sp.HighCheckouts[j].MatchDate)
		})
		sort.SliceStable(sp.ShortLegs, func(i, j int) bool {
			if sp.ShortLegs[i].Value != sp.ShortLegs[j].Value {
				return sp.ShortLegs[i].Value < sp.ShortLegs[j].Value
			}
			return sp.ShortLegs[i].MatchDate.Before(sp.ShortLegs[j].MatchDate)
		})
		out = append(out, *sp)
	}
	// Gleiche Reihenfolge wie Ligatabelle beibehalten.
	pos := map[string]int{}
	for i, p := range ov.Players {
		pos[keyName(p.Name)] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, iok := pos[keyName(out[i].Name)]
		pj, jok := pos[keyName(out[j].Name)]
		if iok && jok {
			return pi < pj
		}
		if iok {
			return true
		}
		if jok {
			return false
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func seasonHighlightLabel(x SeasonHighlightItem, kind string) string {
	prefix := strconv.Itoa(x.Value)
	if kind == "short" {
		prefix = fmt.Sprintf("%d Darts", x.Value)
	}
	where := fmt.Sprintf("Leg %d", x.Leg)
	if x.Matchday >= 1 && x.Matchday <= 10 {
		where = fmt.Sprintf("ST%d · Leg %d", x.Matchday, x.Leg)
	}
	return prefix + " · " + where
}

func writeSeasonHighlightCell(b *strings.Builder, items []SeasonHighlightItem, kind string) {
	if len(items) == 0 {
		b.WriteString(`<span class="season-none">–</span>`)
		return
	}
	fmt.Fprintf(b, `<div class="season-count">%d×</div><div class="season-pills">`, len(items))
	for _, x := range items {
		fmt.Fprintf(b, `<span class="season-pill %s" title="gegen %s">%s</span>`, kind, htmlE(x.Opponent), htmlE(seasonHighlightLabel(x, kind)))
	}
	b.WriteString(`</div>`)
}

func writeSeasonHighlightsHTML(b *strings.Builder, ov LeagueOverview) {
	rows := collectSeasonHighlights(ov)
	b.WriteString(`<div class="panel season-highlights"><h2>Saison-Highlights</h2><div class="muted season-note">Alle Highlights der bisherigen Liga-Saison. Kriterien: Score &gt; 170, Checkout &gt; 100 und Short Leg ≤ 24 Darts.</div><table class="season-table"><thead><tr><th>Spieler</th><th>Scores &gt; 170</th><th>Checkouts &gt; 100</th><th>Short Legs ≤ 24</th></tr></thead><tbody>`)
	for _, p := range rows {
		fmt.Fprintf(b, `<tr><td><b>%s</b></td><td>`, htmlE(p.Name))
		writeSeasonHighlightCell(b, p.HighScores, "score")
		b.WriteString(`</td><td>`)
		writeSeasonHighlightCell(b, p.HighCheckouts, "finish")
		b.WriteString(`</td><td>`)
		writeSeasonHighlightCell(b, p.ShortLegs, "short")
		b.WriteString(`</td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
}

func writeHTMLOverview(path string, ov LeagueOverview, seasons []SeasonInfo, currentSeasonID string) error {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Autodarts Liga</title><style>`)
	b.WriteString(`:root{color-scheme:dark}*{box-sizing:border-box}body{margin:0;background:#07111f;color:#eef6ff;font-family:Inter,system-ui,-apple-system,Segoe UI,sans-serif}.wrap{max-width:1180px;margin:auto;padding:22px}.head{display:flex;justify-content:space-between;gap:18px;align-items:end;flex-wrap:wrap}.eyebrow{font-size:12px;letter-spacing:.16em;color:#77b9ff;font-weight:800}.head h1{margin:5px 0 4px;font-size:32px}.muted{color:#9cb0c5;font-size:13px}.cards{display:grid;grid-template-columns:repeat(4,minmax(140px,1fr));gap:12px;margin:22px 0}.card,.panel{background:#111d2b;border:1px solid #24384c;border-radius:14px}.card{padding:16px}.card strong{display:block;font-size:26px}.card span{font-size:12px;color:#9cb0c5}.panel{padding:18px;margin:14px 0;overflow:auto}h2{font-size:18px;margin:0 0 14px}table{width:100%;border-collapse:collapse;min-width:760px}th,td{border-bottom:1px solid #24384c;padding:10px 9px;text-align:center;font-variant-numeric:tabular-nums}th{font-size:11px;color:#8ec4ff;text-transform:uppercase;letter-spacing:.08em}td:first-child,th:first-child{text-align:left}.pos{color:#59e391}.neg{color:#ff7b83}.days{display:grid;grid-template-columns:repeat(10,minmax(70px,1fr));gap:8px}.day{padding:10px;border:1px solid #24384c;border-radius:10px;text-align:center}.day.has{border-color:#2b8d68;background:#102b27}.day b{display:block;font-size:20px}.matches{display:grid;gap:8px}.matchday-heading{margin-top:12px;padding:10px 12px;border:1px solid #2d5d86;border-radius:10px;background:#0d2338;display:flex;align-items:center;justify-content:space-between;gap:12px;color:#9bcfff}.matchday-heading:first-child{margin-top:0}.matchday-heading div{display:flex;align-items:baseline;gap:8px}.matchday-heading strong{font-size:20px;color:#eef6ff}.matchday-heading>span{font-size:12px;color:#9cb0c5}.matchday-kicker{font-size:10px;letter-spacing:.14em;font-weight:900;color:#5eafff}.match{display:grid;grid-template-columns:75px 150px 1fr 70px 1fr 95px;gap:8px;align-items:center;padding:10px 8px;border-bottom:1px solid #24384c}.score{font-weight:900;font-size:18px;text-align:center}.tag{font-size:11px;color:#8ec4ff}.winner{color:#59e391;font-weight:800}.match-detail{border:1px solid #24384c;border-radius:12px;overflow:hidden;background:#0d1826}.match-detail+ .match-detail{margin-top:8px}.match-detail summary{list-style:none;cursor:pointer}.match-detail summary::-webkit-details-marker{display:none}.match-detail[open]{border-color:#356b98}.match-detail[open] summary{background:#12263a}.stats-toggle{color:#8ec4ff;font-size:12px;font-weight:800;text-align:right}.detail{padding:12px;border-top:1px solid #24384c}.detail-head{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-bottom:10px}.detail-player{padding:10px;border:1px solid #24384c;border-radius:10px}.detail-player b{display:block;font-size:15px}.detail-player span{font-size:11px;color:#9cb0c5}.stat-table{display:grid;grid-template-columns:1fr 1fr 1fr;gap:1px;background:#24384c;border:1px solid #24384c;border-radius:10px;overflow:hidden}.stat-row{display:contents}.stat-cell{background:#101b28;padding:9px 8px;text-align:center;font-variant-numeric:tabular-nums}.stat-label{color:#8ec4ff;font-size:11px;font-weight:800}.stat-value{font-weight:800}.autodarts-link{display:inline-block;margin-top:10px;color:#8ec4ff;text-decoration:none;font-size:12px;font-weight:800}.highlights{margin-top:12px;padding:12px;border:1px solid #31506b;border-radius:10px;background:#0b1623}.highlights-title{font-size:12px;font-weight:900;letter-spacing:.08em;color:#ffd166;text-transform:uppercase;margin-bottom:9px}.highlight-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}.highlight-player{padding:9px;border:1px solid #24384c;border-radius:9px;background:#101b28}.highlight-player-name{font-size:12px;font-weight:800;margin-bottom:7px}.highlight-line{display:flex;flex-wrap:wrap;gap:6px;align-items:center;margin:5px 0}.highlight-kind{font-size:10px;color:#9cb0c5;min-width:84px}.highlight-pill{display:inline-block;border:1px solid #375775;background:#15273a;border-radius:999px;padding:4px 7px;font-size:11px;font-weight:800}.highlight-pill.score{border-color:#7d5b1f;background:#2a2210;color:#ffd166}.highlight-pill.finish{border-color:#7d355d;background:#2b1424;color:#ff9bd0}.highlight-pill.short{border-color:#2b7758;background:#102a21;color:#6de5ac}.season-note{margin:-6px 0 12px}.season-table{min-width:900px}.season-table td{vertical-align:top}.season-table td:first-child{white-space:nowrap}.season-count{font-size:11px;font-weight:900;color:#8ec4ff;margin-bottom:6px}.season-pills{display:flex;flex-wrap:wrap;justify-content:center;gap:5px}.season-pill{display:inline-block;border:1px solid #375775;background:#15273a;border-radius:999px;padding:4px 7px;font-size:10px;font-weight:800;white-space:nowrap}.season-pill.score{border-color:#7d5b1f;background:#2a2210;color:#ffd166}.season-pill.finish{border-color:#7d355d;background:#2b1424;color:#ff9bd0}.season-pill.short{border-color:#2b7758;background:#102a21;color:#6de5ac}.season-none{color:#60778d}.season-nav{display:flex;align-items:center;gap:9px;flex-wrap:wrap;margin-top:12px}.season-nav label{font-size:11px;color:#9cb0c5;font-weight:800}.season-nav select{background:#111d2b;color:#eef6ff;border:1px solid #356b98;border-radius:9px;padding:8px 10px;font-weight:800}.season-status{font-size:11px;color:#59e391;font-weight:800}.season-status.archived{color:#ffd166}@media(max-width:760px){.cards{grid-template-columns:repeat(2,1fr)}.days{grid-template-columns:repeat(5,1fr)}.match{grid-template-columns:55px 1fr 54px 1fr}.match .date,.match .winnerLabel{display:none}.wrap{padding:14px}.stats-toggle{display:none}.detail{padding:10px}.stat-table{font-size:12px}.stat-cell{padding:8px 4px}.highlight-grid{grid-template-columns:1fr}.highlight-kind{min-width:76px}}`)
	b.WriteString(`</style></head><body><div class="wrap"><div class="head"><div><div class="eyebrow">AUTODARTS LIGA</div><h1>` + htmlE(ov.LeagueName) + `</h1><div class="muted">` + htmlE(ov.SeasonName) + ` · automatisch aus dem zentralen Liga-OneDrive erstellt</div><div class="season-nav"><label for="seasonSelect">Saison</label><select id="seasonSelect" onchange="if(this.value)location.href=this.value">`)
	for _, season := range seasons {
		href := "season-" + safeOverviewPart(season.ID) + ".html"
		if season.ID == currentSeasonID {
			href = "index.html"
		}
		selected := ""
		if season.ID == ov.SeasonID {
			selected = " selected"
		}
		fmt.Fprintf(&b, `<option value="%s"%s>%s%s</option>`, htmlE(href), selected, htmlE(season.Name), map[bool]string{true: " (aktiv)", false: ""}[season.ID == currentSeasonID])
	}
	b.WriteString(`</select><span class="season-status` + map[bool]string{true: "", false: " archived"}[ov.IsCurrent] + `">` + map[bool]string{true: "Aktive Saison", false: "Archiv"}[ov.IsCurrent] + `</span></div></div><div class="muted">Stand: ` + htmlE(formatDateDE(ov.Generated)) + `</div></div>`)
	played := 0
	for _, c := range ov.DayCounts {
		if c > 0 {
			played++
		}
	}
	latest := "–"
	if len(ov.Matches) > 0 {
		latest = formatDateDE(ov.Matches[len(ov.Matches)-1].Date)
	}
	fmt.Fprintf(&b, `<div class="cards"><div class="card"><strong>%d</strong><span>Matches</span></div><div class="card"><strong>%d/10</strong><span>Spieltage</span></div><div class="card"><strong>%d</strong><span>Spieler</span></div><div class="card"><strong style="font-size:18px">%s</strong><span>Letztes Match</span></div></div>`, len(ov.Matches), played, len(ov.Players), htmlE(latest))
	b.WriteString(`<div class="panel"><h2>Spieltage</h2><div class="days">`)
	for i, c := range ov.DayCounts {
		cl := "day"
		if c > 0 {
			cl += " has"
		}
		fmt.Fprintf(&b, `<div class="%s"><span class="tag">ST %d</span><b>%d</b><span class="muted">%s</span></div>`, cl, i+1, c, map[bool]string{true: "Match", false: "Matches"}[c == 1])
	}
	b.WriteString(`</div></div>`)
	b.WriteString(`<div class="panel"><h2>Ligatabelle</h2><table><thead><tr><th>Spieler</th><th>Sp.</th><th>S</th><th>N</th><th>Legs</th><th>+/-</th><th>Ø AVG</th><th>CO %</th><th>95+</th><th>114+</th><th>133+</th><th>171</th><th>174</th><th>177</th><th>180</th><th>Max. Finish</th></tr></thead><tbody>`)
	for _, p := range ov.Players {
		cls := ""
		if p.LegDiff > 0 {
			cls = "pos"
		} else if p.LegDiff < 0 {
			cls = "neg"
		}
		aliases := ""
		if len(p.Aliases) > 0 {
			aliases = `<div class="muted">` + htmlE(strings.Join(p.Aliases, " · ")) + `</div>`
		}
		fmt.Fprintf(&b, `<tr><td><b>%s</b>%s</td><td>%d</td><td>%d</td><td>%d</td><td>%d:%d</td><td class="%s">%+d</td><td>%.2f</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%s</td></tr>`, htmlE(p.Name), aliases, p.Matches, p.Wins, p.Losses, p.LegsFor, p.LegsAgainst, cls, p.LegDiff, p.Average, fmtCheckoutPct(p.CheckoutPct, p.CheckoutAttempts), scoreOrDash(p.ScoringComplete, p.Scores95Plus), scoreOrDash(p.ScoringComplete, p.Scores114Plus), scoreOrDash(p.ScoringComplete, p.Scores133Plus), scoreOrDash(p.ScoringComplete, p.Scores171), scoreOrDash(p.ScoringComplete, p.Scores174), scoreOrDash(p.ScoringComplete, p.Scores177), p.OneEighties, func() string {
			if p.HighFinish > 0 {
				return strconv.Itoa(p.HighFinish)
			}
			return "–"
		}())
	}
	b.WriteString(`</tbody></table><div class="muted" style="margin-top:9px">Scoring exklusiv: 95+ = 95–113 · 114+ = 114–132 · 133+ = 133–170 · 171 / 174 / 177 / 180 jeweils exakt. „–“ bedeutet: mindestens ein altes Match muss einmal neu ausgewertet werden.</div></div>`)
	writeSeasonHighlightsHTML(&b, ov)
	b.WriteString(`<div class="panel"><h2>Ergebnisse</h2><div class="matches">`)
	matches := append([]MatchOverview(nil), ov.Matches...)
	sort.SliceStable(matches, func(i, j int) bool {
		di, dj := matches[i].Matchday, matches[j].Matchday
		if di < 1 || di > 10 {
			di = 99
		}
		if dj < 1 || dj > 10 {
			dj = 99
		}
		if di != dj {
			return di < dj
		}
		return matches[i].Date.After(matches[j].Date)
	})
	currentMatchday := -1
	for _, m := range matches {
		groupDay := m.Matchday
		if groupDay < 1 || groupDay > 10 {
			groupDay = 0
		}
		if groupDay != currentMatchday {
			currentMatchday = groupDay
			if groupDay > 0 {
				count := 0
				for _, x := range matches {
					if x.Matchday == groupDay {
						count++
					}
				}
				fmt.Fprintf(&b, `<div class="matchday-heading"><div><span class="matchday-kicker">SPIELTAG</span><strong>%d</strong></div><span>%d %s</span></div>`, groupDay, count, map[bool]string{true: "Spiel", false: "Spiele"}[count == 1])
			} else {
				b.WriteString(`<div class="matchday-heading"><div><span class="matchday-kicker">SPIELTAG</span><strong>–</strong></div><span>ohne Zuordnung</span></div>`)
			}
		}

		winner := m.Winner
		if winner == "" {
			winner = "–"
		}
		floatText := func(v *float64, decimals int) string {
			if v == nil {
				return "–"
			}
			return strconv.FormatFloat(*v, 'f', decimals, 64)
		}
		coText := func(pct *float64, hits, attempts int) string {
			if attempts > 0 {
				return fmt.Sprintf("%s %% (%d/%d)", floatText(pct, 1), hits, attempts)
			}
			if pct != nil {
				return floatText(pct, 1) + " %"
			}
			return "–"
		}
		hf := func(v int) string {
			if v > 0 {
				return strconv.Itoa(v)
			}
			return "–"
		}
		matchURL := "https://play.autodarts.com/history/matches/" + m.MatchID
		fmt.Fprintf(&b, `<details class="match-detail"><summary><div class="match"><div class="tag">ST %d</div><div class="date muted">%s</div><div>%s</div><div class="score">%d : %d</div><div>%s</div><div class="winnerLabel"><span class="muted">Sieger </span><span class="winner">%s</span><div class="stats-toggle">Statistik ▾</div></div></div></summary>`, m.Matchday, htmlE(formatDateDE(m.Date)), htmlE(m.Player1), m.Score1, m.Score2, htmlE(m.Player2), htmlE(winner))
		fmt.Fprintf(&b, `<div class="detail"><div class="detail-head"><div class="detail-player"><b>%s</b><span>%s</span></div><div class="detail-player"><b>%s</b><span>%s</span></div></div>`, htmlE(m.Player1), htmlE(m.Alias1), htmlE(m.Player2), htmlE(m.Alias2))
		scoreText := func(ok bool, v int) string {
			if !ok {
				return "–"
			}
			return strconv.Itoa(v)
		}
		rows := [][3]string{
			{"Average", floatText(m.Avg1, 2), floatText(m.Avg2, 2)},
			{"First 9", floatText(m.First91, 2), floatText(m.First92, 2)},
			{"Checkout", coText(m.Checkout1, m.CheckoutHits1, m.CheckoutAttempts1), coText(m.Checkout2, m.CheckoutHits2, m.CheckoutAttempts2)},
			{"Höchstes Finish", hf(m.HF1), hf(m.HF2)},
			{"57+", scoreText(m.ScoringComplete1, m.Scores57P1), scoreText(m.ScoringComplete2, m.Scores57P2)},
			{"95+", scoreText(m.ScoringComplete1, m.Scores95P1), scoreText(m.ScoringComplete2, m.Scores95P2)},
			{"114+", scoreText(m.ScoringComplete1, m.Scores114P1), scoreText(m.ScoringComplete2, m.Scores114P2)},
			{"133+", scoreText(m.ScoringComplete1, m.Scores133P1), scoreText(m.ScoringComplete2, m.Scores133P2)},
			{"171", scoreText(m.ScoringComplete1, m.Scores171P1), scoreText(m.ScoringComplete2, m.Scores171P2)},
			{"174", scoreText(m.ScoringComplete1, m.Scores174P1), scoreText(m.ScoringComplete2, m.Scores174P2)},
			{"177", scoreText(m.ScoringComplete1, m.Scores177P1), scoreText(m.ScoringComplete2, m.Scores177P2)},
			{"180", strconv.Itoa(m.OneEighties1), strconv.Itoa(m.OneEighties2)},
			{"Darts", strconv.Itoa(m.Darts1), strconv.Itoa(m.Darts2)},
		}
		b.WriteString(`<div class="stat-table">`)
		for _, r := range rows {
			fmt.Fprintf(&b, `<div class="stat-row"><div class="stat-cell stat-value">%s</div><div class="stat-cell stat-label">%s</div><div class="stat-cell stat-value">%s</div></div>`, htmlE(r[1]), htmlE(r[0]), htmlE(r[2]))
		}
		b.WriteString(`</div>`)
		if hasWebHighlights(m.Highlights1) || hasWebHighlights(m.Highlights2) {
			b.WriteString(`<div class="highlights"><div class="highlights-title">Highlights</div><div class="highlight-grid">`)
			writeHighlightPlayerHTML(&b, m.Player1, m.Highlights1)
			writeHighlightPlayerHTML(&b, m.Player2, m.Highlights2)
			b.WriteString(`</div></div>`)
		}
		b.WriteString(`<a class="autodarts-link" target="_blank" rel="noopener" href="` + htmlE(matchURL) + `">Match bei Autodarts öffnen ↗</a></div></details>`)
	}
	b.WriteString(`</div></div><div class="muted" style="padding:10px 0 24px">Für eine Matchstatistik einfach ein Ergebnis antippen. Die Werte stammen aus den zentral gespeicherten Liga-Matches.</div></div></body></html>`)
	return writeAtomic(path, []byte(b.String()), 0o644)
}

func (s *Server) handleOverviewPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	path := filepath.Join(s.overviewDir(), "Liga-Übersicht.html")
	if _, err := os.Stat(path); err != nil {
		for _, league := range s.knownLeagueNames() {
			s.generateOverviewForLeague(league)
			break
		}
	}
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "Noch keine Liga-Übersicht vorhanden.", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}

// Minimaler, standardkonformer XLSX-Writer ohne externe Bibliotheken.
type xCell struct {
	V     interface{}
	Style int
}
type xSheet struct {
	Name       string
	Rows       [][]xCell
	ColWidths  []float64
	FreezeRow  int
	AutoFilter string
	Merges     []string
}

func xs(v string, style int) xCell      { return xCell{V: v, Style: style} }
func xn(v interface{}, style int) xCell { return xCell{V: v, Style: style} }
func colName(n int) string {
	var s string
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}
func xmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return r.Replace(s)
}
func cellXML(ref string, c xCell) string {
	style := ""
	if c.Style > 0 {
		style = fmt.Sprintf(` s="%d"`, c.Style)
	}
	switch v := c.V.(type) {
	case nil:
		return ""
	case int:
		return fmt.Sprintf(`<c r="%s"%s><v>%d</v></c>`, ref, style, v)
	case int64:
		return fmt.Sprintf(`<c r="%s"%s><v>%d</v></c>`, ref, style, v)
	case float64:
		return fmt.Sprintf(`<c r="%s"%s><v>%s</v></c>`, ref, style, strconv.FormatFloat(v, 'f', -1, 64))
	case float32:
		return fmt.Sprintf(`<c r="%s"%s><v>%s</v></c>`, ref, style, strconv.FormatFloat(float64(v), 'f', -1, 64))
	default:
		return fmt.Sprintf(`<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, style, xmlEsc(fmt.Sprint(v)))
	}
}
func sheetXML(sh xSheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	if sh.FreezeRow > 0 {
		fmt.Fprintf(&b, `<sheetViews><sheetView workbookViewId="0"><pane ySplit="%d" topLeftCell="A%d" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`, sh.FreezeRow, sh.FreezeRow+1)
	}
	if len(sh.ColWidths) > 0 {
		b.WriteString(`<cols>`)
		for i, w := range sh.ColWidths {
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, w)
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	for r, row := range sh.Rows {
		if len(row) == 0 {
			continue
		}
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, cell := range row {
			if cell.V == nil {
				continue
			}
			b.WriteString(cellXML(fmt.Sprintf("%s%d", colName(c+1), r+1), cell))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if sh.AutoFilter != "" {
		fmt.Fprintf(&b, `<autoFilter ref="%s"/>`, sh.AutoFilter)
	}
	if len(sh.Merges) > 0 {
		fmt.Fprintf(&b, `<mergeCells count="%d">`, len(sh.Merges))
		for _, m := range sh.Merges {
			fmt.Fprintf(&b, `<mergeCell ref="%s"/>`, m)
		}
		b.WriteString(`</mergeCells>`)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}
func addZipFile(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, content)
	return err
}
func xlsxStyles() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="0.00"/></numFmts><fonts count="4"><font><sz val="11"/><name val="Calibri"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="18"/><name val="Calibri"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="11"/><name val="Calibri"/></font><font><b/><color rgb="FF0B1F33"/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="5"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF0D2743"/><bgColor indexed="64"/></patternFill></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1D5D8C"/><bgColor indexed="64"/></patternFill></fill><fill><patternFill patternType="solid"><fgColor rgb="FFE8F2FA"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="2"><border/><border><left style="thin"><color rgb="FFD6E0E8"/></left><right style="thin"><color rgb="FFD6E0E8"/></right><top style="thin"><color rgb="FFD6E0E8"/></top><bottom style="thin"><color rgb="FFD6E0E8"/></bottom></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="8"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFill="1" applyFont="1"><alignment vertical="center"/></xf><xf numFmtId="0" fontId="2" fillId="3" borderId="1" xfId="0" applyFill="1" applyFont="1" applyBorder="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf><xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1"><alignment vertical="center"/></xf><xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1"><alignment horizontal="center" vertical="center"/></xf><xf numFmtId="164" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1" applyNumberFormat="1"><alignment horizontal="center" vertical="center"/></xf><xf numFmtId="0" fontId="3" fillId="4" borderId="1" xfId="0" applyFill="1" applyFont="1" applyBorder="1"><alignment vertical="center"/></xf><xf numFmtId="0" fontId="3" fillId="4" borderId="1" xfId="0" applyFill="1" applyFont="1" applyBorder="1"><alignment horizontal="center" vertical="center"/></xf></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`
}

func writeXLSXOverview(path string, ov LeagueOverview) error {
	played := 0
	for _, c := range ov.DayCounts {
		if c > 0 {
			played++
		}
	}
	latest := "–"
	if len(ov.Matches) > 0 {
		latest = formatDateDE(ov.Matches[len(ov.Matches)-1].Date)
	}
	overviewRows := [][]xCell{{xs("Autodarts Liga – "+ov.LeagueName+" · "+ov.SeasonName, 1)}, {xs("Stand: "+formatDateDE(ov.Generated), 0)}, {}, {xs("Kennzahl", 2), xs("Wert", 2)}, {xs("Matches", 6), xn(len(ov.Matches), 7)}, {xs("Spieltage mit Daten", 6), xs(fmt.Sprintf("%d/10", played), 7)}, {xs("Spieler", 6), xn(len(ov.Players), 7)}, {xs("Letztes Match", 6), xs(latest, 7)}, {}, {xs("Spieler", 2), xs("Sp.", 2), xs("S", 2), xs("N", 2), xs("Legs", 2), xs("+/-", 2), xs("Ø AVG", 2), xs("CO %", 2), xs("95+", 2), xs("114+", 2), xs("133+", 2), xs("171", 2), xs("174", 2), xs("177", 2), xs("180", 2), xs("HF", 2)}}
	for _, p := range ov.Players {
		hf := interface{}("–")
		if p.HighFinish > 0 {
			hf = p.HighFinish
		}
		co := interface{}("–")
		if p.CheckoutAttempts > 0 {
			co = p.CheckoutPct
		}
		overviewRows = append(overviewRows, []xCell{xs(p.Name, 3), xn(p.Matches, 4), xn(p.Wins, 4), xn(p.Losses, 4), xs(fmt.Sprintf("%d:%d", p.LegsFor, p.LegsAgainst), 4), xn(p.LegDiff, 4), xn(p.Average, 5), xn(co, 5), scoreCell(p.ScoringComplete, p.Scores95Plus), scoreCell(p.ScoringComplete, p.Scores114Plus), scoreCell(p.ScoringComplete, p.Scores133Plus), scoreCell(p.ScoringComplete, p.Scores171), scoreCell(p.ScoringComplete, p.Scores174), scoreCell(p.ScoringComplete, p.Scores177), xn(p.OneEighties, 4), xn(hf, 4)})
	}
	overviewRows = append(overviewRows, []xCell{}, []xCell{xs("Spieltage", 2), xs("Matches", 2)})
	for i, c := range ov.DayCounts {
		overviewRows = append(overviewRows, []xCell{xs(fmt.Sprintf("Spieltag %d", i+1), 3), xn(c, 4)})
	}
	sheets := []xSheet{{Name: "Übersicht", Rows: overviewRows, ColWidths: []float64{28, 11, 8, 8, 12, 10, 12, 12, 9, 9, 9, 9, 9, 9, 9, 11}, FreezeRow: 10, Merges: []string{"A1:P1"}}}
	matchHeader := []xCell{xs("Spieltag", 2), xs("Datum", 2), xs("Spieler 1", 2), xs("Spieler 2", 2), xs("Ergebnis", 2), xs("Sieger", 2), xs("AVG 1", 2), xs("AVG 2", 2), xs("57+ 1", 2), xs("57+ 2", 2), xs("95+ 1", 2), xs("95+ 2", 2), xs("114+ 1", 2), xs("114+ 2", 2), xs("133+ 1", 2), xs("133+ 2", 2), xs("171 1", 2), xs("171 2", 2), xs("174 1", 2), xs("174 2", 2), xs("177 1", 2), xs("177 2", 2), xs("180 1", 2), xs("180 2", 2), xs("HF 1", 2), xs("HF 2", 2)}
	dayRows := [][]xCell{matchHeader}
	allRows := [][]xCell{append(append([]xCell{}, matchHeader...), xs("Match-ID", 2), xs("Autodarts-Link", 2))}
	for _, m := range ov.Matches {
		avg1, avg2 := interface{}("–"), interface{}("–")
		if m.Avg1 != nil {
			avg1 = *m.Avg1
		}
		if m.Avg2 != nil {
			avg2 = *m.Avg2
		}
		row := []xCell{xn(m.Matchday, 4), xs(formatDateDE(m.Date), 3), xs(m.Player1, 3), xs(m.Player2, 3), xs(fmt.Sprintf("%d:%d", m.Score1, m.Score2), 4), xs(m.Winner, 3), xn(avg1, 5), xn(avg2, 5), xn(m.Scores57P1, 4), xn(m.Scores57P2, 4), xn(m.Scores95P1, 4), xn(m.Scores95P2, 4), xn(m.Scores114P1, 4), xn(m.Scores114P2, 4), xn(m.Scores133P1, 4), xn(m.Scores133P2, 4), xn(m.Scores171P1, 4), xn(m.Scores171P2, 4), xn(m.Scores174P1, 4), xn(m.Scores174P2, 4), xn(m.Scores177P1, 4), xn(m.Scores177P2, 4), xn(m.OneEighties1, 4), xn(m.OneEighties2, 4), xn(m.HF1, 4), xn(m.HF2, 4)}
		dayRows = append(dayRows, row)
		allRows = append(allRows, append(append([]xCell{}, row...), xs(m.MatchID, 3), xs("https://play.autodarts.com/history/matches/"+m.MatchID, 3)))
	}
	sheets = append(sheets, xSheet{Name: "Spieltage", Rows: dayRows, ColWidths: []float64{10, 18, 24, 24, 12, 24, 11, 11, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}, FreezeRow: 1, AutoFilter: fmt.Sprintf("A1:Z%d", len(dayRows))})
	sheets = append(sheets, xSheet{Name: "Alle Spiele", Rows: allRows, ColWidths: []float64{10, 18, 24, 24, 12, 24, 11, 11, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 38, 58}, FreezeRow: 1, AutoFilter: fmt.Sprintf("A1:AB%d", len(allRows))})
	playerRows := [][]xCell{{xs("Spieler", 2), xs("Autodarts-Name(n)", 2), xs("Spiele", 2), xs("Siege", 2), xs("Niederlagen", 2), xs("Legs +", 2), xs("Legs -", 2), xs("+/-", 2), xs("Ø AVG", 2), xs("Checkout %", 2), xs("95+", 2), xs("114+", 2), xs("133+", 2), xs("171", 2), xs("174", 2), xs("177", 2), xs("180", 2), xs("Höchstes Finish", 2), xs("Darts", 2)}}
	for _, p := range ov.Players {
		aliases := strings.Join(p.Aliases, " · ")
		playerRows = append(playerRows, []xCell{xs(p.Name, 3), xs(aliases, 3), xn(p.Matches, 4), xn(p.Wins, 4), xn(p.Losses, 4), xn(p.LegsFor, 4), xn(p.LegsAgainst, 4), xn(p.LegDiff, 4), xn(p.Average, 5), xn(p.CheckoutPct, 5), scoreCell(p.ScoringComplete, p.Scores95Plus), scoreCell(p.ScoringComplete, p.Scores114Plus), scoreCell(p.ScoringComplete, p.Scores133Plus), scoreCell(p.ScoringComplete, p.Scores171), scoreCell(p.ScoringComplete, p.Scores174), scoreCell(p.ScoringComplete, p.Scores177), xn(p.OneEighties, 4), xn(p.HighFinish, 4), xn(p.Darts, 4)})
	}
	sheets = append(sheets, xSheet{Name: "Spielerstatistik", Rows: playerRows, ColWidths: []float64{24, 28, 10, 10, 12, 10, 10, 10, 11, 14, 9, 9, 9, 9, 9, 9, 9, 12, 10}, FreezeRow: 1, AutoFilter: fmt.Sprintf("A1:S%d", len(playerRows))})
	return createXLSX(path, sheets)
}

func createXLSX(path string, sheets []xSheet) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := range sheets {
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
	}
	ct.WriteString(`</Types>`)
	if err := addZipFile(zw, "[Content_Types].xml", ct.String()); err != nil {
		return err
	}
	if err := addZipFile(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`); err != nil {
		return err
	}
	var wb, rels strings.Builder
	wb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, sh := range sheets {
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEsc(sh.Name), i+1, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
	}
	styleID := len(sheets) + 1
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, styleID)
	wb.WriteString(`</sheets></workbook>`)
	rels.WriteString(`</Relationships>`)
	if err := addZipFile(zw, "xl/workbook.xml", wb.String()); err != nil {
		return err
	}
	if err := addZipFile(zw, "xl/_rels/workbook.xml.rels", rels.String()); err != nil {
		return err
	}
	if err := addZipFile(zw, "xl/styles.xml", xlsxStyles()); err != nil {
		return err
	}
	for i, sh := range sheets {
		if err := addZipFile(zw, fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML(sh)); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return writeAtomic(path, buf.Bytes(), 0o644)
}
