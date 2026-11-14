package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	expectedLeaguePlayers = 6
	expectedLeagueMatches = 30
	expectedMatchdays     = 10
	expectedMatchesPerDay = 3
)

type LeagueProgress struct {
	ExpectedPlayers   int      `json:"expectedPlayers"`
	PlayerCount       int      `json:"playerCount"`
	ExpectedMatches   int      `json:"expectedMatches"`
	CompletedMatches  int      `json:"completedMatches"`
	MatchdaysComplete int      `json:"matchdaysComplete"`
	PairingsComplete  int      `json:"pairingsComplete"`
	PairingsExpected  int      `json:"pairingsExpected"`
	Complete          bool     `json:"complete"`
	Reason            string   `json:"reason,omitempty"`
	Players           []string `json:"players,omitempty"`
}

type FrozenStanding struct {
	Rank        int      `json:"rank"`
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Matches     int      `json:"matches"`
	Wins        int      `json:"wins"`
	Losses      int      `json:"losses"`
	LegsFor     int      `json:"legsFor"`
	LegsAgainst int      `json:"legsAgainst"`
	LegDiff     int      `json:"legDiff"`
	Average     float64  `json:"average"`
}

type FinalFixture struct {
	ID          string `json:"id"`
	Order       int    `json:"order"`
	Label       string `json:"label"`
	Player1     string `json:"player1,omitempty"`
	Player2     string `json:"player2,omitempty"`
	Status      string `json:"status"`
	MatchID     string `json:"matchId,omitempty"`
	Winner      string `json:"winner,omitempty"`
	Loser       string `json:"loser,omitempty"`
	Score       []int  `json:"score,omitempty"`
	Walkover    bool   `json:"walkover,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

type FinalPlayerStat struct {
	Name          string  `json:"name"`
	Matches       int     `json:"matches"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	LegsFor       int     `json:"legsFor"`
	LegsAgainst   int     `json:"legsAgainst"`
	LegDiff       int     `json:"legDiff"`
	Average       float64 `json:"average"`
	CheckoutPct   float64 `json:"checkoutPct"`
	Scores95Plus  int     `json:"scores95Plus"`
	Scores114Plus int     `json:"scores114Plus"`
	Scores133Plus int     `json:"scores133Plus"`
	Scores171     int     `json:"scores171"`
	Scores174     int     `json:"scores174"`
	Scores177     int     `json:"scores177"`
	OneEighties   int     `json:"oneEighties"`
	HighFinish    int     `json:"highFinish"`
	Darts         int     `json:"darts"`
}

type FinalState struct {
	Format          string            `json:"format"`
	Version         int               `json:"version"`
	LeagueName      string            `json:"leagueName"`
	SeasonID        string            `json:"seasonId"`
	SeasonName      string            `json:"seasonName"`
	Status          string            `json:"status"`
	Available       bool              `json:"available"`
	RegularLocked   bool              `json:"regularLocked"`
	CreatedAt       string            `json:"createdAt,omitempty"`
	UpdatedAt       string            `json:"updatedAt,omitempty"`
	CompletedAt     string            `json:"completedAt,omitempty"`
	Champion        string            `json:"champion,omitempty"`
	Progress        LeagueProgress    `json:"progress"`
	FrozenTable     []FrozenStanding  `json:"frozenTable,omitempty"`
	Fixtures        []FinalFixture    `json:"fixtures"`
	FinalStats      []FinalPlayerStat `json:"finalStats,omitempty"`
	FinalStatsGames int               `json:"finalStatsGames"`
}

type FinalMatchFile struct {
	Format     string          `json:"format"`
	Version    int             `json:"version"`
	League     LeagueInfo      `json:"league"`
	SeasonID   string          `json:"seasonId"`
	SeasonName string          `json:"seasonName"`
	GameID     string          `json:"gameId"`
	UpdatedAt  string          `json:"updatedAt"`
	Walkover   bool            `json:"walkover,omitempty"`
	Match      json.RawMessage `json:"match"`
}

type FinalMatchRequest struct {
	League   LeagueInfo      `json:"league"`
	SeasonID string          `json:"seasonId"`
	GameID   string          `json:"gameId"`
	Match    json.RawMessage `json:"match"`
}

type WalkoverRequest struct {
	Scope    string     `json:"scope"`
	League   LeagueInfo `json:"league"`
	SeasonID string     `json:"seasonId"`
	Matchday int        `json:"matchday,omitempty"`
	GameID   string     `json:"gameId,omitempty"`
	Player1  string     `json:"player1"`
	Player2  string     `json:"player2"`
	Winner   string     `json:"winner"`
}

func (s *Server) finalsDir() string {
	base := s.basePath()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Finals")
}
func (s *Server) finalMatchesDir() string {
	base := s.basePath()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "FinalMatches")
}
func (s *Server) finalStatePath(seasonID string) string {
	if s.finalsDir() == "" {
		return ""
	}
	return filepath.Join(s.finalsDir(), safeOverviewPart(seasonID)+".json")
}
func (s *Server) finalMatchPath(seasonID, gameID string) string {
	if s.finalMatchesDir() == "" {
		return ""
	}
	return filepath.Join(s.finalMatchesDir(), safeOverviewPart(seasonID)+"__"+safeOverviewPart(gameID)+".json")
}

func pairKey(a, b string) string {
	a, b = keyName(a), keyName(b)
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}

