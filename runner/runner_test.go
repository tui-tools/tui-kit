package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeBin writes an executable shell script and returns its directory, so a
// test can drive a real exec without depending on what the host has installed.
func fakeBin(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil { //nolint:gosec // the fake binary has to be executable, and it lives in the test's own temp dir
		t.Fatalf("write %s: %v", path, err)
	}
	return dir
}

func TestCommandString(t *testing.T) {
	cmd := Command{Argv: []string{"ufw", "--force", "delete", "1"}}
	if got := cmd.String(); got != "ufw --force delete 1" {
		t.Errorf("String() = %q", got)
	}
}

func TestNewMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := New(Options{Bin: "definitely-not-here",
		InstallHint: "install it with your package manager"})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("err = %v, want ErrNotAvailable", err)
	}
	if !strings.Contains(err.Error(), "install it with your package manager") {
		t.Errorf("the install hint is missing from %q", err)
	}
}

func TestNewFindsSearchPath(t *testing.T) {
	dir := fakeBin(t, "toolctl", "echo hi")
	t.Setenv("PATH", t.TempDir())
	r, err := New(Options{Bin: "toolctl",
		SearchPaths: []string{filepath.Join(dir, "toolctl")}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Bin != filepath.Join(dir, "toolctl") {
		t.Errorf("Bin = %q", r.Bin)
	}
}

func TestPreviewAndDescribe(t *testing.T) {
	tests := []struct {
		name         string
		privilege    []string
		wantPreview  string
		wantDescribe string
	}{
		{
			name:         "no escalation",
			wantPreview:  "toolctl status",
			wantDescribe: "toolctl (root)",
		},
		{
			name:         "sudo -n",
			privilege:    []string{"/usr/bin/sudo", "-n"},
			wantPreview:  "/usr/bin/sudo -n toolctl status",
			wantDescribe: "toolctl via /usr/bin/sudo -n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{Name: "toolctl", Bin: "/usr/sbin/toolctl",
				Privilege: tc.privilege}
			cmd := Command{Argv: []string{"toolctl", "status"}}
			if got := r.Preview(cmd); got != tc.wantPreview {
				t.Errorf("Preview() = %q, want %q", got, tc.wantPreview)
			}
			if got := r.Describe(); got != tc.wantDescribe {
				t.Errorf("Describe() = %q, want %q", got, tc.wantDescribe)
			}
		})
	}
}

func TestArgv(t *testing.T) {
	r := &Runner{Name: "toolctl", Bin: "/usr/sbin/toolctl",
		Privilege: []string{"/usr/bin/sudo", "-n"}}
	tests := []struct {
		name       string
		argv       []string
		privileged bool
		wantBin    string
		wantArgs   []string
	}{
		{
			name: "argv[0] is replaced by the resolved path",
			argv: []string{"toolctl", "status"}, privileged: false,
			wantBin: "/usr/sbin/toolctl", wantArgs: []string{"status"},
		},
		{
			name: "the escalation prefix wraps the resolved path",
			argv: []string{"toolctl", "reload"}, privileged: true,
			wantBin:  "/usr/bin/sudo",
			wantArgs: []string{"-n", "/usr/sbin/toolctl", "reload"},
		},
		{
			name: "an argv that does not repeat the tool name is kept whole",
			argv: []string{"status", "verbose"}, privileged: false,
			wantBin: "/usr/sbin/toolctl", wantArgs: []string{"status", "verbose"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin, args := r.argv(Command{Argv: tc.argv}, tc.privileged)
			if bin != tc.wantBin {
				t.Errorf("bin = %q, want %q", bin, tc.wantBin)
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Errorf("args = %q, want %q", args, tc.wantArgs)
			}
		})
	}
}

