package controller

import "sync"

// ActionType categorizes what kind of value an action expects.
type ActionType int

const (
	ActionTypeTrigger    ActionType = iota // Momentary press
	ActionTypeToggle                       // On/off state
	ActionTypeContinuous                   // 0.0-1.0 float
	ActionTypeRelative                     // Signed delta
)

// ActionDescriptor describes a registered action.
type ActionDescriptor struct {
	Name    string
	Type    ActionType
	HighRes bool
}

// ActionHandler is called when an input triggers an action.
type ActionHandler func(ctx ActionContext)

// ActionContext carries all information about an input event.
type ActionContext struct {
	Action  string
	Deck    int
	Pressed bool
	Value   float64 // Normalized 0.0-1.0
	Delta   float64 // Signed delta for relative
	Layer   string
}

// ActionRegistry maps action names to handlers and metadata.
type ActionRegistry struct {
	mu          sync.RWMutex
	descriptors map[string]ActionDescriptor
	handlers    map[string]ActionHandler
}

// NewActionRegistry creates a new action registry.
func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{
		descriptors: make(map[string]ActionDescriptor),
		handlers:    make(map[string]ActionHandler),
	}
}

// Register adds an action handler.
func (r *ActionRegistry) Register(name string, desc ActionDescriptor, handler ActionHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.descriptors[name] = desc
	r.handlers[name] = handler
}

// Lookup returns the handler and descriptor for an action name.
func (r *ActionRegistry) Lookup(name string) (ActionDescriptor, ActionHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	desc, ok := r.descriptors[name]
	if !ok {
		return ActionDescriptor{}, nil, false
	}
	return desc, r.handlers[name], true
}

// Actions returns all registered action names.
func (r *ActionRegistry) Actions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.descriptors))
	for name := range r.descriptors {
		names = append(names, name)
	}
	return names
}
