package config

import (
	"os"
	"path/filepath"
)

const (
	DefaultSampleRate = 48000
	DefaultBufferSize = 512
	DefaultNumDecks   = 2
)

// DefaultConfig returns the default application configuration.
func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		SampleRate:        DefaultSampleRate,
		BufferSize:        DefaultBufferSize,
		NumDecks:          DefaultNumDecks,
		MusicDirs:         []string{filepath.Join(home, "Music")},
		DatabasePath:      filepath.Join(dataDir(), "boom.db"),
		MIDIMappingDir:    "configs/controllers",
		MasterVolume:      0.8,
		HeadphoneVolume:   0.8,
		AudioOutputDevice:     "", // system default
		CueOutputDevice:       "", // disabled
		AutoAnalyzeOnDeckLoad: true,
		AutoAnalyzeOnImport:   false,
		BPMRange:              "normal",
		AutoCue:               true,
		Loop: LoopSettings{
			Quantize:        true,
			DefaultBeatLoop: 4,
			MinBeats:        1.0 / 32.0,
			MaxBeats:        32,
			SmartLoop:       true,
		},
	}
}

func dataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "Boom")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".boom")
}
