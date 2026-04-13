package library

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// supportedExtensions lists the audio formats we can scan for.
var supportedExtensions = map[string]bool{
	".mp3":  true,
	".wav":  true,
	".flac": true,
	".aac":  true,
	".m4a":  true,
	".ogg":  true,
}

// Scanner walks directories to find audio files.
type Scanner struct {
	store *Store
}

// NewScanner creates a new filesystem scanner.
func NewScanner(store *Store) *Scanner {
	return &Scanner{store: store}
}

// ScanDir recursively scans a directory for audio files and adds them to the store.
// Returns the number of tracks found.
func (s *Scanner) ScanDir(dir string) (int, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}

	count := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExtensions[ext] {
			return nil
		}

		// Fast path: compare file mtime against what we already have.
		// Tag reads hit disk (~ms each); skipping unchanged files makes
		// rescans of large libraries near-instant. Rows whose duration was
		// never persisted (pre-duration scanner) are re-read so the TIME
		// column backfills without needing a manual re-scan.
		info, statErr := d.Info()
		if statErr == nil {
			existing, durMs, mErr := s.store.MtimeByPath(path)
			if mErr == nil && existing != 0 && existing == info.ModTime().Unix() && durMs > 0 {
				count++
				return nil
			}
		}

		track, err := ReadMetadata(path)
		if err != nil {
			log.Printf("skip %s: %v", path, err)
			return nil
		}

		if err := s.store.UpsertTrack(track); err != nil {
			log.Printf("store %s: %v", path, err)
			return nil
		}

		count++
		return nil
	})

	return count, err
}
