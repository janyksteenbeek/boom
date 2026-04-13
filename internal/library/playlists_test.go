package library

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

func newTestService(t *testing.T) (*PlaylistService, *Store, *event.Bus) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(dbPath, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	bus := event.New()
	t.Cleanup(bus.Stop)
	return NewPlaylistService(store, bus), store, bus
}

func makeTrack(id, title, artist, genre string, bpm float64) model.Track {
	return model.Track{
		ID:       id,
		Path:     "/music/" + id + ".mp3",
		Title:    title,
		Artist:   artist,
		Genre:    genre,
		BPM:      bpm,
		Duration: 3 * time.Minute,
		Format:   "mp3",
		Source:   "local",
		AddedAt:  time.Now().UTC(),
		CuePoint: -1,
	}
}

func seedTrack(t *testing.T, s *Store, tr model.Track) {
	t.Helper()
	if err := s.UpsertTrack(&tr); err != nil {
		t.Fatalf("UpsertTrack: %v", err)
	}
	if tr.BPM > 0 {
		if err := s.UpdateAnalysis(tr.ID, tr.BPM, tr.Key, nil, 0, time.Now().UTC()); err != nil {
			t.Fatalf("UpdateAnalysis: %v", err)
		}
	}
}

func TestCreateAndTreeOrdering(t *testing.T) {
	p, _, _ := newTestService(t)

	folder, err := p.CreateFolder("", "Sets")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	pl, err := p.CreatePlaylist(folder.ID, "Saturday")
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}

	tree, err := p.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(tree))
	}

	var gotFolder, gotPlaylist bool
	for _, n := range tree {
		if n.ID == folder.ID && n.Kind == model.KindFolder {
			gotFolder = true
		}
		if n.ID == pl.ID && n.Kind == model.KindManual && n.ParentID == folder.ID {
			gotPlaylist = true
		}
	}
	if !gotFolder || !gotPlaylist {
		t.Fatalf("tree contents wrong: %+v", tree)
	}
}

func TestAddRemoveAndManualOrder(t *testing.T) {
	p, s, _ := newTestService(t)
	pl, err := p.CreatePlaylist("", "Warmup")
	if err != nil {
		t.Fatal(err)
	}

	for i, title := range []string{"A", "B", "C"} {
		seedTrack(t, s, makeTrack(title, title, "x", "House", 120+float64(i)))
	}
	if err := p.AddTracks(pl.ID, []string{"A", "B", "C"}); err != nil {
		t.Fatalf("AddTracks: %v", err)
	}

	got, err := p.Tracks(pl.ID)
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if len(got) != 3 || got[0].ID != "A" || got[2].ID != "C" {
		t.Fatalf("wrong initial order: %v", ids(got))
	}

	// Adding a duplicate must be a no-op.
	if err := p.AddTracks(pl.ID, []string{"B"}); err != nil {
		t.Fatal(err)
	}
	got, _ = p.Tracks(pl.ID)
	if len(got) != 3 {
		t.Fatalf("duplicate should be ignored: %v", ids(got))
	}

	if err := p.RemoveTracks(pl.ID, []string{"B"}); err != nil {
		t.Fatal(err)
	}
	got, _ = p.Tracks(pl.ID)
	if len(got) != 2 || got[0].ID != "A" || got[1].ID != "C" {
		t.Fatalf("remove wrong: %v", ids(got))
	}
}

func TestReorderSingle(t *testing.T) {
	p, s, _ := newTestService(t)
	pl, _ := p.CreatePlaylist("", "X")
	for _, tid := range []string{"A", "B", "C", "D"} {
		seedTrack(t, s, makeTrack(tid, tid, "x", "House", 120))
	}
	_ = p.AddTracks(pl.ID, []string{"A", "B", "C", "D"})

	// Move "A" to index 2 → expect B, C, A, D.
	if err := p.Reorder(pl.ID, "A", 2); err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	got, _ := p.Tracks(pl.ID)
	want := []string{"B", "C", "A", "D"}
	if !sameIDs(got, want) {
		t.Fatalf("got %v want %v", ids(got), want)
	}
}

func TestReorderMany(t *testing.T) {
	p, s, _ := newTestService(t)
	pl, _ := p.CreatePlaylist("", "X")
	for _, tid := range []string{"A", "B", "C", "D", "E", "F"} {
		seedTrack(t, s, makeTrack(tid, tid, "x", "House", 120))
	}
	_ = p.AddTracks(pl.ID, []string{"A", "B", "C", "D", "E", "F"})

	// Move [B, D, F] to index 0 → expect B, D, F, A, C, E (order within
	// the moving set is preserved).
	if err := p.ReorderMany(pl.ID, []string{"B", "D", "F"}, 0); err != nil {
		t.Fatalf("ReorderMany: %v", err)
	}
	got, _ := p.Tracks(pl.ID)
	want := []string{"B", "D", "F", "A", "C", "E"}
	if !sameIDs(got, want) {
		t.Fatalf("got %v want %v", ids(got), want)
	}
}

