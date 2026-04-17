package ui

// Options configures startup-time UI behavior. Passed from the CLI layer
// down through app.New to NewWindow. All zero values produce the default
// desktop experience, so callers can leave fields blank to opt out.
type Options struct {
	// Layout selects which layout.Layout implementation to instantiate.
	// "", "desktop" → desktop; "mini" → mini-mode (Pi / small screen).
	Layout string

	// Fullscreen makes the window borderless fullscreen on startup.
	Fullscreen bool

	// Kiosk hides the settings gear and disables any controls that
	// could exit or reconfigure the app. Intended for unattended Pi
	// deployments.
	Kiosk bool

	// ForceWidth / ForceHeight override the default window size.
	// Useful for simulating a Pi screen on a dev machine via
	// --force-size=800x480.
	ForceWidth  int
	ForceHeight int
}