func regularProgress(ov LeagueOverview) LeagueProgress {
	p := LeagueProgress{
		ExpectedPlayers:  expectedLeaguePlayers,
		ExpectedMatches:  expectedLeagueMatches,
		PairingsExpected: expectedLeaguePlayers * (expectedLeaguePlayers - 1) / 2,
		CompletedMatches: len(ov.Matches),
	}
	names := make([]string, 0, len(ov.Players))
	for _, pl := range ov.Players {
		if cleanPlayerName(pl.Name) != "" {
			names = append(names, cleanPlayerName(pl.Name))
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	p.Players = names
	p.PlayerCount = len(names)
	for _, c := range ov.DayCounts {
		if c == expectedMatchesPerDay {
			p.MatchdaysComplete++
		}
	}
	pairs := map[string]int{}
	for _, m := range ov.Matches {
		a, b := cleanPlayerName(m.Player1), cleanPlayerName(m.Player2)
		if a == "" || b == "" || keyName(a) == keyName(b) {
			continue
		}
		pairs[pairKey(a, b)]++
	}
	if len(names) == expectedLeaguePlayers {
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				if pairs[pairKey(names[i], names[j])] == 2 {
					p.PairingsComplete++
				}
			}
		}
	}
	p.Complete = p.PlayerCount == expectedLeaguePlayers && p.CompletedMatches == expectedLeagueMatches && p.MatchdaysComplete == expectedMatchdays && p.PairingsComplete == p.PairingsExpected
	switch {
	case p.PlayerCount != expectedLeaguePlayers:
		p.Reason = fmt.Sprintf("%d von %d Spielern erkannt", p.PlayerCount, expectedLeaguePlayers)
	case p.CompletedMatches != expectedLeagueMatches:
		p.Reason = fmt.Sprintf("%d von %d Ligaspielen abgeschlossen", p.CompletedMatches, expectedLeagueMatches)
	case p.MatchdaysComplete != expectedMatchdays:
		p.Reason = fmt.Sprintf("%d von %d Spieltagen enthalten genau 3 Spiele", p.MatchdaysComplete, expectedMatchdays)
	case p.PairingsComplete != p.PairingsExpected:
		p.Reason = fmt.Sprintf("%d von %d Paarungen wurden exakt zweimal abgeschlossen", p.PairingsComplete, p.PairingsExpected)
	default:
		p.Reason = "Alle Ligaspiele vollständig – Finalspieltag freigegeben"
	}
	return p
}

func frozenTableFromOverview(ov LeagueOverview) []FrozenStanding {
	out := make([]FrozenStanding, 0, len(ov.Players))
	for i, p := range ov.Players {
		out = append(out, FrozenStanding{Rank: i + 1, Name: p.Name, Aliases: append([]string(nil), p.Aliases...), Matches: p.Matches, Wins: p.Wins, Losses: p.Losses, LegsFor: p.LegsFor, LegsAgainst: p.LegsAgainst, LegDiff: p.LegDiff, Average: p.Average})
	}
	return out
}

func initialFinalFixtures(table []FrozenStanding) []FinalFixture {
	fixtures := []FinalFixture{
		{ID: "place56", Order: 1, Label: "Spiel um Platz 5/6", Status: "locked"},
		{ID: "semi1", Order: 2, Label: "Halbfinale 1 · Platz 1 gegen Platz 4", Status: "locked"},
		{ID: "semi2", Order: 3, Label: "Halbfinale 2 · Platz 2 gegen Platz 3", Status: "locked"},
		{ID: "third", Order: 4, Label: "Spiel um Platz 3", Status: "locked"},
		{ID: "final", Order: 5, Label: "Finale", Status: "locked"},
	}
	if len(table) == 6 {
		fixtures[0].Player1, fixtures[0].Player2 = table[4].Name, table[5].Name
		fixtures[1].Player1, fixtures[1].Player2 = table[0].Name, table[3].Name
		fixtures[2].Player1, fixtures[2].Player2 = table[1].Name, table[2].Name
		fixtures[0].Status = "ready"
		fixtures[1].Status = "ready"
		fixtures[2].Status = "ready"
	}
	return fixtures
}

func previewFinalState(ov LeagueOverview) *FinalState {
	progress := regularProgress(ov)
	fixtures := []FinalFixture{
		{ID: "place56", Order: 1, Label: "Spiel um Platz 5/6", Status: "locked"},
		{ID: "semi1", Order: 2, Label: "Halbfinale 1 · Platz 1 gegen Platz 4", Status: "locked"},
		{ID: "semi2", Order: 3, Label: "Halbfinale 2 · Platz 2 gegen Platz 3", Status: "locked"},
		{ID: "third", Order: 4, Label: "Spiel um Platz 3", Status: "locked"},
		{ID: "final", Order: 5, Label: "Finale", Status: "locked"},
	}
	return &FinalState{Format: "autodarts-match-analytics-final", Version: 1, LeagueName: ov.LeagueName, SeasonID: ov.SeasonID, SeasonName: ov.SeasonName, Status: "locked", Available: false, RegularLocked: false, Progress: progress, Fixtures: fixtures}
}

