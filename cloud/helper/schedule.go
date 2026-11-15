package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ScheduleRow is the central, season-aware fixture format consumed by the
// GitHub-Pages Spielplan renderer. Dates/times deliberately remain empty.
type ScheduleRow struct {
	Index   int
	Season  string
	Day     int
	Date    string
	Time    string
	Player1 string
	Player2 string
	Place   string
	Note    string
}

type ScheduleSummary struct {
	SeasonID    string   `json:"seasonId"`
	Matchdays   int      `json:"matchdays"`
	Matches     int      `json:"matches"`
	Players     []string `json:"players"`
	Path        string   `json:"path"`
	ArchivePath string   `json:"archivePath"`
}

var scheduleHeader = []string{"index", "Saison", "Spieltag", "Datum", "Uhrzeit", "Spieler1", "Spieler2", "Ort", "Hinweis"}

func uniqueCleanPlayers(players []string) ([]string, error) {
	if len(players) != expectedLeaguePlayers {
		return nil, fmt.Errorf("für einen automatischen Spielplan werden genau %d Spieler benötigt (gefunden: %d)", expectedLeaguePlayers, len(players))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(players))
	for _, p := range players {
		p = cleanPlayerName(p)
		k := keyName(p)
		if k == "" {
			return nil, errors.New("Spielername darf nicht leer sein")
		}
		if seen[k] {
			return nil, fmt.Errorf("Spieler %q ist doppelt vorhanden", p)
		}
		seen[k] = true
		out = append(out, p)
	}
	return out, nil
}

// generateSeasonSchedule uses the circle method for six players. The first
// five rounds are the first leg; rounds 6-10 repeat the exact pairings with
// reversed player order, giving every pairing one nominal home/away direction.
func generateSeasonSchedule(seasonID string, players []string) ([]ScheduleRow, error) {
	players, err := uniqueCleanPlayers(players)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(seasonID) == "" {
		return nil, errors.New("Saison-ID fehlt")
	}
	ring := append([]string(nil), players...)
	first := make([]ScheduleRow, 0, expectedLeagueMatches/2)
	idx := 0
	for round := 0; round < expectedMatchdays/2; round++ {
		for i := 0; i < len(ring)/2; i++ {
			a, b := ring[i], ring[len(ring)-1-i]
			// Alternate the fixed player's side to avoid a visually one-sided
			// first leg. The return leg is always the exact reverse.
			if i == 0 && round%2 == 1 {
				a, b = b, a
			}
			first = append(first, ScheduleRow{Index: idx, Season: seasonID, Day: round + 1, Player1: a, Player2: b})
			idx++
		}
		// Keep position 0 fixed and rotate the remaining players clockwise.
		last := ring[len(ring)-1]
		copy(ring[2:], ring[1:len(ring)-1])
		ring[1] = last
	}
	out := append([]ScheduleRow(nil), first...)
	for _, r := range first {
		out = append(out, ScheduleRow{Index: idx, Season: seasonID, Day: r.Day + expectedMatchdays/2, Player1: r.Player2, Player2: r.Player1})
		idx++
	}
	if err := validateSeasonSchedule(out, seasonID, players); err != nil {
		return nil, err
	}
	return out, nil
}

