package browser

import (
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

const sidebarWidth float32 = 180

// Sidebar is a macOS-style source list for library navigation.
type Sidebar struct {
	widget.BaseWidget

	mu           sync.RWMutex
	items        []*SidebarItem
	selectedID   string
	onSelect     func(id string)
	onNodeAction func(nodeID, action string, at fyne.Position) // right-click actions
	content      *fyne.Container
	scroll       *container.Scroll
	bg           *canvas.Rectangle

	genres   []string
	tree     []*model.PlaylistNode
	expanded map[string]bool
}

func NewSidebar(onSelect func(id string)) *Sidebar {
	s := &Sidebar{
		onSelect:   onSelect,
		selectedID: "all",
		expanded:   make(map[string]bool),
	}

	s.bg = canvas.NewRectangle(boomtheme.ColorSidebarBg)
	// Tight stack — Fyne's default VBox adds theme.Padding() (6px) between
	// every child which makes the sidebar feel roomy. tightVBoxLayout has
	// zero inter-row gap so rows sit flush and section spacers are the
	// only vertical breathing space.
	s.content = container.New(&tightVBoxLayout{})
	s.scroll = container.NewVScroll(s.content)

	s.rebuild()
	s.ExtendBaseWidget(s)
	return s
}

// SetOnNodeAction registers the handler invoked when the user opens a
// context menu on a sidebar row ("menu") or on the section-header "+"
// affordance ("menu_root"). The position is in absolute canvas coordinates
// and should be used as the popup anchor.
func (s *Sidebar) SetOnNodeAction(fn func(nodeID, action string, at fyne.Position)) {
	s.onNodeAction = fn
}

// SetGenres updates the genre section of the sidebar and rebuilds.
func (s *Sidebar) SetGenres(genres []string) {
	s.mu.Lock()
	s.genres = genres
	s.mu.Unlock()
	s.rebuild()
}

// SetPlaylistTree replaces the playlist tree and rebuilds. Nodes are the flat
// list returned by PlaylistService.Tree(); children are resolved by ParentID.
func (s *Sidebar) SetPlaylistTree(tree []*model.PlaylistNode) {
	s.mu.Lock()
	s.tree = tree
	s.mu.Unlock()
	s.rebuild()
}

// rebuild regenerates the whole sidebar from the current state. Callers may
// invoke this after mutating genres, tree, or expanded state — it is safe to
// call on any goroutine because the final Refresh is marshalled onto the
// Fyne driver thread.
func (s *Sidebar) rebuild() {
	s.mu.Lock()
	genres := s.genres
	tree := s.tree
	s.content.Objects = nil
	s.items = nil

	// Top padding
	s.content.Add(spacerRect(4))

	// Library section
	s.content.Add(sectionHeader("LIBRARY"))
	s.content.Add(s.newItem("All Tracks", "all", 0, sbKindStatic))
	s.content.Add(s.newItem("Recently Added", "recent", 0, sbKindStatic))
	s.content.Add(s.newItem("Unanalyzed", "unanalyzed", 0, sbKindStatic))

	// Playlists section — always visible so right-click "New Playlist" has
	// a target header to click on.
	s.content.Add(spacerRect(8))
	s.content.Add(s.sectionHeaderRow("PLAYLISTS", "root"))
	s.appendPlaylistNodes(tree, "", 0)

	// Genre section
	if len(genres) > 0 {
		s.content.Add(spacerRect(8))
		s.content.Add(sectionHeader("GENRE"))
		for _, g := range genres {
			s.content.Add(s.newItem(g, "genre:"+g, 0, sbKindStatic))
		}
	}

	// BPM Range section
	s.content.Add(spacerRect(8))
	s.content.Add(sectionHeader("BPM RANGE"))
	bpmRanges := []struct {
		label string
		id    string
	}{
		{"Downtempo (60–90)", "bpm:60-90"},
		{"Hip-Hop (90–115)", "bpm:90-115"},
		{"House (115–130)", "bpm:115-130"},
		{"Techno (130–145)", "bpm:130-145"},
		{"D&B (145–175)", "bpm:145-175"},
		{"Hardcore (175+)", "bpm:175-300"},
	}
	for _, r := range bpmRanges {
		s.content.Add(s.newItem(r.label, r.id, 0, sbKindStatic))
	}

	// Bottom padding
	s.content.Add(spacerRect(4))

	selected := s.selectedID
	s.mu.Unlock()

	s.updateSelection(selected)

	fyne.Do(func() {
		s.content.Refresh()
	})
}

// appendPlaylistNodes walks the flat tree and appends the visible children of
// parentID at the given depth, recursing through expanded folders. Must be
// called with s.mu held.
func (s *Sidebar) appendPlaylistNodes(tree []*model.PlaylistNode, parentID string, depth int) {
	for _, n := range tree {
		if n.ParentID != parentID {
			continue
		}
		label := n.Name
		kind := sbKindPlaylist
		switch n.Kind {
		case model.KindFolder:
			kind = sbKindFolder
			if s.expanded[n.ID] {
				label = "▼  " + n.Name
			} else {
				label = "▶  " + n.Name
			}
		case model.KindSmart:
			kind = sbKindSmart
			label = "⚡ " + n.Name
		default:
			label = "♪  " + n.Name
		}
		item := s.newItem(label, "playlist:"+n.ID, depth, kind)
		item.nodeID = n.ID
		s.content.Add(item)

		if n.Kind == model.KindFolder && s.expanded[n.ID] {
			s.appendPlaylistNodes(tree, n.ID, depth+1)
		}
	}
}

func (s *Sidebar) newItem(label, id string, depth int, kind sbItemKind) *SidebarItem {
	onTap := func(itemID string) {
		s.mu.Lock()
		s.selectedID = itemID
		s.mu.Unlock()
		s.updateSelection(itemID)
		if s.onSelect != nil {
			s.onSelect(itemID)
		}
	}
	item := NewSidebarItem(label, id, onTap)
	item.depth = depth
	item.kind = kind
	if kind == sbKindFolder {
		// Folder taps toggle expansion AND also select the folder so any
		// keyboard shortcut (rename/delete) has a target.
		item.onTap = func(itemID string) {
			s.mu.Lock()
			nodeID := item.nodeID
			s.expanded[nodeID] = !s.expanded[nodeID]
			s.selectedID = itemID
			s.mu.Unlock()
			s.rebuild()
			s.updateSelection(itemID)
		}
	}
	item.onContext = func(at fyne.Position) {
		if s.onNodeAction != nil && item.nodeID != "" {
			s.onNodeAction(item.nodeID, "menu", at)
		}
	}
	s.items = append(s.items, item)
	if id == s.selectedID {
		item.SetSelected(true)
	}
	return item
}

// sectionHeaderRow returns a section header with a small "+" affordance that
// triggers the section's "new" action through onNodeAction (nodeID is empty
// for root-level sections so the handler knows to create under root).
func (s *Sidebar) sectionHeaderRow(text, nodeID string) fyne.CanvasObject {
	plus := newPlusGlyph(func(at fyne.Position) {
		if s.onNodeAction != nil {
			s.onNodeAction(nodeID, "menu_root", at)
		}
	})
	return newSectionHeader(text, plus)
}

// newSectionHeader lays out a bold caption with an optional trailing action
// at a fixed compact height. Used for both plain section labels and the
// "PLAYLISTS" header with its "+" affordance.
func newSectionHeader(text string, trailing fyne.CanvasObject) fyne.CanvasObject {
	label := canvas.NewText(text, boomtheme.ColorLabelTertiary)
	label.TextSize = 10
	label.TextStyle = fyne.TextStyle{Bold: true}

	row := &sectionHeaderWidget{label: label, trailing: trailing}
	row.ExtendBaseWidget(row)
	return row
}

type sectionHeaderWidget struct {
	widget.BaseWidget
	label    *canvas.Text
	trailing fyne.CanvasObject
}

func (s *sectionHeaderWidget) CreateRenderer() fyne.WidgetRenderer {
	objs := []fyne.CanvasObject{s.label}
	if s.trailing != nil {
		objs = append(objs, s.trailing)
	}
	return &sectionHeaderRenderer{w: s, objs: objs}
}

type sectionHeaderRenderer struct {
	w    *sectionHeaderWidget
	objs []fyne.CanvasObject
}

func (r *sectionHeaderRenderer) Layout(size fyne.Size) {
	r.w.label.Move(fyne.NewPos(8, (size.Height-12)/2))
	r.w.label.Resize(fyne.NewSize(size.Width-28, 12))
	if r.w.trailing != nil {
		ts := r.w.trailing.MinSize()
		r.w.trailing.Move(fyne.NewPos(size.Width-ts.Width-8, (size.Height-ts.Height)/2))
		r.w.trailing.Resize(ts)
	}
}

func (r *sectionHeaderRenderer) MinSize() fyne.Size         { return fyne.NewSize(sidebarWidth-16, 22) }
func (r *sectionHeaderRenderer) Refresh()                   { r.w.label.Refresh() }
func (r *sectionHeaderRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *sectionHeaderRenderer) Destroy()                   {}

// plusGlyph is a small clickable "+" sitting next to a section header. Using
// a widget.Button here would fight the compact row height set by the theme
// padding, so we roll a minimal tappable glyph instead.
type plusGlyph struct {
	widget.BaseWidget
	text    *canvas.Text
	onTap   func(at fyne.Position)
	hovered bool
}

var _ fyne.Tappable = (*plusGlyph)(nil)
var _ desktop.Hoverable = (*plusGlyph)(nil)

func newPlusGlyph(onTap func(at fyne.Position)) *plusGlyph {
	g := &plusGlyph{
		text:  canvas.NewText("+", boomtheme.ColorLabelSecondary),
		onTap: onTap,
	}
	g.text.TextSize = 14
	g.text.TextStyle = fyne.TextStyle{Bold: true}
	g.text.Alignment = fyne.TextAlignCenter
	g.ExtendBaseWidget(g)
	return g
}

func (g *plusGlyph) MinSize() fyne.Size { return fyne.NewSize(16, 16) }

func (g *plusGlyph) Tapped(e *fyne.PointEvent) {
	if g.onTap != nil {
		g.onTap(e.AbsolutePosition)
	}
}

func (g *plusGlyph) MouseIn(*desktop.MouseEvent) {
	g.hovered = true
	g.text.Color = boomtheme.ColorLabel
	g.text.Refresh()
}
func (g *plusGlyph) MouseMoved(*desktop.MouseEvent) {}
func (g *plusGlyph) MouseOut() {
	g.hovered = false
	g.text.Color = boomtheme.ColorLabelSecondary
	g.text.Refresh()
}

func (g *plusGlyph) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(g.text)
}

