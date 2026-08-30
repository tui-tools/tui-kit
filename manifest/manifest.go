// Package manifest reads the tool.json every tui-tools repository carries at
// its root, from inside the running binary.
//
// The manifest is already the single source the family website reads. Loading
// the same file at runtime makes it the single source for the *tool* too: the
// backend list, the minimum version and the version-ranged notes are declared
// once, rendered into the README by tui-kit/tools/render-compat.py, published
// by the site, and shown in the header by the binary — with no copy to keep in
// sync.
//
// A tool embeds its own manifest and hands the bytes to Load:
//
//	//go:embed tool.json
//	var manifestJSON []byte
//
//	m, err := manifest.Load(manifestJSON)
//	backend, ok := m.Backend("ufw")
//
// Only the fields a running tool needs are decoded. Everything else in the
// schema (screenshots, install channels, security) is the website's business
// and is ignored here rather than duplicated.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tui-tools/tui-kit/compat"
)

// Manifest is the subset of tool.json a binary reads at runtime.
type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Name          string `json:"name"`
	Binary        string `json:"binary"`
	Tagline       string `json:"tagline"`
	Category      string `json:"category"`
	Homepage      string `json:"homepage"`
	Repo          string `json:"repo"`
	// Backends is the compatibility block, optional: a tool that drives no
	// external binary declares none.
	Backends []compat.Backend `json:"backends,omitempty"`
}

// Load parses an embedded manifest.
func Load(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	if m.Name == "" {
		return Manifest{}, fmt.Errorf("manifest: no name")
	}
	return m, nil
}

// LoadFile reads a manifest from disk. Tools embed theirs; this is for the
// scripts and tests that work on a checkout.
func LoadFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reading a caller-named file is the whole function; callers are the family's own scripts and tests
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	return Load(data)
}

// Backend returns the declared backend with that name.
func (m Manifest) Backend(name string) (compat.Backend, bool) {
	for _, b := range m.Backends {
		if b.Name == name {
			return b, true
		}
	}
	return compat.Backend{}, false
}

// FirstBackend returns the first declared backend, which is the one a tool
// with a single backend wants without naming it.
func (m Manifest) FirstBackend() (compat.Backend, bool) {
	if len(m.Backends) == 0 {
		return compat.Backend{}, false
	}
	return m.Backends[0], true
}
