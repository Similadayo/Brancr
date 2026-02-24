package executor

import (
	"github.com/similadayo/brancr/internal/rules"
)

// Registry holds all executable actions mapped by their canonical type.
type Registry struct {
	actions map[rules.ActionType]Action
}

// NewRegistry creates a new registry with provided actions.
// (In a real app, we'd register them via init or constructor injection).
func NewRegistry() *Registry {
	return &Registry{
		actions: make(map[rules.ActionType]Action),
	}
}

// Register adds an action to the registry.
func (r *Registry) Register(t rules.ActionType, a Action) {
	r.actions[t] = a
}

// Get retrieves an action by type.
func (r *Registry) Get(action rules.ActionType) (Action, bool) {
	a, ok := r.actions[action]
	return a, ok
}