func (s *Sidebar) updateSelection(id string) {
	for _, item := range s.items {
		item.SetSelected(item.id == id)
	}
}

// ScrollBy moves the sidebar selection by one item in the direction of delta
// and fires onSelect so the library filter updates. Called from MIDI browse
// events when sidebar focus is active.
func (s *Sidebar) ScrollBy(delta int) {
	if delta == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}

	s.mu.Lock()
	if len(s.items) == 0 {
		s.mu.Unlock()
		return
	}
	currentIdx := -1
	for i, item := range s.items {
		if item.id == s.selectedID {
			currentIdx = i
			break
		}
	}
	newIdx := currentIdx + step
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(s.items) {
		newIdx = len(s.items) - 1
	}
	newID := s.items[newIdx].id
	s.selectedID = newID
	s.mu.Unlock()

	s.updateSelection(newID)
	if s.onSelect != nil {
		s.onSelect(newID)
	}
}

func (s *Sidebar) MinSize() fyne.Size {
	return fyne.NewSize(sidebarWidth, 100)
}

func (s *Sidebar) CreateRenderer() fyne.WidgetRenderer {
	wrapped := container.NewStack(s.bg, container.NewPadded(s.scroll))
	return widget.NewSimpleRenderer(wrapped)
}

func sectionHeader(text string) fyne.CanvasObject {
	return newSectionHeader(text, nil)
}

