package library

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/janyksteenbeek/boom/pkg/model"
)

// Store provides persistent track metadata storage using SQLite.
type Store struct {
	db *sql.DB
}

// NewStore opens or creates a SQLite database at the given path.
func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Enable WAL mode for better concurrent access.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
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
		CREATE INDEX IF NOT EXISTS idx_tracks_title ON tracks(title);
		CREATE INDEX IF NOT EXISTS idx_tracks_artist ON tracks(artist);
		CREATE INDEX IF NOT EXISTS idx_tracks_bpm ON tracks(bpm);
	`)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// v2: analysis columns (safe to re-run; duplicate column errors are ignored)
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN analyzed_at TEXT NOT NULL DEFAULT ''`)
	s.db.Exec(`ALTER TABLE tracks ADD COLUMN beat_grid TEXT NOT NULL DEFAULT ''`)

	return nil
}

// UpsertTrack inserts or updates a track in the database.
func (s *Store) UpsertTrack(t *model.Track) error {
	_, err := s.db.Exec(`
		INSERT INTO tracks (id, path, title, artist, album, genre, bpm, key, duration, bitrate, format, size, source, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			title=excluded.title, artist=excluded.artist, album=excluded.album,
			genre=excluded.genre, bpm=excluded.bpm, key=excluded.key,
			duration=excluded.duration, bitrate=excluded.bitrate, format=excluded.format,
			size=excluded.size
	`, t.ID, t.Path, t.Title, t.Artist, t.Album, t.Genre, t.BPM, t.Key,
		t.Duration.Milliseconds(), t.Bitrate, t.Format, t.Size, t.Source,
		t.AddedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("upsert track: %w", err)
	}
	return nil
}

// Search returns tracks matching the query string (searches title and artist).
func (s *Store) Search(query string, limit int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 100
	}
	pattern := "%" + query + "%"
	rows, err := s.db.Query(`
		SELECT id, path, title, artist, album, genre, bpm, key, duration, bitrate, format, size, source, added_at
		FROM tracks
		WHERE title LIKE ? OR artist LIKE ? OR album LIKE ?
		ORDER BY title ASC
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
		SELECT id, path, title, artist, album, genre, bpm, key, duration, bitrate, format, size, source, added_at
		FROM tracks
		ORDER BY added_at DESC, title ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("all tracks: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// GetByPath returns a track by its file path.
func (s *Store) GetByPath(path string) (*model.Track, error) {
	row := s.db.QueryRow(`
		SELECT id, path, title, artist, album, genre, bpm, key, duration, bitrate, format, size, source, added_at
		FROM tracks WHERE path = ?
	`, path)
	t, err := scanTrack(row)
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
		SELECT id, path, title, artist, album, genre, bpm, key, duration, bitrate, format, size, source, added_at
		FROM tracks WHERE genre = ?
		ORDER BY artist ASC, title ASC
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
		SELECT id, path, title, artist, album, genre, bpm, key, duration, bitrate, format, size, source, added_at
		FROM tracks
		ORDER BY added_at DESC
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
		SELECT id, path, title, artist, album, genre, bpm, key, duration, bitrate, format, size, source, added_at
		FROM tracks WHERE bpm >= ? AND bpm < ?
		ORDER BY bpm ASC, title ASC
		LIMIT ?
	`, low, high, limit)
	if err != nil {
		return nil, fmt.Errorf("tracks by bpm range: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// UpdateAnalysis updates only the analysis fields for a track.
func (s *Store) UpdateAnalysis(trackID string, bpm float64, key string, beatGrid string, analyzedAt time.Time) error {
	_, err := s.db.Exec(`
		UPDATE tracks SET bpm = ?, key = ?, beat_grid = ?, analyzed_at = ?
		WHERE id = ?
	`, bpm, key, beatGrid, analyzedAt.Format(time.RFC3339), trackID)
	if err != nil {
		return fmt.Errorf("update analysis: %w", err)
	}
	return nil
}

// UnanalyzedTracks returns tracks that have not been analyzed yet.
func (s *Store) UnanalyzedTracks(limit int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT id, path, title, artist, album, genre, bpm, key, duration, bitrate, format, size, source, added_at
		FROM tracks WHERE analyzed_at = ''
		ORDER BY title ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("unanalyzed tracks: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

func scanTracks(rows *sql.Rows) ([]model.Track, error) {
	var tracks []model.Track
	for rows.Next() {
		var t model.Track
		var durMs int64
		var addedStr string
		err := rows.Scan(&t.ID, &t.Path, &t.Title, &t.Artist, &t.Album, &t.Genre,
			&t.BPM, &t.Key, &durMs, &t.Bitrate, &t.Format, &t.Size, &t.Source, &addedStr)
		if err != nil {
			return nil, fmt.Errorf("scan track: %w", err)
		}
		t.Duration = time.Duration(durMs) * time.Millisecond
		t.AddedAt, _ = time.Parse(time.RFC3339, addedStr)
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func scanTrack(row *sql.Row) (*model.Track, error) {
	var t model.Track
	var durMs int64
	var addedStr string
	err := row.Scan(&t.ID, &t.Path, &t.Title, &t.Artist, &t.Album, &t.Genre,
		&t.BPM, &t.Key, &durMs, &t.Bitrate, &t.Format, &t.Size, &t.Source, &addedStr)
	if err != nil {
		return nil, err
	}
	t.Duration = time.Duration(durMs) * time.Millisecond
	t.AddedAt, _ = time.Parse(time.RFC3339, addedStr)
	return &t, nil
}
