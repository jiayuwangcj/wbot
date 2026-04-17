package agent

// Facade is what an autonomous agent exposes to the control plane (HTTPS later).
type Facade interface {
	Identity() string
}

// Stub is a no-op identity holder for tests and early wiring.
type Stub struct {
	ID string
}

// Identity implements Facade.
func (s Stub) Identity() string {
	if s.ID == "" {
		return "stub-agent"
	}
	return s.ID
}
