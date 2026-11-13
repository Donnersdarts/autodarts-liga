package main

// v0.9.4: Guard every single-match AND bulk upload against regressions from
// stale browser history. Zero is a real statistic. Missing/null is not zero.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var exclusiveScoringKeys = []string{
	"scores57Plus", "scores95Plus", "scores114Plus", "scores133Plus",
	"scores171", "scores174", "scores177", "oneEighties",
}

func matchMap(raw json.RawMessage) (map[string]any, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("ungültige Match-JSON-Struktur")
	}
	return obj, nil
}

func metricCount(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 1000000 && n == math.Trunc(n)
}

func scoringMetrics(p map[string]any) (map[string]any, error) {
	m, has := p["metrics"]
	if !has || m == nil {
		return nil, nil
	}
	obj, ok := m.(map[string]any)
	if !ok {
		return nil, errors.New("metrics ist kein Objekt")
	}
	for _, key := range exclusiveScoringKeys {
		if v, exists := obj[key]; exists && v != nil {
			if _, valid := metricCount(v); !valid {
				return nil, fmt.Errorf("%s: ungueltiger Zaehlwert (auch 0 ist gueltig)", key)
			}
		}
	}
	return obj, nil
}

func playerIdentity(p map[string]any) string {
	for _, key := range []string{"autodartsName", "name"} {
		if str, ok := p[key].(string); ok && strings.TrimSpace(str) != "" {
			return strings.ToLower(strings.TrimSpace(str))
		}
	}
	return ""
}

func playerList(m map[string]any) ([]any, error) {
	ps, ok := m["players"].([]any)
	if !ok || len(ps) != 2 {
		return nil, errors.New("genau zwei Spieler erwartet")
	}
	for i, p := range ps {
		obj, ok := p.(map[string]any)
		if !ok || playerIdentity(obj) == "" {
			return nil, fmt.Errorf("Spieler %d: Identitaet fehlt", i+1)
		}
		if _, err := scoringMetrics(obj); err != nil {
			return nil, fmt.Errorf("Spieler %d: %w", i+1, err)
		}
	}
	return ps, nil
}

// Preserve nested keys of the cloud copy if a local historic copy omits them.
// For non-scoring metadata, incoming nonempty values are kept as before.
func preserveMissing(dst, old map[string]any) {
	for k, v := range old {
		newer, exists := dst[k]
		if !exists || newer == nil {
			dst[k] = v
			continue
		}
		if a, ok := newer.(map[string]any); ok {
			if b, ok := v.(map[string]any); ok {
				preserveMissing(a, b)
			}
		}
		if (k == "realName" || k == "nameMappingUpdatedAt") && newer == "" && v != nil && v != "" {
			dst[k] = v
		}
	}
}

// Existing valid scoring counts always win over missing input, even when zero.
// Conflicting numeric values are rejected rather than silently overwritten.
func mergeProtectedMatch(stored, incoming json.RawMessage) (json.RawMessage, error) {
	old, err := matchMap(stored)
	if err != nil {
		return nil, fmt.Errorf("bestehender Eintrag: %w", err)
	}
	next, err := matchMap(incoming)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(fmt.Sprint(old["matchId"]), fmt.Sprint(next["matchId"])) {
		return nil, errors.New("Match-ID stimmt nicht überein")
	}
	if old["usageMode"] != next["usageMode"] {
		return nil, errors.New("Match-Modus stimmt nicht überein")
	}
	oscore, ok1 := old["score"].([]any)
	nscore, ok2 := next["score"].([]any)
	if !ok1 || !ok2 || len(oscore) != len(nscore) {
		return nil, errors.New("Match-Ergebnis fehlt oder unterscheidet sich")
	}
	b1, _ := json.Marshal(oscore)
	b2, _ := json.Marshal(nscore)
	if !bytes.Equal(b1, b2) {
		return nil, errors.New("Match-Ergebnis ist abweichend; kein automatisches Ueberschreiben")
	}
	oldPS, err := playerList(old)
	if err != nil {
		return nil, err
	}
	nextPS, err := playerList(next)
	if err != nil {
		return nil, err
	}
	for i := range oldPS {
		op := oldPS[i].(map[string]any)
		np := nextPS[i].(map[string]any)
		if playerIdentity(op) != playerIdentity(np) {
			return nil, fmt.Errorf("Spieler %d weicht ab; keine automatische Zuordnung", i+1)
		}
		om, _ := scoringMetrics(op)
		nm, _ := scoringMetrics(np)
		for _, key := range exclusiveScoringKeys {
			var ov, nv any
			if om != nil {
				ov = om[key]
			}
			if nm != nil {
				nv = nm[key]
			}
			if ov != nil && nv != nil && ov.(float64) != nv.(float64) {
				return nil, fmt.Errorf("Scoring-Konflikt bei %s, Spieler %s: OneDrive=%v, lokal=%v", key, playerIdentity(op), ov, nv)
			}
		}
		preserveMissing(np, op)
	}
	preserveMissing(next, old)
	b, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func validateNewMatch(raw json.RawMessage) error {
	m, err := matchMap(raw)
	if err != nil {
		return err
	}
	_, err = playerList(m)
	return err
}

func randomSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Immutable, byte-verified backup *before* modifying an existing shared file.
func backupOriginal(base, matchID string, data []byte) error {
	dir := filepath.Join(base, "Backup-Matches-v0.9.4")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := strings.ToLower(matchID) + "-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + randomSuffix() + ".json"
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	copied, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, copied) {
		return errors.New("Backup-Kontrolle fehlgeschlagen; Match wird nicht geschrieben")
	}
	return nil
}

