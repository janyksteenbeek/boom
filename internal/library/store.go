package library

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/janyksteenbeek/boom/pkg/model"
)

// AnalysisVersion bumps whenever the BPM/key/beat-grid algorithms change in
// a way that makes existing stored results invalid. Bumping this causes
// UnanalyzedTracks to surface tracks with an older version for re-analysis.
const AnalysisVersion = 1

// WaveformVersion bumps when the waveform generator changes in a way that
// invalidates cached blobs.
const WaveformVersion = 1

// Column lists kept as constants so the hot-path SELECTs don't mis-align with
// the scan functions when columns are added.
const (
	trackCols = `t.id, t.path, t.title, t.artist, t.album, t.genre,
		t.duration, t.bitrate, t.format, t.size, t.source, t.added_at, t.cue_point,
		t.file_mtime, t.play_count, t.first_played, t.last_played,
		COALESCE(a.bpm, 0), COALESCE(a.key, ''), COALESCE(a.analyzed_at, ''), COALESCE(a.gain, 0)`

	trackColsWithGrid = trackCols + `, a.beat_grid`
)

// Store provides persistent track metadata storage using SQLite.
//
// Schema v4 splits tracks and track_analysis so list queries (browser,
// search, genre/bpm filters) don't drag the beat-grid blob across every row.
// The beat grid is only loaded on the deck-load path via GetByPath.
type Store struct {
	db *sql.DB

	// Prepared statements for hot paths.
	stmtUpsertTrack    *sql.Stmt
	stmtUpsertAnalysis *sql.Stmt
	stmtUpdateCue      *sql.Stmt
	stmtGetByPath      *sql.Stmt
	stmtMtimeByPath    *sql.Stmt
	stmtMarkPlayed     *sql.Stmt
	stmtGetWaveform    *sql.Stmt
	stmtPutWaveform    *sql.Stmt
}

