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
		env        []string
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
		{
			name: "escalated, the variables go through env after the prefix",
			argv: []string{"toolctl", "install", "-y", "pkg"}, privileged: true,
			env:     []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a"},
			wantBin: "/usr/bin/sudo",
			wantArgs: []string{"-n", "env", "DEBIAN_FRONTEND=noninteractive",
				"NEEDRESTART_MODE=a", "/usr/sbin/toolctl", "install", "-y", "pkg"},
		},
		{
			name: "run directly, the variables stay out of the argv",
			argv: []string{"toolctl", "install"}, privileged: false,
			env:     []string{"DEBIAN_FRONTEND=noninteractive"},
			wantBin: "/usr/sbin/toolctl", wantArgs: []string{"install"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin, args := r.argv(Command{Argv: tc.argv, Env: tc.env}, tc.privileged)
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

// TestPreviewShowsTheEnvironment: a command's own variables are part of what
// the user confirms, so the preview carries them in the form that runs: env(1)
// after an escalation prefix, plain assignments when there is none. Both are
// lines a shell runs the same way.
func TestPreviewShowsTheEnvironment(t *testing.T) {
	cmd := Command{Argv: []string{"apt-get", "install", "-y", "headscale"},
		Env: []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a",
			"NOTE=two words"}}
	for _, tc := range []struct {
		privilege []string
		want      string
	}{
		{nil, "DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a " +
			"NOTE='two words' apt-get install -y headscale"},
		{[]string{"/usr/bin/sudo", "-n"}, "/usr/bin/sudo -n env " +
			"DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a " +
			"NOTE='two words' apt-get install -y headscale"},
	} {
		r := &Runner{Name: "apt-get", Bin: "/usr/bin/apt-get", Privilege: tc.privilege}
		if got := r.Preview(cmd); got != tc.want {
			t.Errorf("Preview = %s\nwant      %s", got, tc.want)
		}
	}
	f := &Fake{Prefix: "sudo -n"}
	if got, want := f.Preview(cmd), "sudo -n env "+cmd.String(); got != want {
		t.Errorf("Fake.Preview = %s, want %s", got, want)
	}
}

// TestRunPassesTheEnvironment runs both paths for real: directly, the
// variables are in the process environment; escalated through a sudo that
// resets the environment the way the real one does, they still arrive,
// because env(1) sets them after it.
func TestRunPassesTheEnvironment(t *testing.T) {
	dir := fakeBin(t, "toolctl", `echo "$DEBIAN_FRONTEND/$NEEDRESTART_MODE"`)
	// A sudo that drops -n and runs the rest with an emptied environment.
	sudoDir := fakeBin(t, "sudo", `shift; exec /usr/bin/env -i PATH=/usr/bin:/bin "$@"`)
	t.Setenv("PATH", dir+":"+sudoDir+":/usr/bin:/bin")
	t.Setenv("DEBIAN_FRONTEND", "")
	cmd := Command{Argv: []string{"toolctl", "install"},
		Env: []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a"}}

	direct, err := New(Options{Bin: "toolctl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if out, err := direct.Run(context.Background(), cmd); err != nil ||
		out != "noninteractive/a" {
		t.Errorf("direct run = %q, %v", out, err)
	}

	if os.Geteuid() == 0 {
		t.Skip("running as root: no escalation to exercise")
	}
	escalated, err := New(Options{Bin: "toolctl", SudoPrefix: []string{"sudo", "-n"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if out, err := escalated.Run(context.Background(), cmd); err != nil ||
		out != "noninteractive/a" {
		t.Errorf("escalated run = %q, %v", out, err)
	}
	// The same sudo without env(1) loses them, which is why it is there.
	if out, _ := escalated.Run(context.Background(),
		Command{Argv: []string{"toolctl"}}); out != "/" {
		t.Errorf("the fake sudo did not reset the environment: %q", out)
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

// ttyProbe reports whether the process has a controlling terminal and whether
// it leads its own session. It is the probe from tui-kit#35: /dev/tty opens
// only for a process that has a controlling terminal, and field 6 of
// /proc/<pid>/stat is the session id, equal to the pid for a session leader.
const ttyProbe = `if (exec 3</dev/tty) 2>/dev/null; then echo ctty=yes; else echo ctty=no; fi
sid=$(cut -d' ' -f6 /proc/$$/stat)
if [ "$sid" = "$$" ]; then echo session=own; else echo session=inherited; fi`

// fakeSudo is an escalation prefix that drops -n and execs the command, so
// the escalated path runs for real without root.
func fakeSudo(t *testing.T) string {
	t.Helper()
	return filepath.Join(fakeBin(t, "sudo", `[ "$1" = -n ] && shift
exec "$@"`), "sudo")
}

// TestChildrenHaveNoControllingTerminal: every child the runner starts, read
// or mutation, escalated or not, leads a session of its own and so cannot
// reach the terminal the TUI is drawing on (tui-kit#35).
func TestChildrenHaveNoControllingTerminal(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("needs /proc")
	}
	dir := fakeBin(t, "probe", ttyProbe)
	r := &Runner{Name: "probe", Bin: filepath.Join(dir, "probe"),
		Privilege:       []string{fakeSudo(t), "-n"},
		privilegedReads: true, Timeout: 5 * time.Second}
	plain := &Runner{Name: "probe", Bin: filepath.Join(dir, "probe"),
		Timeout: 5 * time.Second}
	want := "ctty=no\nsession=own"
	cases := map[string]func() (string, error){
		"read":               func() (string, error) { return plain.Read(context.Background(), "probe") },
		"mutation":           func() (string, error) { return plain.Run(context.Background(), Command{Argv: []string{"probe"}}) },
		"escalated read":     func() (string, error) { return r.Read(context.Background(), "probe") },
		"escalated mutation": func() (string, error) { return r.Run(context.Background(), Command{Argv: []string{"probe"}}) },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := run()
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if out != want {
				t.Errorf("probe = %q, want %q", out, want)
			}
		})
	}
}

// TestChildReadingTheTerminalFailsFast: a child that asks the terminal for an
// answer (debconf, a password prompt) gets an error at once instead of
// waiting behind the TUI for an answer nobody can type.
func TestChildReadingTheTerminalFailsFast(t *testing.T) {
	dir := fakeBin(t, "asker", `printf 'Continue? ' >/dev/tty || exit 3
read -r answer </dev/tty || exit 4
echo "answered $answer"`)
	r := &Runner{Name: "asker", Bin: filepath.Join(dir, "asker"),
		Privilege: []string{fakeSudo(t), "-n"}, Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err := r.Run(ctx, Command{Argv: []string{"asker"}})
	if err == nil {
		t.Fatal("a child reading /dev/tty succeeded; it must fail without a terminal")
	}
	if ctx.Err() != nil || time.Since(start) > 5*time.Second {
		t.Fatalf("the child waited on the terminal (%s): %v", time.Since(start), err)
	}
}