func (s *Server) loadFinalState(seasonID string) (*FinalState, error) {
	path := s.finalStatePath(seasonID)
	if path == "" {
		return nil, errors.New("Liga-Datenordner ist noch nicht eingerichtet")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st FinalState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	if st.Format != "autodarts-match-analytics-final" || st.SeasonID != seasonID {
		return nil, errors.New("Finalstatus ist ungültig")
	}
	return &st, nil
}

func (s *Server) saveFinalState(st *FinalState) error {
	if st == nil {
		return errors.New("Finalstatus fehlt")
	}
	dir := s.finalsDir()
	if dir == "" {
		return errors.New("Liga-Datenordner ist noch nicht eingerichtet")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	st.Format = "autodarts-match-analytics-final"
	st.Version = 1
	st.Available = true
	st.RegularLocked = true
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.finalStatePath(st.SeasonID), b, 0o644)
}

func (s *Server) isSeasonFrozen(seasonID string) bool {
	if strings.TrimSpace(seasonID) == "" {
		return false
	}
	_, err := os.Stat(s.finalStatePath(seasonID))
	return err == nil
}

func fixtureByID(st *FinalState, id string) *FinalFixture {
	for i := range st.Fixtures {
		if st.Fixtures[i].ID == id {
			return &st.Fixtures[i]
		}
	}
	return nil
}

func clearFixtureResult(f *FinalFixture) {
	if f == nil {
		return
	}
	f.MatchID = ""
	f.Winner = ""
	f.Loser = ""
	f.Score = nil
	f.Walkover = false
	f.CompletedAt = ""
}

func updateFinalAvailability(st *FinalState) {
	if st == nil || len(st.Fixtures) != 5 {
		return
	}
	// Static pairings are derived only from the frozen regular-season table.
	if len(st.FrozenTable) == 6 {
		st.Fixtures[0].Player1, st.Fixtures[0].Player2 = st.FrozenTable[4].Name, st.FrozenTable[5].Name
		st.Fixtures[1].Player1, st.Fixtures[1].Player2 = st.FrozenTable[0].Name, st.FrozenTable[3].Name
		st.Fixtures[2].Player1, st.Fixtures[2].Player2 = st.FrozenTable[1].Name, st.FrozenTable[2].Name
	}
	for i := range st.Fixtures {
		if st.Fixtures[i].Status != "completed" {
			st.Fixtures[i].Status = "locked"
		}
	}
	// Dependency logic instead of a forced chronological order:
	// place 5/6 and both semifinals can be played independently as soon as
	// the regular season is frozen.
	for _, idx := range []int{0, 1, 2} {
		if st.Fixtures[idx].Status != "completed" {
			st.Fixtures[idx].Status = "ready"
		}
	}
	// Third-place match and final depend only on BOTH semifinals. They do not
	// depend on place 5/6 and may be played in either order once both semis end.
	if finalFixtureComplete(st.Fixtures[1]) && finalFixtureComplete(st.Fixtures[2]) {
		st.Fixtures[3].Player1, st.Fixtures[3].Player2 = st.Fixtures[1].Loser, st.Fixtures[2].Loser
		st.Fixtures[4].Player1, st.Fixtures[4].Player2 = st.Fixtures[1].Winner, st.Fixtures[2].Winner
		for _, idx := range []int{3, 4} {
			if st.Fixtures[idx].Status != "completed" {
				st.Fixtures[idx].Status = "ready"
			}
		}
	}
}

func finalFixtureComplete(f FinalFixture) bool {
	return f.Status == "completed" && f.Winner != "" && f.Loser != ""
}

func (s *Server) readFinalMatchFiles(seasonID string) map[string]FinalMatchFile {
	out := map[string]FinalMatchFile{}
	dir := s.finalMatchesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var fm FinalMatchFile
		if json.Unmarshal(b, &fm) != nil || fm.Format != "autodarts-match-analytics-final-match" || fm.SeasonID != seasonID || fm.GameID == "" {
			continue
		}
		if prev, ok := out[fm.GameID]; !ok || parseAnyTime(fm.UpdatedAt).After(parseAnyTime(prev.UpdatedAt)) {
			out[fm.GameID] = fm
		}
	}
	return out
}

func finalMatchWinner(m OverviewMatch) (string, string, bool) {
	if len(m.Players) != 2 || m.Winner < 0 || m.Winner > 1 {
		return "", "", false
	}
	n0, _ := canonicalPlayer(m.Players[0], "Spieler 1")
	n1, _ := canonicalPlayer(m.Players[1], "Spieler 2")
	if m.Winner == 0 {
		return n0, n1, true
	}
	return n1, n0, true
}

func namesMatch(a, b string) bool { return keyName(a) != "" && keyName(a) == keyName(b) }

func standingMatches(st FrozenStanding, candidates ...string) bool {
	for _, c := range candidates {
		if namesMatch(st.Name, c) {
			return true
		}
		for _, a := range st.Aliases {
			if namesMatch(a, c) {
				return true
			}
		}
	}
	return false
}

func canonicalStandingName(st *FinalState, candidates ...string) string {
	for _, row := range st.FrozenTable {
		if standingMatches(row, candidates...) {
			return row.Name
		}
	}
	return ""
}

func matchPlayersCanonical(raw json.RawMessage, st *FinalState) ([]string, error) {
	var m OverviewMatch
	if err := json.Unmarshal(raw, &m); err != nil || len(m.Players) != 2 {
		return nil, errors.New("Finalmatch enthält nicht genau zwei Spieler")
	}
	out := make([]string, 2)
	for i, p := range m.Players {
		real, alias := canonicalPlayer(p, fmt.Sprintf("Spieler %d", i+1))
		out[i] = canonicalStandingName(st, real, alias, p.Name, p.AutodartsName, p.RealName)
		if out[i] == "" {
			return nil, fmt.Errorf("Spieler %q ist nicht Teil der eingefrorenen Sechser-Tabelle", real)
		}
	}
	if namesMatch(out[0], out[1]) {
		return nil, errors.New("Finalmatch enthält denselben Spieler zweimal")
	}
	return out, nil
}

func injectFinalMatchFields(raw json.RawMessage, season SeasonInfo, gameID string, canonical []string) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("Matchdaten sind ungültig")
	}
	b, _ := json.Marshal(season.ID)
	obj["seasonId"] = b
	b, _ = json.Marshal(season.Name)
	obj["seasonName"] = b
	b, _ = json.Marshal("final")
	obj["usageMode"] = b
	b, _ = json.Marshal(gameID)
	obj["finalGameId"] = b
	b, _ = json.Marshal(0)
	obj["matchday"] = b
	if len(canonical) == 2 {
		var players []map[string]any
		if json.Unmarshal(obj["players"], &players) == nil && len(players) == 2 {
			for i := range players {
				players[i]["realName"] = canonical[i]
			}
			pb, _ := json.Marshal(players)
			obj["players"] = pb
		}
	}
	return json.Marshal(obj)
}

