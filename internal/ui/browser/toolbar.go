package browser

import (
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/ui/components"
	boomtheme "github.com/janyksteenbeek/boom/internal/ui/theme"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// BrowserToolbar is the toolbar above the track list with search, track count, and deck selector.
type BrowserToolbar struct {
	widget.BaseWidget

	mu           sync.RWMutex
	bus          *event.Bus
	search       *widget.Entry
	analyzeBtn   *widget.Button
	progressText *canvas.Text
	trackCount   *canvas.Text
	deckSelect   *components.SegmentedControl
	content      *fyne.Container
	analyzing    bool
	getUnanalyzed func() []model.Track
}

func NewBrowserToolbar(bus *event.Bus, onDeckChanged func(deck int), getUnanalyzed func() []model.Track) *BrowserToolbar {
	t := &BrowserToolbar{bus: bus, getUnanalyzed: getUnanalyzed}

	// Search field
	t.search = widget.NewEntry()
	t.search.SetPlaceHolder("Search library...")
	t.search.OnChanged = func(query string) {
		bus.PublishAsync(event.Event{
			Topic: event.TopicLibrary, Action: event.ActionSearchQuery, Payload: query,
		})
	}

	// Analyze button
	t.analyzeBtn = widget.NewButton("Analyze", func() {
		t.mu.RLock()
		isAnalyzing := t.analyzing
		t.mu.RUnlock()

		if isAnalyzing {
			bus.PublishAsync(event.Event{
				Topic: event.TopicAnalysis, Action: event.ActionAnalyzeCancel,
			})
			return
		}

		if t.getUnanalyzed != nil {
			tracks := t.getUnanalyzed()
			if len(tracks) > 0 {
				bus.PublishAsync(event.Event{
					Topic:   event.TopicAnalysis,
					Action:  event.ActionAnalyzeRequest,
					Payload: tracks,
				})
			}
		}
	})

	// Progress text
	t.progressText = canvas.NewText("", boomtheme.ColorLabelTertiary)
	t.progressText.TextSize = 11
	t.progressText.Alignment = fyne.TextAlignCenter

	// Track count
	t.trackCount = canvas.NewText("0 tracks", boomtheme.ColorLabelTertiary)
	t.trackCount.TextSize = 11
	t.trackCount.Alignment = fyne.TextAlignCenter

	// Deck selector
	t.deckSelect = components.NewSegmentedControl(
		[]string{"Deck 1", "Deck 2"},
		[]color.Color{boomtheme.ColorDeck1, boomtheme.ColorDeck2},
		func(index int) {
			if onDeckChanged != nil {
				onDeckChanged(index + 1)
			}
		},
	)

	// Load to label
	loadLabel := canvas.NewText("Load to:", boomtheme.ColorLabelTertiary)
	loadLabel.TextSize = 11

	// Background
	bg := canvas.NewRectangle(boomtheme.ColorToolbarBg)
	sep := canvas.NewRectangle(boomtheme.ColorSeparator)
	sep.SetMinSize(fyne.NewSize(0, 0.5))

	// Search wrapper with fixed width
	searchWrap := container.New(layout.NewGridWrapLayout(fyne.NewSize(220, 28)), t.search)
	analyzeBtnWrap := container.New(layout.NewGridWrapLayout(fyne.NewSize(80, 28)), t.analyzeBtn)

	row := container.NewHBox(
		searchWrap,
		analyzeBtnWrap,
		layout.NewSpacer(),
		t.progressText,
		t.trackCount,
		layout.NewSpacer(),
		loadLabel,
		container.New(layout.NewGridWrapLayout(fyne.NewSize(4, 0))), // small gap
		t.deckSelect,
	)

	padded := container.NewPadded(row)

	t.content = container.NewStack(
		bg,
		container.NewBorder(nil, sep, nil, nil, padded),
	)

	t.subscribeEvents()
	t.ExtendBaseWidget(t)
	return t
}

func (t *BrowserToolbar) subscribeEvents() {
	t.bus.Subscribe(event.TopicAnalysis, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionAnalyzeProgress:
			p, ok := ev.Payload.(*event.AnalysisProgress)
			if !ok {
				return nil
			}
			t.mu.Lock()
			t.analyzing = true
			t.mu.Unlock()
			fyne.Do(func() {
				t.progressText.Text = fmt.Sprintf("Analyzing %d/%d...", p.Current, p.Total)
				t.progressText.Refresh()
				t.analyzeBtn.SetText("Cancel")
			})
		case event.ActionAnalyzeBatchDone:
			t.mu.Lock()
			t.analyzing = false
			t.mu.Unlock()
			fyne.Do(func() {
				t.progressText.Text = ""
				t.progressText.Refresh()
				t.analyzeBtn.SetText("Analyze")
			})
		}
		return nil
	})
}

func (t *BrowserToolbar) UpdateTrackCount(count int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	label := fmt.Sprintf("%d tracks", count)
	if count == 1 {
		label = "1 track"
	}
	fyne.Do(func() {
		t.trackCount.Text = label
		t.trackCount.Refresh()
	})
}

func (t *BrowserToolbar) MinSize() fyne.Size {
	return fyne.NewSize(200, 40)
}

func (t *BrowserToolbar) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}
