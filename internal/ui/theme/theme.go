package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
	fynetheme "fyne.io/fyne/v2/theme"
)

// BoomTheme provides an Apple HIG dark mode theme for the DJ interface.
type BoomTheme struct{}

// New returns a new Boom theme instance.
func New() fyne.Theme {
	return &BoomTheme{}
}

func (t *BoomTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case fynetheme.ColorNameBackground:
		return ColorBackground
	case fynetheme.ColorNameButton:
		return ColorBackgroundTertiary
	case fynetheme.ColorNameDisabledButton:
		return ColorBackgroundSecondary
	case fynetheme.ColorNamePrimary:
		return ColorBlue
	case fynetheme.ColorNameForeground:
		return ColorLabel
	case fynetheme.ColorNamePlaceHolder:
		return ColorLabelTertiary
	case fynetheme.ColorNameHover:
		return ColorFill
	case fynetheme.ColorNameInputBackground:
		return ColorBackgroundTertiary
	case fynetheme.ColorNameInputBorder:
		return ColorSeparator
	case fynetheme.ColorNameSeparator:
		return ColorSeparator
	case fynetheme.ColorNameDisabled:
		return ColorLabelQuaternary
	case fynetheme.ColorNameScrollBar:
		return ColorFillSecondary
	case fynetheme.ColorNameShadow:
		return color.Transparent // Apple HIG: no drop shadows on flat UI
	case fynetheme.ColorNameHeaderBackground:
		return ColorHeaderBg
	case fynetheme.ColorNameSelection:
		return color.RGBA{R: 255, G: 255, B: 255, A: 30} // Subtle white selection
	case fynetheme.ColorNameFocus:
		return ColorBlue
	case fynetheme.ColorNamePressed:
		return ColorFillSecondary
	case fynetheme.ColorNameMenuBackground:
		return ColorBackgroundSecondary
	case fynetheme.ColorNameOverlayBackground:
		return ColorBackgroundSecondary
	default:
		return fynetheme.DefaultTheme().Color(name, fynetheme.VariantDark)
	}
}

func (t *BoomTheme) Font(style fyne.TextStyle) fyne.Resource {
	return fynetheme.DefaultTheme().Font(style)
}

func (t *BoomTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return fynetheme.DefaultTheme().Icon(name)
}

func (t *BoomTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case fynetheme.SizeNamePadding:
		return 6 // Apple uses compact padding in pro apps
	case fynetheme.SizeNameInnerPadding:
		return 8
	case fynetheme.SizeNameText:
		return 13 // SF Pro default
	case fynetheme.SizeNameHeadingText:
		return 17
	case fynetheme.SizeNameSubHeadingText:
		return 11
	case fynetheme.SizeNameCaptionText:
		return 10
	case fynetheme.SizeNameSeparatorThickness:
		return 0.5 // Apple's hairline separators
	case fynetheme.SizeNameInputBorder:
		return 1 // Thin input border, Apple style
	case fynetheme.SizeNameInputRadius:
		return 8 // Apple's rounded input fields
	case fynetheme.SizeNameSelectionRadius:
		return 6 // Apple's selection highlight radius
	case fynetheme.SizeNameScrollBar:
		return 6
	case fynetheme.SizeNameScrollBarSmall:
		return 3
	case fynetheme.SizeNameLineSpacing:
		return 4
	default:
		return fynetheme.DefaultTheme().Size(name)
	}
}
