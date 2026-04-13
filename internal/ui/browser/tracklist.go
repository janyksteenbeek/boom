package browser

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// rowHeight is the vertical space reserved for a single track row; used to
// turn drag deltas into index offsets during DnD reorder.
const rowHeight float32 = 36

// TrackList displays tracks in a scrollable list with alternating row
// backgrounds, supports multi-select (Shift/Cmd click), and provides hooks
// for right-click context menus and drag-and-drop reorder.
type TrackList struct {
	widget.BaseWidget

	mu               sync.RWMutex
	tracks           []model.Track
	list             *widget.List
	onSelect         func(track model.Track)
	sortCol          string
	sortAsc          bool
	selectedIdx int              // primary (keyboard/MIDI) selection
	selected    map[int]struct{} // full multi-select set
	anchorIdx   int              // Shift+Click anchor
	reorderable bool
	playlistID  string

	// onContext fires on right-click. idx is the row the user clicked on;
	// if it was not part of the existing selection, the caller should treat
	// only that single row as the context. at is the absolute canvas
	// position of the click — use it to anchor the popup menu.
	onContext func(idx int, at fyne.Position)

	// onReorder fires on DnD drop. Caller receives the set of track IDs in
	// their original visual order and the target index within the current
	// (unchanged) list.
	onReorder func(trackIDs []string, newIndex int)
}

func NewTrackList(onSelect func(track model.Track)) *TrackList {
	t := &TrackList{
		onSelect: onSelect,
		sortCol:  "title",
		sortAsc:  true,
		selected: map[int]struct{}{},
	}

	t.list = widget.NewList(
		func() int {
			t.mu.RLock()
			defer t.mu.RUnlock()
			return len(t.tracks)
		},
		func() fyne.CanvasObject {
			return newTrackRow(t)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*trackRow)
			t.mu.RLock()
			if i >= len(t.tracks) {
				t.mu.RUnlock()
				return
			}
			track := t.tracks[i]
			_, multi := t.selected[i]
			t.mu.RUnlock()
			row.setIndex(int(i))
			row.setData(track, multi, i%2 == 1)
		},
	)

	// Keep the inner list's cursor in sync with our multi-select state. We
	// deliberately do not fire onSelect from here — single-click should
	// only select, never load. Double-click is the explicit load gesture
	// (see trackRow.DoubleTapped).
	t.list.OnSelected = func(i widget.ListItemID) {
		t.mu.Lock()
		t.selectedIdx = int(i)
		t.mu.Unlock()
	}

	t.ExtendBaseWidget(t)
	return t
}

// SetReorderable toggles whether DnD reorder is active (true when viewing a
// manual playlist; false for library filters or auto playlists).
func (t *TrackList) SetReorderable(r bool) {
	t.mu.Lock()
	t.reorderable = r
	t.mu.Unlock()
}

// SetPlaylistID records which playlist the current view belongs to. Used by
// the context menu and reorder handlers.
func (t *TrackList) SetPlaylistID(id string) {
	t.mu.Lock()
	t.playlistID = id
	t.mu.Unlock()
}

// PlaylistID returns the currently open playlist ID (empty when viewing a
// library filter).
func (t *TrackList) PlaylistID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.playlistID
}

// SetOnContext installs the right-click handler.
func (t *TrackList) SetOnContext(fn func(idx int, at fyne.Position)) { t.onContext = fn }

// SetOnReorder installs the DnD-drop handler. trackIDs is in visual order,
// newIndex is the target insertion index in the list after removing the
// moving rows.
func (t *TrackList) SetOnReorder(fn func(trackIDs []string, newIndex int)) { t.onReorder = fn }

func (t *TrackList) SetTracks(tracks []model.Track) {
	t.mu.Lock()
	t.tracks = tracks
	t.selected = map[int]struct{}{}
	t.selectedIdx = 0
	t.anchorIdx = 0
	t.mu.Unlock()
	fyne.Do(func() {
		t.list.Refresh()
	})
}

