package browser

import (
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
)

const sidebarWidth float32 = 180

// Sidebar is a macOS-style source list for library navigation.
type Sidebar struct {
	widget.BaseWidget

	mu         sync.RWMutex
	items      []*SidebarItem
	selectedID string
	onSelect   func(id string)
	content    *fyne.Container
	scroll     *container.Scroll
	bg         *canvas.Rectangle
}

func NewSidebar(onSelect func(id string)) *Sidebar {
	s := &Sidebar{
		onSelect:   onSelect,
		selectedID: "all",
	}

	s.bg = canvas.NewRectangle(boomtheme.ColorSidebarBg)
	s.content = container.NewVBox()
	s.scroll = container.NewVScroll(s.content)

	s.buildStaticItems()
	s.ExtendBaseWidget(s)
	return s
}

func (s *Sidebar) buildStaticItems() {
	s.content.Objects = nil

	// Top padding
	s.content.Add(spacerRect(8))

	// Library section
	s.content.Add(sectionHeader("LIBRARY"))
	allItem := s.newItem("All Tracks", "all")
	recentItem := s.newItem("Recently Added", "recent")
	unanalyzedItem := s.newItem("Unanalyzed", "unanalyzed")
	s.content.Add(allItem)
	s.content.Add(recentItem)
	s.content.Add(unanalyzedItem)
	s.items = []*SidebarItem{allItem, recentItem, unanalyzedItem}

	// BPM Range section
	s.content.Add(spacerRect(12))
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
		item := s.newItem(r.label, r.id)
		s.content.Add(item)
		s.items = append(s.items, item)
	}

	// Mark initial selection
	s.updateSelection("all")
}

// SetGenres updates the genre section of the sidebar.
func (s *Sidebar) SetGenres(genres []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Rebuild content: keep static items, add genres
	s.content.Objects = nil
	s.items = nil

	// Top padding
	s.content.Add(spacerRect(8))

	// Library section
	s.content.Add(sectionHeader("LIBRARY"))
	allItem := s.newItem("All Tracks", "all")
	recentItem := s.newItem("Recently Added", "recent")
	unanalyzedItem := s.newItem("Unanalyzed", "unanalyzed")
	s.content.Add(allItem)
	s.content.Add(recentItem)
	s.content.Add(unanalyzedItem)
	s.items = append(s.items, allItem, recentItem, unanalyzedItem)

	// Genre section
	if len(genres) > 0 {
		s.content.Add(spacerRect(12))
		s.content.Add(sectionHeader("GENRE"))
		for _, g := range genres {
			item := s.newItem(g, "genre:"+g)
			s.content.Add(item)
			s.items = append(s.items, item)
		}
	}

	// BPM Range section
	s.content.Add(spacerRect(12))
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
		item := s.newItem(r.label, r.id)
		s.content.Add(item)
		s.items = append(s.items, item)
	}

	// Bottom padding
	s.content.Add(spacerRect(8))

	s.updateSelection(s.selectedID)

	fyne.Do(func() {
		s.content.Refresh()
	})
}

func (s *Sidebar) newItem(label, id string) *SidebarItem {
	item := NewSidebarItem(label, id, func(itemID string) {
		s.mu.Lock()
		s.selectedID = itemID
		s.mu.Unlock()
		s.updateSelection(itemID)
		if s.onSelect != nil {
			s.onSelect(itemID)
		}
	})
	if id == s.selectedID {
		item.SetSelected(true)
	}
	return item
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
	label := canvas.NewText(text, boomtheme.ColorLabelTertiary)
	label.TextSize = 10
	label.TextStyle = fyne.TextStyle{Bold: true}
	padded := container.NewHBox(
		canvas.NewRectangle(color.Transparent), // 8px left inset
		label,
		layout.NewSpacer(),
	)
	return padded
}

func spacerRect(height float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(0, height))
	return r
}

// SidebarItem is a single clickable item in the sidebar.
type SidebarItem struct {
	widget.BaseWidget

	mu       sync.RWMutex
	label    string
	id       string
	selected bool
	hovered  bool
	onTap    func(id string)
}

var _ desktop.Hoverable = (*SidebarItem)(nil)
var _ fyne.Tappable = (*SidebarItem)(nil)

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
	return fyne.NewSize(sidebarWidth-16, 28)
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
	r.label.Move(fyne.NewPos(8, (size.Height-14)/2))
	r.label.Resize(fyne.NewSize(size.Width-16, 14))

	r.bg.Refresh()
	r.label.Refresh()
}
