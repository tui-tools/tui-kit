package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tui-tools/tui-kit/internal/trust"
)

// asRoot makes Load believe the process is root, with root's home at home.
// The file check stays the real one unless check is given.
func asRoot(t *testing.T, home string, check func(string) error) {
	t.Helper()
	oldEUID, oldCheck, oldHome := geteuid, checkFile, homeOf
	t.Cleanup(func() { geteuid, checkFile, homeOf = oldEUID, oldCheck, oldHome })
	geteuid = func() int { return 0 }
	if check != nil {
		checkFile = check
	}
	homeOf = func(int) (string, error) { return home, nil }
}

// TestRootRefusesAFileItDoesNotControl is F9 of the 2026-11 review: a root
// process does not read a configuration file an ordinary account can write.
// The test user's temp dir is exactly such a file.
func TestRootRefusesAFileItDoesNotControl(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: the temp dir is root's")
	}
	path := writeConfig(t, "config.toml", `sudo = "/home/u/evil"`)
	asRoot(t, t.TempDir(), nil)
	_, err := LoadFrom(opts(), path)
	if !errors.Is(err, trust.ErrUntrusted) {
		t.Fatalf("LoadFrom as root = %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "refusing "+path) {
		t.Errorf("the refusal does not name the file: %v", err)
	}
}

// TestRootReadsAFileItControls: the same load with a file that passes the
// check is read as before, and a missing one is still just absent.
func TestRootReadsAFileItControls(t *testing.T) {
	path := writeConfig(t, "config.toml", `backend = "nftables"`)
	missing := filepath.Join(t.TempDir(), "absent.toml")
	var checked []string
	asRoot(t, t.TempDir(), func(p string) error {
		checked = append(checked, p)
		if _, err := os.Stat(p); err != nil {
			return err
		}
		return nil
	})
	cfg, err := LoadFrom(opts(), missing, path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.String("backend", ""); got != "nftables" {
		t.Errorf("backend = %q", got)
	}
	if len(checked) != 2 {
		t.Errorf("checked %v, want both paths", checked)
	}
}

// TestRootExpandsTildeToItsOwnHome: under `sudo -E` $HOME is the invoking
// user's; as root, "~" is root's home from the account database instead.
func TestRootExpandsTildeToItsOwnHome(t *testing.T) {
	userHome, rootHome := t.TempDir(), t.TempDir()
	for _, home := range []string{userHome, rootHome} {
		dir := filepath.Join(home, ".config", "tui-firewall")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	userFile := filepath.Join(userHome, ".config", "tui-firewall", "config.toml")
	rootFile := filepath.Join(rootHome, ".config", "tui-firewall", "config.toml")
	if err := os.WriteFile(userFile, []byte(`backend = "user"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootFile, []byte(`backend = "root"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", userHome)
	asRoot(t, rootHome, func(string) error { return nil })
	cfg, err := LoadFrom(opts(), UserPathFor("tui-firewall"))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.String("backend", ""); got != "root" {
		t.Errorf("backend = %q, want root's file", got)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0] != rootFile {
		t.Errorf("Sources = %v", cfg.Sources)
	}
}

// TestRootInASudoSessionRefusesTheEnvironment: TUI_<TOOL>_* variables that
// reached a root process through sudo stop the load, naming the variable and
// never its value.
func TestRootInASudoSessionRefusesTheEnvironment(t *testing.T) {
	o := opts()
	o.EnvPrefix = ""
	t.Setenv("TUI_FIREWALL_SUDO", "/home/u/evil")
	t.Setenv("SUDO_USER", "u")
	asRoot(t, t.TempDir(), func(string) error { return nil })
	_, err := LoadFrom(o)
	if err == nil {
		t.Fatal("LoadFrom accepted a sudo session's environment as root")
	}
	if !strings.Contains(err.Error(), "TUI_FIREWALL_SUDO") ||
		strings.Contains(err.Error(), "/home/u/evil") {
		t.Errorf("message = %v", err)
	}

	// Root's own session (no SUDO_USER) keeps the variables.
	t.Setenv("SUDO_USER", "")
	if err := os.Unsetenv("SUDO_USER"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(o)
	if err != nil || cfg.String(KeySudo, "") != "/home/u/evil" {
		t.Errorf("root's own environment = %v, %v", cfg.Values, err)
	}
}

// TestUnprivilegedLoadIsUnchanged: an ordinary user still reads files it owns
// and its own environment, inside a sudo session or not.
func TestUnprivilegedLoadIsUnchanged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	path := writeConfig(t, "config.toml", `backend = "nftables"`)
	o := opts()
	o.EnvPrefix = ""
	t.Setenv("TUI_FIREWALL_THEME", "/x")
	t.Setenv("SUDO_USER", "u")
	cfg, err := LoadFrom(o, path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.String("backend", "") != "nftables" || cfg.Theme() != "/x" {
		t.Errorf("values = %v", cfg.Values)
	}
}