func TestDeleteFolderCascades(t *testing.T) {
	p, s, _ := newTestService(t)
	folder, _ := p.CreateFolder("", "Parent")
	pl, _ := p.CreatePlaylist(folder.ID, "Child")
	seedTrack(t, s, makeTrack("A", "A", "x", "House", 120))
	_ = p.AddTracks(pl.ID, []string{"A"})

	if err := p.Delete(folder.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	tree, _ := p.Tree()
	if len(tree) != 0 {
		t.Fatalf("tree should be empty after cascade delete: %v", tree)
	}

	var count int
	_ = p.store.db.QueryRow(`SELECT COUNT(*) FROM playlist_tracks`).Scan(&count)
	if count != 0 {
		t.Fatalf("playlist_tracks rows leaked: %d", count)
	}
}

func TestSmartPlaylistBPMBetween(t *testing.T) {
	p, s, _ := newTestService(t)
	seedTrack(t, s, makeTrack("slow", "Slow", "x", "House", 100))
	seedTrack(t, s, makeTrack("mid", "Mid", "x", "House", 125))
	seedTrack(t, s, makeTrack("fast", "Fast", "x", "Techno", 140))

	rules := model.SmartRules{
		Match: "all",
		Rules: []model.SmartRule{
			{Field: "bpm", Op: "between", Value: []interface{}{120.0, 130.0}},
			{Field: "genre", Op: "eq", Value: "House"},
		},
	}
	smart, err := p.CreateSmart("", "120-130 House", rules)
	if err != nil {
		t.Fatalf("CreateSmart: %v", err)
	}

	got, err := p.Tracks(smart.ID)
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if len(got) != 1 || got[0].ID != "mid" {
		t.Fatalf("expected only 'mid', got %v", ids(got))
	}
}

func TestMigrateIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "idem.db")
	s1, err := NewStore(dbPath, 0)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// Create a node so we can check it survives the reopen.
	p1 := NewPlaylistService(s1, nil)
	if _, err := p1.CreatePlaylist("", "persist"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s2, err := NewStore(dbPath, 0)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer s2.Close()
	p2 := NewPlaylistService(s2, nil)
	tree, err := p2.Tree()
	if err != nil {
		t.Fatalf("tree after reopen: %v", err)
	}
	if len(tree) != 1 || tree[0].Name != "persist" {
		t.Fatalf("reopened db lost state: %v", tree)
	}
}

func TestAnalysisInvalidatesSmartPlaylist(t *testing.T) {
	p, _, bus := newTestService(t)

	// Smart playlist that references BPM — analysis completions should
	// invalidate it.
	rules := model.SmartRules{Match: "all", Rules: []model.SmartRule{{Field: "bpm", Op: "gt", Value: 100}}}
	smart, _ := p.CreateSmart("", "Fast", rules)

	// A smart playlist that does NOT reference analysis fields — should be
	// left alone.
	static, _ := p.CreateSmart("", "Rocker", model.SmartRules{
		Match: "all",
		Rules: []model.SmartRule{{Field: "genre", Op: "eq", Value: "Rock"}},
	})

	invalidated := make(chan string, 4)
	bus.Subscribe(event.TopicPlaylist, func(ev event.Event) error {
		if ev.Action == event.ActionPlaylistInvalidated {
			invalidated <- ev.Payload.(string)
		}
		return nil
	})

	bus.Publish(event.Event{Topic: event.TopicAnalysis, Action: event.ActionAnalyzeComplete, Payload: &event.AnalysisResult{TrackID: "foo"}})

	deadline := time.After(500 * time.Millisecond)
	var got []string
collect:
	for {
		select {
		case id := <-invalidated:
			got = append(got, id)
			if len(got) >= 1 {
				// Drain for a tick to see if the static one also fires.
				select {
				case extra := <-invalidated:
					got = append(got, extra)
				case <-time.After(50 * time.Millisecond):
					break collect
				}
			}
		case <-deadline:
			break collect
		}
	}

	foundSmart := false
	for _, id := range got {
		if id == static.ID {
			t.Fatalf("static smart playlist should not be invalidated")
		}
		if id == smart.ID {
			foundSmart = true
		}
	}
	if !foundSmart {
		t.Fatalf("expected invalidation for %s, got %v", smart.ID, got)
	}
}

func ids(ts []model.Track) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}

func sameIDs(got []model.Track, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].ID != want[i] {
			return false
		}
	}
	return true
}