func validateSeasonSchedule(rows []ScheduleRow, seasonID string, players []string) error {
	players, err := uniqueCleanPlayers(players)
	if err != nil {
		return err
	}
	if len(rows) != expectedLeagueMatches {
		return fmt.Errorf("Spielplan enthält %d statt %d Spiele", len(rows), expectedLeagueMatches)
	}
	allowed := map[string]bool{}
	for _, p := range players {
		allowed[keyName(p)] = true
	}
	dayCount := map[int]int{}
	dayPlayers := map[int]map[string]bool{}
	pairCount := map[string]int{}
	direction := map[string]int{}
	for _, r := range rows {
		if r.Season != seasonID {
			return fmt.Errorf("falsche Saison-ID im Spielplan: %q", r.Season)
		}
		if r.Day < 1 || r.Day > expectedMatchdays {
			return fmt.Errorf("ungültiger Spieltag %d", r.Day)
		}
		a, b := keyName(r.Player1), keyName(r.Player2)
		if !allowed[a] || !allowed[b] || a == b {
			return fmt.Errorf("ungültige Paarung an Spieltag %d: %s / %s", r.Day, r.Player1, r.Player2)
		}
		if dayPlayers[r.Day] == nil {
			dayPlayers[r.Day] = map[string]bool{}
		}
		if dayPlayers[r.Day][a] || dayPlayers[r.Day][b] {
			return fmt.Errorf("Spieler doppelt an Spieltag %d", r.Day)
		}
		dayPlayers[r.Day][a], dayPlayers[r.Day][b] = true, true
		dayCount[r.Day]++
		pairCount[pairKey(r.Player1, r.Player2)]++
		direction[a+"\x00"+b]++
	}
	for day := 1; day <= expectedMatchdays; day++ {
		if dayCount[day] != expectedMatchesPerDay || len(dayPlayers[day]) != expectedLeaguePlayers {
			return fmt.Errorf("Spieltag %d ist unvollständig: %d Spiele, %d Spieler", day, dayCount[day], len(dayPlayers[day]))
		}
	}
	if len(pairCount) != expectedLeaguePlayers*(expectedLeaguePlayers-1)/2 {
		return fmt.Errorf("Spielplan enthält %d statt 15 unterschiedliche Paarungen", len(pairCount))
	}
	for pair, n := range pairCount {
		if n != 2 {
			return fmt.Errorf("Paarung %q kommt %d statt zweimal vor", pair, n)
		}
		parts := strings.Split(pair, "\x00")
		if len(parts) == 2 && (direction[parts[0]+"\x00"+parts[1]] != 1 || direction[parts[1]+"\x00"+parts[0]] != 1) {
			return fmt.Errorf("Paarung %q ist nicht einmal je Richtung angesetzt", pair)
		}
	}
	return nil
}

