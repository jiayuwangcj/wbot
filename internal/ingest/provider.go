package ingest

import (
	"fmt"
)

// Config carries non-sensitive provider options (e.g. "path", "url") from the
// CLI to the provider constructor. Credentials never live here: providers read
// them from env vars instead (doc/PRIVACY.md).
type Config map[string]string

// Provider builds a Source by name; New receives the caller's Config.
type Provider struct {
	Name string
	New  func(Config) (Source, error)
}

var registry = map[string]Provider{}

// Register adds p to the global registry; empty names, nil New and
// duplicate names panic (registration errors are programming errors).
func Register(p Provider) {
	if p.Name == "" {
		panic("ingest: provider: empty name")
	}
	if p.New == nil {
		panic(fmt.Sprintf("ingest: provider %q: nil New", p.Name))
	}
	if _, ok := registry[p.Name]; ok {
		panic(fmt.Sprintf("ingest: provider %q: already registered", p.Name))
	}
	registry[p.Name] = p
}

// NewProvider builds a Source for a registered name; unknown names error.
func NewProvider(name string, cfg Config) (Source, error) {
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("ingest: provider %q: not registered", name)
	}
	return p.New(cfg)
}

// Built-in providers wrap the mock/file/http sources directly, so provider-
// constructed sources behave exactly like the plain structs (doc/DATA_PIPELINE.md).
func init() {
	Register(Provider{Name: "mock", New: func(Config) (Source, error) { return mockSource{}, nil }})
	Register(Provider{Name: "file", New: func(cfg Config) (Source, error) { return FileSource{Path: cfg["path"]}, nil }})
	Register(Provider{Name: "url", New: func(cfg Config) (Source, error) { return HTTPSource{URL: cfg["url"]}, nil }})
}
