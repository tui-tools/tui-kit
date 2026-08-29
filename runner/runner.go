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
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNotAvailable reports that the runner cannot drive this host: the target
// binary is missing, or the configured escalation command is not installed.
// Errors wrapping it carry a message meant to be shown to the user verbatim.
var ErrNotAvailable = errors.New("command not available")

// DefaultTimeout bounds one invocation, so a stuck command cannot freeze the
// UI behind it.
const DefaultTimeout = 15 * time.Second

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
}

// String renders the command the way the user reads it in the preview.
func (c Command) String() string { return strings.Join(c.Argv, " ") }

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
	// Timeout bounds one invocation; zero uses DefaultTimeout.
	Timeout time.Duration
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
	// Timeout bounds each invocation.
	Timeout time.Duration

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
	return strings.Join(r.Privilege, " ") + " " + cmd.String()
}

// argv builds the invocation: the resolved binary, the command's own
// arguments, and the privilege prefix when the call needs it.
func (r *Runner) argv(cmd Command, privileged bool) (bin string, args []string) {
	rest := cmd.Argv
	// Argv[0] names the tool; the resolved path replaces it.
	if len(rest) > 0 && rest[0] == r.Name {
		rest = rest[1:]
	}
	if !privileged || !r.Privileged() {
		return r.Bin, rest
	}
	prefix := append([]string{}, r.Privilege[1:]...)
	return r.Privilege[0], append(prefix, append([]string{r.Bin}, rest...)...)
}

// Run executes a previewed command with escalation.
func (r *Runner) Run(ctx context.Context, cmd Command) (string, error) {
	return r.exec(ctx, cmd, true)
}

// Read runs a read-only invocation. It escalates only when the runner was
// built with PrivilegedReads (the default): `ufw status` needs root, while
// `systemctl list-units` does not.
func (r *Runner) Read(ctx context.Context, argv ...string) (string, error) {
	return r.exec(ctx, Command{Argv: argv}, r.privilegedReads)
}

// exec runs one invocation and returns its combined output, trimmed.
func (r *Runner) exec(ctx context.Context, cmd Command, privileged bool) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bin, args := r.argv(cmd, privileged)
	c := exec.CommandContext(ctx, bin, args...) //nolint:gosec // argv is built here, never from a shell string
	c.Env = append(os.Environ(), r.env...)
	out, err := c.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		// A process killed by the context deadline reports "signal: killed",
		// not the context error, so the deadline is checked separately.
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
		return text, r.wrapErr(cmd, text, err)
	}
	return text, nil
}

// wrapErr turns an exec failure into a message worth putting in a status line:
// one line, naming the command the user saw in the preview.
func (r *Runner) wrapErr(cmd Command, output string, err error) error {
	preview := r.Preview(cmd)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("`%s` timed out after %s", preview, r.Timeout)
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
