package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultDeviceSentinel is a value users can set in device fields to request
// the system default (equivalent to leaving the field empty).
const DefaultDeviceSentinel = "DEFAULT"

const defaultConfigPath = "configs/boom.yaml"

// BPMRangePreset defines a named BPM range with min/max values.
type BPMRangePreset struct {
	Label string
	Min   float64
	Max   float64
}

// BPMRangePresets are the available presets.
var BPMRangePresets = []BPMRangePreset{
	{"Normal (78–180)", 78, 180},
	{"Wide (60–220)", 60, 220},
	{"House / Techno (115–150)", 115, 150},
	{"Drum & Bass (150–190)", 150, 190},
	{"Hip-Hop / R&B (70–115)", 70, 115},
	{"Downtempo (55–90)", 55, 90},
}

// BPMRangeLabels returns the labels for the BPM range presets.
func BPMRangeLabels() []string {
	labels := make([]string, len(BPMRangePresets))
	for i, p := range BPMRangePresets {
		labels[i] = p.Label
	}
	return labels
}

// ResolveBPMRange returns the min/max BPM for the configured preset.
func (c *Config) ResolveBPMRange() (float64, float64) {
	for _, p := range BPMRangePresets {
		if p.Label == c.BPMRange {
			return p.Min, p.Max
		}
	}
	// Default to "Normal" if not found
	return 78, 180
}

// Config holds the application configuration.
type Config struct {
	SampleRate        int      `yaml:"sample_rate"`
	BufferSize        int      `yaml:"buffer_size"`
	NumDecks          int      `yaml:"num_decks"`
	MusicDirs         []string `yaml:"music_dirs"`
	DatabasePath      string   `yaml:"database_path"`
	MIDIMappingDir    string   `yaml:"midi_mapping_dir"`
	MasterVolume      float64  `yaml:"master_volume"`
	HeadphoneVolume   float64  `yaml:"headphone_volume"`
	AudioOutputDevice     string `yaml:"audio_output_device"`      // empty = system default
	CueOutputDevice       string `yaml:"cue_output_device"`       // empty = disabled
	AutoAnalyzeOnDeckLoad bool   `yaml:"auto_analyze_on_deck_load"`
	AutoAnalyzeOnImport   bool   `yaml:"auto_analyze_on_import"`
	BPMRange              string `yaml:"bpm_range"` // "normal", "wide", or genre presets
	AutoCue               bool   `yaml:"auto_cue"`  // Seek to first audio frame on track load (fallback cue)
	Loop                  LoopSettings `yaml:"loop"`
	Jog                   JogSettings  `yaml:"jog"`
}

// JogSettings holds per-app jog wheel preferences. Sensitivities are unitless
// gains applied to each MIDI encoder tick. Defaults are tuned for a typical
// 4-channel club controller; other hardware may want different values.
type JogSettings struct {
	VinylMode          bool    `yaml:"vinyl_mode"`          // top touch enables scratching when true
	ScratchSensitivity float64 `yaml:"scratch_sensitivity"` // default ~0.4 (vinyl scratch feel)
	PitchSensitivity   float64 `yaml:"pitch_sensitivity"`   // default ~0.04 (pitch bend nudge)
}

// LoopSettings holds per-app loop preferences: quantize in/out points to the
// beat grid, auto-beat default length, bounds for halve/double, and "smart
// loop" handling near track boundaries.
type LoopSettings struct {
	Quantize        bool    `yaml:"quantize"`         // snap loop in/out to beat grid
	DefaultBeatLoop float64 `yaml:"default_beat_loop"` // beats (default 4)
	MinBeats        float64 `yaml:"min_beats"`         // default 1/32 = 0.03125
	MaxBeats        float64 `yaml:"max_beats"`         // default 32
	SmartLoop       bool    `yaml:"smart_loop"`        // clamp instead of skip near boundaries
}

// Load reads the configuration from the default config file.
func Load() (*Config, error) {
	cfg := DefaultConfig()
	return LoadFrom(defaultConfigPath, cfg)
}

