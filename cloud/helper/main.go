package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	appName       = "Autodarts Liga Sync Helper"
	appVersion    = "0.9.3"
	listenAddress = "127.0.0.1:17653"
)

type Config struct {
	BasePath string `json:"basePath"`
}

type SyncHealth struct {
	PathExists    bool   `json:"pathExists"`
	MountDetected bool   `json:"mountDetected"`
	MountPoint    string `json:"mountPoint,omitempty"`
	MountType     string `json:"mountType,omitempty"`
	MountSource   string `json:"mountSource,omitempty"`
	RcloneMounted bool   `json:"rcloneMounted"`
	SyncReady     bool   `json:"syncReady"`
	Status        string `json:"syncStatus"`
}

type LeagueInfo struct {
	Name       string `json:"name"`
	Matchdays  int    `json:"matchdays,omitempty"`
	SeasonID   string `json:"seasonId,omitempty"`
	SeasonName string `json:"seasonName,omitempty"`
}

type SeasonInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"createdAt"`
	ArchivedAt string `json:"archivedAt,omitempty"`
	Active     bool   `json:"active"`
}

type SeasonRegistry struct {
	Format          string       `json:"format"`
	Version         int          `json:"version"`
	LeagueName      string       `json:"leagueName"`
	CurrentSeasonID string       `json:"currentSeasonId"`
	UpdatedAt       string       `json:"updatedAt"`
	Seasons         []SeasonInfo `json:"seasons"`
}

type CreateSeasonRequest struct {
	League LeagueInfo `json:"league"`
	Name   string     `json:"name"`
}

type MatchEntry struct {
	MatchID             string          `json:"matchId"`
	SeasonID            string          `json:"seasonId,omitempty"`
	SeasonName          string          `json:"seasonName,omitempty"`
	SavedAt             string          `json:"savedAt,omitempty"`
	MatchDate           string          `json:"matchDate,omitempty"`
	Matchday            int             `json:"matchday"`
	AssignmentUpdatedAt string          `json:"assignmentUpdatedAt,omitempty"`
	UsageMode           string          `json:"usageMode"`
	Raw                 json.RawMessage `json:"-"`
}

type MatchFile struct {
	Format    string          `json:"format"`
	Version   int             `json:"version"`
	League    LeagueInfo      `json:"league"`
	UpdatedAt string          `json:"updatedAt"`
	Match     json.RawMessage `json:"match"`
}

type BulkRequest struct {
	League  LeagueInfo        `json:"league"`
	Matches []json.RawMessage `json:"matches"`
}

type DeleteRequest struct {
	League  LeagueInfo `json:"league"`
	MatchID string     `json:"matchId"`
}

type DeleteMarker struct {
	Format    string     `json:"format"`
	Version   int        `json:"version"`
	League    LeagueInfo `json:"league"`
	MatchID   string     `json:"matchId"`
	DeletedAt string     `json:"deletedAt"`
}

type Server struct {
	mu            sync.RWMutex
	seasonMu      sync.Mutex
	overviewMu    sync.Mutex
	webPublishMu  sync.Mutex
	webStatusMu   sync.RWMutex
	config        Config
	configPath    string
	webConfigPath string
	webStatus     WebPublishStatus
}

func main() {
	cfgPath, err := getConfigPath()
	if err != nil {
		log.Fatal(err)
	}
	srv := &Server{configPath: cfgPath, webConfigPath: filepath.Join(filepath.Dir(cfgPath), "webpublish.json")}
	_ = srv.loadConfig()
	srv.refreshWebPublishStatus()

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleRoot)
	mux.HandleFunc("/api/status", srv.handleStatus)
	mux.HandleFunc("/api/config", srv.handleConfig)
	mux.HandleFunc("/api/matches", srv.handleMatches)
	mux.HandleFunc("/api/match", srv.handleMatch)
	mux.HandleFunc("/api/bulk", srv.handleBulk)
	mux.HandleFunc("/api/delete", srv.handleDelete)
	mux.HandleFunc("/api/restore", srv.handleRestore)
	mux.HandleFunc("/api/seasons", srv.handleSeasons)
	mux.HandleFunc("/api/seasons/create", srv.handleSeasonCreate)
	mux.HandleFunc("/api/overview", srv.handleOverview)
	mux.HandleFunc("/api/web-status", srv.handleWebStatus)
	mux.HandleFunc("/api/web-publish", srv.handleWebPublish)
	mux.HandleFunc("/overview", srv.handleOverviewPage)

	go srv.overviewLoop()

	handler := srv.corsAndOriginGuard(mux)
	fmt.Printf("%s v%s\n", appName, appVersion)
	fmt.Printf("Lokaler Dienst: http://%s\n", listenAddress)
	if srv.basePath() == "" {
		fmt.Println("Noch kein Liga-Datenordner eingerichtet. Im Add-on unter Liga den Ordnerpfad speichern.")
	} else {
		fmt.Printf("Liga-Datenordner: %s\n", srv.basePath())
	}
	fmt.Println("Dieses Fenster geöffnet lassen, solange das Autodarts-Liga-Tool verwendet wird.")

	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func getConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "AutodartsLigaSync")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func (s *Server) loadConfig() error {
	b, err := os.ReadFile(s.configPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	return nil
}

func (s *Server) saveConfig(cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.configPath, b, 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	return nil
}

func (s *Server) basePath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.BasePath
}

func (s *Server) matchesPath() string {
	base := s.basePath()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Matches")
}

func (s *Server) deletedPath() string {
	base := s.basePath()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Deleted")
}

func (s *Server) seasonRegistryPath() string {
	base := s.basePath()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Seasons.json")
}

func defaultSeasonName(t time.Time) string {
	y := t.Year()
	if t.Month() < time.September {
		y--
	}
	return fmt.Sprintf("Saison %d/%d", y, y+1)
}

func seasonSlug(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	var b strings.Builder
	dash := false
	for _, r := range v {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "saison"
	}
	if len(out) > 36 {
		out = out[:36]
	}
	return out
}

func normalizeSeasonRegistry(reg *SeasonRegistry) {
	if reg.Format == "" {
		reg.Format = "autodarts-match-analytics-seasons"
	}
	if reg.Version == 0 {
		reg.Version = 1
	}
	for i := range reg.Seasons {
		reg.Seasons[i].Active = reg.Seasons[i].ID == reg.CurrentSeasonID
	}
}

func (s *Server) saveSeasonRegistryLocked(reg SeasonRegistry) error {
	normalizeSeasonRegistry(&reg)
	reg.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	path := s.seasonRegistryPath()
	if path == "" {
		return errors.New("Liga-Datenordner ist noch nicht eingerichtet")
	}
	return writeAtomic(path, b, 0o644)
}

func injectSeasonIntoMatch(raw json.RawMessage, season SeasonInfo) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("Matchdaten sind ungültig")
	}
	id, _ := json.Marshal(season.ID)
	name, _ := json.Marshal(season.Name)
	obj["seasonId"] = id
	obj["seasonName"] = name
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) migrateLegacyMatchesToSeason(league string, season SeasonInfo) {
	dir := s.matchesPath()
	entries, _ := os.ReadDir(dir)
	leagueNorm := strings.ToLower(strings.TrimSpace(league))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var mf MatchFile
		if json.Unmarshal(b, &mf) != nil || mf.Format != "autodarts-match-analytics-league-match" || strings.ToLower(strings.TrimSpace(mf.League.Name)) != leagueNorm {
			continue
		}
		var me MatchEntry
		if json.Unmarshal(mf.Match, &me) != nil || !looksLikeUUID(me.MatchID) || strings.TrimSpace(me.SeasonID) != "" {
			continue
		}
		preserved, err := injectSeasonIntoMatch(mf.Match, season)
		if err != nil {
			continue
		}
		mf.League.SeasonID, mf.League.SeasonName = season.ID, season.Name
		mf.Match = preserved
		mf.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		out, _ := json.MarshalIndent(mf, "", "  ")
		_ = writeAtomic(path, out, 0o644)
	}
}

func (s *Server) ensureSeasonRegistry(league string) (SeasonRegistry, error) {
	s.seasonMu.Lock()
	defer s.seasonMu.Unlock()
	if _, err := s.ensureConfigured(); err != nil {
		return SeasonRegistry{}, err
	}
	league = strings.TrimSpace(league)
	if league == "" {
		return SeasonRegistry{}, errors.New("Liga-Name fehlt")
	}
	path := s.seasonRegistryPath()
	if b, err := os.ReadFile(path); err == nil {
		var reg SeasonRegistry
		if json.Unmarshal(b, &reg) == nil && reg.Format == "autodarts-match-analytics-seasons" && len(reg.Seasons) > 0 {
			if strings.TrimSpace(reg.LeagueName) == "" {
				reg.LeagueName = league
			}
			if reg.CurrentSeasonID == "" {
				reg.CurrentSeasonID = reg.Seasons[len(reg.Seasons)-1].ID
			}
			normalizeSeasonRegistry(&reg)
			return reg, nil
		}
	}
	now := time.Now()
	name := defaultSeasonName(now)
	season := SeasonInfo{ID: seasonSlug(name), Name: name, CreatedAt: now.UTC().Format(time.RFC3339Nano), Active: true}
	reg := SeasonRegistry{Format: "autodarts-match-analytics-seasons", Version: 1, LeagueName: league, CurrentSeasonID: season.ID, Seasons: []SeasonInfo{season}}
	if err := s.saveSeasonRegistryLocked(reg); err != nil {
		return SeasonRegistry{}, err
	}
	s.migrateLegacyMatchesToSeason(league, season)
	return reg, nil
}

func seasonByID(reg SeasonRegistry, id string) (SeasonInfo, bool) {
	id = strings.TrimSpace(id)
	for _, season := range reg.Seasons {
		if season.ID == id {
			return season, true
		}
	}
	return SeasonInfo{}, false
}

func currentSeason(reg SeasonRegistry) (SeasonInfo, bool) {
	return seasonByID(reg, reg.CurrentSeasonID)
}

func (s *Server) resolveSeason(league, id, name string) (SeasonInfo, SeasonRegistry, error) {
	reg, err := s.ensureSeasonRegistry(league)
	if err != nil {
		return SeasonInfo{}, reg, err
	}
	if strings.TrimSpace(id) != "" {
		if season, ok := seasonByID(reg, strings.TrimSpace(id)); ok {
			return season, reg, nil
		}
	}
	if strings.TrimSpace(name) != "" {
		for _, season := range reg.Seasons {
			if strings.EqualFold(strings.TrimSpace(season.Name), strings.TrimSpace(name)) {
				return season, reg, nil
			}
		}
	}
	if season, ok := currentSeason(reg); ok {
		return season, reg, nil
	}
	return SeasonInfo{}, reg, errors.New("keine aktive Saison vorhanden")
}

func (s *Server) handleSeasons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	league := strings.TrimSpace(r.URL.Query().Get("league"))
	reg, err := s.ensureSeasonRegistry(league)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	current, _ := currentSeason(reg)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "league": league, "currentSeason": current, "currentSeasonId": reg.CurrentSeasonID, "seasons": reg.Seasons})
}

func (s *Server) handleSeasonCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req CreateSeasonRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	league := strings.TrimSpace(req.League.Name)
	name := strings.TrimSpace(req.Name)
	if league == "" || name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Liga- und Saisonname sind erforderlich"})
		return
	}
	if _, err := s.ensureConfigured(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.seasonMu.Lock()
	defer s.seasonMu.Unlock()
	// Registry innerhalb desselben Locks direkt lesen/erzeugen, um einen globalen Saisonwechsel atomar zu halten.
	path := s.seasonRegistryPath()
	var reg SeasonRegistry
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &reg)
	}
	if reg.Format != "autodarts-match-analytics-seasons" || len(reg.Seasons) == 0 {
		now := time.Now()
		oldName := defaultSeasonName(now)
		old := SeasonInfo{ID: seasonSlug(oldName), Name: oldName, CreatedAt: now.UTC().Format(time.RFC3339Nano), Active: true}
		reg = SeasonRegistry{Format: "autodarts-match-analytics-seasons", Version: 1, LeagueName: league, CurrentSeasonID: old.ID, Seasons: []SeasonInfo{old}}
		s.migrateLegacyMatchesToSeason(league, old)
	}
	for _, season := range reg.Seasons {
		if strings.EqualFold(strings.TrimSpace(season.Name), name) {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "Eine Saison mit diesem Namen existiert bereits"})
			return
		}
	}
	now := time.Now().UTC()
	for i := range reg.Seasons {
		if reg.Seasons[i].ID == reg.CurrentSeasonID {
			reg.Seasons[i].Active = false
			if reg.Seasons[i].ArchivedAt == "" {
				reg.Seasons[i].ArchivedAt = now.Format(time.RFC3339Nano)
			}
		}
	}
	id := seasonSlug(name) + "-" + now.Format("20060102t150405")
	newSeason := SeasonInfo{ID: id, Name: name, CreatedAt: now.Format(time.RFC3339Nano), Active: true}
	reg.Seasons = append(reg.Seasons, newSeason)
	reg.CurrentSeasonID = id
	reg.LeagueName = league
	if err := s.saveSeasonRegistryLocked(reg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	go s.generateOverviewForLeague(league)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "currentSeason": newSeason, "currentSeasonId": id, "seasons": reg.Seasons})
}

func (s *Server) readDeletedIDs(league string) map[string]bool {
	out := map[string]bool{}
	dir := s.deletedPath()
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	leagueNorm := strings.ToLower(strings.TrimSpace(league))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var dm DeleteMarker
		if json.Unmarshal(b, &dm) != nil || dm.Format != "autodarts-match-analytics-delete" || !looksLikeUUID(dm.MatchID) {
			continue
		}
		if leagueNorm != "" && strings.ToLower(strings.TrimSpace(dm.League.Name)) != leagueNorm {
			continue
		}
		out[strings.ToLower(dm.MatchID)] = true
	}
	return out
}

func expandPath(v string) (string, error) {
	v = strings.TrimSpace(strings.Trim(v, `"`))
	if v == "" {
		return "", errors.New("kein Ordnerpfad angegeben")
	}
	if strings.HasPrefix(v, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if v == "~" {
				v = home
			} else if strings.HasPrefix(v, "~/") || strings.HasPrefix(v, `~\`) {
				v = filepath.Join(home, v[2:])
			}
		}
	}
	abs, err := filepath.Abs(filepath.Clean(v))
	if err != nil {
		return "", err
	}
	return abs, nil
}

func looksLikeOneDrivePath(v string) bool {
	v = strings.ToLower(filepath.ToSlash(filepath.Clean(v)))
	return strings.Contains(v, "/onedrive") || strings.HasSuffix(v, "onedrive")
}

func decodeMountField(v string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(v)
}

func pathWithinMount(path, mount string) bool {
	path = filepath.Clean(path)
	mount = filepath.Clean(mount)
	rel, err := filepath.Rel(mount, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func linuxMountForPath(path string) (mountPoint, fsType, source string, ok bool) {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", "", "", false
	}
	bestLen := -1
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) || len(fields) < 5 {
			continue
		}
		mp := decodeMountField(fields[4])
		if !pathWithinMount(path, mp) || len(mp) <= bestLen {
			continue
		}
		bestLen = len(mp)
		mountPoint = mp
		fsType = fields[sep+1]
		source = decodeMountField(fields[sep+2])
		ok = true
	}
	return
}

func inspectSyncPath(base string) SyncHealth {
	h := SyncHealth{Status: "Nicht eingerichtet"}
	base = strings.TrimSpace(base)
	if base == "" {
		return h
	}
	if st, err := os.Stat(base); err == nil && st.IsDir() {
		h.PathExists = true
	}
	if runtime.GOOS == "linux" {
		mp, fsType, source, ok := linuxMountForPath(base)
		if ok {
			h.MountPoint = mp
			h.MountType = fsType
			h.MountSource = source
			h.MountDetected = mp != "/"
			low := strings.ToLower(fsType + " " + source)
			h.RcloneMounted = strings.Contains(low, "rclone")
		}
		if looksLikeOneDrivePath(base) {
			if !h.RcloneMounted {
				h.SyncReady = false
				h.Status = "OneDrive/rclone nicht eingebunden"
				return h
			}
			if !h.PathExists {
				h.Status = "OneDrive eingebunden · Liga-Ordner fehlt"
				return h
			}
			h.SyncReady = true
			h.Status = "OneDrive über rclone eingebunden"
			return h
		}
	}
	if h.PathExists {
		h.SyncReady = true
		h.Status = "Ordner verfügbar"
	} else {
		h.Status = "Ordner nicht verfügbar"
	}
	return h
}

func (s *Server) ensureConfigured() (string, error) {
	base := s.basePath()
	if base == "" {
		return "", errors.New("Liga-Datenordner ist noch nicht eingerichtet")
	}
	health := inspectSyncPath(base)
	if runtime.GOOS == "linux" && looksLikeOneDrivePath(base) && !health.RcloneMounted {
		return "", errors.New("OneDrive ist nicht mit rclone eingebunden. Aus Sicherheitsgründen werden keine lokalen Schattenordner angelegt")
	}
	p := filepath.Join(base, "Matches")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(base, "Deleted"), 0o755); err != nil {
		return "", err
	}
	return p, nil
}

func (s *Server) corsAndOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin == "" ||
			strings.HasPrefix(origin, "moz-extension://") ||
			strings.HasPrefix(origin, "chrome-extension://") ||
			origin == "http://127.0.0.1:17653" || origin == "http://localhost:17653"
		if !allowed {
			writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "Origin nicht erlaubt"})
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	base := s.basePath()
	configured := base != ""
	status := "Noch nicht eingerichtet"
	if configured {
		status = base
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="de"><head><meta charset="utf-8"><title>%s</title><style>body{font-family:system-ui;background:#0b1015;color:#eef5ff;max-width:760px;margin:60px auto;padding:0 24px}code{background:#151d25;padding:3px 6px;border-radius:5px} .ok{color:#56e39f}</style></head><body><h1>%s</h1><p class="ok">Der lokale Sync-Dienst läuft.</p><p>Version: <code>%s</code></p><p>Liga-Datenordner: <code>%s</code></p><p>Die Einrichtung des Ordners erfolgt direkt im Autodarts Match Analytics Add-on.</p><p><a style="color:#77b9ff" href="/overview">Liga-Übersicht ohne Add-on öffnen</a></p></body></html>`, appName, appName, appVersion, htmlEscape(status))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	base := s.basePath()
	health := inspectSyncPath(base)
	count := 0
	if base != "" && health.SyncReady {
		if entries, err := os.ReadDir(filepath.Join(base, "Matches")); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
					count++
				}
			}
		}
	}
	overviewDir := s.overviewDir()
	overviewXLSX, overviewHTML := "", ""
	if overviewDir != "" {
		overviewXLSX = filepath.Join(overviewDir, "Liga-Übersicht.xlsx")
		overviewHTML = filepath.Join(overviewDir, "Liga-Übersicht.html")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"version":       appVersion,
		"configured":    base != "",
		"basePath":      base,
		"matchesPath":   s.matchesPath(),
		"fileCount":     count,
		"platform":      runtime.GOOS + "/" + runtime.GOARCH,
		"pathExists":    health.PathExists,
		"mountDetected": health.MountDetected,
		"mountPoint":    health.MountPoint,
		"mountType":     health.MountType,
		"mountSource":   health.MountSource,
		"rcloneMounted": health.RcloneMounted,
		"syncReady":     health.SyncReady,
		"syncStatus":    health.Status,
		"overviewXlsx":  overviewXLSX,
		"overviewHtml":  overviewHTML,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req Config
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	p, err := expandPath(req.BasePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	health := inspectSyncPath(p)
	if runtime.GOOS == "linux" && looksLikeOneDrivePath(p) && !health.RcloneMounted {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "OneDrive ist nicht mit rclone eingebunden. Bitte zuerst den OneDrive-Mount starten."})
		return
	}
	if err := os.MkdirAll(filepath.Join(p, "Matches"), 0o755); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Ordner kann nicht angelegt/geöffnet werden: " + err.Error()})
		return
	}
	if err := os.MkdirAll(filepath.Join(p, "Deleted"), 0o755); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Löschordner kann nicht angelegt werden: " + err.Error()})
		return
	}
	probe := filepath.Join(p, ".autodarts-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Keine Schreibberechtigung im Ordner: " + err.Error()})
		return
	}
	_ = os.Remove(probe)
	if err := s.saveConfig(Config{BasePath: p}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "basePath": p, "matchesPath": filepath.Join(p, "Matches")})
}