// tightVBoxLayout stacks children vertically with no gap between them. It
// honours each child's MinSize height but expands width to fill the
// container.
type tightVBoxLayout struct{}

var _ fyne.Layout = (*tightVBoxLayout)(nil)

func (tightVBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var h float32
	var w float32
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		ms := o.MinSize()
		h += ms.Height
		if ms.Width > w {
			w = ms.Width
		}
	}
	return fyne.NewSize(w, h)
}

func (tightVBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var y float32
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h
	}
}

func spacerRect(height float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(0, height))
	return r
}

// sbItemKind discriminates how a SidebarItem should behave on tap and how it
// should be drawn.
type sbItemKind int

const (
	sbKindStatic sbItemKind = iota
	sbKindFolder
	sbKindPlaylist
	sbKindSmart
)

// SidebarItem is a single clickable item in the sidebar.
type SidebarItem struct {
	widget.BaseWidget

	mu        sync.RWMutex
	label     string
	id        string
	nodeID    string // playlist_nodes.id for playlist/folder/auto rows
	depth     int
	kind      sbItemKind
	selected  bool
	hovered   bool
	onTap     func(id string)
	onContext func(at fyne.Position)
}

var _ desktop.Hoverable = (*SidebarItem)(nil)
var _ fyne.Tappable = (*SidebarItem)(nil)
var _ fyne.SecondaryTappable = (*SidebarItem)(nil)