// Sort sorts the current tracks by the given column.
func (t *TrackList) Sort(colID string, ascending bool) {
	t.mu.Lock()
	t.sortCol = colID
	t.sortAsc = ascending

	sort.SliceStable(t.tracks, func(i, j int) bool {
		var less bool
		switch colID {
		case "title":
			less = strings.ToLower(t.tracks[i].Title) < strings.ToLower(t.tracks[j].Title)
		case "artist":
			less = strings.ToLower(t.tracks[i].Artist) < strings.ToLower(t.tracks[j].Artist)
		case "album":
			less = strings.ToLower(t.tracks[i].Album) < strings.ToLower(t.tracks[j].Album)
		case "bpm":
			less = t.tracks[i].BPM < t.tracks[j].BPM
		case "key":
			less = t.tracks[i].Key < t.tracks[j].Key
		case "time":
			less = t.tracks[i].Duration < t.tracks[j].Duration
		default:
			less = strings.ToLower(t.tracks[i].Title) < strings.ToLower(t.tracks[j].Title)
		}
		if !ascending {
			return !less
		}
		return less
	})
	t.mu.Unlock()

	fyne.Do(func() {
		t.list.Refresh()
	})
}

// ScrollBy moves the selection by one item in the direction of delta.
func (t *TrackList) ScrollBy(delta int) {
	if delta == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	t.mu.Lock()
	if len(t.tracks) == 0 {
		t.mu.Unlock()
		return
	}
	newIdx := t.selectedIdx + step
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(t.tracks) {
		newIdx = len(t.tracks) - 1
	}
	t.selectedIdx = newIdx
	t.selected = map[int]struct{}{newIdx: {}}
	t.anchorIdx = newIdx
	t.mu.Unlock()

	fyne.Do(func() {
		t.list.Select(widget.ListItemID(newIdx))
		t.list.ScrollTo(widget.ListItemID(newIdx))
	})
}

// UnanalyzedTracks returns all visible tracks that have not been analyzed.
func (t *TrackList) UnanalyzedTracks() []model.Track {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var result []model.Track
	for _, tr := range t.tracks {
		if tr.BPM == 0 && tr.Key == "" {
			result = append(result, tr)
		}
	}
	return result
}

// UpdateTrackAnalysis updates BPM and Key for a specific track in-place.
func (t *TrackList) UpdateTrackAnalysis(trackID string, bpm float64, key string) {
	t.mu.Lock()
	for i := range t.tracks {
		if t.tracks[i].ID == trackID {
			t.tracks[i].BPM = bpm
			t.tracks[i].Key = key
			break
		}
	}
	t.mu.Unlock()
	fyne.Do(func() {
		t.list.Refresh()
	})
}

// SelectedTrack returns the primary highlighted track, preserving the
// existing single-track contract for MIDI-load and deck-load paths.
func (t *TrackList) SelectedTrack() *model.Track {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.selectedIdx < 0 || t.selectedIdx >= len(t.tracks) {
		return nil
	}
	track := t.tracks[t.selectedIdx]
	return &track
}

// SelectedTracks returns every highlighted track in visual order. Falls back
// to the single-selection track if the multi-select set is empty (e.g. right
// after switching categories).
func (t *TrackList) SelectedTracks() []model.Track {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.selected) == 0 {
		if t.selectedIdx >= 0 && t.selectedIdx < len(t.tracks) {
			return []model.Track{t.tracks[t.selectedIdx]}
		}
		return nil
	}
	idxs := make([]int, 0, len(t.selected))
	for i := range t.selected {
		if i >= 0 && i < len(t.tracks) {
			idxs = append(idxs, i)
		}
	}
	sort.Ints(idxs)
	out := make([]model.Track, len(idxs))
	for i, idx := range idxs {
		out[i] = t.tracks[idx]
	}
	return out
}

// Tracks returns a copy of the current visible slice.
func (t *TrackList) Tracks() []model.Track {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]model.Track, len(t.tracks))
	copy(out, t.tracks)
	return out
}

// handleRowClick processes a primary click with modifier support. idx is the
// row clicked. mods is the currently held modifier mask.
func (t *TrackList) handleRowClick(idx int, mods desktop.Modifier) {
	t.mu.Lock()
	switch {
	case mods&desktop.ShiftModifier != 0:
		// Range: [anchor, idx] inclusive, replace selection.
		t.selected = map[int]struct{}{}
		lo, hi := t.anchorIdx, idx
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi; i++ {
			t.selected[i] = struct{}{}
		}
	case mods&(desktop.ControlModifier|desktop.SuperModifier) != 0:
		// Toggle in the multi-selection set.
		if _, ok := t.selected[idx]; ok {
			delete(t.selected, idx)
		} else {
			t.selected[idx] = struct{}{}
		}
		t.anchorIdx = idx
	default:
		t.selected = map[int]struct{}{idx: {}}
		t.anchorIdx = idx
	}
	t.selectedIdx = idx
	t.mu.Unlock()

	fyne.Do(func() {
		t.list.Select(widget.ListItemID(idx))
		t.list.Refresh()
	})
}