func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var mf MatchFile
	if err := decodeJSON(r, &mf); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := s.writeMatchFile(mf); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	go s.generateOverviewForLeague(mf.League.Name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req BulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	written := 0
	failed := 0
	for _, raw := range req.Matches {
		mf := MatchFile{
			Format:    "autodarts-match-analytics-league-match",
			Version:   1,
			League:    req.League,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Match:     raw,
		}
		if err := s.writeMatchFile(mf); err != nil {
			failed++
		} else {
			written++
		}
	}
	if written > 0 {
		go s.generateOverviewForLeague(req.League.Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "written": written, "failed": failed})
}

func (s *Server) writeMatchFile(mf MatchFile) error {
	if mf.Format != "autodarts-match-analytics-league-match" {
		return errors.New("unbekanntes Match-Dateiformat")
	}
	if strings.TrimSpace(mf.League.Name) == "" {
		return errors.New("Liga-Name fehlt")
	}
	var entry MatchEntry
	if err := json.Unmarshal(mf.Match, &entry); err != nil {
		return errors.New("Matchdaten sind ungültig")
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
		return err
	}
	entry.SeasonID, entry.SeasonName = season.ID, season.Name
	mf.League.SeasonID, mf.League.SeasonName = season.ID, season.Name
	preserved, err := injectSeasonIntoMatch(mf.Match, season)
	if err != nil {
		return err
	}
	mf.Match = preserved
	if !looksLikeUUID(entry.MatchID) {
		return errors.New("ungültige Match-ID")
	}
	if entry.UsageMode != "league" || entry.Matchday < 1 || entry.Matchday > 10 {
		return errors.New("nur Liga-Matches mit Spieltag 1–10 dürfen synchronisiert werden")
	}
	if s.readDeletedIDs(mf.League.Name)[strings.ToLower(entry.MatchID)] {
		return errors.New("dieses Liga-Match wurde zentral gelöscht")
	}
	if mf.Version == 0 {
		mf.Version = 1
	}
	mf.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	dir, err := s.ensureConfigured()
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(dir, strings.ToLower(entry.MatchID)+".json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	// Windows cannot always rename over an existing target.
	_ = os.Remove(target)
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req DeleteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	req.MatchID = strings.ToLower(strings.TrimSpace(req.MatchID))
	if !looksLikeUUID(req.MatchID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "ungültige Match-ID"})
		return
	}
	if strings.TrimSpace(req.League.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Liga-Name fehlt"})
		return
	}
	dir, err := s.ensureConfigured()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	removed := 0
	entries, _ := os.ReadDir(dir)
	leagueNorm := strings.ToLower(strings.TrimSpace(req.League.Name))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var mf MatchFile
		if json.Unmarshal(b, &mf) != nil || mf.Format != "autodarts-match-analytics-league-match" || strings.ToLower(strings.TrimSpace(mf.League.Name)) != leagueNorm {
			continue
		}
		var me MatchEntry
		if json.Unmarshal(mf.Match, &me) != nil || strings.ToLower(strings.TrimSpace(me.MatchID)) != req.MatchID {
			continue
		}
		if os.Remove(path) == nil {
			removed++
		}
	}
	delDir := filepath.Join(s.basePath(), "Deleted")
	if err := os.MkdirAll(delDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	marker := DeleteMarker{Format: "autodarts-match-analytics-delete", Version: 1, League: req.League, MatchID: req.MatchID, DeletedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	out, _ := json.MarshalIndent(marker, "", "  ")
	target := filepath.Join(delDir, req.MatchID+".json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = os.Remove(target)
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	go s.generateOverviewForLeague(req.League.Name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "matchId": req.MatchID, "removedFiles": removed, "deletedAt": marker.DeletedAt})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req DeleteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	req.MatchID = strings.ToLower(strings.TrimSpace(req.MatchID))
	if !looksLikeUUID(req.MatchID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "ungültige Match-ID"})
		return
	}
	if strings.TrimSpace(req.League.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Liga-Name fehlt"})
		return
	}
	if _, err := s.ensureConfigured(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	path := filepath.Join(s.deletedPath(), req.MatchID+".json")
	removed := false
	if b, err := os.ReadFile(path); err == nil {
		var dm DeleteMarker
		if json.Unmarshal(b, &dm) == nil && dm.Format == "autodarts-match-analytics-delete" && strings.EqualFold(strings.TrimSpace(dm.League.Name), strings.TrimSpace(req.League.Name)) && strings.EqualFold(strings.TrimSpace(dm.MatchID), req.MatchID) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			removed = true
		}
	}
	go s.generateOverviewForLeague(req.League.Name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "matchId": req.MatchID, "deleteMarkerRemoved": removed})
}

