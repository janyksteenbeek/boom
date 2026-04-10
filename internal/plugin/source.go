package plugin

import "github.com/janyksteenbeek/boom/pkg/model"

// LibrarySource provides tracks from an external source (local filesystem,
// Beatport, Tidal, etc.).
type LibrarySource interface {
	Name() string
	Search(query string, limit int) ([]model.Track, error)
	Browse(path string) ([]model.Track, error)
	// StreamURL returns a playable URL or local path for a track.
	StreamURL(trackID string) (string, error)
	Available() bool
}
