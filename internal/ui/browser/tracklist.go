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
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// TrackList displays tracks in a scrollable list with alternating row backgrounds.
type TrackList struct {
	widget.BaseWidget

	mu               sync.RWMutex
	tracks           []model.Track
	list             *widget.List
	onSelect         func(track model.Track)
	sortCol          string
	sortAsc          bool
	selectedIdx      int
	suppressOnSelect bool // guards against onSelect during programmatic Select()
}

func NewTrackList(onSelect func(track model.Track)) *TrackList {
	t := &TrackList{
		onSelect: onSelect,
		sortCol:  "title",
		sortAsc:  true,
	}

	t.list = widget.NewList(
		func() int {
			t.mu.RLock()
			defer t.mu.RUnlock()
			return len(t.tracks)
		},
		func() fyne.CanvasObject {
			bg := canvas.NewRectangle(color.Transparent)

			title := canvas.NewText("", boomtheme.ColorLabel)
			title.TextSize = 13

			artist := canvas.NewText("", boomtheme.ColorLabelSecondary)
			artist.TextSize = 12

			album := canvas.NewText("", boomtheme.ColorLabelTertiary)
			album.TextSize = 12

			bpm := canvas.NewText("", boomtheme.ColorLabelSecondary)
			bpm.TextSize = 11
			bpm.TextStyle = fyne.TextStyle{Monospace: true}
			bpm.Alignment = fyne.TextAlignTrailing

			key := canvas.NewText("", boomtheme.ColorLabelTertiary)
			key.TextSize = 11
			key.Alignment = fyne.TextAlignCenter

			dur := canvas.NewText("", boomtheme.ColorLabelTertiary)
			dur.TextSize = 11
			dur.TextStyle = fyne.TextStyle{Monospace: true}
			dur.Alignment = fyne.TextAlignTrailing

			// Use GridWithColumns for proportional layout:
			// 6 columns total, with relative weighting achieved by spanning
			row := container.New(&columnLayout{},
				title,  // flex 3
				artist, // flex 2
				album,  // flex 2
				bpm,    // fixed 55
				key,    // fixed 45
				dur,    // fixed 55
			)

			return container.NewStack(bg, container.NewPadded(row))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			t.mu.RLock()
			if i >= len(t.tracks) {
				t.mu.RUnlock()
				return
			}
			track := t.tracks[i]
			t.mu.RUnlock()

			stack := o.(*fyne.Container)
			bg := stack.Objects[0].(*canvas.Rectangle)
			padded := stack.Objects[1].(*fyne.Container)
			row := padded.Objects[0].(*fyne.Container)

			// Alternating row background
			if i%2 == 1 {
				bg.FillColor = boomtheme.ColorRowAlternate
			} else {
				bg.FillColor = color.Transparent
			}
			bg.Refresh()

			title := row.Objects[0].(*canvas.Text)
			title.Text = truncate(track.Title, 40)
			title.Refresh()

			artist := row.Objects[1].(*canvas.Text)
			artist.Text = truncate(track.Artist, 28)
			artist.Refresh()

			albumText := row.Objects[2].(*canvas.Text)
			albumText.Text = truncate(track.Album, 24)
			albumText.Refresh()

			bpmText := row.Objects[3].(*canvas.Text)
			if track.BPM > 0 {
				bpmText.Text = fmt.Sprintf("%.0f", track.BPM)
			} else {
				bpmText.Text = ""
			}
			bpmText.Refresh()

			keyText := row.Objects[4].(*canvas.Text)
			keyText.Text = track.Key
			keyText.Refresh()

			durText := row.Objects[5].(*canvas.Text)
			durText.Text = fmt.Sprintf("%d:%02d", int(track.Duration.Minutes()), int(track.Duration.Seconds())%60)
			durText.Refresh()
		},
	)

	t.list.OnSelected = func(i widget.ListItemID) {
		t.mu.Lock()
		t.selectedIdx = i
		t.mu.Unlock()

		if t.suppressOnSelect {
			return
		}

		t.mu.RLock()
		defer t.mu.RUnlock()
		if i < len(t.tracks) && t.onSelect != nil {
			t.onSelect(t.tracks[i])
		}
	}

	t.ExtendBaseWidget(t)
	return t
}

func (t *TrackList) SetTracks(tracks []model.Track) {
	t.mu.Lock()
	t.tracks = tracks
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

// ScrollBy moves the selection by delta items (negative = up, positive = down).
// Called from MIDI browse_scroll events.
func (t *TrackList) ScrollBy(delta int) {
	t.mu.Lock()
	newIdx := t.selectedIdx + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(t.tracks) {
		newIdx = len(t.tracks) - 1
	}
	if len(t.tracks) == 0 {
		t.mu.Unlock()
		return
	}
	t.selectedIdx = newIdx
	t.mu.Unlock()

	fyne.Do(func() {
		t.suppressOnSelect = true
		t.list.Select(widget.ListItemID(newIdx))
		t.list.ScrollTo(widget.ListItemID(newIdx))
		t.suppressOnSelect = false
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

// SelectedTrack returns the currently highlighted track, or nil if none.
func (t *TrackList) SelectedTrack() *model.Track {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.selectedIdx < 0 || t.selectedIdx >= len(t.tracks) {
		return nil
	}
	track := t.tracks[t.selectedIdx]
	return &track
}

func (t *TrackList) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.list)
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
	fixedTotal := fixedBPM + fixedKey + fixedDur + gap*5 // 5 gaps between 6 columns

	remaining := containerSize.Width - fixedTotal
	if remaining < 0 {
		remaining = 0
	}

	// Flex distribution: title=3, artist=2, album=2 → total=7
	titleW := remaining * 3 / 7
	artistW := remaining * 2 / 7
	albumW := remaining * 2 / 7
	h := containerSize.Height

	x := float32(0)

	// Title
	objects[0].Move(fyne.NewPos(x, 0))
	objects[0].Resize(fyne.NewSize(titleW, h))
	x += titleW + gap

	// Artist
	objects[1].Move(fyne.NewPos(x, 0))
	objects[1].Resize(fyne.NewSize(artistW, h))
	x += artistW + gap

	// Album
	objects[2].Move(fyne.NewPos(x, 0))
	objects[2].Resize(fyne.NewSize(albumW, h))
	x += albumW + gap

	// BPM
	objects[3].Move(fyne.NewPos(x, 0))
	objects[3].Resize(fyne.NewSize(fixedBPM, h))
	x += fixedBPM + gap

	// Key
	objects[4].Move(fyne.NewPos(x, 0))
	objects[4].Resize(fyne.NewSize(fixedKey, h))
	x += fixedKey + gap

	// Duration
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