func applyFinalFile(st *FinalState, f *FinalFixture, fm FinalMatchFile) bool {
	if st == nil || f == nil {
		return false
	}
	var m OverviewMatch
	if json.Unmarshal(fm.Match, &m) != nil || len(m.Players) != 2 || m.Winner < 0 || m.Winner > 1 {
		return false
	}
	canon, err := matchPlayersCanonical(fm.Match, st)
	if err != nil {
		return false
	}
	if !((namesMatch(canon[0], f.Player1) && namesMatch(canon[1], f.Player2)) || (namesMatch(canon[1], f.Player1) && namesMatch(canon[0], f.Player2))) {
		return false
	}
	winner := canon[m.Winner]
	loser := canon[1-m.Winner]
	f.Status = "completed"
	f.MatchID = m.MatchID
	f.Winner = winner
	f.Loser = loser
	f.Score = append([]int(nil), m.Score...)
	f.Walkover = fm.Walkover || m.Walkover
	f.CompletedAt = fm.UpdatedAt
	return true
}

func (s *Server) finalStatsForState(st *FinalState, files map[string]FinalMatchFile) ([]FinalPlayerStat, int) {
	raw := make([]OverviewMatch, 0, 5)
	for _, f := range st.Fixtures {
		fm, ok := files[f.ID]
		if !ok || fm.Walkover {
			continue
		}
		var m OverviewMatch
		if json.Unmarshal(fm.Match, &m) == nil && !m.Walkover {
			raw = append(raw, m)
		}
	}
	if len(raw) == 0 {
		return nil, 0
	}
	ov := makeLeagueOverview(st.LeagueName, st.SeasonID, st.SeasonName, false, raw)
	out := make([]FinalPlayerStat, 0, len(ov.Players))
	for _, p := range ov.Players {
		out = append(out, FinalPlayerStat{Name: p.Name, Matches: p.Matches, Wins: p.Wins, Losses: p.Losses, LegsFor: p.LegsFor, LegsAgainst: p.LegsAgainst, LegDiff: p.LegDiff, Average: p.Average, CheckoutPct: p.CheckoutPct, Scores95Plus: p.Scores95Plus, Scores114Plus: p.Scores114Plus, Scores133Plus: p.Scores133Plus, Scores171: p.Scores171, Scores174: p.Scores174, Scores177: p.Scores177, OneEighties: p.OneEighties, HighFinish: p.HighFinish, Darts: p.Darts})
	}
	return out, len(raw)
}

