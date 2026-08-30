package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// sample is a manifest cut down to the fields a running binary reads, plus a
// block it must ignore, because a real tool.json carries much more.
const sample = `{
  "schemaVersion": 1,
  "name": "tui-firewall",
  "binary": "tui-firewall",
  "tagline": "A terminal UI for the machine's firewall",
  "category": "firewall",
  "security": { "preview_before_run": true },
  "backends": [
    {
      "name": "ufw",
      "binary": "ufw",
      "versionCommand": ["ufw", "--version"],
      "minimum": "0.36",
      "tested": ["0.36.1", "0.36.2"],
      "notes": [{ "range": "<0.36", "impact": "no numbered status" }],
      "features": [{ "name": "app-profiles", "since": "0.36" }]
    },
    { "name": "firewalld", "binary": "firewall-cmd",
      "versionCommand": ["firewall-cmd", "--version"] }
  ]
}`

func TestLoad(t *testing.T) {
	m, err := Load([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "tui-firewall" || m.Binary != "tui-firewall" {
		t.Errorf("identity = %q / %q", m.Name, m.Binary)
	}
	if len(m.Backends) != 2 {
		t.Fatalf("backends = %d, want 2", len(m.Backends))
	}

	ufw, ok := m.Backend("ufw")
	if !ok {
		t.Fatal("no ufw backend")
	}
	if ufw.Minimum != "0.36" || len(ufw.Tested) != 2 {
		t.Errorf("ufw = %+v", ufw)
	}
	if len(ufw.Notes) != 1 || ufw.Notes[0].Range != "<0.36" {
		t.Errorf("ufw notes = %+v", ufw.Notes)
	}
	if len(ufw.Features) != 1 || ufw.Features[0].Since != "0.36" {
		t.Errorf("ufw features = %+v", ufw.Features)
	}

	first, ok := m.FirstBackend()
	if !ok || first.Name != "ufw" {
		t.Errorf("FirstBackend = %+v, %v", first, ok)
	}
	if _, ok := m.Backend("nftables"); ok {
		t.Error("an undeclared backend must not be found")
	}
}

func TestLoadRejectsRubbish(t *testing.T) {
	if _, err := Load([]byte("not json")); err == nil {
		t.Error("invalid JSON must fail")
	}
	if _, err := Load([]byte(`{"binary": "x"}`)); err == nil {
		t.Error("a manifest with no name must fail")
	}
}

// A manifest with no backends block is valid: not every tool drives an
// external binary, and the field is optional by design.
func TestLoadWithoutBackends(t *testing.T) {
	m, err := Load([]byte(`{"name": "tui-template", "binary": "tui-template"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.FirstBackend(); ok {
		t.Error("no backends means FirstBackend reports none")
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.json")
	if err := os.WriteFile(path, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "tui-firewall" {
		t.Errorf("name = %q", m.Name)
	}
	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("a missing file must fail")
	}
}
