package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultConfigPath = "configs/boom.yaml"

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
	AudioOutputDevice string   `yaml:"audio_output_device"` // empty = system default
	CueOutputDevice   string   `yaml:"cue_output_device"`   // empty = disabled
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
