// Package runner is the trust boundary every tui-tools tool is built around:
// preview the exact command line, let the user confirm it, then run that same
// command line and nothing else.
//
// A tool never assembles a shell string. It builds a Command — an argv plus a
// human description — hands it to ui.Confirm for the preview, and gives it
// back to the Runner once the user answered yes. Because Preview and Run
// consume the same value, the text in the dialog is guaranteed to be what
// executes.
//
// Privilege escalation is part of that contract. The Runner resolves the
// configured prefix ("sudo -n") once at construction, so a tool that cannot
// escalate says so at startup instead of failing halfway through a change.
//
// No child gets a terminal. Every process the Runner starts, read or mutation,
// escalated or not, leads a session of its own with no controlling terminal,
// so nothing it runs can prompt on a terminal hidden behind the TUI: a program
// that opens /dev/tty fails at once instead of waiting forever. A step that
// genuinely needs the terminal is a hand-off (tea.Exec), not a runner step.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ErrNotAvailable reports that the runner cannot drive this host: the target
// binary is missing, or the configured escalation command is not installed.
// Errors wrapping it carry a message meant to be shown to the user verbatim.
var ErrNotAvailable = errors.New("command not available")

// DefaultTimeout bounds one read, so a stuck query cannot freeze the UI
// behind it.
//
// It bounds reads only. A mutation (Run) has no wall-clock timeout unless the
// runner is given one (Options.MutationTimeout): a mutation is the command the
// user confirmed, and killing it halfway is worse than letting it finish. A
// package manager killed during a download or a transaction leaves its lock
// behind (pacman's db.lck, dpkg's lock, a half-configured package, a stale dnf
// transaction), and every later call fails until someone cleans up by hand.
// That happened for real: a `pacman -Syu` downloading a few dozen packages was
// killed at 15 s. A mutation stops only when the caller cancels its context,
// and even then it is asked to stop (SIGTERM, see MutationGrace) before it is
// killed.
const DefaultTimeout = 15 * time.Second

// MutationGrace is how long a cancelled mutation is given to stop on its own
// after SIGTERM before it is killed. sudo relays the signal to the command, and
// pacman, apt and dnf all clean up their locks on SIGTERM; SIGKILL leaves them.
const MutationGrace = 10 * time.Second

// Command is a single invocation the user is about to run. Argv excludes any
// privilege wrapper: the Runner adds it when previewing and when executing.
// Argv[0] is the tool's own name ("ufw", "systemctl"); the Runner replaces it
// with the resolved absolute path.
type Command struct {
	Argv        []string
	Description string
	// Destructive marks a command that can lock the user out or drop state,
	// so the confirm dialog can paint itself in the danger color.
	Destructive bool
	// Stdin is written to the process's standard input and closed. It exists
	// for the commands whose input must never appear in an argv — `chpasswd`
	// reads "user:password" from stdin, `tee -a` reads the line it appends —
	// because a command line is visible in `ps` to every user on the machine.
	//
	// It is deliberately absent from String and from Preview: what the confirm
	// dialog shows is the command line, and a secret has no business being
	// rendered anywhere. A tool that wants to say something about the input
	// says it in the dialog's own body.
	Stdin string
	// Env holds NAME=value variables set for this command alone, on top of the
	// runner's own (Options.Env). Unlike those, they are part of what the
	// user confirms, so they appear in the preview: as assignments before the
	// command when it runs directly, and through `env` after the escalation
	// prefix when it is escalated, because sudo resets the caller's
	// environment and would silently drop them.
	Env []string
}

// String renders the command the way the user reads it in the preview: its
// variables as shell assignments, then each argument shell-quoted when it
// needs it (see Join), so the line pasted into a shell runs the same argv in
// the same environment.
func (c Command) String() string {
	if len(c.Env) == 0 {
		return Join(c.Argv)
	}
	return joinEnv(c.Env) + " " + Join(c.Argv)
}

// joinEnv renders NAME=value variables as shell assignments, quoting only the
// value, so they read as assignments before a command and as arguments to
// `env` after an escalation prefix.
func joinEnv(env []string) string {
	out := make([]string, len(env))
	for i, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			out[i] = Quote(kv)
			continue
		}
		out[i] = name + "=" + Quote(value)
	}
	return strings.Join(out, " ")
}

// Interface is the part of a Runner a UI needs. Tools depend on it so a fake
// can stand in for the real host in tests and in --demo mode.
type Interface interface {
	// Preview renders the exact command line Run will execute.
	Preview(cmd Command) string
	// Run executes a previously previewed command and returns its output.
	Run(ctx context.Context, cmd Command) (string, error)
}