func scheduleCSVBytes(rows []ScheduleRow) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	if err := w.Write(scheduleHeader); err != nil {
		return nil, err
	}
	for i, r := range rows {
		idx := r.Index
		if idx < 0 {
			idx = i
		}
		rec := []string{strconv.Itoa(idx), r.Season, strconv.Itoa(r.Day), r.Date, r.Time, r.Player1, r.Player2, r.Place, r.Note}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func readScheduleCSV(path string) ([]ScheduleRow, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})))
	r.Comma = ';'
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("Spielplan.csv ist nicht lesbar: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	pos := map[string]int{}
	for i, h := range records[0] {
		pos[strings.TrimSpace(h)] = i
	}
	for _, h := range scheduleHeader[1:] {
		if _, ok := pos[h]; !ok {
			return nil, fmt.Errorf("Spielplan.csv: Spalte %q fehlt", h)
		}
	}
	val := func(rec []string, key string) string {
		if i, ok := pos[key]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	out := make([]ScheduleRow, 0, len(records)-1)
	for line, rec := range records[1:] {
		if len(rec) == 0 {
			continue
		}
		a, b := val(rec, "Spieler1"), val(rec, "Spieler2")
		if a == "" && b == "" {
			continue
		}
		day, e := strconv.Atoi(val(rec, "Spieltag"))
		if e != nil {
			return nil, fmt.Errorf("Spielplan.csv Zeile %d: ungültiger Spieltag", line+2)
		}
		idx := len(out)
		if s := val(rec, "index"); s != "" {
			if x, e := strconv.Atoi(s); e == nil {
				idx = x
			}
		}
		out = append(out, ScheduleRow{Index: idx, Season: val(rec, "Saison"), Day: day, Date: val(rec, "Datum"), Time: val(rec, "Uhrzeit"), Player1: a, Player2: b, Place: val(rec, "Ort"), Note: val(rec, "Hinweis")})
	}
	return out, nil
}

func (s *Server) participantsForNextSeason(league string, current SeasonInfo) ([]string, error) {
	if !current.Closed {
		return nil, errors.New("die aktuelle Saison ist noch nicht abgeschlossen; der automatische Saisonwechsel ist erst nach dem vollständigen Finalspieltag möglich")
	}
	if st, err := s.loadFinalState(current.ID); err == nil {
		if p := finalPlacements(st); len(p) == expectedLeaguePlayers {
			if cleaned, e := uniqueCleanPlayers(p); e == nil {
				return cleaned, nil
			}
		}
	}
	raw, err := s.collectOverviewMatches(league, current.ID)
	if err != nil {
		return nil, err
	}
	ov := makeLeagueOverview(league, current.ID, current.Name, false, raw)
	if len(ov.Players) != expectedLeaguePlayers {
		return nil, fmt.Errorf("aus der abgeschlossenen Saison wurden %d statt %d Spieler erkannt", len(ov.Players), expectedLeaguePlayers)
	}
	names := make([]string, 0, expectedLeaguePlayers)
	for _, p := range ov.Players {
		names = append(names, p.Name)
	}
	return uniqueCleanPlayers(names)
}

// installSeasonSchedule writes both the central multi-season Spielplan.csv and
// a season-specific archive copy. It returns a rollback closure so season
// creation remains atomic if writing Seasons.json fails afterwards.
func (s *Server) installSeasonSchedule(season SeasonInfo, players []string) (ScheduleSummary, func(), error) {
	base := s.basePath()
	if base == "" {
		return ScheduleSummary{}, func() {}, errors.New("Liga-Datenordner ist noch nicht eingerichtet")
	}
	rows, err := generateSeasonSchedule(season.ID, players)
	if err != nil {
		return ScheduleSummary{}, func() {}, err
	}
	central := filepath.Join(base, "Spielplan.csv")
	oldBytes, readErr := os.ReadFile(central)
	oldExists := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return ScheduleSummary{}, func() {}, readErr
	}
	existing, err := readScheduleCSV(central)
	if err != nil {
		return ScheduleSummary{}, func() {}, err
	}
	for _, r := range existing {
		if strings.EqualFold(strings.TrimSpace(r.Season), season.ID) {
			return ScheduleSummary{}, func() {}, errors.New("für die neue Saison existiert bereits ein Spielplan")
		}
	}
	combined := append([]ScheduleRow(nil), existing...)
	combined = append(combined, rows...)
	for i := range combined {
		combined[i].Index = i
	}
	centralBytes, err := scheduleCSVBytes(combined)
	if err != nil {
		return ScheduleSummary{}, func() {}, err
	}
	archiveRows := append([]ScheduleRow(nil), rows...)
	for i := range archiveRows {
		archiveRows[i].Index = i
	}
	archiveBytes, err := scheduleCSVBytes(archiveRows)
	if err != nil {
		return ScheduleSummary{}, func() {}, err
	}
	archiveDir := filepath.Join(base, "Spielplaene")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return ScheduleSummary{}, func() {}, err
	}
	archive := filepath.Join(archiveDir, "Spielplan_"+safeOverviewPart(season.ID)+".csv")
	if _, err := os.Stat(archive); err == nil {
		return ScheduleSummary{}, func() {}, errors.New("saisonbezogener Spielplan existiert bereits")
	}
	if err := writeAtomic(central, centralBytes, 0o644); err != nil {
		return ScheduleSummary{}, func() {}, err
	}
	if err := writeAtomic(archive, archiveBytes, 0o644); err != nil {
		if oldExists {
			_ = writeAtomic(central, oldBytes, 0o644)
		} else {
			_ = os.Remove(central)
		}
		return ScheduleSummary{}, func() {}, err
	}
	rollback := func() {
		_ = os.Remove(archive)
		if oldExists {
			_ = writeAtomic(central, oldBytes, 0o644)
		} else {
			_ = os.Remove(central)
		}
	}
	summary := ScheduleSummary{SeasonID: season.ID, Matchdays: expectedMatchdays, Matches: expectedLeagueMatches, Players: append([]string(nil), players...), Path: central, ArchivePath: archive}
	return summary, rollback, nil
}

func schedulePairStrings(rows []ScheduleRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%02d:%s-%s", r.Day, r.Player1, r.Player2))
	}
	sort.Strings(out)
	return out
}