func (s *Server) reconcileFinalState(st *FinalState) error {
	if st == nil {
		return errors.New("Finalstatus fehlt")
	}
	// Rebuild the derived bracket from durable final-match files. The first
	// tier is independent; the second tier only depends on both semifinals.
	base := initialFinalFixtures(st.FrozenTable)
	for i := range base {
		st.Fixtures[i].Player1 = base[i].Player1
		st.Fixtures[i].Player2 = base[i].Player2
		st.Fixtures[i].Status = base[i].Status
		clearFixtureResult(&st.Fixtures[i])
	}
	files := s.readFinalMatchFiles(st.SeasonID)
	updateFinalAvailability(st)

	// Independent games available immediately after the regular-season freeze.
	for _, id := range []string{"place56", "semi1", "semi2"} {
		f := fixtureByID(st, id)
		if f == nil || f.Status != "ready" {
			continue
		}
		if fm, ok := files[id]; ok {
			_ = applyFinalFile(st, f, fm)
		}
	}
	updateFinalAvailability(st)

	// Once both semifinals are complete, third place and final are both
	// independently playable. A missing third-place match must not block final.
	for _, id := range []string{"third", "final"} {
		f := fixtureByID(st, id)
		if f == nil || f.Status != "ready" {
			continue
		}
		if fm, ok := files[id]; ok {
			_ = applyFinalFile(st, f, fm)
		}
	}
	updateFinalAvailability(st)

	completed := 0
	for _, f := range st.Fixtures {
		if finalFixtureComplete(f) {
			completed++
		}
	}
	// The final winner can already be shown while another independent final-day
	// fixture (e.g. place 5/6) is still open. The season itself closes only at 5/5.
	if finalFixtureComplete(st.Fixtures[4]) {
		st.Champion = st.Fixtures[4].Winner
	} else {
		st.Champion = ""
	}
	switch {
	case completed == 5:
		st.Status = "completed"
		latest := time.Time{}
		for _, f := range st.Fixtures {
			t := parseAnyTime(f.CompletedAt)
			if t.After(latest) {
				latest = t
			}
		}
		if !latest.IsZero() {
			st.CompletedAt = latest.UTC().Format(time.RFC3339Nano)
		} else if st.CompletedAt == "" {
			st.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
	case completed > 0:
		st.Status = "running"
		st.CompletedAt = ""
	default:
		st.Status = "ready"
		st.CompletedAt = ""
	}
	st.FinalStats, st.FinalStatsGames = s.finalStatsForState(st, files)
	return nil
}

func (s *Server) ensureFinalStateForOverview(ov LeagueOverview) (*FinalState, error) {
	s.finalMu.Lock()
	defer s.finalMu.Unlock()
	return s.ensureFinalStateForOverviewLocked(ov)
}

func (s *Server) ensureFinalStateForOverviewLocked(ov LeagueOverview) (*FinalState, error) {
	path := s.finalStatePath(ov.SeasonID)
	if path == "" {
		return previewFinalState(ov), nil
	}
	if st, err := s.loadFinalState(ov.SeasonID); err == nil {
		if err := s.reconcileFinalState(st); err != nil {
			return nil, err
		}
		_ = s.saveFinalState(st)
		if st.Status == "completed" {
			_ = s.markSeasonCompleted(st.LeagueName, st.SeasonID, st.Champion, st.CompletedAt)
		}
		return st, nil
	}
	progress := regularProgress(ov)
	if !progress.Complete {
		return previewFinalState(ov), nil
	}
	table := frozenTableFromOverview(ov)
	if len(table) != 6 {
		return previewFinalState(ov), nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	st := &FinalState{Format: "autodarts-match-analytics-final", Version: 1, LeagueName: ov.LeagueName, SeasonID: ov.SeasonID, SeasonName: ov.SeasonName, Status: "ready", Available: true, RegularLocked: true, CreatedAt: now, Progress: progress, FrozenTable: table, Fixtures: initialFinalFixtures(table)}
	if err := s.saveFinalState(st); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Server) markSeasonCompleted(league, seasonID, champion, completedAt string) error {
	s.seasonMu.Lock()
	defer s.seasonMu.Unlock()
	path := s.seasonRegistryPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var reg SeasonRegistry
	if err := json.Unmarshal(b, &reg); err != nil {
		return err
	}
	changed := false
	for i := range reg.Seasons {
		if reg.Seasons[i].ID == seasonID {
			if reg.Seasons[i].CompletedAt != completedAt || reg.Seasons[i].Champion != champion || !reg.Seasons[i].Closed {
				reg.Seasons[i].CompletedAt = completedAt
				reg.Seasons[i].Champion = champion
				reg.Seasons[i].Closed = true
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	reg.LeagueName = league
	return s.saveSeasonRegistryLocked(reg)
}

func (s *Server) finalStateFor(league, seasonID string) (*FinalState, SeasonInfo, error) {
	reg, err := s.ensureSeasonRegistry(league)
	if err != nil {
		return nil, SeasonInfo{}, err
	}
	var season SeasonInfo
	var ok bool
	if strings.TrimSpace(seasonID) != "" {
		season, ok = seasonByID(reg, seasonID)
	} else {
		season, ok = currentSeason(reg)
	}
	if !ok {
		return nil, SeasonInfo{}, errors.New("Saison nicht gefunden")
	}
	raw, err := s.collectOverviewMatches(league, season.ID)
	if err != nil {
		return nil, season, err
	}
	ov := makeLeagueOverview(league, season.ID, season.Name, season.ID == reg.CurrentSeasonID, raw)
	st, err := s.ensureFinalStateForOverview(ov)
	return st, season, err
}

func (s *Server) handleFinals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	league := strings.TrimSpace(r.URL.Query().Get("league"))
	seasonID := strings.TrimSpace(r.URL.Query().Get("season"))
	st, season, err := s.finalStateFor(league, seasonID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "season": season, "final": st})
}

func fixtureExpectedPlayers(f *FinalFixture) []string {
	if f == nil {
		return nil
	}
	return []string{cleanPlayerName(f.Player1), cleanPlayerName(f.Player2)}
}
func samePair(a, b []string) bool {
	return len(a) == 2 && len(b) == 2 && ((namesMatch(a[0], b[0]) && namesMatch(a[1], b[1])) || (namesMatch(a[0], b[1]) && namesMatch(a[1], b[0])))
}

func (s *Server) writeFinalMatchFile(st *FinalState, season SeasonInfo, gameID string, raw json.RawMessage, walkover bool) (string, error) {
	s.finalMu.Lock()
	defer s.finalMu.Unlock()
	// Always re-read the durable bracket after acquiring the final lock. This
	// prevents a slower overview regeneration from overwriting a just-finished
	// final game with an older bracket state.
	if fresh, err := s.loadFinalState(season.ID); err == nil {
		if err := s.reconcileFinalState(fresh); err != nil {
			return "", err
		}
		*st = *fresh
	}
	f := fixtureByID(st, gameID)
	if f == nil {
		return "", errors.New("unbekanntes Finalspiel")
	}
	if f.Status != "ready" {
		return "", fmt.Errorf("%s ist aktuell nicht spielbar", f.Label)
	}
	var m OverviewMatch
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", errors.New("Matchdaten sind ungültig")
	}
	if !looksLikeUUID(m.MatchID) {
		return "", errors.New("ungültige Match-ID")
	}
	if len(m.Players) != 2 || m.Winner < 0 || m.Winner > 1 {
		return "", errors.New("Finalmatch muss genau zwei Spieler und einen eindeutigen Sieger enthalten")
	}
	canon, err := matchPlayersCanonical(raw, st)
	if err != nil {
		return "", err
	}
	if !samePair(canon, fixtureExpectedPlayers(f)) {
		return "", fmt.Errorf("Dieses Match gehört nicht zu %s (%s gegen %s)", f.Label, f.Player1, f.Player2)
	}
	raw, err = injectFinalMatchFields(raw, season, gameID, canon)
	if err != nil {
		return "", err
	}
	var finalObj map[string]any
	if json.Unmarshal(raw, &finalObj) == nil {
		finalObj["walkover"] = walkover
		raw, _ = json.Marshal(finalObj)
	}
	if err := validateNewMatch(raw); err != nil {
		return "", err
	}
	dir := s.finalMatchesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := s.finalMatchPath(season.ID, gameID)
	fm := FinalMatchFile{Format: "autodarts-match-analytics-final-match", Version: 1, League: LeagueInfo{Name: st.LeagueName, SeasonID: season.ID, SeasonName: season.Name}, SeasonID: season.ID, SeasonName: season.Name, GameID: gameID, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), Walkover: walkover, Match: raw}
	action := "created"
	if oldBytes, err := os.ReadFile(path); err == nil {
		var old FinalMatchFile
		if json.Unmarshal(oldBytes, &old) != nil || old.Format != fm.Format {
			return "", errors.New("bestehende Finaldatei ist beschädigt")
		}
		var oldM OverviewMatch
		if json.Unmarshal(old.Match, &oldM) != nil {
			return "", errors.New("bestehendes Finalmatch ist ungültig")
		}
		if !strings.EqualFold(oldM.MatchID, m.MatchID) {
			return "", errors.New("Finalspiel wurde bereits mit einer anderen Match-ID abgeschlossen")
		}
		merged, err := mergeProtectedMatch(old.Match, fm.Match)
		if err != nil {
			return "", err
		}
		fm.Match = merged
		action = "updated"
		if err := backupOriginal(s.basePath(), "final-"+m.MatchID, oldBytes); err != nil {
			return "", err
		}
	}
	b, err := json.MarshalIndent(fm, "", "  ")
	if err != nil {
		return "", err
	}
	if err := safeMatchReplace(path, b); err != nil {
		return "", err
	}
	if err := s.reconcileFinalState(st); err != nil {
		return "", err
	}
	if err := s.saveFinalState(st); err != nil {
		return "", err
	}
	if st.Status == "completed" {
		_ = s.markSeasonCompleted(st.LeagueName, st.SeasonID, st.Champion, st.CompletedAt)
	}
	return action, nil
}

func (s *Server) handleFinalMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req FinalMatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	league := strings.TrimSpace(req.League.Name)
	st, season, err := s.finalStateFor(league, req.SeasonID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !st.Available {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "Finalspieltag ist noch gesperrt: " + st.Progress.Reason})
		return
	}
	action, err := s.writeFinalMatchFile(st, season, strings.TrimSpace(req.GameID), req.Match, false)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	go s.generateOverviewForLeague(league)
	s.queuePublishTrigger()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": action, "final": st})
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", time.Now().UnixNano()&0xffffffffffff)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	x := hex.EncodeToString(b[:])
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