func safeMatchReplace(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".autodarts-match-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// On Linux this is an atomic replacement. On Windows the fallback moves
	// the old file aside first, then restores it if the new rename fails.
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return os.Rename(tmp, path)
	}
	displaced := path + ".rollback-" + randomSuffix()
	if err := os.Rename(path, displaced); err != nil {
		return fmt.Errorf("Vorversion kann nicht geschuetzt werden: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		restoreErr := os.Rename(displaced, path)
		if restoreErr != nil {
			return fmt.Errorf("Neuschreiben fehlgeschlagen (%v), ROLLBACK FEHLGESCHLAGEN (%v), Sicherheitskopie: %s", err, restoreErr, displaced)
		}
		return err
	}
	if err := os.Remove(displaced); err != nil {
		return fmt.Errorf("Datei aktualisiert, alte Zwischenkopie konnte nicht entfernt werden: %w", err)
	}
	return nil
}

func (s *Server) writeMatchFileLocked(mf MatchFile) (string, error) {
	if mf.Format != "autodarts-match-analytics-league-match" {
		return "", errors.New("unbekanntes Match-Dateiformat")
	}
	if strings.TrimSpace(mf.League.Name) == "" {
		return "", errors.New("Liga-Name fehlt")
	}
	var entry MatchEntry
	if err := json.Unmarshal(mf.Match, &entry); err != nil {
		return "", errors.New("Matchdaten sind ungültig")
	}
	if !looksLikeUUID(entry.MatchID) {
		return "", errors.New("ungültige Match-ID")
	}
	if entry.UsageMode != "league" || entry.Matchday < 1 || entry.Matchday > 10 {
		return "", errors.New("nur Liga-Matches mit Spieltag 1–10 dürfen synchronisiert werden")
	}
	if s.readDeletedIDs(mf.League.Name)[strings.ToLower(entry.MatchID)] {
		return "", errors.New("dieses Liga-Match wurde zentral gelöscht")
	}
	dir, err := s.ensureConfigured()
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, strings.ToLower(entry.MatchID)+".json")
	existingBytes, err := os.ReadFile(target)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var previous MatchFile
	if exists {
		if err := json.Unmarshal(existingBytes, &previous); err != nil || previous.Format != mf.Format ||
			!strings.EqualFold(strings.TrimSpace(previous.League.Name), strings.TrimSpace(mf.League.Name)) {
			return "", errors.New("bestehende Matchdatei beschädigt oder andere Liga; Schreibzugriff abgebrochen")
		}
		var priorEntry MatchEntry
		if err := json.Unmarshal(previous.Match, &priorEntry); err != nil || !strings.EqualFold(priorEntry.MatchID, entry.MatchID) {
			return "", errors.New("bestehende Matchdatei hat andere Match-ID; Schreibzugriff abgebrochen")
		}
		if previous.League.SeasonID != "" && entry.SeasonID == "" && mf.League.SeasonID == "" {
			entry.SeasonID = previous.League.SeasonID
			entry.SeasonName = previous.League.SeasonName
		}
	}
	seasonID := strings.TrimSpace(entry.SeasonID)
	if seasonID == "" {
		seasonID = strings.TrimSpace(mf.League.SeasonID)
	}
	seasonName := strings.TrimSpace(entry.SeasonName)
	if seasonName == "" {
		seasonName = strings.TrimSpace(mf.League.SeasonName)
	}
	season, _, err := s.resolveSeason(mf.League.Name, seasonID, seasonName)
	if err != nil {
		return "", err
	}
	frozen := s.isSeasonFrozen(season.ID)
	if exists && previous.League.SeasonID != "" && previous.League.SeasonID != season.ID {
		return "", errors.New("Saisonkonflikt: bestehendes Match darf nicht in andere Saison verschoben werden")
	}
	mf.League.SeasonID, mf.League.SeasonName = season.ID, season.Name
	mf.Match, err = injectSeasonIntoMatch(mf.Match, season)
	if err != nil {
		return "", err
	}
	if mf.Version == 0 {
		mf.Version = 1
	}
	if exists {
		mf.Match, err = mergeProtectedMatch(previous.Match, mf.Match)
		if err != nil {
			return "", err
		}
		// Ignore administrative timestamp. Identical payloads need no file write.
		newData, _ := matchMap(mf.Match)
		oldData, _ := matchMap(previous.Match)
		nb, _ := json.Marshal(newData)
		ob, _ := json.Marshal(oldData)
		if bytes.Equal(nb, ob) {
			return "unchanged", nil
		}
		if frozen {
			return "", errors.New("Ligatabelle ist eingefroren; reguläre Saisonspiele können nach Freigabe des Finalspieltags nicht mehr geändert werden")
		}
	} else {
		if frozen {
			return "", errors.New("Ligatabelle ist eingefroren; reguläre Saisonspiele können nach Freigabe des Finalspieltags nicht mehr geändert werden")
		}
		if err := validateNewMatch(mf.Match); err != nil {
			return "", err
		}
	}
	mf.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	out, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return "", err
	}
	if exists {
		if err := backupOriginal(s.basePath(), entry.MatchID, existingBytes); err != nil {
			return "", fmt.Errorf("Backup vor Aktualisierung fehlgeschlagen: %w", err)
		}
	}
	if err := safeMatchReplace(target, out); err != nil {
		return "", err
	}
	if exists {
		return "updated", nil
	}
	return "created", nil
}
