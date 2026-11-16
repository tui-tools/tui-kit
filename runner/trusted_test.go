package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tui-tools/tui-kit/internal/trust"
)

// trustPath makes the test's PATH the trusted directories and accepts every
// file in them, so a test can escalate through fake binaries in temp dirs.
// The real lookup and check are covered on their own below.
func trustPath(t *testing.T) {
	t.Helper()
	dirs, verify := trustedDirs, verifyTrusted
	t.Cleanup(func() { trustedDirs, verifyTrusted = dirs, verify })
	trustedDirs = func() []string { return filepath.SplitList(os.Getenv("PATH")) }
	verifyTrusted = func(string) error { return nil }
}

// trustOnly makes dirs the trusted directories, keeping the given check.
func trustOnly(t *testing.T, verify func(string) error, dirs ...string) {
	t.Helper()
	oldDirs, oldVerify := trustedDirs, verifyTrusted
	t.Cleanup(func() { trustedDirs, verifyTrusted = oldDirs, oldVerify })
	trustedDirs = func() []string { return dirs }
	verifyTrusted = verify
}

// actAsRoot makes the runner believe the process is root.
func actAsRoot(t *testing.T) {
	t.Helper()
	old := geteuid
	t.Cleanup(func() { geteuid = old })
	geteuid = func() int { return 0 }
}

func acceptAll(string) error { return nil }

// TestEscalatingRunnerIgnoresPATH is F8 of the 2026-11 review: a binary that
// is only on the caller's PATH (~/.local/bin comes first on Fedora) is never
// what an escalated command runs.
func TestEscalatingRunnerIgnoresPATH(t *testing.T) {
	userDir := fakeBin(t, "toolctl", "echo planted")
	t.Setenv("PATH", userDir)
	trustOnly(t, acceptAll, t.TempDir())
	_, err := New(Options{Bin: "toolctl", SudoPrefix: []string{"sudo", "-n"}})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("New = %v, want ErrNotAvailable", err)
	}
	if !strings.Contains(err.Error(), "PATH is not used") {
		t.Errorf("the message does not say why the PATH copy was passed over: %v", err)
	}
	// Run unescalated, as the caller, PATH is the caller's to choose.
	if _, err := New(Options{Bin: "toolctl"}); err != nil {
		t.Errorf("an unescalated runner lost PATH lookup: %v", err)
	}
}

// TestEscalatingRunnerPrefersTheSystemCopy: with a planted copy first on PATH
// and the real one in a system directory, the system one is resolved, and so
// are sudo and setsid(1).
func TestEscalatingRunnerPrefersTheSystemCopy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: New never escalates")
	}
	userDir := fakeBin(t, "toolctl", "echo planted")
	sysDir := fakeBin(t, "toolctl", "echo real")
	sudo := fakeSudo(t)
	t.Setenv("PATH", userDir+":"+filepath.Dir(sudo))
	trustOnly(t, acceptAll, sysDir, filepath.Dir(sudo))
	r, err := New(Options{Bin: "toolctl", SudoPrefix: []string{"sudo", "-n"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Bin != filepath.Join(sysDir, "toolctl") {
		t.Errorf("Bin = %s, want the system copy", r.Bin)
	}
	if r.Privilege[0] != sudo {
		t.Errorf("sudo = %s, want %s", r.Privilege[0], sudo)
	}
	out, err := r.Run(context.Background(), Command{Argv: []string{"toolctl"}})
	if err != nil || out != "real" {
		t.Errorf("Run = %q, %v", out, err)
	}
}

// TestEscalatingRunnerRefusesAnUntrustedBinary runs the real check: a binary
// in a directory the test user owns is refused with a message naming it,
// rather than skipped for a later candidate.
func TestEscalatingRunnerRefusesAnUntrustedBinary(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: the temp dir is root's")
	}
	dir := fakeBin(t, "toolctl", "true")
	later := fakeBin(t, "toolctl", "true")
	trustOnly(t, trust.CheckFile, dir, later)
	_, err := New(Options{Bin: "toolctl", SudoPrefix: []string{"sudo", "-n"}})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("New = %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "refusing "+filepath.Join(dir, "toolctl")) {
		t.Errorf("the refusal does not name the file: %v", err)
	}
}

// TestEscalatingRunnerChecksSearchPaths: a SearchPaths fallback is held to
// the same check as the system directories.
func TestEscalatingRunnerChecksSearchPaths(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: the temp dir is root's")
	}
	dir := fakeBin(t, "toolctl", "true")
	trustOnly(t, trust.CheckFile, t.TempDir())
	_, err := New(Options{Bin: "toolctl", SearchPaths: []string{filepath.Join(dir, "toolctl")},
		SudoPrefix: []string{"sudo", "-n"}})
	if !errors.Is(err, ErrNotAvailable) || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("New = %v, want a refusal", err)
	}
}