func walkoverRaw(season SeasonInfo, scope, gameID string, matchday int, p1, p2, winner string) json.RawMessage {
	wi := 0
	if namesMatch(winner, p2) {
		wi = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	players := []OverviewPlayer{{Name: p1, AutodartsName: p1, RealName: p1}, {Name: p2, AutodartsName: p2, RealName: p2}}
	players[wi].Wins = 1
	m := OverviewMatch{MatchID: newUUID(), SeasonID: season.ID, SeasonName: season.Name, SavedAt: now, Variant: "Kampflos", Winner: wi, Score: []int{0, 0}, Players: players, Matchday: matchday, UsageMode: scope, Walkover: true, FinalGameID: gameID}
	b, _ := json.Marshal(m)
	return b
}

func matchPairExistsOnDay(ov LeagueOverview, day int, p1, p2 string) bool {
	want := pairKey(p1, p2)
	for _, m := range ov.Matches {
		if m.Matchday == day && pairKey(m.Player1, m.Player2) == want {
			return true
		}
	}
	return false
}

func knownPlayerAllowed(ov LeagueOverview, name string) bool {
	if len(ov.Players) < expectedLeaguePlayers {
		return true
	}
	for _, p := range ov.Players {
		if namesMatch(p.Name, name) {
			return true
		}
		for _, a := range p.Aliases {
			if namesMatch(a, name) {
				return true
			}
		}
	}
	return false
}

func canonicalKnownPlayer(ov LeagueOverview, name string) string {
	for _, p := range ov.Players {
		if namesMatch(p.Name, name) {
			return p.Name
		}
		for _, a := range p.Aliases {
			if namesMatch(a, name) {
				return p.Name
			}
		}
	}
	return cleanPlayerName(name)
}

func (s *Server) handleWalkover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req WalkoverRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	league := strings.TrimSpace(req.League.Name)
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "league"
	}
	reg, err := s.ensureSeasonRegistry(league)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	season, ok := seasonByID(reg, strings.TrimSpace(req.SeasonID))
	if !ok {
		season, ok = currentSeason(reg)
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Saison nicht gefunden"})
		return
	}
	p1, p2, winner := cleanPlayerName(req.Player1), cleanPlayerName(req.Player2), cleanPlayerName(req.Winner)
	if p1 == "" || p2 == "" || namesMatch(p1, p2) || (!namesMatch(winner, p1) && !namesMatch(winner, p2)) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Spieler und Sieger sind nicht eindeutig"})
		return
	}
	if scope == "final" {
		st, _, err := s.finalStateFor(league, season.ID)
		if err != nil || !st.Available {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "Finalspieltag ist noch nicht freigegeben"})
			return
		}
		f := fixtureByID(st, req.GameID)
		if f == nil || f.Status != "ready" {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "Dieses Finalspiel ist noch nicht freigegeben"})
			return
		}
		if !samePair([]string{p1, p2}, fixtureExpectedPlayers(f)) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Spieler passen nicht zum ausgewählten Finalspiel"})
			return
		}
		raw := walkoverRaw(season, "final", f.ID, 0, f.Player1, f.Player2, winner)
		action, err := s.writeFinalMatchFile(st, season, f.ID, raw, true)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		go s.generateOverviewForLeague(league)
		s.queuePublishTrigger()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": action, "final": st})
		return
	}
	if scope != "league" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "scope muss league oder final sein"})
		return
	}
	if req.Matchday < 1 || req.Matchday > 10 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Spieltag 1–10 erforderlich"})
		return
	}
	rawMatches, err := s.collectOverviewMatches(league, season.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ov := makeLeagueOverview(league, season.ID, season.Name, season.ID == reg.CurrentSeasonID, rawMatches)
	if s.isSeasonFrozen(season.ID) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "Ligatabelle ist bereits eingefroren; reguläre Wertungen sind gesperrt"})
		return
	}
	if !knownPlayerAllowed(ov, p1) || !knownPlayerAllowed(ov, p2) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Es sind bereits sechs Ligaspieler bekannt; bitte vorhandene Spielernamen verwenden"})
		return
	}
	p1, p2 = canonicalKnownPlayer(ov, p1), canonicalKnownPlayer(ov, p2)
	winner = canonicalKnownPlayer(ov, winner)
	if matchPairExistsOnDay(ov, req.Matchday, p1, p2) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "Diese Paarung ist an diesem Spieltag bereits als abgeschlossen gespeichert"})
		return
	}
	raw := walkoverRaw(season, "league", "", req.Matchday, p1, p2, winner)
	mf := MatchFile{Format: "autodarts-match-analytics-league-match", Version: 1, League: LeagueInfo{Name: league, Matchdays: 10, SeasonID: season.ID, SeasonName: season.Name}, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), Match: raw}
	action, err := s.writeMatchFile(mf)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	go s.generateOverviewForLeague(league)
	s.queuePublishTrigger()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": action, "match": json.RawMessage(raw)})
}