// Options describes the binary a Runner drives.
type Options struct {
	// Bin is the command name, as it appears in Command.Argv[0]. Required.
	Bin string
	// SearchPaths are absolute fallbacks tried when Bin is not on PATH.
	// Administrative tools commonly live in an sbin directory a non-root
	// PATH does not carry.
	SearchPaths []string
	// SudoPrefix is the escalation command line, split into argv
	// ("sudo", "-n"). Empty runs Bin directly.
	SudoPrefix []string
	// Timeout bounds one read (Read); zero uses DefaultTimeout.
	Timeout time.Duration
	// MutationTimeout bounds one mutation (Run, privileged or not). Zero, the
	// default, means no wall-clock limit: a mutation ends when it finishes or
	// when the caller cancels its context. See DefaultTimeout for why. Set it
	// only for a mutation that can legitimately hang and is safe to stop.
	MutationTimeout time.Duration
	// PrivilegedReads reports whether Read also needs escalation. It
	// defaults to true, which is the safe answer: a tool whose reads work
	// unprivileged (systemctl) sets it to false explicitly.
	PrivilegedReads *bool
	// Env adds variables to the child environment. It defaults to
	// LANG=C and LC_ALL=C, which keeps a tool's output in the English form
	// the parsers expect.
	Env []string
	// InstallHint is appended to the "not found" error, so the message can
	// name the package to install.
	InstallHint string
}

// Runner executes previewed commands against one binary.
type Runner struct {
	// Bin is the resolved absolute path of the target binary.
	Bin string
	// Name is the command name as the user knows it.
	Name string
	// Privilege is the resolved escalation prefix; nil when running directly.
	Privilege []string
	// Timeout bounds each read.
	Timeout time.Duration
	// MutationTimeout bounds each mutation; zero means none.
	MutationTimeout time.Duration

	privilegedReads bool
	env             []string
}

// Available reports whether a binary can be found, without building a Runner.
// Backend selection uses it to tell "installed" from "usable".
func Available(bin string, searchPaths ...string) bool {
	_, err := look(bin, searchPaths, "")
	return err == nil
}

