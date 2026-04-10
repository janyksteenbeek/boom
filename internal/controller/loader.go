package controller

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// Loader handles loading and hot-reloading controller configs.
type Loader struct {
	configDir string
	configs   map[string]*ControllerConfig
	mu        sync.RWMutex
	watcher   *fsnotify.Watcher
	onReload  func(name string, cfg *ControllerConfig)
}

// NewLoader creates a config loader for the given directory.
func NewLoader(configDir string) *Loader {
	return &Loader{
		configDir: configDir,
		configs:   make(map[string]*ControllerConfig),
	}
}

// LoadAll loads all YAML controller configs from the config directory.
func (l *Loader) LoadAll() error {
	entries, err := os.ReadDir(l.configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No configs directory is fine
		}
		return fmt.Errorf("read config dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(l.configDir, entry.Name())
		cfg, err := l.loadFile(path)
		if err != nil {
			log.Printf("skip controller config %s: %v", entry.Name(), err)
			continue
		}

		l.mu.Lock()
		l.configs[cfg.Controller.Name] = cfg
		l.mu.Unlock()

		log.Printf("loaded controller config: %s", cfg.Controller.Name)
	}

	return nil
}

// Get returns the config for a controller by name.
func (l *Loader) Get(name string) (*ControllerConfig, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	cfg, ok := l.configs[name]
	return cfg, ok
}

// All returns all loaded controller configs.
func (l *Loader) All() map[string]*ControllerConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make(map[string]*ControllerConfig, len(l.configs))
	for k, v := range l.configs {
		result[k] = v
	}
	return result
}

// OnReload sets a callback that fires when a config is reloaded.
func (l *Loader) OnReload(fn func(name string, cfg *ControllerConfig)) {
	l.onReload = fn
}

// Watch starts watching the config directory for changes.
func (l *Loader) Watch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	l.watcher = watcher

	if err := watcher.Add(l.configDir); err != nil {
		watcher.Close()
		return fmt.Errorf("watch dir: %w", err)
	}

	go l.watchLoop()
	return nil
}

// Close stops the file watcher.
func (l *Loader) Close() error {
	if l.watcher != nil {
		return l.watcher.Close()
	}
	return nil
}

func (l *Loader) watchLoop() {
	for {
		select {
		case event, ok := <-l.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				ext := filepath.Ext(event.Name)
				if ext != ".yaml" && ext != ".yml" {
					continue
				}
				cfg, err := l.loadFile(event.Name)
				if err != nil {
					log.Printf("reload error %s: %v", event.Name, err)
					continue
				}

				l.mu.Lock()
				l.configs[cfg.Controller.Name] = cfg
				l.mu.Unlock()

				log.Printf("reloaded controller config: %s", cfg.Controller.Name)
				if l.onReload != nil {
					l.onReload(cfg.Controller.Name, cfg)
				}
			}

		case err, ok := <-l.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

func (l *Loader) loadFile(path string) (*ControllerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	var cfg ControllerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	if cfg.Controller.Name == "" {
		return nil, fmt.Errorf("controller name is required")
	}

	return &cfg, nil
}