func finalPlacements(st *FinalState) []string {
	if st == nil || st.Status != "completed" || len(st.Fixtures) != 5 {
		return nil
	}
	return []string{st.Fixtures[4].Winner, st.Fixtures[4].Loser, st.Fixtures[3].Winner, st.Fixtures[3].Loser, st.Fixtures[0].Winner, st.Fixtures[0].Loser}
}

func writeFinalSectionHTML(b *strings.Builder, st *FinalState) {
	if st == nil {
		return
	}
	b.WriteString(`<style>.final-panel{position:relative}.final-lock{opacity:.58;filter:grayscale(.45)}.final-progress{display:flex;gap:10px;flex-wrap:wrap;margin:10px 0 16px}.final-progress span{border:1px solid #30465d;border-radius:999px;padding:6px 10px;font-size:12px;color:#bdd4e9}.frozen-badge{display:inline-block;margin-left:8px;border:1px solid #4aa882;background:#102b27;color:#74efbc;border-radius:999px;padding:4px 8px;font-size:11px;font-weight:800}.final-order{display:grid;gap:10px}.final-game{position:relative;border:1px solid #2b4054;background:#0d1826;border-radius:12px;padding:12px}.final-game.ready{border-color:#52a8f5;box-shadow:0 0 0 1px #52a8f533}.final-game.completed{border-color:#2b8d68;background:#102722}.final-game.locked{opacity:.58}.final-game .ord{font-size:10px;letter-spacing:.12em;color:#8ec4ff;font-weight:900}.final-game h3{margin:4px 0 10px;font-size:14px}.final-players{display:grid;grid-template-columns:1fr auto 1fr;gap:8px;align-items:center}.final-players span:last-child{text-align:right}.final-score{font-weight:900;font-size:18px}.final-winner{color:#59e391;font-weight:800}.champion{margin:12px 0 18px;padding:20px;border:1px solid #e7b84b;background:linear-gradient(135deg,#352711,#151d2a);border-radius:16px;text-align:center}.champion .cup{font-size:28px}.champion strong{display:block;font-size:28px;margin:4px}.final-bracket{display:grid;grid-template-columns:1.15fr .9fr 1fr;gap:28px;align-items:center;margin:18px 0}.bracket-col{display:grid;gap:12px;position:relative}.bracket-col.link-right:after{content:'';position:absolute;right:-28px;top:50%;width:28px;border-top:2px solid #35536e}.bracket-title{font-size:11px;letter-spacing:.12em;color:#8ec4ff;font-weight:900;text-align:center}.bracket-placeholder{display:grid;grid-template-columns:1fr auto 1fr;gap:8px;align-items:center;color:#9cb0c5;font-size:12px}.bracket-placeholder span:last-child{text-align:right}.final-sequence{display:flex;gap:7px;flex-wrap:wrap;margin:12px 0 16px}.final-sequence span{font-size:11px;border:1px solid #2b4054;border-radius:999px;padding:5px 8px;color:#a9bdd0}.final-sequence b{color:#e9f5ff}.final-stats-note{margin:8px 0;color:#9cb0c5;font-size:12px}@media(max-width:760px){.final-bracket{grid-template-columns:1fr}.final-players{grid-template-columns:1fr auto 1fr}}</style>`)
	class := "panel final-panel"
	if !st.Available {
		class += " final-lock"
	}
	b.WriteString(`<div class="` + class + `"><h2>Finalspieltag`)
	if st.RegularLocked {
		b.WriteString(`<span class="frozen-badge">Ligatabelle eingefroren</span>`)
	}
	b.WriteString(`</h2>`)
	completedFinals := 0
	for _, f := range st.Fixtures {
		if finalFixtureComplete(f) {
			completedFinals++
		}
	}
	fmt.Fprintf(b, `<div class="final-progress"><span>%d/%d Ligaspiele</span><span>%d/%d vollständige Spieltage</span><span>%d/%d Paarungen vollständig</span>`, st.Progress.CompletedMatches, st.Progress.ExpectedMatches, st.Progress.MatchdaysComplete, expectedMatchdays, st.Progress.PairingsComplete, st.Progress.PairingsExpected)
	if st.Available {
		fmt.Fprintf(b, `<span>%d/5 Finalspiele</span>`, completedFinals)
	}
	b.WriteString(`</div>`)
	if !st.Available {
		b.WriteString(`<div class="muted">Noch gesperrt: ` + htmlE(st.Progress.Reason) + `. Die Finalrunde wird automatisch erzeugt, sobald die reguläre Saison vollständig ist.</div>`)
		b.WriteString(`<div class="final-sequence"><span><b>1</b> Platz 5/6</span><span><b>2</b> Halbfinale 1</span><span><b>3</b> Halbfinale 2</span><span><b>4</b> Platz 3</span><span><b>5</b> Finale</span></div>`)
		renderLocked := func(f FinalFixture, p1, p2 string) {
			fmt.Fprintf(b, `<div class="final-game locked"><span class="ord">SPIEL %d</span><h3>%s</h3><div class="bracket-placeholder"><span>%s</span><b>vs.</b><span>%s</span></div></div>`, f.Order, htmlE(f.Label), htmlE(p1), htmlE(p2))
		}
		b.WriteString(`<div class="bracket-title" style="text-align:left;margin-top:14px">PLATZ 5/6</div>`)
		renderLocked(st.Fixtures[0], "Platz 5", "Platz 6")
		b.WriteString(`<div class="final-bracket"><div class="bracket-col link-right"><div class="bracket-title">HALBFINALE</div>`)
		renderLocked(st.Fixtures[1], "Platz 1", "Platz 4")
		renderLocked(st.Fixtures[2], "Platz 2", "Platz 3")
		b.WriteString(`</div><div class="bracket-col link-right"><div class="bracket-title">SPIEL UM PLATZ 3</div>`)
		renderLocked(st.Fixtures[3], "Verlierer HF1", "Verlierer HF2")
		b.WriteString(`</div><div class="bracket-col"><div class="bracket-title">FINALE</div>`)
		renderLocked(st.Fixtures[4], "Sieger HF1", "Sieger HF2")
		b.WriteString(`</div></div></div>`)
		return
	}
	if st.Champion != "" {
		title := "FINALSIEGER"
		note := " · Saisonabschluss wartet auf die übrigen Finalspiele"
		if st.Status == "completed" {
			title = "LIGAMEISTER"
			note = ""
		}
		b.WriteString(`<div class="champion"><div class="cup">🏆</div><span class="muted">` + htmlE(title) + ` · ` + htmlE(st.SeasonName) + htmlE(note) + `</span><strong>` + htmlE(st.Champion) + `</strong></div>`)
	}
	b.WriteString(`<div class="muted" style="margin:8px 0 12px">Flexible Reihenfolge: Platz 5/6 und beide Halbfinals sind unabhängig spielbar. Nach Abschluss beider Halbfinals werden Platz 3 und Finale gleichzeitig freigegeben.</div>`)
	renderGame := func(f FinalFixture) {
		score := "–"
		if f.Walkover {
			score = "WO"
		} else if len(f.Score) >= 2 {
			score = fmt.Sprintf("%d : %d", f.Score[0], f.Score[1])
		}
		p1, p2 := f.Player1, f.Player2
		if p1 == "" {
			p1 = "offen"
		}
		if p2 == "" {
			p2 = "offen"
		}
		fmt.Fprintf(b, `<div class="final-game %s"><span class="ord">SPIEL %d</span><h3>%s</h3><div class="final-players"><span class="%s">%s</span><span class="final-score">%s</span><span class="%s">%s</span></div>`, htmlE(f.Status), f.Order, htmlE(f.Label), map[bool]string{true: "final-winner", false: ""}[namesMatch(f.Winner, f.Player1)], htmlE(p1), htmlE(score), map[bool]string{true: "final-winner", false: ""}[namesMatch(f.Winner, f.Player2)], htmlE(p2))
		if f.Status == "ready" {
			b.WriteString(`<div class="muted" style="margin-top:8px">Spielbar · Reihenfolge frei wählbar</div>`)
		} else if f.Walkover {
			b.WriteString(`<div class="muted" style="margin-top:8px">kampflos gewertet · keine Dartstatistik erzeugt</div>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`<div class="final-bracket"><div class="bracket-col"><div class="bracket-title">HALBFINALE</div>`)
	renderGame(st.Fixtures[1])
	renderGame(st.Fixtures[2])
	b.WriteString(`</div><div class="bracket-col"><div class="bracket-title">PLATZ 3</div>`)
	renderGame(st.Fixtures[3])
	b.WriteString(`</div><div class="bracket-col"><div class="bracket-title">FINALE</div>`)
	renderGame(st.Fixtures[4])
	b.WriteString(`</div></div><div class="bracket-title" style="text-align:left;margin-top:16px">PLATZ 5/6</div>`)
	renderGame(st.Fixtures[0])
	if st.Status == "completed" {
		places := finalPlacements(st)
		if len(places) == 6 {
			b.WriteString(`<h2 style="margin-top:20px">Saisonabschluss</h2><table><thead><tr><th>Platz</th><th>Spieler</th></tr></thead><tbody>`)
			for i, n := range places {
				fmt.Fprintf(b, `<tr><td>%d.</td><td><b>%s</b></td></tr>`, i+1, htmlE(n))
			}
			b.WriteString(`</tbody></table>`)
		}
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div class="panel"><h2>Finalspieltag · Statistik</h2><div class="final-stats-note">Diese Werte enthalten ausschließlich tatsächlich gespielte Finalspiele. Kampflose Wertungen erzeugen keine künstlichen Dartstatistiken und die reguläre Saisonstatistik bleibt unverändert.</div>`)
	if len(st.FinalStats) == 0 {
		b.WriteString(`<div class="muted">Noch keine tatsächlich gespielten Finalmatches mit Statistik.</div></div>`)
		return
	}
	b.WriteString(`<table><thead><tr><th>Spieler</th><th>Sp.</th><th>S</th><th>N</th><th>Legs</th><th>+/-</th><th>Ø AVG</th><th>CO %</th><th>95+</th><th>114+</th><th>133+</th><th>171</th><th>174</th><th>177</th><th>180</th><th>HF</th></tr></thead><tbody>`)
	for _, p := range st.FinalStats {
		fmt.Fprintf(b, `<tr><td><b>%s</b></td><td>%d</td><td>%d</td><td>%d</td><td>%d:%d</td><td>%+d</td><td>%.2f</td><td>%.1f %%</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td></tr>`, htmlE(p.Name), p.Matches, p.Wins, p.Losses, p.LegsFor, p.LegsAgainst, p.LegDiff, p.Average, p.CheckoutPct, p.Scores95Plus, p.Scores114Plus, p.Scores133Plus, p.Scores171, p.Scores174, p.Scores177, p.OneEighties, p.HighFinish)
	}
	b.WriteString(`</tbody></table></div>`)
}