// NewStore opens or creates a SQLite database at the given path. mmapBytes
// caps how much of the DB SQLite may memory-map; pass 0 to fall back to the
// built-in default (64 MB). Larger values speed up browser queries on big
// libraries at the cost of higher reported RSS.
func NewStore(dbPath string, mmapBytes int64) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if mmapBytes <= 0 {
		mmapBytes = 64 * 1024 * 1024
	}

	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA temp_store=MEMORY`,
		fmt.Sprintf(`PRAGMA mmap_size=%d`, mmapBytes),
		`PRAGMA cache_size=-20000`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.prepare(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database connection and any prepared statements.
func (s *Store) Close() error {
	for _, st := range []*sql.Stmt{
		s.stmtUpsertTrack, s.stmtUpsertAnalysis, s.stmtUpdateCue, s.stmtGetByPath,
		s.stmtMtimeByPath, s.stmtMarkPlayed, s.stmtGetWaveform, s.stmtPutWaveform,
	} {
		if st != nil {
			st.Close()
		}
	}
	return s.db.Close()
}

// DBPath returns the on-disk path of the SQLite database file (for display
// in settings/diagnostics).
func (s *Store) DBPath() string {
	var path string
	// main database file path via PRAGMA database_list
	rows, err := s.db.Query(`PRAGMA database_list`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return ""
		}
		if name == "main" {
			path = file
			break
		}
	}
	return path
}

func (s *Store) migrate() error {
	// v1: initial tracks table.
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS tracks (
			id       TEXT PRIMARY KEY,
			path     TEXT UNIQUE NOT NULL,
			title    TEXT NOT NULL DEFAULT '',
			artist   TEXT NOT NULL DEFAULT '',
			album    TEXT NOT NULL DEFAULT '',
			genre    TEXT NOT NULL DEFAULT '',
			bpm      REAL NOT NULL DEFAULT 0,
			key      TEXT NOT NULL DEFAULT '',
			duration INTEGER NOT NULL DEFAULT 0,
			bitrate  INTEGER NOT NULL DEFAULT 0,
			format   TEXT NOT NULL DEFAULT '',
			size     INTEGER NOT NULL DEFAULT 0,
			source   TEXT NOT NULL DEFAULT 'local',
			added_at TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_tracks_title  ON tracks(title);
		CREATE INDEX IF NOT EXISTS idx_tracks_artist ON tracks(artist);
		CREATE INDEX IF NOT EXISTS idx_tracks_bpm    ON tracks(bpm);
	`); err != nil {
		return fmt.Errorf("migrate v1: %w", err)
	}

	// v2 + v3: analysis + cue columns on tracks (idempotent — ignore
	// duplicate-column errors on re-run).
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN analyzed_at TEXT NOT NULL DEFAULT ''`)
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN beat_grid TEXT NOT NULL DEFAULT ''`)
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN cue_point REAL NOT NULL DEFAULT -1`)

	// v5: play stats + mtime on tracks (idempotent).
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN file_mtime   INTEGER NOT NULL DEFAULT 0`)
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN play_count   INTEGER NOT NULL DEFAULT 0`)
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN first_played TEXT    NOT NULL DEFAULT ''`)
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN last_played  TEXT    NOT NULL DEFAULT ''`)

	// v4: dedicated track_analysis table with binary beat grid + version.
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS track_analysis (
			track_id    TEXT PRIMARY KEY,
			bpm         REAL NOT NULL DEFAULT 0,
			key         TEXT NOT NULL DEFAULT '',
			beat_grid   BLOB,
			analyzed_at TEXT NOT NULL DEFAULT '',
			version     INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(track_id) REFERENCES tracks(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_analysis_bpm ON track_analysis(bpm);
	`); err != nil {
		return fmt.Errorf("migrate v4: %w", err)
	}

	// v5: gain column on analysis + waveform cache table.
	s.db.Exec(`ALTER TABLE track_analysis ADD COLUMN gain REAL NOT NULL DEFAULT 0`)

	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS track_waveform (
			track_id    TEXT PRIMARY KEY,
			sample_rate INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			resolution  INTEGER NOT NULL,
			num_samples INTEGER NOT NULL,
			peaks       BLOB NOT NULL,
			peaks_low   BLOB NOT NULL,
			peaks_mid   BLOB NOT NULL,
			peaks_high  BLOB NOT NULL,
			mtime       INTEGER NOT NULL DEFAULT 0,
			version     INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(track_id) REFERENCES tracks(id) ON DELETE CASCADE
		);
	`); err != nil {
		return fmt.Errorf("migrate v5 waveform: %w", err)
	}

	// One-shot backfill: copy any existing analysis rows from the legacy
	// columns into track_analysis. Only runs when track_analysis is empty,
	// so upgrading in place is transparent and re-running is a no-op.
	var analysisCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM track_analysis`).Scan(&analysisCount); err != nil {
		return fmt.Errorf("count analysis: %w", err)
	}
	if analysisCount == 0 {
		if err := s.backfillAnalysis(); err != nil {
			return fmt.Errorf("backfill analysis: %w", err)
		}
	}

	return nil
}

