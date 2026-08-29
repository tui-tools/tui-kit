package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeConfig(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadFromDefaults(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.toml")
	cfg, err := LoadFrom(missing)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Backend != BackendAuto || cfg.Sudo != "sudo -n" || cfg.Theme != "" {
		t.Errorf("cfg = %+v, want the defaults", cfg)
	}
	if len(cfg.Sources) != 0 {
		t.Errorf("Sources = %v, want empty", cfg.Sources)
	}
}

func TestLoadFromOverrides(t *testing.T) {
	system := writeConfig(t, "system.toml", `# machine-wide
backend = "ufw"
sudo = "doas"
theme = "/etc/theme/colors.toml"
`)
	user := writeConfig(t, "user.toml", `backend = "firewalld"  # user wins
`)

	cfg, err := LoadFrom(system, user)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Backend != BackendFirewalld {
		t.Errorf("Backend = %q, want firewalld", cfg.Backend)
	}
	// Keys absent from the user file keep the system value.
	if cfg.Sudo != "doas" {
		t.Errorf("Sudo = %q, want doas", cfg.Sudo)
	}
	if cfg.Theme != "/etc/theme/colors.toml" {
		t.Errorf("Theme = %q", cfg.Theme)
	}
	if len(cfg.Sources) != 2 {
		t.Errorf("Sources = %v, want both files", cfg.Sources)
	}
}

func TestSudoPrefix(t *testing.T) {
	tests := []struct {
		sudo string
		want []string
	}{
		{sudo: "sudo -n", want: []string{"sudo", "-n"}},
		{sudo: "doas", want: []string{"doas"}},
		{sudo: "", want: nil},
		{sudo: "   ", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.sudo, func(t *testing.T) {
			if got := (Config{Sudo: tc.sudo}).SudoPrefix(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SudoPrefix() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadFromInvalid(t *testing.T) {
	bad := writeConfig(t, "bad.toml", "backend ufw\n")
	if _, err := LoadFrom(bad); err == nil {
		t.Error("expected a parse error")
	}

	unknown := writeConfig(t, "unknown.toml", `backend = "iptables"`)
	if _, err := LoadFrom(unknown); err == nil {
		t.Error("expected a validation error for an unknown backend")
	}
}

func TestLoadFromIgnoresUnknownKeys(t *testing.T) {
	path := writeConfig(t, "extra.toml", `backend = "ufw"
future_option = "whatever"
`)
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Backend != BackendUFW {
		t.Errorf("Backend = %q, want ufw", cfg.Backend)
	}
}