func NewSidebarItem(label, id string, onTap func(id string)) *SidebarItem {
	item := &SidebarItem{
		label: label,
		id:    id,
		onTap: onTap,
	}
	item.ExtendBaseWidget(item)
	return item
}

func (i *SidebarItem) SetSelected(selected bool) {
	i.mu.Lock()
	i.selected = selected
	i.mu.Unlock()
	fyne.Do(func() { i.Refresh() })
}

func (i *SidebarItem) Tapped(_ *fyne.PointEvent) {
	if i.onTap != nil {
		i.onTap(i.id)
	}
}

func (i *SidebarItem) TappedSecondary(e *fyne.PointEvent) {
	if i.onContext != nil {
		i.onContext(e.AbsolutePosition)
	}
}

func (i *SidebarItem) MouseIn(_ *desktop.MouseEvent) {
	i.mu.Lock()
	i.hovered = true
	i.mu.Unlock()
	i.Refresh()
}

func (i *SidebarItem) MouseMoved(_ *desktop.MouseEvent) {}

func (i *SidebarItem) MouseOut() {
	i.mu.Lock()
	i.hovered = false
	i.mu.Unlock()
	i.Refresh()
}

func (i *SidebarItem) MinSize() fyne.Size {
	return fyne.NewSize(sidebarWidth-16, 24)
}

func (i *SidebarItem) CreateRenderer() fyne.WidgetRenderer {
	r := &sidebarItemRenderer{item: i}
	r.build()
	return r
}

type sidebarItemRenderer struct {
	item    *SidebarItem
	bg      *canvas.Rectangle
	label   *canvas.Text
	allObjs []fyne.CanvasObject
}

func (r *sidebarItemRenderer) build() {
	r.bg = canvas.NewRectangle(color.Transparent)
	r.bg.CornerRadius = 6

	r.label = canvas.NewText("", boomtheme.ColorLabelSecondary)
	r.label.TextSize = 12

	r.allObjs = []fyne.CanvasObject{r.bg, r.label}
	r.Refresh()
}

func (r *sidebarItemRenderer) Layout(size fyne.Size) { r.layout(size) }
func (r *sidebarItemRenderer) MinSize() fyne.Size    { return r.item.MinSize() }
func (r *sidebarItemRenderer) Destroy()              {}
func (r *sidebarItemRenderer) Objects() []fyne.CanvasObject { return r.allObjs }

func (r *sidebarItemRenderer) Refresh() {
	r.layout(r.item.Size())
}

func (r *sidebarItemRenderer) layout(size fyne.Size) {
	r.item.mu.RLock()
	label := r.item.label
	selected := r.item.selected
	hovered := r.item.hovered
	r.item.mu.RUnlock()

	if size.Width <= 0 || size.Height <= 0 {
		return
	}

	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	if selected {
		r.bg.FillColor = boomtheme.ColorSidebarItemSelected
		r.label.Color = boomtheme.ColorLabel
	} else if hovered {
		r.bg.FillColor = boomtheme.ColorSidebarItemHover
		r.label.Color = boomtheme.ColorLabel
	} else {
		r.bg.FillColor = color.Transparent
		r.label.Color = boomtheme.ColorLabelSecondary
	}

	r.label.Text = label
	indent := float32(8 + r.item.depth*12)
	r.label.Move(fyne.NewPos(indent, (size.Height-14)/2))
	r.label.Resize(fyne.NewSize(size.Width-indent-8, 14))

	r.bg.Refresh()
	r.label.Refresh()
}