// backfillAnalysis copies legacy analysis data off `tracks` into
// `track_analysis`, converting the JSON beat_grid text into a binary blob.
func (s *Store) backfillAnalysis() error {
	rows, err := s.db.Query(`
		SELECT id, bpm, key, beat_grid, analyzed_at
		FROM tracks
		WHERE (bpm > 0 AND key != '') OR analyzed_at != '' OR beat_grid != ''
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO track_analysis (track_id, bpm, key, beat_grid, analyzed_at, version)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		var id, key, beatGridJSON, analyzedAt string
		var bpm float64
		if err := rows.Scan(&id, &bpm, &key, &beatGridJSON, &analyzedAt); err != nil {
			return err
		}
		blob := legacyBeatGridJSONToBlob(beatGridJSON)
		if _, err := stmt.Exec(id, bpm, key, blob, analyzedAt, AnalysisVersion); err != nil {
			return err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if count > 0 {
		fmt.Printf("library: backfilled %d analysis rows into track_analysis\n", count)
	}
	return nil
}

func (s *Store) prepare() error {
	var err error
	s.stmtUpsertTrack, err = s.db.Prepare(`
		INSERT INTO tracks (id, path, title, artist, album, genre, duration, bitrate, format, size, source, added_at, cue_point, file_mtime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			title=excluded.title, artist=excluded.artist, album=excluded.album,
			genre=excluded.genre, duration=excluded.duration, bitrate=excluded.bitrate,
			format=excluded.format, size=excluded.size, file_mtime=excluded.file_mtime
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert track: %w", err)
	}

	s.stmtUpsertAnalysis, err = s.db.Prepare(`
		INSERT INTO track_analysis (track_id, bpm, key, beat_grid, analyzed_at, version, gain)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(track_id) DO UPDATE SET
			bpm=excluded.bpm, key=excluded.key, beat_grid=excluded.beat_grid,
			analyzed_at=excluded.analyzed_at, version=excluded.version, gain=excluded.gain
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert analysis: %w", err)
	}

	s.stmtUpdateCue, err = s.db.Prepare(`UPDATE tracks SET cue_point = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare update cue: %w", err)
	}

	s.stmtGetByPath, err = s.db.Prepare(`
		SELECT ` + trackColsWithGrid + `
		FROM tracks t
		LEFT JOIN track_analysis a ON a.track_id = t.id
		WHERE t.path = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare get by path: %w", err)
	}

	s.stmtMtimeByPath, err = s.db.Prepare(`SELECT file_mtime, duration FROM tracks WHERE path = ?`)
	if err != nil {
		return fmt.Errorf("prepare mtime by path: %w", err)
	}

	s.stmtMarkPlayed, err = s.db.Prepare(`
		UPDATE tracks
		SET play_count   = play_count + 1,
		    first_played = CASE WHEN first_played = '' THEN ? ELSE first_played END,
		    last_played  = ?
		WHERE id = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare mark played: %w", err)
	}

	s.stmtGetWaveform, err = s.db.Prepare(`
		SELECT sample_rate, duration_ms, resolution, num_samples, peaks, peaks_low, peaks_mid, peaks_high
		FROM track_waveform
		WHERE track_id = ? AND sample_rate = ? AND mtime = ? AND version = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare get waveform: %w", err)
	}

	s.stmtPutWaveform, err = s.db.Prepare(`
		INSERT INTO track_waveform
			(track_id, sample_rate, duration_ms, resolution, num_samples, peaks, peaks_low, peaks_mid, peaks_high, mtime, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(track_id) DO UPDATE SET
			sample_rate=excluded.sample_rate, duration_ms=excluded.duration_ms,
			resolution=excluded.resolution,   num_samples=excluded.num_samples,
			peaks=excluded.peaks,             peaks_low=excluded.peaks_low,
			peaks_mid=excluded.peaks_mid,     peaks_high=excluded.peaks_high,
			mtime=excluded.mtime,             version=excluded.version
	`)
	if err != nil {
		return fmt.Errorf("prepare put waveform: %w", err)
	}
	return nil
}

// UpsertTrack inserts or updates a track in the database.
//
// Note: cue_point is intentionally NOT touched on conflict so we don't blow
// away a saved cue point during a re-scan. Analysis data lives in its own
// table and is untouched by this path.
func (s *Store) UpsertTrack(t *model.Track) error {
	_, err := s.stmtUpsertTrack.Exec(
		t.ID, t.Path, t.Title, t.Artist, t.Album, t.Genre,
		t.Duration.Milliseconds(), t.Bitrate, t.Format, t.Size, t.Source,
		t.AddedAt.Format(time.RFC3339), t.CuePoint, t.FileMtime,
	)
	if err != nil {
		return fmt.Errorf("upsert track: %w", err)
	}
	return nil
}

// MtimeByPath returns the stored file mtime and duration-in-ms for a path,
// or zeros if the track is not in the database yet. Used by the scanner to
// skip unchanged files; the duration is also returned so the scanner can
// re-read metadata for legacy rows whose duration was never populated.
func (s *Store) MtimeByPath(path string) (mtime int64, durationMs int64, err error) {
	err = s.stmtMtimeByPath.QueryRow(path).Scan(&mtime, &durationMs)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return mtime, durationMs, err
}

// MarkPlayed bumps the play counter for a track and sets last_played
// (and first_played if it was still empty).
func (s *Store) MarkPlayed(trackID string, when time.Time) error {
	ts := when.Format(time.RFC3339)
	if _, err := s.stmtMarkPlayed.Exec(ts, ts, trackID); err != nil {
		return fmt.Errorf("mark played: %w", err)
	}
	return nil
}

// WaveformBlob is the raw, binary-encoded waveform payload as stored in
// track_waveform. Callers decode the peak blobs with DecodeFloat64s.
type WaveformBlob struct {
	SampleRate int
	DurationMs int
	Resolution int
	NumSamples int
	Peaks      []byte
	PeaksLow   []byte
	PeaksMid   []byte
	PeaksHigh  []byte
}

// GetWaveform returns a cached waveform if one exists for the given track
// at the requested sample_rate/mtime/version. The returned boolean reports
// whether a valid entry was found.
func (s *Store) GetWaveform(trackID string, sampleRate int, mtime int64) (*WaveformBlob, bool, error) {
	var b WaveformBlob
	row := s.stmtGetWaveform.QueryRow(trackID, sampleRate, mtime, WaveformVersion)
	err := row.Scan(
		&b.SampleRate, &b.DurationMs, &b.Resolution, &b.NumSamples,
		&b.Peaks, &b.PeaksLow, &b.PeaksMid, &b.PeaksHigh,
	)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &b, true, nil
}

// PutWaveform stores a computed waveform for a track. Peak blobs should be
// encoded with EncodeFloat64s before passing in.
func (s *Store) PutWaveform(trackID string, b *WaveformBlob, mtime int64) error {
	if b == nil {
		return nil
	}
	_, err := s.stmtPutWaveform.Exec(
		trackID, b.SampleRate, b.DurationMs, b.Resolution, b.NumSamples,
		b.Peaks, b.PeaksLow, b.PeaksMid, b.PeaksHigh, mtime, WaveformVersion,
	)
	if err != nil {
		return fmt.Errorf("put waveform: %w", err)
	}
	return nil
}

// EncodeFloat64s packs a float64 slice as a little-endian binary blob.
func EncodeFloat64s(xs []float64) []byte {
	if len(xs) == 0 {
		return nil
	}
	buf := make([]byte, 8*len(xs))
	for i, v := range xs {
		binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(v))
	}
	return buf
}

// DecodeFloat64s unpacks a little-endian binary blob into a float64 slice.
func DecodeFloat64s(blob []byte) []float64 {
	if len(blob) == 0 || len(blob)%8 != 0 {
		return nil
	}
	n := len(blob) / 8
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(blob[i*8:]))
	}
	return out
}

// UpdateCuePoint persists the cue point for a track. Pass a negative value
// (e.g. -1) to clear the cue point.
func (s *Store) UpdateCuePoint(trackID string, pos float64) error {
	if _, err := s.stmtUpdateCue.Exec(pos, trackID); err != nil {
		return fmt.Errorf("update cue point: %w", err)
	}
	return nil
}

// UpdateAnalysis persists the analysis results for a track into
// track_analysis as a single upsert.
func (s *Store) UpdateAnalysis(trackID string, bpm float64, key string, beatGrid []float64, gain float64, analyzedAt time.Time) error {
	blob := encodeBeatGrid(beatGrid)
	_, err := s.stmtUpsertAnalysis.Exec(
		trackID, bpm, key, blob, analyzedAt.Format(time.RFC3339), AnalysisVersion, gain,
	)
	if err != nil {
		return fmt.Errorf("update analysis: %w", err)
	}
	return nil
}

// Search returns tracks matching the query string (searches title and artist).
// Beat grid is NOT loaded — list views never need it.
func (s *Store) Search(query string, limit int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 100
	}
	pattern := "%" + query + "%"
	rows, err := s.db.Query(`
		SELECT `+trackCols+`
		FROM tracks t
		LEFT JOIN track_analysis a ON a.track_id = t.id
		WHERE t.title LIKE ? OR t.artist LIKE ? OR t.album LIKE ?
		ORDER BY t.title ASC
		LIMIT ?
	`, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// AllTracks returns all tracks with pagination.
func (s *Store) AllTracks(offset, limit int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT `+trackCols+`
		FROM tracks t
		LEFT JOIN track_analysis a ON a.track_id = t.id
		ORDER BY t.added_at DESC, t.title ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("all tracks: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// GetByPath returns a track by its file path, fully hydrated with beat grid.
// This is the deck-load path — the only place that needs the beat grid blob.
func (s *Store) GetByPath(path string) (*model.Track, error) {
	row := s.stmtGetByPath.QueryRow(path)
	t, err := scanTrackWithGrid(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// Count returns the total number of tracks.
func (s *Store) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM tracks").Scan(&count)
	return count, err
}

// DistinctGenres returns all unique non-empty genres sorted alphabetically.
func (s *Store) DistinctGenres() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT genre FROM tracks WHERE genre != '' ORDER BY genre ASC`)
	if err != nil {
		return nil, fmt.Errorf("distinct genres: %w", err)
	}
	defer rows.Close()
	var genres []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, fmt.Errorf("scan genre: %w", err)
		}
		genres = append(genres, g)
	}
	return genres, rows.Err()
}

