package master

import "sync"

// Facade is the control-plane side that tracks agents (HTTPS polling later).
type Facade interface {
	Register(agentID string) bool
	Agents() []string
}

// Memory is an in-memory registry for tests and local dev.
type Memory struct {
	mu     sync.Mutex
	agents map[string]struct{}
}

// NewMemory returns an empty registry.
func NewMemory() *Memory {
	return &Memory{agents: make(map[string]struct{})}
}

// Register records agentID if non-empty; reports whether it was newly added.
func (m *Memory) Register(agentID string) bool {
	if agentID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.agents == nil {
		m.agents = make(map[string]struct{})
	}
	if _, ok := m.agents[agentID]; ok {
		return false
	}
	m.agents[agentID] = struct{}{}
	return true
}

// Agents returns registered IDs in arbitrary order.
func (m *Memory) Agents() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.agents))
	for id := range m.agents {
		out = append(out, id)
	}
	return out
}
