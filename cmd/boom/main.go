package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/janyksteenbeek/boom/internal/app"
	"github.com/janyksteenbeek/boom/internal/ui"
)

func main() {
	cleanup := setupCrashLog()
	defer cleanup()

	opts := parseFlags()

	a, err := app.NewWithOptions(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "boom: %v\n", err)
		os.Exit(1)
	}
	a.Run()
}

// parseFlags reads the CLI flags and translates them into ui.Options.
// --mini is a shorthand for --layout=mini. --force-size=WxH lets a dev
// simulate the Pi 5" screen on a larger monitor without fullscreen.
func parseFlags() ui.Options {
	var (
		layoutFlag = flag.String("layout", "", "UI layout: desktop | mini")
		miniFlag   = flag.Bool("mini", false, "shortcut for --layout=mini")
		fullscreen = flag.Bool("fullscreen", false, "start fullscreen (Pi kiosk)")
		kiosk      = flag.Bool("kiosk", false, "kiosk mode — hide settings gear, disable exit")
		forceSize  = flag.String("force-size", "", "WxH override window size (e.g. 800x480)")
	)
	flag.Parse()

	opts := ui.Options{
		Layout:     *layoutFlag,
		Fullscreen: *fullscreen,
		Kiosk:      *kiosk,
	}
	if *miniFlag && opts.Layout == "" {
		opts.Layout = "mini"
	}
	if *forceSize != "" {
		w, h, ok := parseWxH(*forceSize)
		if !ok {
			fmt.Fprintf(os.Stderr, "boom: invalid --force-size %q (want WxH, e.g. 800x480)\n", *forceSize)
			os.Exit(2)
		}
		opts.ForceWidth = w
		opts.ForceHeight = h
	}
	return opts
}

// parseWxH parses "800x480" / "1024X600" into (w, h). Returns ok=false on
// malformed input.
func parseWxH(s string) (int, int, bool) {
	sep := strings.IndexAny(s, "xX")
	if sep <= 0 || sep == len(s)-1 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(s[:sep]))
	h, err2 := strconv.Atoi(strings.TrimSpace(s[sep+1:]))
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func setupCrashLog() func() {
	noop := func() {}

	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "boom", "crashes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "boom: crash log dir: %v\n", err)
		return noop
	}
	path := filepath.Join(dir, fmt.Sprintf("crash-%s.log", time.Now().Format("20060102-150405")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "boom: crash log open: %v\n", err)
		return noop
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "boom: SetCrashOutput: %v\n", err)
		f.Close()
		os.Remove(path)
		return noop
	}

	return func() {
		debug.SetCrashOutput(nil, debug.CrashOptions{})
		f.Close()
		if st, err := os.Stat(path); err == nil && st.Size() == 0 {
			os.Remove(path)
		}
	}
}
