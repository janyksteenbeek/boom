package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/janyksteenbeek/boom/internal/app"
)

func main() {
	setupCrashLog()

	a, err := app.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "boom: %v\n", err)
		os.Exit(1)
	}
	a.Run()
}

func setupCrashLog() {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "boom", "crashes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "boom: crash log dir: %v\n", err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("crash-%s.log", time.Now().Format("20060102-150405")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "boom: crash log open: %v\n", err)
		return
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "boom: SetCrashOutput: %v\n", err)
		f.Close()
		return
	}
	fmt.Fprintf(os.Stderr, "boom: crash log -> %s\n", path)
}