// handleRowDoubleClick fires on a double click — loads the row's track via
// the onSelect callback the browser view supplied at construction, which
// publishes the ActionLoadTrack event for the current target deck.
func (t *TrackList) handleRowDoubleClick(idx int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if idx < 0 || idx >= len(t.tracks) || t.onSelect == nil {
		return
	}
	t.onSelect(t.tracks[idx])
}

// handleRowContext handles a right-click. If the clicked row is not in the
// current selection, it replaces the selection with just that row first so
// menu actions have an obvious target.
func (t *TrackList) handleRowContext(idx int, at fyne.Position) {
	t.mu.Lock()
	if _, ok := t.selected[idx]; !ok {
		t.selected = map[int]struct{}{idx: {}}
		t.anchorIdx = idx
		t.selectedIdx = idx
	}
	t.mu.Unlock()
	fyne.Do(func() { t.list.Refresh() })
	if t.onContext != nil {
		t.onContext(idx, at)
	}
}

// handleRowDragEnd converts a drag delta (in pixels) into an index shift
// and, if reorder is active, calls the reorder callback with the currently
// selected track IDs.
func (t *TrackList) handleRowDragEnd(startIdx int, totalDeltaY float32) {
	t.mu.RLock()
	reorder := t.reorderable
	t.mu.RUnlock()
	if !reorder || t.onReorder == nil {
		return
	}
	rowOffset := int(totalDeltaY / rowHeight)
	if totalDeltaY < 0 {
		rowOffset = int((totalDeltaY - rowHeight/2) / rowHeight)
	} else {
		rowOffset = int((totalDeltaY + rowHeight/2) / rowHeight)
	}
	newIndex := startIdx + rowOffset
	if newIndex < 0 {
		newIndex = 0
	}

	tracks := t.SelectedTracks()
	if len(tracks) == 0 {
		return
	}
	ids := make([]string, len(tracks))
	for i, tr := range tracks {
		ids[i] = tr.ID
	}
	t.onReorder(ids, newIndex)
}

func (t *TrackList) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.list)
}

// -------------------- row widget --------------------

// trackRow is a single clickable/draggable row inside the list template.
// The list recycles these widgets, so setIndex/setData rewrite its state on
// every update pass.
type trackRow struct {
	widget.BaseWidget

	tl       *TrackList
	idx      int
	track    model.Track
	multi    bool
	zebra    bool
	dragFrom int
	dragDY   float32

	bg     *canvas.Rectangle
	title  *canvas.Text
	artist *canvas.Text
	album  *canvas.Text
	bpm    *canvas.Text
	key    *canvas.Text
	dur    *canvas.Text
}

func newTrackRow(tl *TrackList) *trackRow {
	r := &trackRow{tl: tl}
	r.bg = canvas.NewRectangle(color.Transparent)
	r.title = canvas.NewText("", boomtheme.ColorLabel)
	r.title.TextSize = 13
	r.artist = canvas.NewText("", boomtheme.ColorLabelSecondary)
	r.artist.TextSize = 12
	r.album = canvas.NewText("", boomtheme.ColorLabelTertiary)
	r.album.TextSize = 12
	r.bpm = canvas.NewText("", boomtheme.ColorLabelSecondary)
	r.bpm.TextSize = 11
	r.bpm.TextStyle = fyne.TextStyle{Monospace: true}
	r.bpm.Alignment = fyne.TextAlignTrailing
	r.key = canvas.NewText("", boomtheme.ColorLabelTertiary)
	r.key.TextSize = 11
	r.key.Alignment = fyne.TextAlignCenter
	r.dur = canvas.NewText("", boomtheme.ColorLabelTertiary)
	r.dur.TextSize = 11
	r.dur.TextStyle = fyne.TextStyle{Monospace: true}
	r.dur.Alignment = fyne.TextAlignTrailing
	r.ExtendBaseWidget(r)
	return r
}

func (r *trackRow) setIndex(idx int) { r.idx = idx }

func (r *trackRow) setData(track model.Track, multi, zebra bool) {
	r.track = track
	r.multi = multi
	r.zebra = zebra

	r.title.Text = truncate(track.Title, 40)
	r.artist.Text = truncate(track.Artist, 28)
	r.album.Text = truncate(track.Album, 24)
	if track.BPM > 0 {
		r.bpm.Text = fmt.Sprintf("%.0f", track.BPM)
	} else {
		r.bpm.Text = ""
	}
	r.key.Text = track.Key
	r.dur.Text = fmt.Sprintf("%d:%02d", int(track.Duration.Minutes()), int(track.Duration.Seconds())%60)

	r.Refresh()
}

