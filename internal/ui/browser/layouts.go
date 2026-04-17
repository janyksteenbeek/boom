package browser

import "fyne.io/fyne/v2"

// fixedWidthLayout forces its single child to a fixed pixel width while
// letting the height stretch. Used by SidebarModeFolder to pin the
// sidebar narrow inside the mini overlay.
type fixedWidthLayout struct {
	w float32
}

func (f *fixedWidthLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(fyne.NewSize(f.w, size.Height))
		o.Move(fyne.NewPos(0, 0))
	}
}

func (f *fixedWidthLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		m := o.MinSize()
		if m.Height > h {
			h = m.Height
		}
	}
	return fyne.NewSize(f.w, h)
}
