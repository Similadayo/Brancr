package foundation

// Guard interface definition (Phase 0)
type Guard interface {
	// Checks if an action is permissible
	Check() bool
}
