package library

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"github.com/janyksteenbeek/boom/pkg/model"
)

// ReadMetadata extracts audio metadata tags from a file.
func ReadMetadata(path string) (*model.Track, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}

	t := &model.Track{
		ID:        generateID(path),
		Path:      path,
		Format:    strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		Size:      info.Size(),
		FileMtime: info.ModTime().Unix(),
		Source:    "local",
		AddedAt:   time.Now(),
		CuePoint:  -1, // unset by default
	}

	meta, err := tag.ReadFrom(f)
	if err != nil {
		// If we can't read tags, use filename as title.
		t.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		return t, nil
	}

	t.Title = meta.Title()
	if t.Title == "" {
		t.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	t.Artist = meta.Artist()
	t.Album = meta.Album()
	t.Genre = meta.Genre()

	return t, nil
}

func generateID(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:8])
}