// TestRootRunnerIgnoresPATH: started as root (sudo -E, su without -), there
// is no prefix, and the binary is still resolved without PATH.
func TestRootRunnerIgnoresPATH(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "toolctl", "true"))
	actAsRoot(t)
	trustOnly(t, acceptAll, t.TempDir())
	if _, err := New(Options{Bin: "toolctl"}); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("New as root = %v, want ErrNotAvailable", err)
	}
	if Available("toolctl") {
		t.Error("Available as root found the PATH copy")
	}
}

// TestCommandsRunAsRootGetATrustedEnvironment: what the caller had in its
// environment does not reach a command run as root, escalated or direct.
func TestCommandsRunAsRootGetATrustedEnvironment(t *testing.T) {
	script := `echo "path=$PATH home=$HOME leak=$TUI_KIT_LEAK tz=$TZ lang=$LANG"`
	t.Setenv("TUI_KIT_LEAK", "yes")
	t.Setenv("TZ", "UTC")
	t.Setenv("HOME", "/home/someone")

	t.Run("escalated", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: New never escalates")
		}
		sysDir := fakeBin(t, "toolctl", script)
		// A sudo that keeps the whole environment, like `sudo -E`.
		sudo := fakeSudo(t)
		trustOnly(t, acceptAll, sysDir, filepath.Dir(sudo))
		r, err := New(Options{Bin: "toolctl", SudoPrefix: []string{"sudo", "-n"}})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		out, err := r.Run(context.Background(), Command{Argv: []string{"toolctl"}})
		want := "path=" + sysDir + ":" + filepath.Dir(sudo) + " home= leak= tz=UTC lang=C"
		if err != nil || out != want {
			t.Errorf("escalated env = %q, %v; want %q", out, err, want)
		}
	})

	t.Run("started as root", func(t *testing.T) {
		sysDir := fakeBin(t, "toolctl", script)
		actAsRoot(t)
		trustOnly(t, acceptAll, sysDir)
		r, err := New(Options{Bin: "toolctl", SudoPrefix: []string{"sudo", "-n"}})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		out, err := r.Run(context.Background(), Command{Argv: []string{"toolctl"}})
		want := "path=" + sysDir + " home=" + rootHome() + " leak= tz=UTC lang=C"
		if err != nil || out != want {
			t.Errorf("root env = %q, %v; want %q", out, err, want)
		}
		if strings.Contains(out, "/home/someone") {
			t.Errorf("the caller's HOME reached a root command: %q", out)
		}
	})

	t.Run("unescalated keeps the caller's", func(t *testing.T) {
		dir := fakeBin(t, "toolctl", script)
		t.Setenv("PATH", dir)
		r, err := New(Options{Bin: "toolctl"})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		out, err := r.Run(context.Background(), Command{Argv: []string{"toolctl"}})
		if err != nil || !strings.Contains(out, "leak=yes") ||
			!strings.Contains(out, "home=/home/someone") {
			t.Errorf("unescalated env = %q, %v", out, err)
		}
	})
}

// TestRootHomeComesFromTheAccountDatabase: HOME for a root command is root's
// entry in the account database, whatever $HOME says.
func TestRootHomeComesFromTheAccountDatabase(t *testing.T) {
	actAsRoot(t)
	t.Setenv("HOME", "/home/someone")
	if got := rootHome(); got == "/home/someone" || got == "" {
		t.Errorf("rootHome = %q", got)
	}
}
