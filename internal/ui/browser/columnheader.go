package browser

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

// ColumnDef defines a column in the track table.
type ColumnDef struct {
	Label     string
	ID        string
	MinWidth  float32
	Flex      float32 // 0 = fixed at MinWidth, >0 = proportional
	Alignment fyne.TextAlign
	Monospace bool
}

// Default column definitions for the track table.
var defaultColumns = []ColumnDef{
	{Label: "TITLE", ID: "title", MinWidth: 120, Flex: 3, Alignment: fyne.TextAlignLeading},
	{Label: "ARTIST", ID: "artist", MinWidth: 100, Flex: 2, Alignment: fyne.TextAlignLeading},
	{Label: "ALBUM", ID: "album", MinWidth: 80, Flex: 2, Alignment: fyne.TextAlignLeading},
	{Label: "BPM", ID: "bpm", MinWidth: 55, Flex: 0, Alignment: fyne.TextAlignTrailing, Monospace: true},
	{Label: "KEY", ID: "key", MinWidth: 45, Flex: 0, Alignment: fyne.TextAlignCenter},
	{Label: "TIME", ID: "time", MinWidth: 55, Flex: 0, Alignment: fyne.TextAlignTrailing, Monospace: true},
}

// ColumnHeader is a clickable column header bar with sort indicators.
type ColumnHeader struct {
	widget.BaseWidget

	mu      sync.RWMutex
	columns []ColumnDef
	sortCol string
	sortAsc bool
	onSort  func(colID string, ascending bool)
}

var _ fyne.Tappable = (*ColumnHeader)(nil)

func NewColumnHeader(columns []ColumnDef, onSort func(colID string, ascending bool)) *ColumnHeader {
	h := &ColumnHeader{
		columns: columns,
		sortCol: "title",
		sortAsc: true,
		onSort:  onSort,
	}
	h.ExtendBaseWidget(h)
	return h
}

func (h *ColumnHeader) SetSort(colID string, ascending bool) {
	h.mu.Lock()
	h.sortCol = colID
	h.sortAsc = ascending
	h.mu.Unlock()
	fyne.Do(func() { h.Refresh() })
}

func (h *ColumnHeader) Tapped(ev *fyne.PointEvent) {
	h.mu.RLock()
	sortCol := h.sortCol
	sortAsc := h.sortAsc
	h.mu.RUnlock()

	// Determine which column was clicked using the same layout math
	size := h.Size()
	gap := float32(8)
	padding := float32(10) // matches container.NewPadded
	fixedTotal := float32(55+45+55) + gap*5
	remaining := size.Width - fixedTotal - padding*2
	if remaining < 0 {
		remaining = 0
	}

	widths := []float32{
		remaining * 3 / 7, // title
		remaining * 2 / 7, // artist
		remaining * 2 / 7, // album
		55,                // bpm
		45,                // key
		55,                // dur
	}

	x := padding
	for i, w := range widths {
		if ev.Position.X >= x && ev.Position.X < x+w {
			colID := h.columns[i].ID
			if colID == sortCol {
				sortAsc = !sortAsc
			} else {
				sortCol = colID
				sortAsc = true
			}
			h.mu.Lock()
			h.sortCol = sortCol
			h.sortAsc = sortAsc
			h.mu.Unlock()
			h.Refresh()
			if h.onSort != nil {
				h.onSort(sortCol, sortAsc)
			}
			return
		}
		x += w + gap
	}
}

func (h *ColumnHeader) MinSize() fyne.Size {
	return fyne.NewSize(200, 24)
}

func (h *ColumnHeader) CreateRenderer() fyne.WidgetRenderer {
	r := &colHeaderRenderer{header: h}
	r.build()
	return r
}

type colHeaderRenderer struct {
	header  *ColumnHeader
	bg      *canvas.Rectangle
	sep     *canvas.Rectangle
	labels  []*canvas.Text
	allObjs []fyne.CanvasObject
}

func (r *colHeaderRenderer) build() {
	r.bg = canvas.NewRectangle(boomtheme.ColorHeaderBg)
	r.sep = canvas.NewRectangle(boomtheme.ColorSeparator)

	r.allObjs = []fyne.CanvasObject{r.bg, r.sep}

	for _, col := range r.header.columns {
		l := canvas.NewText(col.Label, boomtheme.ColorLabelSecondary)
		l.TextSize = 11
		l.TextStyle = fyne.TextStyle{Bold: true}
		l.Alignment = col.Alignment
		r.labels = append(r.labels, l)
		r.allObjs = append(r.allObjs, l)
	}

	r.Refresh()
}

func (r *colHeaderRenderer) Layout(size fyne.Size) { r.layout(size) }
func (r *colHeaderRenderer) MinSize() fyne.Size    { return r.header.MinSize() }
func (r *colHeaderRenderer) Destroy()              {}
func (r *colHeaderRenderer) Objects() []fyne.CanvasObject { return r.allObjs }

func (r *colHeaderRenderer) Refresh() {
	r.layout(r.header.Size())
}

func (r *colHeaderRenderer) layout(size fyne.Size) {
	r.header.mu.RLock()
	sortCol := r.header.sortCol
	sortAsc := r.header.sortAsc
	columns := r.header.columns
	r.header.mu.RUnlock()

	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Refresh()

	r.sep.Resize(fyne.NewSize(size.Width, 0.5))
	r.sep.Move(fyne.NewPos(0, size.Height-0.5))
	r.sep.Refresh()

	// Use same layout math as columnLayout in tracklist.go
	gap := float32(8)
	padding := float32(10) // matches container.NewPadded inset
	fixedTotal := float32(55+45+55) + gap*5
	remaining := size.Width - fixedTotal - padding*2
	if remaining < 0 {
		remaining = 0
	}

	widths := []float32{
		remaining * 3 / 7, // title
		remaining * 2 / 7, // artist
		remaining * 2 / 7, // album
		55,                // bpm
		45,                // key
		55,                // dur
	}

	x := padding
	for i, label := range r.labels {
		if i >= len(columns) || i >= len(widths) {
			break
		}

		text := columns[i].Label
		if columns[i].ID == sortCol {
			if sortAsc {
				text += " \u25B2"
			} else {
				text += " \u25BC"
			}
			label.Color = boomtheme.ColorLabel
		} else {
			label.Color = boomtheme.ColorLabelSecondary
		}

		label.Text = text
		label.Move(fyne.NewPos(x, (size.Height-13)/2))
		label.Resize(fyne.NewSize(widths[i], 13))
		label.Refresh()

		x += widths[i] + gap
	}
}
