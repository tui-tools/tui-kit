package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// setsidOrSkip returns the host's setsid(1), or skips: the escalated path
// under test is the one that has it.
func setsidOrSkip(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("needs setsid(1)")
	}
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("needs /proc")
	}
	return path
}

// sessionOf reads the session id of a process from /proc.
func sessionOf(t *testing.T, pid string) string {
	t.Helper()
	data, err := os.ReadFile("/proc/" + pid + "/stat") //nolint:gosec // the test names the pid itself
	if err != nil {
		t.Fatalf("read stat: %v", err)
	}
	text := string(data)
	return strings.Fields(text[strings.LastIndexByte(text, ')')+1:])[3]
}

// TestEscalatedArgvPutsSetsidAfterThePrefix: setsid(1) comes right after the
// escalation prefix, before env(1) and the command, so sudo runs in the
// caller's session and the command in one of its own.
func TestEscalatedArgvPutsSetsidAfterThePrefix(t *testing.T) {
	r := &Runner{Name: "toolctl", Bin: "/usr/sbin/toolctl",
		Privilege: []string{"/usr/bin/sudo", "-n"},
		Detach:    []string{"/usr/bin/setsid", "-w"}}
	cmd := Command{Argv: []string{"toolctl", "install"},
		Env: []string{"DEBIAN_FRONTEND=noninteractive"}}
	bin, args := r.argv(cmd, true)
	want := []string{"-n", "/usr/bin/setsid", "-w", "env",
		"DEBIAN_FRONTEND=noninteractive", "/usr/sbin/toolctl", "install"}
	if bin != "/usr/bin/sudo" || !reflect.DeepEqual(args, want) {
		t.Errorf("argv = %s %q, want /usr/bin/sudo %q", bin, args, want)
	}
	if got, wantPreview := r.Preview(cmd), "/usr/bin/sudo -n /usr/bin/setsid -w env "+
		"DEBIAN_FRONTEND=noninteractive toolctl install"; got != wantPreview {
		t.Errorf("Preview = %q, want %q", got, wantPreview)
	}
	// An unescalated read never gets either.
	if bin, args := r.argv(Command{Argv: []string{"toolctl", "status"}}, false); bin !=
		"/usr/sbin/toolctl" || !reflect.DeepEqual(args, []string{"status"}) {
		t.Errorf("unescalated argv = %s %q", bin, args)
	}
}

