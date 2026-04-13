package library

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"github.com/janyksteenbeek/boom/internal/audio"
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

	if dur, ok := readDuration(path); ok {
		t.Duration = dur
	}

	return t, nil
}

// readDuration opens the file with the audio decoder just long enough to
// pull sample-rate + length from the header. beep's decoders return length
// up-front for all formats we support (mp3/wav/flac), so this is cheap —
// we immediately close the streamer.
func readDuration(path string) (time.Duration, bool) {
	streamer, format, err := audio.Decode(path)
	if err != nil {
		return 0, false
	}
	defer streamer.Close()
	n := streamer.Len()
	if n <= 0 {
		return 0, false
	}
	return format.SampleRate.D(n), true
}

func generateID(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:8])
}