func TestRunCapturesOutput(t *testing.T) {
	dir := fakeBin(t, "toolctl", `echo "ran: $*"`)
	t.Setenv("PATH", dir)
	r, err := New(Options{Bin: "toolctl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := r.Run(context.Background(),
		Command{Argv: []string{"toolctl", "reload"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran: reload" {
		t.Errorf("output = %q", out)
	}
}

// TestRunFeedsStdinAndKeepsItOutOfThePreview covers the one input that must
// never reach an argv: a password handed to `chpasswd` is visible in `ps` to
// every user on the machine if it goes on the command line, so it goes on
// stdin — and the preview, which is the promise the tool makes, still shows
// only the command line.
func TestRunFeedsStdinAndKeepsItOutOfThePreview(t *testing.T) {
	// A shell builtin rather than `cat`: PATH holds only the fake binary.
	dir := fakeBin(t, "toolctl", `read -r line; echo "$line"`)
	t.Setenv("PATH", dir)
	r, err := New(Options{Bin: "toolctl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cmd := Command{Argv: []string{"toolctl"}, Stdin: "alice:hunter2\n"}
	out, err := r.Run(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "alice:hunter2" {
		t.Errorf("the command did not receive its stdin: %q", out)
	}
	if got := r.Preview(cmd); strings.Contains(got, "hunter2") {
		t.Errorf("the preview leaked the stdin: %q", got)
	}
	if got := cmd.String(); strings.Contains(got, "hunter2") {
		t.Errorf("String() leaked the stdin: %q", got)
	}
}

func TestRunFailureNamesThePreview(t *testing.T) {
	dir := fakeBin(t, "toolctl", `echo "not permitted" >&2; exit 1`)
	t.Setenv("PATH", dir)
	r, err := New(Options{Bin: "toolctl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = r.Run(context.Background(), Command{Argv: []string{"toolctl", "reload"}})
	if err == nil {
		t.Fatal("expected a failure")
	}
	// The message must name the command the user saw in the preview.
	if !strings.Contains(err.Error(), "toolctl reload") ||
		!strings.Contains(err.Error(), "not permitted") {
		t.Errorf("error = %q", err)
	}
}

// TestReadTimeout: a read is bounded, so a stuck query cannot freeze the UI.
func TestReadTimeout(t *testing.T) {
	// A busy loop rather than `sleep`, so the test does not depend on what
	// the isolated PATH carries.
	dir := fakeBin(t, "toolctl", "while : ; do : ; done")
	t.Setenv("PATH", dir)
	r, err := New(Options{Bin: "toolctl", Timeout: 100 * time.Millisecond,
		PrivilegedReads: new(false)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = r.Read(context.Background(), "toolctl", "wait")
	if err == nil || !strings.Contains(err.Error(), "timed out after 100ms") {
		t.Fatalf("error = %v, want a timeout", err)
	}
}

// TestRunIsNotBoundByTheReadTimeout: a mutation outlives the read timeout of
// its own runner. It is the regression test for a pacman download killed
// mid-transaction by the timeout meant for reads.
func TestRunIsNotBoundByTheReadTimeout(t *testing.T) {
	dir := fakeBin(t, "toolctl", `/bin/sleep 0.5; echo "done: $*"`)
	t.Setenv("PATH", dir)
	r, err := New(Options{Bin: "toolctl", Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := r.Run(context.Background(), Command{Argv: []string{"toolctl", "install"}})
	if err != nil {
		t.Fatalf("Run was cut short by the read timeout: %v", err)
	}
	if out != "done: install" {
		t.Errorf("output = %q", out)
	}
}

// TestNewDefaults: reads get DefaultTimeout, mutations get no limit.
func TestNewDefaults(t *testing.T) {
	dir := fakeBin(t, "toolctl", "true")
	t.Setenv("PATH", dir)
	r, err := New(Options{Bin: "toolctl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Timeout != DefaultTimeout || r.MutationTimeout != 0 {
		t.Errorf("Timeout = %s, MutationTimeout = %s; want %s and none",
			r.Timeout, r.MutationTimeout, DefaultTimeout)
	}
}

// TestDefaultsAtFifteenSeconds is the issue's case at its real scale: with
// the default options a mutation that runs past DefaultTimeout finishes, and
// a read of the same length is killed at DefaultTimeout. It takes about 16 s,
// so -short skips it; the two halves run in parallel.
func TestDefaultsAtFifteenSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("takes DefaultTimeout plus a second")
	}
	// The binary is found through SearchPaths rather than PATH, so the
	// subtests need no t.Setenv and can run in parallel.
	dir := fakeBin(t, "slowtoolctl", `/bin/sleep 16; echo finished`)
	bin := filepath.Join(dir, "slowtoolctl")
	newRunner := func(t *testing.T) *Runner {
		t.Helper()
		r, err := New(Options{Bin: "slowtoolctl", SearchPaths: []string{bin},
			PrivilegedReads: new(false)})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return r
	}
	t.Run("mutation finishes", func(t *testing.T) {
		t.Parallel()
		out, err := newRunner(t).Run(context.Background(),
			Command{Argv: []string{"slowtoolctl", "install"}})
		if err != nil || out != "finished" {
			t.Errorf("Run = %q, %v; want it to finish", out, err)
		}
	})
	t.Run("read is killed", func(t *testing.T) {
		t.Parallel()
		start := time.Now()
		_, err := newRunner(t).Read(context.Background(), "slowtoolctl", "list")
		if err == nil || !strings.Contains(err.Error(), "timed out after 15s") {
			t.Errorf("Read error = %v, want a 15s timeout", err)
		}
		if took := time.Since(start); took > DefaultTimeout+2*time.Second {
			t.Errorf("Read took %s", took)
		}
	})
}

// TestRunMutationTimeoutIsOptIn: a runner that asks for a mutation limit gets
// one, and the message names it.
func TestRunMutationTimeoutIsOptIn(t *testing.T) {
	dir := fakeBin(t, "toolctl", "while : ; do : ; done")
	t.Setenv("PATH", dir)
	r, err := New(Options{Bin: "toolctl", MutationTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = r.Run(context.Background(), Command{Argv: []string{"toolctl", "wait"}})
	if err == nil || !strings.Contains(err.Error(), "timed out after 100ms") {
		t.Fatalf("error = %v, want a timeout", err)
	}
}

// TestRunCancelSendsSIGTERM: cancelling a mutation asks it to stop, so a
// package manager gets the chance to release its lock, instead of killing it.
func TestRunCancelSendsSIGTERM(t *testing.T) {
	dir := fakeBin(t, "toolctl",
		`trap 'kill $!; echo cleaned up; exit 3' TERM; /bin/sleep 5 & wait`)
	t.Setenv("PATH", dir)
	r, err := New(Options{Bin: "toolctl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, err := r.Run(ctx, Command{Argv: []string{"toolctl", "install"}})
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("error = %v, want the caller's deadline", err)
	}
	if !strings.Contains(out, "cleaned up") {
		t.Errorf("the mutation was not given SIGTERM: output %q", out)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("cancel took %s: it waited for the child instead of stopping it", took)
	}
}

func TestReadHonoursPrivilegedReads(t *testing.T) {
	dir := fakeBin(t, "toolctl", `echo "read: $*"`)
	// The escalation prefix points at a command that does not exist: Read
	// must never reach it, which is exactly what the assertion proves.
	r := &Runner{Name: "toolctl", Bin: filepath.Join(dir, "toolctl"),
		Privilege: []string{"/nonexistent/sudo", "-n"},
		Timeout:   5 * time.Second, env: []string{"LANG=C"}}
	out, err := r.Read(context.Background(), "toolctl", "list")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out != "read: list" {
		t.Errorf("output = %q", out)
	}
}

func TestAvailable(t *testing.T) {
	dir := fakeBin(t, "toolctl", "true")
	t.Setenv("PATH", dir)
	if !Available("toolctl") {
		t.Error("Available(toolctl) = false")
	}
	if Available("definitely-not-here") {
		t.Error("Available(definitely-not-here) = true")
	}
}

func TestFirstLine(t *testing.T) {
	if got := FirstLine("one\ntwo\n"); got != "one" {
		t.Errorf("FirstLine = %q", got)
	}
	if got := FirstLine("only"); got != "only" {
		t.Errorf("FirstLine = %q", got)
	}
}

func TestFake(t *testing.T) {
	f := &Fake{Prefix: "sudo -n", Outputs: map[string]string{
		"toolctl reload": "Firewall reloaded",
	}}
	cmd := Command{Argv: []string{"toolctl", "reload"}}
	if got := f.Preview(cmd); got != "sudo -n toolctl reload" {
		t.Errorf("Preview = %q", got)
	}
	out, err := f.Run(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "Firewall reloaded" {
		t.Errorf("output = %q", out)
	}
	// An uncovered command still answers, so a demo never dead-ends.
	if out, _ = f.Run(context.Background(),
		Command{Argv: []string{"toolctl", "status"}}); out != "ok" {
		t.Errorf("default output = %q", out)
	}
	if len(f.Ran) != 2 {
		t.Fatalf("Ran = %v, want 2 commands", f.Ran)
	}
	last, ok := f.Last()
	if !ok || last.String() != "toolctl status" {
		t.Errorf("Last() = %v, %v", last, ok)
	}
	f.Reset()
	if len(f.Ran) != 0 {
		t.Error("Reset did not clear the recorded commands")
	}
}

func TestFakeHook(t *testing.T) {
	// The Hook is what lets a --demo backend mutate its in-memory state the
	// way the real command would.
	var applied []string
	f := &Fake{Hook: func(cmd Command) (string, error) {
		applied = append(applied, cmd.String())
		return "applied", nil
	}}
	out, err := f.Run(context.Background(), Command{Argv: []string{"toolctl", "add"}})
	if err != nil || out != "applied" {
		t.Fatalf("Run = %q, %v", out, err)
	}
	if !reflect.DeepEqual(applied, []string{"toolctl add"}) {
		t.Errorf("applied = %v", applied)
	}
}

func TestFakeError(t *testing.T) {
	want := errors.New("boom")
	f := &Fake{Err: want}
	if _, err := f.Run(context.Background(),
		Command{Argv: []string{"toolctl"}}); !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}
