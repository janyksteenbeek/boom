package plugin

import "sync"

// Registry holds all registered plugins by type.
type Registry struct {
	mu        sync.RWMutex
	effects   map[string]func() Effect
	analyzers map[string]func() Analyzer
	sources   map[string]func() LibrarySource
}

// NewRegistry creates a new plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		effects:   make(map[string]func() Effect),
		analyzers: make(map[string]func() Analyzer),
		sources:   make(map[string]func() LibrarySource),
	}
}

// RegisterEffect adds an effect factory to the registry.
func (r *Registry) RegisterEffect(name string, factory func() Effect) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.effects[name] = factory
}

// RegisterAnalyzer adds an analyzer factory to the registry.
func (r *Registry) RegisterAnalyzer(name string, factory func() Analyzer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.analyzers[name] = factory
}

// RegisterSource adds a library source factory to the registry.
func (r *Registry) RegisterSource(name string, factory func() LibrarySource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[name] = factory
}

// Effect returns a new instance of the named effect.
func (r *Registry) Effect(name string) (Effect, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.effects[name]
	if !ok {
		return nil, false
	}
	return f(), true
}

// Analyzer returns a new instance of the named analyzer.
func (r *Registry) Analyzer(name string) (Analyzer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.analyzers[name]
	if !ok {
		return nil, false
	}
	return f(), true
}

// Source returns a new instance of the named library source.
func (r *Registry) Source(name string) (LibrarySource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.sources[name]
	if !ok {
		return nil, false
	}
	return f(), true
}

// Effects returns all registered effect names.
func (r *Registry) Effects() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.effects))
	for name := range r.effects {
		names = append(names, name)
	}
	return names
}

// Analyzers returns all registered analyzer names.
func (r *Registry) Analyzers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.analyzers))
	for name := range r.analyzers {
		names = append(names, name)
	}
	return names
}

// Sources returns all registered library source names.
func (r *Registry) Sources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.sources))
	for name := range r.sources {
		names = append(names, name)
	}
	return names
}
