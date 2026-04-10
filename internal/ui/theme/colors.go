package theme

import "image/color"

// Apple HIG Dark Mode system colors.
// Reference: developer.apple.com/design/human-interface-guidelines/color
var (
	// Backgrounds — Apple dark mode layered system
	ColorBackground          = color.RGBA{R: 0, G: 0, B: 0, A: 255}       // systemBackground (pure black)
	ColorBackgroundSecondary = color.RGBA{R: 28, G: 28, B: 30, A: 255}    // secondarySystemBackground
	ColorBackgroundTertiary  = color.RGBA{R: 44, G: 44, B: 46, A: 255}    // tertiarySystemBackground
	ColorBackgroundElevated  = color.RGBA{R: 28, G: 28, B: 30, A: 255}    // elevated surface
	ColorGroupedBg           = color.RGBA{R: 28, G: 28, B: 30, A: 255}    // grouped background
	ColorFill                = color.RGBA{R: 120, G: 120, B: 128, A: 92}  // systemFill
	ColorFillSecondary       = color.RGBA{R: 120, G: 120, B: 128, A: 41}  // secondaryFill
	ColorSeparator           = color.RGBA{R: 84, G: 84, B: 88, A: 153}   // separator

	// Text — pure white hierarchy for clean dark mode
	ColorLabel               = color.RGBA{R: 255, G: 255, B: 255, A: 255} // primary text
	ColorLabelSecondary      = color.RGBA{R: 255, G: 255, B: 255, A: 178} // secondary text (70%)
	ColorLabelTertiary       = color.RGBA{R: 255, G: 255, B: 255, A: 128} // tertiary text (50%)
	ColorLabelQuaternary     = color.RGBA{R: 255, G: 255, B: 255, A: 76}  // quaternary text (30%)

	// Apple system accent colors (dark mode variants)
	ColorBlue   = color.RGBA{R: 10, G: 132, B: 255, A: 255}   // systemBlue
	ColorGreen  = color.RGBA{R: 48, G: 209, B: 88, A: 255}    // systemGreen
	ColorOrange = color.RGBA{R: 255, G: 159, B: 10, A: 255}   // systemOrange
	ColorRed    = color.RGBA{R: 255, G: 69, B: 58, A: 255}    // systemRed
	ColorYellow = color.RGBA{R: 255, G: 214, B: 10, A: 255}   // systemYellow
	ColorPurple = color.RGBA{R: 191, G: 90, B: 242, A: 255}   // systemPurple
	ColorCyan   = color.RGBA{R: 100, G: 210, B: 255, A: 255}  // systemCyan
	ColorTeal   = color.RGBA{R: 106, G: 196, B: 220, A: 255}  // systemTeal
	ColorMint   = color.RGBA{R: 99, G: 230, B: 226, A: 255}   // systemMint
)

// Deck identity — using Apple system colors.
// Deck 1 = Cyan/Blue (cool), Deck 2 = Orange (warm).
var (
	ColorDeck1       = ColorCyan
	ColorDeck1Dim    = color.RGBA{R: 100, G: 210, B: 255, A: 70}
	ColorDeck1Bg     = color.RGBA{R: 100, G: 210, B: 255, A: 15}
	ColorDeck2       = ColorOrange
	ColorDeck2Dim    = color.RGBA{R: 255, G: 159, B: 10, A: 70}
	ColorDeck2Bg     = color.RGBA{R: 255, G: 159, B: 10, A: 15}
)

// Waveform.
var (
	ColorWaveformBg       = color.RGBA{R: 15, G: 15, B: 18, A: 255}
	ColorWaveformGrid     = color.RGBA{R: 40, G: 40, B: 45, A: 255}
	ColorWaveformGridMajor = color.RGBA{R: 55, G: 55, B: 60, A: 255}
	ColorPlayhead         = color.RGBA{R: 255, G: 255, B: 255, A: 230}
)

// Browser — Apple HIG layered surfaces for the library browser.
var (
	ColorSidebarBg           = color.RGBA{R: 21, G: 21, B: 23, A: 255}    // Darker than main background
	ColorSidebarItemHover    = color.RGBA{R: 255, G: 255, B: 255, A: 20}  // Subtle hover overlay
	ColorSidebarItemSelected = color.RGBA{R: 255, G: 255, B: 255, A: 30}  // White selection (subtle)
	ColorRowAlternate        = color.RGBA{R: 255, G: 255, B: 255, A: 8}   // Subtle zebra striping
	ColorRowHover            = color.RGBA{R: 255, G: 255, B: 255, A: 18}  // Row hover highlight
	ColorHeaderBg            = color.RGBA{R: 32, G: 32, B: 34, A: 255}    // Column header background
	ColorToolbarBg           = color.RGBA{R: 24, G: 24, B: 26, A: 255}    // Toolbar background
	ColorSearchBg            = color.RGBA{R: 56, G: 56, B: 58, A: 255}    // Search field background
)

// Transport.
var (
	ColorPlayActive = ColorGreen
	ColorCueActive  = ColorOrange
	ColorSyncActive = ColorYellow
)

// DeckColor returns the primary color for a deck.
func DeckColor(deckID int) color.Color {
	if deckID == 2 {
		return ColorDeck2
	}
	return ColorDeck1
}

// DeckColorDim returns the dimmed color for a deck.
func DeckColorDim(deckID int) color.Color {
	if deckID == 2 {
		return ColorDeck2Dim
	}
	return ColorDeck1Dim
}

// DeckColorBg returns the subtle background tint for a deck.
func DeckColorBg(deckID int) color.Color {
	if deckID == 2 {
		return ColorDeck2Bg
	}
	return ColorDeck1Bg
}
