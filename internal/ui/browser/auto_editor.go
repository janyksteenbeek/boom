package browser

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/janyksteenbeek/boom/pkg/model"
)

// autoFieldOptions is the user-facing list of rule fields. Keep in sync
// with library.smartFields whitelist.
var autoFieldOptions = []string{"bpm", "genre", "artist", "title", "album", "key", "gain"}

// autoOpOptions is the set of operators offered per rule. The service
// ignores unknown ops so pruning is safe.
var autoOpOptions = []string{"eq", "neq", "contains", "gt", "lt", "between"}

// autoRuleRow captures the widgets for one editable rule row so we can pull
// their current values back when the user hits Save.
type autoRuleRow struct {
	field  *widget.Select
	op     *widget.Select
	value  *widget.Entry
	value2 *widget.Entry // only used for "between"
	row    *fyne.Container
}

func (r *autoRuleRow) toRule() (model.SmartRule, bool) {
	field := r.field.Selected
	op := r.op.Selected
	if field == "" || op == "" {
		return model.SmartRule{}, false
	}
	if op == "between" {
		lo, err1 := strconv.ParseFloat(strings.TrimSpace(r.value.Text), 64)
		hi, err2 := strconv.ParseFloat(strings.TrimSpace(r.value2.Text), 64)
		if err1 != nil || err2 != nil {
			return model.SmartRule{}, false
		}
		return model.SmartRule{Field: field, Op: op, Value: []interface{}{lo, hi}}, true
	}
	text := strings.TrimSpace(r.value.Text)
	if text == "" {
		return model.SmartRule{}, false
	}
	// Numeric fields get parsed; strings stay as-is.
	if field == "bpm" || field == "gain" {
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return model.SmartRule{}, false
		}
		return model.SmartRule{Field: field, Op: op, Value: v}, true
	}
	return model.SmartRule{Field: field, Op: op, Value: text}, true
}

// showAutoPlaylistEditor opens a modal to create a new auto playlist.
// Submitting calls onSave with the final rules + name; the caller is
// responsible for persisting via PlaylistService.
func showAutoPlaylistEditor(win fyne.Window, parentID string, onSave func(parentID, name string, rules model.SmartRules)) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Auto playlist name")

	match := widget.NewRadioGroup([]string{"all", "any"}, nil)
	match.Horizontal = true
	match.SetSelected("all")

	rulesBox := container.NewVBox()
	rows := []*autoRuleRow{}

	addRow := func() {
		row := newAutoRuleRow()
		rows = append(rows, row)
		rulesBox.Add(row.row)
		rulesBox.Refresh()
	}

	addBtn := widget.NewButton("+ Add rule", addRow)

	// Start with a single empty rule.
	addRow()

	content := container.NewVBox(
		widget.NewLabelWithStyle("Name", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nameEntry,
		widget.NewLabelWithStyle("Match", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		match,
		widget.NewLabelWithStyle("Rules", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		rulesBox,
		addBtn,
	)

	d := dialog.NewCustomConfirm("New Auto Playlist", "Create", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(nameEntry.Text)
		if name == "" {
			dialog.ShowInformation("Invalid", "Name is required", win)
			return
		}
		var out []model.SmartRule
		for _, r := range rows {
			if rule, ok := r.toRule(); ok {
				out = append(out, rule)
			}
		}
		if len(out) == 0 {
			dialog.ShowInformation("Invalid", "Add at least one rule", win)
			return
		}
		onSave(parentID, name, model.SmartRules{Match: match.Selected, Rules: out})
	}, win)
	d.Resize(fyne.NewSize(440, 420))
	d.Show()
}

func newAutoRuleRow() *autoRuleRow {
	r := &autoRuleRow{}
	r.field = widget.NewSelect(autoFieldOptions, nil)
	r.op = widget.NewSelect(autoOpOptions, nil)
	r.value = widget.NewEntry()
	r.value.SetPlaceHolder("value")
	r.value2 = widget.NewEntry()
	r.value2.SetPlaceHolder("to")
	r.value2.Hide()

	r.op.OnChanged = func(op string) {
		if op == "between" {
			r.value.SetPlaceHolder("from")
			r.value2.Show()
		} else {
			r.value.SetPlaceHolder("value")
			r.value2.Hide()
		}
	}

	r.row = container.NewGridWithColumns(4, r.field, r.op, r.value, r.value2)
	return r
}
