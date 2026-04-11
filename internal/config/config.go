package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultConfigPath = "configs/boom.yaml"

// BPMRangePreset defines a named BPM range with min/max values.
type BPMRangePreset struct {
	Label string
	Min   float64
	Max   float64
}

// BPMRangePresets are the available presets, matching Rekordbox-style options.
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
}

// Load reads the configuration from the default config file.
func Load() (*Config, error) {
	cfg := DefaultConfig()
	return LoadFrom(defaultConfigPath, cfg)
}

// LoadFrom reads a YAML config file and merges it into the provided config.
func LoadFrom(path string, cfg *Config) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
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