// TestNewResolvesSetsidOnlyWhenEscalating: New fills Detach with the host's
// setsid(1) when it escalates, and leaves it empty when there is nothing to
// escalate through.
func TestNewResolvesSetsidOnlyWhenEscalating(t *testing.T) {
	setsid := setsidOrSkip(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root: New never escalates")
	}
	dir := fakeBin(t, "toolctl", "true")
	t.Setenv("PATH", dir+":"+filepath.Dir(fakeSudo(t))+":"+filepath.Dir(setsid))
	r, err := New(Options{Bin: "toolctl", SudoPrefix: []string{"sudo", "-n"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !reflect.DeepEqual(r.Detach, []string{setsid, "-w"}) {
		t.Errorf("Detach = %q, want [%s -w]", r.Detach, setsid)
	}
	plain, err := New(Options{Bin: "toolctl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if plain.Detach != nil {
		t.Errorf("Detach without escalation = %q, want none", plain.Detach)
	}
}

// TestEscalatedSudoStaysInTheCallersSession is the regression test for
// tui-kit#42: sudo matches its ticket on the session id, so the escalation
// command must run in the caller's session, while the command it runs still
// leads a session of its own and has no terminal (tui-kit#35).
func TestEscalatedSudoStaysInTheCallersSession(t *testing.T) {
	setsid := setsidOrSkip(t)
	// The fake sudo reports its own session, then runs the rest.
	sudoDir := fakeBin(t, "sudo", `echo "sudo-session=$(cut -d' ' -f6 /proc/$$/stat)"
[ "$1" = -n ] && shift
exec "$@"`)
	dir := fakeBin(t, "probe", ttyProbe)
	r := &Runner{Name: "probe", Bin: filepath.Join(dir, "probe"),
		Privilege: []string{filepath.Join(sudoDir, "sudo"), "-n"},
		Detach:    []string{setsid, "-w"}, privilegedReads: true,
		Timeout: 5 * time.Second}
	want := "sudo-session=" + sessionOf(t, "self") + "\nctty=no\nsession=own"
	for name, run := range map[string]func() (string, error){
		"escalated read":     func() (string, error) { return r.Read(context.Background(), "probe") },
		"escalated mutation": func() (string, error) { return r.Run(context.Background(), Command{Argv: []string{"probe"}}) },
	} {
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

// TestEscalatedChildReadingTheTerminalFailsFast is tui-kit#35's test on the
// setsid(1) path: the command cannot wait on a terminal.
func TestEscalatedChildReadingTheTerminalFailsFast(t *testing.T) {
	setsid := setsidOrSkip(t)
	dir := fakeBin(t, "asker", `printf 'Continue? ' >/dev/tty || exit 3
read -r answer </dev/tty || exit 4
echo "answered $answer"`)
	r := &Runner{Name: "asker", Bin: filepath.Join(dir, "asker"),
		Privilege: []string{fakeSudo(t), "-n"}, Detach: []string{setsid, "-w"},
		Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := r.Run(ctx, Command{Argv: []string{"asker"}}); err == nil {
		t.Fatal("a child reading /dev/tty succeeded; it must fail without a terminal")
	}
	if ctx.Err() != nil || time.Since(start) > 5*time.Second {
		t.Fatalf("the child waited on the terminal (%s)", time.Since(start))
	}
}

// TestCancelReachesTheDetachedCommand: cancelling an escalated mutation
// delivers SIGTERM to the command, not only to sudo and setsid(1), so a
// package manager still gets to clean up its lock.
func TestCancelReachesTheDetachedCommand(t *testing.T) {
	setsid := setsidOrSkip(t)
	// Like the real sudo, this one stays alive as the command's parent
	// instead of exec'ing it, so the command is a descendant to be found.
	sudoDir := fakeBin(t, "sudo", `[ "$1" = -n ] && shift
"$@"`)
	dir := fakeBin(t, "toolctl",
		`trap 'kill $!; echo cleaned up; exit 3' TERM; /bin/sleep 5 & wait`)
	r := &Runner{Name: "toolctl", Bin: filepath.Join(dir, "toolctl"),
		Privilege: []string{filepath.Join(sudoDir, "sudo"), "-n"},
		Detach:    []string{setsid, "-w"}, Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, err := r.Run(ctx, Command{Argv: []string{"toolctl", "install"}})
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("error = %v, want the caller's deadline", err)
	}
	if !strings.Contains(out, "cleaned up") {
		t.Errorf("the command was not given SIGTERM: output %q", out)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("cancel took %s: it waited for the command instead of stopping it", took)
	}
}

// writeStat writes a /proc/<pid>/stat fixture.
func writeStat(t *testing.T, root string, pid, ppid, sid int, comm string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	line := strconv.Itoa(pid) + " (" + comm + ") S " + strconv.Itoa(ppid) + " " +
		strconv.Itoa(sid) + " " + strconv.Itoa(sid) + " 34816 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDetachedCommand finds the command below sudo's pty monitor and setsid(1),
// and the command sudo started directly when there is no pty.
func TestDetachedCommand(t *testing.T) {
	t.Run("under sudo's pty", func(t *testing.T) {
		root := t.TempDir()
		writeStat(t, root, 100, 1, 100, "tui-tool")
		writeStat(t, root, 200, 100, 100, "sudo")     // sudo, in the TUI's session
		writeStat(t, root, 201, 200, 201, "sudo")     // its pty monitor leads a session
		writeStat(t, root, 202, 201, 201, "setsid")   // setsid -w, forked
		writeStat(t, root, 203, 202, 203, "apt-get")  // the command
		writeStat(t, root, 204, 203, 203, "dpkg (x)") // its own child
		writeStat(t, root, 300, 1, 300, "unrelated")  // someone else's session
		if pid, ok := detachedCommand(root, 200); !ok || pid != 203 {
			t.Errorf("detachedCommand = %d, %v, want 203", pid, ok)
		}
	})
	t.Run("without a pty", func(t *testing.T) {
		root := t.TempDir()
		writeStat(t, root, 200, 100, 100, "sudo")
		writeStat(t, root, 210, 200, 210, "journalctl")
		if pid, ok := detachedCommand(root, 200); !ok || pid != 210 {
			t.Errorf("detachedCommand = %d, %v, want 210", pid, ok)
		}
	})
	t.Run("nothing to find", func(t *testing.T) {
		root := t.TempDir()
		writeStat(t, root, 200, 100, 100, "sudo")
		writeStat(t, root, 201, 200, 201, "sudo")
		if pid, ok := detachedCommand(root, 200); ok {
			t.Errorf("detachedCommand = %d, want none", pid)
		}
		if _, ok := detachedCommand(root, 999); ok {
			t.Error("found a command under a process that does not exist")
		}
		if _, ok := detachedCommand(filepath.Join(root, "missing"), 200); ok {
			t.Error("found a command without a /proc")
		}
	})
}

// TestPasswordMessage: a refusal for want of a password, in sudo's words or
// sudo-rs's, says what works, and only mentions `sudo -v` when it can help.
func TestPasswordMessage(t *testing.T) {
	cmd := Command{Argv: []string{"toolctl", "reload"}}
	failed := errors.New("exit status 1")
	withSetsid := &Runner{Name: "toolctl", Bin: "/usr/sbin/toolctl",
		Privilege: []string{"/usr/bin/sudo", "-n"}, Detach: []string{"/usr/bin/setsid", "-w"}}
	without := &Runner{Name: "toolctl", Bin: "/usr/sbin/toolctl",
		Privilege: []string{"/usr/bin/sudo", "-n"}}
	for _, output := range []string{
		"sudo: a password is required",
		"sudo: interactive authentication is required",
	} {
		got := withSetsid.wrapErr(cmd, output, failed, 0).Error()
		if !strings.Contains(got, "run `sudo -v` in this terminal") ||
			strings.Contains(got, "another terminal") {
			t.Errorf("with setsid, %q -> %q", output, got)
		}
		got = without.wrapErr(cmd, output, failed, 0).Error()
		if !strings.Contains(got, "start the tool with sudo") {
			t.Errorf("without setsid, %q -> %q", output, got)
		}
	}
	// Any other failure still names the command.
	if got := withSetsid.wrapErr(cmd, "toolctl: no such rule", failed, 0).Error(); !strings.Contains(got, "toolctl reload") {
		t.Errorf("other failure = %q", got)
	}
}