// TracksByGenre returns tracks matching the given genre.
func (s *Store) TracksByGenre(genre string, limit int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT `+trackCols+`
		FROM tracks t
		LEFT JOIN track_analysis a ON a.track_id = t.id
		WHERE t.genre = ?
		ORDER BY t.artist ASC, t.title ASC
		LIMIT ?
	`, genre, limit)
	if err != nil {
		return nil, fmt.Errorf("tracks by genre: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// RecentTracks returns the most recently added tracks.
func (s *Store) RecentTracks(limit int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT `+trackCols+`
		FROM tracks t
		LEFT JOIN track_analysis a ON a.track_id = t.id
		ORDER BY t.added_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("recent tracks: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// TracksByBPMRange returns tracks within the given BPM range.
func (s *Store) TracksByBPMRange(low, high float64, limit int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT `+trackCols+`
		FROM tracks t
		JOIN track_analysis a ON a.track_id = t.id
		WHERE a.bpm >= ? AND a.bpm < ?
		ORDER BY a.bpm ASC, t.title ASC
		LIMIT ?
	`, low, high, limit)
	if err != nil {
		return nil, fmt.Errorf("tracks by bpm range: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// UnanalyzedTracks returns tracks that have not been analyzed yet, or whose
// analysis was produced by an older algorithm version.
func (s *Store) UnanalyzedTracks(limit int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT `+trackCols+`
		FROM tracks t
		LEFT JOIN track_analysis a ON a.track_id = t.id
		WHERE a.track_id IS NULL OR a.version < ? OR a.analyzed_at = ''
		ORDER BY t.title ASC
		LIMIT ?
	`, AnalysisVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("unanalyzed tracks: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// --- scanning helpers ---

func scanTracks(rows *sql.Rows) ([]model.Track, error) {
	var tracks []model.Track
	for rows.Next() {
		t, err := scanTrackRow(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, *t)
	}
	return tracks, rows.Err()
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTrackRow(r rowScanner) (*model.Track, error) {
	var t model.Track
	var durMs int64
	var addedStr, analyzedStr, firstPlayedStr, lastPlayedStr string
	if err := r.Scan(
		&t.ID, &t.Path, &t.Title, &t.Artist, &t.Album, &t.Genre,
		&durMs, &t.Bitrate, &t.Format, &t.Size, &t.Source, &addedStr, &t.CuePoint,
		&t.FileMtime, &t.PlayCount, &firstPlayedStr, &lastPlayedStr,
		&t.BPM, &t.Key, &analyzedStr, &t.Gain,
	); err != nil {
		return nil, fmt.Errorf("scan track: %w", err)
	}
	populateTimes(&t, durMs, addedStr, analyzedStr, firstPlayedStr, lastPlayedStr)
	return &t, nil
}

func scanTrackWithGrid(r rowScanner) (*model.Track, error) {
	var t model.Track
	var durMs int64
	var addedStr, analyzedStr, firstPlayedStr, lastPlayedStr string
	var blob []byte
	if err := r.Scan(
		&t.ID, &t.Path, &t.Title, &t.Artist, &t.Album, &t.Genre,
		&durMs, &t.Bitrate, &t.Format, &t.Size, &t.Source, &addedStr, &t.CuePoint,
		&t.FileMtime, &t.PlayCount, &firstPlayedStr, &lastPlayedStr,
		&t.BPM, &t.Key, &analyzedStr, &t.Gain,
		&blob,
	); err != nil {
		return nil, err
	}
	populateTimes(&t, durMs, addedStr, analyzedStr, firstPlayedStr, lastPlayedStr)
	t.BeatGrid = decodeBeatGrid(blob)
	return &t, nil
}

func populateTimes(t *model.Track, durMs int64, addedStr, analyzedStr, firstPlayedStr, lastPlayedStr string) {
	t.Duration = time.Duration(durMs) * time.Millisecond
	t.AddedAt, _ = time.Parse(time.RFC3339, addedStr)
	if analyzedStr != "" {
		t.AnalyzedAt, _ = time.Parse(time.RFC3339, analyzedStr)
	}
	if firstPlayedStr != "" {
		t.FirstPlayed, _ = time.Parse(time.RFC3339, firstPlayedStr)
	}
	if lastPlayedStr != "" {
		t.LastPlayed, _ = time.Parse(time.RFC3339, lastPlayedStr)
	}
}

// --- beat grid binary codec ---
//
// Format: little-endian float64 stream, 8 bytes per beat, no header. Length
// is implicit from the blob size. This is ~3x smaller than the prior JSON
// representation and avoids the parse allocation on every deck load.

func encodeBeatGrid(beats []float64) []byte {
	if len(beats) == 0 {
		return nil
	}
	buf := make([]byte, 8*len(beats))
	for i, v := range beats {
		binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(v))
	}
	return buf
}

func decodeBeatGrid(blob []byte) []float64 {
	if len(blob) < 8 || len(blob)%8 != 0 {
		return nil
	}
	n := len(blob) / 8
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(blob[i*8:]))
	}
	return out
}

// legacyBeatGridJSONToBlob converts the old JSON text representation used
// before schema v4 into the new binary blob. Anything unparseable becomes
// nil — the track will re-analyze on next load.
func legacyBeatGridJSONToBlob(jsonText string) []byte {
	if jsonText == "" {
		return nil
	}
	// Hand-rolled minimal parser: the legacy format was always `[f,f,f,...]`
	// produced by json.Marshal on []float64. Avoid pulling in encoding/json
	// for a one-shot migration.
	beats := parseFloatArray(jsonText)
	return encodeBeatGrid(beats)
}

func parseFloatArray(s string) []float64 {
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		return nil
	}
	var out []float64
	start := 0
	for i := 0; i <= len(inner); i++ {
		if i == len(inner) || inner[i] == ',' {
			tok := inner[start:i]
			v, err := parseFloat(tok)
			if err != nil {
				return nil
			}
			out = append(out, v)
			start = i + 1
		}
	}
	return out
}

func parseFloat(s string) (float64, error) {
	var v float64
	_, err := fmt.Sscanf(s, "%g", &v)
	return v, err
}
