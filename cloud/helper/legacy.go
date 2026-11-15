package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type HistoricalSeason struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Placements []string `json:"placements"`
	Note       string   `json:"note,omitempty"`
}

type HistoricalSeasonFile struct {
	Format  string             `json:"format"`
	Version int                `json:"version"`
	Seasons []HistoricalSeason `json:"seasons"`
}

type PlacementHistoryItem struct {
	SeasonID   string
	SeasonName string
	Place      int
	Historical bool
}

func historicalSeasonPath(base string) string {
	if strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(base, "Historie", "historische-saisons.json")
}

func validateHistoricalSeason(s HistoricalSeason) error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("ID oder Name fehlt")
	}
	if len(s.Placements) != expectedLeaguePlayers {
		return fmt.Errorf("%s: %d Platzierungen statt %d", s.Name, len(s.Placements), expectedLeaguePlayers)
	}
	seen := map[string]bool{}
	for i, p := range s.Placements {
		k := keyName(p)
		if k == "" {
			return fmt.Errorf("%s: Platz %d ist leer", s.Name, i+1)
		}
		if seen[k] {
			return fmt.Errorf("%s: Spieler %s doppelt", s.Name, p)
		}
		seen[k] = true
	}
	return nil
}

func loadHistoricalSeasons(base string) []HistoricalSeason {
	path := historicalSeasonPath(base)
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f HistoricalSeasonFile
	if json.Unmarshal(b, &f) != nil || f.Format != "autodarts-liga-historical-seasons" || f.Version != 1 {
		return nil
	}
	out := make([]HistoricalSeason, 0, len(f.Seasons))
	ids := map[string]bool{}
	for _, s := range f.Seasons {
		if validateHistoricalSeason(s) != nil || ids[strings.ToLower(strings.TrimSpace(s.ID))] {
			continue
		}
		ids[strings.ToLower(strings.TrimSpace(s.ID))] = true
		for i := range s.Placements {
			s.Placements[i] = cleanPlayerName(s.Placements[i])
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func writeHistoricalSeasonsHTML(b *strings.Builder, seasons []HistoricalSeason) {
	if len(seasons) == 0 {
		return
	}
	b.WriteString(`<div class="panel"><h2>🕰️ Historische Saisons</h2><p class="muted">Saisons vor Einführung der automatischen Liga-Auswertung. Es werden nur sicher bekannte Abschlussplatzierungen übernommen; Matchstatistiken, Elo, Rekorde und Awards werden daraus nicht erfunden.</p>`)
	for _, s := range seasons {
		fmt.Fprintf(b, `<div class="record" style="margin-top:12px"><small>Historisch übernommen</small><strong>%s</strong>`, htmlE(s.Name))
		if strings.TrimSpace(s.Note) != "" {
			fmt.Fprintf(b, `<small>%s</small>`, htmlE(s.Note))
		}
		b.WriteString(`<table style="margin-top:10px;min-width:0"><thead><tr><th>Platz</th><th>Spieler</th></tr></thead><tbody>`)
		for i, p := range s.Placements {
			medal := ""
			switch i {
			case 0:
				medal = " 🏆"
			case 1:
				medal = " 🥈"
			case 2:
				medal = " 🥉"
			}
			fmt.Fprintf(b, `<tr><td>%d%s</td><td><b>%s</b></td></tr>`, i+1, medal, htmlE(p))
		}
		b.WriteString(`</tbody></table></div>`)
	}
	b.WriteString(`</div>`)
}