func (r *trackRow) CreateRenderer() fyne.WidgetRenderer {
	row := container.New(&columnLayout{}, r.title, r.artist, r.album, r.bpm, r.key, r.dur)
	stack := container.NewStack(r.bg, container.NewPadded(row))
	return widget.NewSimpleRenderer(stack)
}

func (r *trackRow) Refresh() {
	switch {
	case r.multi:
		r.bg.FillColor = boomtheme.ColorSidebarItemSelected
	case r.zebra:
		r.bg.FillColor = boomtheme.ColorRowAlternate
	default:
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
	r.title.Refresh()
	r.artist.Refresh()
	r.album.Refresh()
	r.bpm.Refresh()
	r.key.Refresh()
	r.dur.Refresh()
}

// --- desktop.Mouseable: primary click with modifier support ---

var _ desktop.Mouseable = (*trackRow)(nil)
var _ fyne.SecondaryTappable = (*trackRow)(nil)
var _ fyne.DoubleTappable = (*trackRow)(nil)
var _ fyne.Draggable = (*trackRow)(nil)

func (r *trackRow) MouseDown(e *desktop.MouseEvent) {
	if e.Button != desktop.MouseButtonPrimary {
		return
	}
	r.tl.handleRowClick(r.idx, e.Modifier)
}

func (r *trackRow) MouseUp(_ *desktop.MouseEvent) {}

// TappedSecondary fires on right-click.
func (r *trackRow) TappedSecondary(e *fyne.PointEvent) {
	r.tl.handleRowContext(r.idx, e.AbsolutePosition)
}

// DoubleTapped is the load-to-deck gesture: single click selects, double
// click publishes the row as the current selection which the BrowserView
// translates into an ActionLoadTrack event.
func (r *trackRow) DoubleTapped(_ *fyne.PointEvent) {
	r.tl.handleRowDoubleClick(r.idx)
}

// --- fyne.Draggable: DnD reorder ---

func (r *trackRow) Dragged(ev *fyne.DragEvent) {
	if r.dragFrom == 0 && r.dragDY == 0 {
		r.dragFrom = r.idx
	}
	r.dragDY += ev.Dragged.DY
}

func (r *trackRow) DragEnd() {
	start := r.dragFrom
	dy := r.dragDY
	r.dragFrom = 0
	r.dragDY = 0
	r.tl.handleRowDragEnd(start, dy)
}

// columnLayout distributes width proportionally for track columns.
// Objects order: title(flex3), artist(flex2), album(flex2), bpm(55), key(45), dur(55)
type columnLayout struct{}

var _ fyne.Layout = (*columnLayout)(nil)

func (l *columnLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(400, 18)
}

func (l *columnLayout) Layout(objects []fyne.CanvasObject, containerSize fyne.Size) {
	if len(objects) < 6 {
		return
	}

	gap := float32(8)
	fixedBPM := float32(55)
	fixedKey := float32(45)
	fixedDur := float32(55)
	fixedTotal := fixedBPM + fixedKey + fixedDur + gap*5

	remaining := containerSize.Width - fixedTotal
	if remaining < 0 {
		remaining = 0
	}

	titleW := remaining * 3 / 7
	artistW := remaining * 2 / 7
	albumW := remaining * 2 / 7
	h := containerSize.Height

	x := float32(0)

	objects[0].Move(fyne.NewPos(x, 0))
	objects[0].Resize(fyne.NewSize(titleW, h))
	x += titleW + gap

	objects[1].Move(fyne.NewPos(x, 0))
	objects[1].Resize(fyne.NewSize(artistW, h))
	x += artistW + gap

	objects[2].Move(fyne.NewPos(x, 0))
	objects[2].Resize(fyne.NewSize(albumW, h))
	x += albumW + gap

	objects[3].Move(fyne.NewPos(x, 0))
	objects[3].Resize(fyne.NewSize(fixedBPM, h))
	x += fixedBPM + gap

	objects[4].Move(fyne.NewPos(x, 0))
	objects[4].Resize(fyne.NewSize(fixedKey, h))
	x += fixedKey + gap

	objects[5].Move(fyne.NewPos(x, 0))
	objects[5].Resize(fyne.NewSize(fixedDur, h))
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "\u2026"
}