// look resolves a binary on PATH, then in the explicit fallbacks.
func look(bin string, searchPaths []string, hint string) (string, error) {
	if path, err := exec.LookPath(bin); err == nil {
		return path, nil
	}
	for _, candidate := range searchPaths {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	message := fmt.Sprintf("%%w: the %s command was not found", bin)
	if hint != "" {
		message += "; " + hint
	}
	return "", fmt.Errorf(message, ErrNotAvailable)
}

// New resolves the binary and the escalation prefix. It fails when the binary
// is missing, or when escalation is configured, needed and unavailable.
func New(opts Options) (*Runner, error) {
	if opts.Bin == "" {
		return nil, fmt.Errorf("runner: Options.Bin is required")
	}
	bin, err := look(opts.Bin, opts.SearchPaths, opts.InstallHint)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	env := opts.Env
	if env == nil {
		env = []string{"LANG=C", "LC_ALL=C"}
	}
	privilegedReads := true
	if opts.PrivilegedReads != nil {
		privilegedReads = *opts.PrivilegedReads
	}

	r := &Runner{
		Bin:             bin,
		Name:            opts.Bin,
		Timeout:         timeout,
		MutationTimeout: max(opts.MutationTimeout, 0),
		privilegedReads: privilegedReads,
		env:             env,
	}
	// Running as root, or with escalation switched off, needs no prefix.
	if os.Geteuid() == 0 || len(opts.SudoPrefix) == 0 {
		return r, nil
	}
	resolved, err := exec.LookPath(opts.SudoPrefix[0])
	if err != nil {
		return nil, fmt.Errorf(
			"%w: not running as root and %q was not found; re-run with sudo, "+
				"or use --demo to explore the UI",
			ErrNotAvailable, opts.SudoPrefix[0])
	}
	r.Privilege = append([]string{resolved}, opts.SudoPrefix[1:]...)
	return r, nil
}

// Privileged reports whether commands are wrapped in an escalation prefix.
func (r *Runner) Privileged() bool { return len(r.Privilege) > 0 }

// Describe is the one-line summary a tool shows in its header: the binary and
// how it is reached.
func (r *Runner) Describe() string {
	if !r.Privileged() {
		return r.Name + " (root)"
	}
	return r.Name + " via " + strings.Join(r.Privilege, " ")
}

// Preview renders the exact command line Run will execute. This is the text
// the confirm dialog shows, and the only promise the tool makes to the user.
func (r *Runner) Preview(cmd Command) string {
	if !r.Privileged() {
		return cmd.String()
	}
	if len(cmd.Env) > 0 {
		// The variables are set by env(1) after escalation; see Command.Env.
		return Join(r.Privilege) + " env " + cmd.String()
	}
	return Join(r.Privilege) + " " + cmd.String()
}

// argv builds the invocation: the resolved binary, the command's own
// arguments, and the privilege prefix when the call needs it. A command with
// variables of its own goes through `env` after the prefix, so escalation
// cannot drop them; run directly, it gets them in its environment instead
// (see exec).
func (r *Runner) argv(cmd Command, privileged bool) (bin string, args []string) {
	rest := cmd.Argv
	// Argv[0] names the tool; the resolved path replaces it.
	if len(rest) > 0 && rest[0] == r.Name {
		rest = rest[1:]
	}
	if !privileged || !r.Privileged() {
		return r.Bin, rest
	}
	args = append([]string{}, r.Privilege[1:]...)
	if len(cmd.Env) > 0 {
		args = append(append(args, "env"), cmd.Env...)
	}
	return r.Privilege[0], append(append(args, r.Bin), rest...)
}

// Run executes a previewed command with escalation. It is a mutation: no
// wall-clock timeout applies unless MutationTimeout is set, and cancelling ctx
// sends SIGTERM first, SIGKILL only after MutationGrace.
func (r *Runner) Run(ctx context.Context, cmd Command) (string, error) {
	return r.exec(ctx, cmd, true, r.MutationTimeout, true)
}

// Read runs a read-only invocation. It escalates only when the runner was
// built with PrivilegedReads (the default): `ufw status` needs root, while
// `systemctl list-units` does not.
func (r *Runner) Read(ctx context.Context, argv ...string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return r.exec(ctx, Command{Argv: argv}, r.privilegedReads, timeout, false)
}

// exec runs one invocation and returns its combined output, trimmed. A zero
// timeout means no deadline beyond the caller's context. A mutation is stopped
// gently: SIGTERM when the context ends, SIGKILL only after MutationGrace.
func (r *Runner) exec(ctx context.Context, cmd Command, privileged bool,
	timeout time.Duration, mutation bool) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	bin, args := r.argv(cmd, privileged)
	c := exec.CommandContext(ctx, bin, args...) //nolint:gosec // argv is built here, never from a shell string
	// Every child, read or mutation, escalated or not, starts in a session of
	// its own and so has no controlling terminal. Pipes and /dev/null on
	// stdio are not enough: a child left in the TUI's session inherits its
	// terminal, and sudo with `Defaults use_pty` (Ubuntu's sudoers, sudo-rs)
	// even hands the command a fresh pty. debconf, whiptail, a password
	// prompt, an editor or a pager then opens /dev/tty and waits there,
	// invisible behind the TUI, forever. Without a controlling terminal that
	// open fails at once (ENXIO) and the step fails with an error instead.
	// A step that genuinely needs the terminal is not a runner step: it is a
	// hand-off (tea.Exec), which gives the terminal away on purpose.
	c.SysProcAttr = detachedSession()
	if mutation {
		c.Cancel = func() error { return c.Process.Signal(syscall.SIGTERM) }
		c.WaitDelay = MutationGrace
	}
	c.Env = append(os.Environ(), r.env...)
	if !privileged || !r.Privileged() {
		// Escalated, the variables travel through env(1) in the argv.
		c.Env = append(c.Env, cmd.Env...)
	}
	if cmd.Stdin != "" {
		c.Stdin = strings.NewReader(cmd.Stdin)
	}
	out, err := c.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		// A process killed by the context deadline reports "signal: killed",
		// not the context error, so the deadline is checked separately.
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
		return text, r.wrapErr(cmd, text, err, timeout)
	}
	return text, nil
}

// wrapErr turns an exec failure into a message worth putting in a status line:
// one line, naming the command the user saw in the preview.
func (r *Runner) wrapErr(cmd Command, output string, err error,
	timeout time.Duration) error {
	preview := r.Preview(cmd)
	if errors.Is(err, context.DeadlineExceeded) {
		if timeout <= 0 {
			// The caller's own deadline, not one the runner set.
			return fmt.Errorf("`%s` ran past the caller's deadline", preview)
		}
		return fmt.Errorf("`%s` timed out after %s", preview, timeout)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("`%s` was cancelled", preview)
	}
	if r.Privileged() && strings.Contains(output, "password is required") {
		return fmt.Errorf(
			"sudo needs a password: run `sudo -v` in another terminal, then retry")
	}
	if output != "" {
		return fmt.Errorf("`%s` failed: %s", preview, FirstLine(output))
	}
	return fmt.Errorf("`%s` failed: %w", preview, err)
}

// FirstLine keeps a status-line message to a single line.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