// LoadFrom reads a YAML config file and merges it into the provided config.
// Missing or invalid fields are filled with defaults; if anything had to be
// repaired, the corrected config is written back to disk.
func LoadFrom(path string, cfg *Config) (*Config, error) {
	fileMissing := false
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		fileMissing = true
	} else if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	changed := cfg.Validate()
	if fileMissing || changed {
		if saveErr := cfg.SaveTo(path); saveErr != nil {
			return cfg, fmt.Errorf("persist config defaults: %w", saveErr)
		}
	}
	return cfg, nil
}

// Validate fills missing or invalid fields with sensible defaults and
// normalizes sentinel values (e.g. "DEFAULT" device names). It returns true
// when the config was mutated so callers can choose to persist the result.
func (c *Config) Validate() bool {
	defaults := DefaultConfig()
	changed := false

	if c.SampleRate <= 0 {
		c.SampleRate = defaults.SampleRate
		changed = true
	}
	if c.BufferSize <= 0 {
		c.BufferSize = defaults.BufferSize
		changed = true
	}
	if c.NumDecks <= 0 {
		c.NumDecks = defaults.NumDecks
		changed = true
	}
	if len(c.MusicDirs) == 0 {
		c.MusicDirs = defaults.MusicDirs
		changed = true
	}
	if c.DatabasePath == "" {
		c.DatabasePath = defaults.DatabasePath
		changed = true
	}
	if c.MIDIMappingDir == "" {
		c.MIDIMappingDir = defaults.MIDIMappingDir
		changed = true
	}
	if c.MasterVolume < 0 || c.MasterVolume > 1 {
		c.MasterVolume = defaults.MasterVolume
		changed = true
	}
	if c.HeadphoneVolume < 0 || c.HeadphoneVolume > 1 {
		c.HeadphoneVolume = defaults.HeadphoneVolume
		changed = true
	}
	if c.BPMRange == "" {
		c.BPMRange = defaults.BPMRange
		changed = true
	}
	if c.Loop.DefaultBeatLoop <= 0 {
		c.Loop.DefaultBeatLoop = defaults.Loop.DefaultBeatLoop
		changed = true
	}
	if c.Loop.MinBeats <= 0 {
		c.Loop.MinBeats = defaults.Loop.MinBeats
		changed = true
	}
	if c.Loop.MaxBeats <= 0 {
		c.Loop.MaxBeats = defaults.Loop.MaxBeats
		changed = true
	}
	// Detect a fresh / pre-jog-block config: both sensitivities zero means
	// the user has never persisted a jog block. Apply *all* jog defaults
	// including VinylMode in that case so we don't silently ship vinyl mode
	// off (the bool zero value) for existing installs.
	jogFresh := c.Jog.ScratchSensitivity == 0 && c.Jog.PitchSensitivity == 0
	if jogFresh {
		c.Jog = defaults.Jog
		changed = true
	} else {
		if c.Jog.ScratchSensitivity <= 0 {
			c.Jog.ScratchSensitivity = defaults.Jog.ScratchSensitivity
			changed = true
		}
		if c.Jog.PitchSensitivity <= 0 {
			c.Jog.PitchSensitivity = defaults.Jog.PitchSensitivity
			changed = true
		}
	}

	// Normalize the "DEFAULT" sentinel to an empty string so downstream code
	// only has to check one representation. Done silently — we don't flag this
	// as a change so users keep whichever spelling they wrote in the file.
	if strings.EqualFold(c.AudioOutputDevice, DefaultDeviceSentinel) {
		c.AudioOutputDevice = ""
	}
	if strings.EqualFold(c.CueOutputDevice, DefaultDeviceSentinel) {
		c.CueOutputDevice = ""
	}

	return changed
}

// Save writes the current configuration to the default config file.
func (c *Config) Save() error {
	return c.SaveTo(defaultConfigPath)
}

// SaveTo writes the current configuration to the specified path.
func (c *Config) SaveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	header := []byte("# Boom DJ — Application Configuration\n# This file is auto-generated. Edit via Settings in the app.\n\n")
	content := append(header, data...)

	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
