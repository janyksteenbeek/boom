package library

import (
	"fmt"
	"log"
	"strings"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// Library manages the music library and integrates with the event bus.
type Library struct {
	bus     *event.Bus
	store   *Store
	scanner *Scanner
}

// NewLibrary creates a new library manager.
func NewLibrary(bus *event.Bus, store *Store) *Library {
	l := &Library{
		bus:     bus,
		store:   store,
		scanner: NewScanner(store),
	}
	l.subscribeEvents()
	return l
}

// ScanDirs scans the given directories for audio files.
func (l *Library) ScanDirs(dirs []string) {
	for _, dir := range dirs {
		count, err := l.scanner.ScanDir(dir)
		if err != nil {
			log.Printf("scan %s: %v", dir, err)
			continue
		}
		log.Printf("scanned %s: %d tracks found", dir, count)
	}
}

// Search returns tracks matching the query.
func (l *Library) Search(query string, limit int) ([]model.Track, error) {
	return l.store.Search(query, limit)
}

// AllTracks returns all tracks with pagination.
func (l *Library) AllTracks(offset, limit int) ([]model.Track, error) {
	return l.store.AllTracks(offset, limit)
}

// Genres returns all distinct genres in the library.
func (l *Library) Genres() ([]string, error) {
	return l.store.DistinctGenres()
}

// Close closes the underlying store.
func (l *Library) Close() error {
	return l.store.Close()
}

func (l *Library) subscribeEvents() {
	l.bus.Subscribe(event.TopicLibrary, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionSearchQuery:
			query, ok := ev.Payload.(string)
			if !ok {
				return nil
			}
			var tracks []model.Track
			var err error
			if query == "" {
				tracks, err = l.store.AllTracks(0, 500)
			} else {
				tracks, err = l.store.Search(query, 500)
			}
			if err != nil {
				log.Printf("search error: %v", err)
				return nil
			}
			l.bus.PublishAsync(event.Event{
				Topic:   event.TopicLibrary,
				Action:  event.ActionSearchResults,
				Payload: tracks,
			})
		case event.ActionFilterCategory:
			categoryID, ok := ev.Payload.(string)
			if !ok {
				return nil
			}
			var tracks []model.Track
			var err error
			switch {
			case categoryID == "all":
				tracks, err = l.store.AllTracks(0, 500)
			case categoryID == "recent":
				tracks, err = l.store.RecentTracks(100)
			case categoryID == "unanalyzed":
				tracks, err = l.store.UnanalyzedTracks(500)
			case strings.HasPrefix(categoryID, "genre:"):
				genre := strings.TrimPrefix(categoryID, "genre:")
				tracks, err = l.store.TracksByGenre(genre, 500)
			case strings.HasPrefix(categoryID, "bpm:"):
				parts := strings.SplitN(strings.TrimPrefix(categoryID, "bpm:"), "-", 2)
				if len(parts) == 2 {
					var low, high float64
					fmt.Sscanf(parts[0], "%f", &low)
					fmt.Sscanf(parts[1], "%f", &high)
					tracks, err = l.store.TracksByBPMRange(low, high, 500)
				}
			}
			if err != nil {
				log.Printf("filter error: %v", err)
				return nil
			}
			if tracks != nil {
				l.bus.PublishAsync(event.Event{
					Topic:   event.TopicLibrary,
					Action:  event.ActionSearchResults,
					Payload: tracks,
				})
			}
		}
		return nil
	})
}
