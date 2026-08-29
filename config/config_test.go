package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// defaults is the key set a tool would declare; only these keys are read from
// the environment.
var defaults = map[string]string{
	"backend": "auto",
	KeySudo:   "sudo -n",
	KeyTheme:  "",
}

func writeConfig(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func opts() Options {
	return Options{Tool: "tui-firewall", Defaults: defaults, EnvPrefix: "-"}
}

func TestDerivedPaths(t *testing.T) {
	if got := SystemPathFor("tui-firewall"); got != "/etc/tui-firewall/config.toml" {
		t.Errorf("SystemPathFor = %q", got)
	}
	if got := UserPathFor("tui-systemd"); got != "~/.config/tui-systemd/config.toml" {
		t.Errorf("UserPathFor = %q", got)
	}
	if got := EnvPrefixFor("tui-firewall"); got != "TUI_FIREWALL_" {
		t.Errorf("EnvPrefixFor = %q", got)
	}
}

func TestLoadFromDefaults(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.toml")
	cfg, err := LoadFrom(opts(), missing)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.String("backend", ""); got != "auto" {
		t.Errorf("backend = %q, want auto", got)
	}
	if got := cfg.String(KeySudo, ""); got != "sudo -n" {
		t.Errorf("sudo = %q, want `sudo -n`", got)
	}
	if len(cfg.Sources) != 0 {
		t.Errorf("Sources = %v, want empty for a missing file", cfg.Sources)
	}
}

func TestLoadFromOverrides(t *testing.T) {
	system := writeConfig(t, "system.toml", `# machine-wide
backend = "ufw"
sudo = "doas"
theme = "/etc/theme/colors.toml"
`)
	user := writeConfig(t, "user.toml", `backend = "firewalld"  # the user file wins
`)

	cfg, err := LoadFrom(opts(), system, user)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.String("backend", ""); got != "firewalld" {
		t.Errorf("backend = %q, want firewalld", got)
	}
	// A key absent from the user file keeps the machine-wide value.
	if got := cfg.String(KeySudo, ""); got != "doas" {
		t.Errorf("sudo = %q, want doas", got)
	}
	if got := cfg.Theme(); got != "/etc/theme/colors.toml" {
		t.Errorf("Theme() = %q", got)
	}
	if len(cfg.Sources) != 2 {
		t.Errorf("Sources = %v, want both files", cfg.Sources)
	}
}

func TestEnvOverridesFiles(t *testing.T) {
	path := writeConfig(t, "system.toml", `backend = "ufw"`)
	t.Setenv("TUI_FIREWALL_BACKEND", "firewalld")
	// An undeclared key must not be picked up from the environment.
	t.Setenv("TUI_FIREWALL_SECRET", "leaked")

	o := opts()
	o.EnvPrefix = ""
	cfg, err := LoadFrom(o, path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.String("backend", ""); got != "firewalld" {
		t.Errorf("backend = %q, want the environment to win", got)
	}
	if cfg.Has("secret") {
		t.Error("an undeclared key must not be read from the environment")
	}
}

func TestSetOverridesEverything(t *testing.T) {
	path := writeConfig(t, "system.toml", `backend = "ufw"`)
	cfg, err := LoadFrom(opts(), path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	cfg.Set("backend", "auto")
	if got := cfg.String("backend", ""); got != "auto" {
		t.Errorf("backend = %q, want the flag to win", got)
	}
}

func TestSudoPrefix(t *testing.T) {
	tests := []struct {
		name string
		sudo string
		want []string
	}{
		{name: "sudo -n", sudo: "sudo -n", want: []string{"sudo", "-n"}},
		{name: "doas", sudo: "doas", want: []string{"doas"}},
		{name: "empty disables escalation", sudo: "", want: nil},
		{name: "blank disables escalation", sudo: "   ", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Values: map[string]string{KeySudo: tc.sudo}}
			if got := cfg.SudoPrefix(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SudoPrefix() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTypedReads(t *testing.T) {
	cfg := Config{Values: map[string]string{
		"follow": "true", "lines": "200", "junk": "nope",
	}}
	if !cfg.Bool("follow", false) {
		t.Error("Bool(follow) = false")
	}
	if got := cfg.Int("lines", 50); got != 200 {
		t.Errorf("Int(lines) = %d", got)
	}
	// An unparsable or absent value falls back instead of failing.
	if got := cfg.Int("junk", 50); got != 50 {
		t.Errorf("Int(junk) = %d, want the fallback", got)
	}
	if cfg.Bool("missing", true) != true {
		t.Error("Bool(missing) should return the fallback")
	}
}

func TestLoadFromInvalid(t *testing.T) {
	bad := writeConfig(t, "bad.toml", "backend ufw\n")
	if _, err := LoadFrom(opts(), bad); err == nil {
		t.Error("expected a parse error naming the line")
	}
}

func TestOneOf(t *testing.T) {
	cfg := Config{Values: map[string]string{"backend": "iptables"}}
	if err := cfg.OneOf("backend", "auto", "ufw", "firewalld"); err == nil {
		t.Error("expected a validation error for an unknown backend")
	}
	cfg.Set("backend", "ufw")
	if err := cfg.OneOf("backend", "auto", "ufw", "firewalld"); err != nil {
		t.Errorf("OneOf: %v", err)
	}
}

func TestLoadFromIgnoresUnknownKeys(t *testing.T) {
	path := writeConfig(t, "extra.toml", `backend = "ufw"
future_option = "whatever"
`)
	cfg, err := LoadFrom(opts(), path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.String("backend", ""); got != "ufw" {
		t.Errorf("backend = %q, want ufw", got)
	}
	// Unknown keys are kept but harmless: an older binary must not break on a
	// newer config file.
	if got := cfg.String("future_option", ""); got != "whatever" {
		t.Errorf("future_option = %q", got)
	}
}

func TestLoadRequiresTool(t *testing.T) {
	if _, err := Load(Options{}); err == nil {
		t.Error("expected an error when Tool is empty")
	}
}