func (s *Server) handleMatches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	league := strings.TrimSpace(r.URL.Query().Get("league"))
	if league == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Liga-Name fehlt"})
		return
	}
	requestedSeason := strings.TrimSpace(r.URL.Query().Get("season"))
	selectedSeason, registry, err := s.resolveSeason(league, requestedSeason, "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	dir, err := s.ensureConfigured()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	type candidate struct {
		Raw       json.RawMessage
		Timestamp time.Time
		Source    string
	}
	byID := map[string]candidate{}
	invalid := 0
	leagueNorm := strings.ToLower(league)
	deleted := s.readDeletedIDs(league)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			invalid++
			continue
		}
		var mf MatchFile
		if err := json.Unmarshal(b, &mf); err != nil || mf.Format != "autodarts-match-analytics-league-match" {
			invalid++
			continue
		}
		if strings.ToLower(strings.TrimSpace(mf.League.Name)) != leagueNorm {
			continue
		}
		var entry MatchEntry
		if err := json.Unmarshal(mf.Match, &entry); err != nil || !looksLikeUUID(entry.MatchID) || entry.UsageMode != "league" {
			invalid++
			continue
		}
		if strings.TrimSpace(entry.SeasonID) == "" {
			entry.SeasonID, entry.SeasonName = selectedSeason.ID, selectedSeason.Name
		}
		if entry.SeasonID != selectedSeason.ID {
			continue
		}
		ts := newestTimestamp(mf.UpdatedAt, entry.AssignmentUpdatedAt, entry.SavedAt, entry.MatchDate)
		key := strings.ToLower(entry.MatchID)
		if deleted[key] {
			continue
		}
		prev, ok := byID[key]
		if !ok || ts.After(prev.Timestamp) {
			byID[key] = candidate{Raw: mf.Match, Timestamp: ts, Source: e.Name()}
		}
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	matches := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		matches = append(matches, byID[id].Raw)
	}
	deletedIDs := make([]string, 0, len(deleted))
	for id := range deleted {
		deletedIDs = append(deletedIDs, id)
	}
	sort.Strings(deletedIDs)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"league":          league,
		"season":          selectedSeason,
		"currentSeasonId": registry.CurrentSeasonID,
		"seasons":         registry.Seasons,
		"matches":         matches,
		"deletedMatchIds": deletedIDs,
		"count":           len(matches),
		"invalid":         invalid,
		"path":            dir,
	})
}

func newestTimestamp(values ...string) time.Time {
	var best time.Time
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			t, err = time.Parse(time.RFC3339, v)
		}
		if err == nil && t.After(best) {
			best = t
		}
	}
	return best
}

func looksLikeUUID(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) != 36 {
		return false
	}
	for i, c := range v {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("ungültiges JSON: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "Methode nicht erlaubt"})
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}
