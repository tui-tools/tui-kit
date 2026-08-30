// Package report renders the block a bug report needs: the tool and kit
// versions, the backend and its version, and the handful of facts about the
// machine that decide whether a bug is reproducible — the distribution, the
// kernel, the terminal, the locale, whether the binary came from a package.
//
// The tool passes what only it knows (its name, its version, the backend it
// selected, the theme and sudo prefix it resolved) and the kit collects the
// rest. One `--report` flag per tool, one shape of output for the whole
// family, so an issue on any repository can be read the same way.
//
// # Privacy
//
// A bug report is pasted into a public issue, so this package is deliberate
// about what it will not print. It never renders the hostname, the user name,
// a home directory, a network address, or any environment variable beyond the
// four named here (LANG, LC_ALL, TERM, TERM_PROGRAM). The executable path is
// printed only when it lies outside /home and /root; anywhere else it is
// replaced by a placeholder, because that path carries the user name. Values
// handed in by the caller are flattened to a single line so nothing can smuggle
// extra keys into the block.
//
// # No exec
//
// Nothing here starts a process. The family's exec boundary allows that only in
// tui-kit's runner and in a tool's internal/<backend>/, and a report is not
// worth an exception: the kernel comes from the uname syscall, the distribution
// from /etc/os-release, the rest from the process's own environment.
package report

import (
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/tui-tools/tui-kit/pkgmgr"
)

// unknown is what a fact the machine would not answer renders as. It is
// printed rather than omitted: a missing line reads as an oversight, an
// explicit "unknown" reads as an answer.
const unknown = "unknown"

// redactedPath replaces an executable path that would name the user. It looks
// like a placeholder on purpose — a reader has to see that something was left
// out, not guess that the tool failed to find its own binary.
const redactedPath = "~elsewhere~"

// kitModule is the module path whose version is reported as the kit version.
const kitModule = "github.com/tui-tools/tui-kit"

// Info is what the tool knows and the kit cannot work out for itself.
//
// Everything else in the block is collected here. A zero field is not an
// error: it renders as "unknown", or the line is dropped when the fact does
// not apply (a tool with no backend, a tool that never escalates).
type Info struct {
	// Tool is the binary name, as it appears in the manifest ("tui-firewall").
	Tool string
	// Version is the tool's own version, stamped by the release build.
	Version string
	// Backend is the backend that was selected ("ufw", "systemd"), or "demo"
	// when the tool is running against its fake.
	Backend string
	// BackendVersion is what the compat probe read, empty when it could not
	// be read.
	BackendVersion string
	// BackendDetail is why the version is unknown, when it is. It comes
	// straight from compat.Result.Detail and is rendered only when there is
	// no version.
	BackendDetail string
	// Demo reports that the tool is running against sample data, which is the
	// single most useful thing to know about a report whose numbers look
	// impossible.
	Demo bool
	// Sudo is the configured privilege escalation prefix ("sudo -n"), empty
	// when escalation is disabled.
	Sudo string
	// Theme is the name of the active palette (theme.Theme.Palette.Name).
	Theme string
	// Extra are tool-specific lines, appended in the order given, after
	// everything the kit collected. Use it for the one fact a tool cannot do
	// without — never for anything that names the machine or its user.
	Extra []Field
}

// Field is one `key: value` line of the block.
type Field struct {
	Key   string
	Value string
}

// Render returns the report block: plain text, one `key: value` per line, in a
// fixed order, with no color and no trailing whitespace. It ends with a
// newline, so a caller prints it as it is.
//
// The block is meant to be pasted verbatim into an issue — the family's
// bug_report.yml has a shell-rendered field for exactly this — so the format
// stays stable across releases and every line is safe to publish. See the
// package comment for what is deliberately left out.
func Render(info Info) string {
	var b strings.Builder
	b.WriteString(headline(info))
	b.WriteByte('\n')
	for _, f := range fields(info) {
		b.WriteString(f.Key)
		b.WriteString(": ")
		b.WriteString(f.Value)
		b.WriteByte('\n')
	}
	return b.String()
}

// headline is the first line: what ran, and what it was built against. It is
// the one line that is not a key/value pair, because it is the line a
// maintainer reads first.
func headline(info Info) string {
	tool := oneLine(info.Tool)
	if tool == "" {
		tool = unknown
	}
	version := oneLine(info.Version)
	if version == "" {
		version = unknown
	}
	return tool + " " + version + " (kit " + KitVersion() + ")"
}

// fields builds the body in its fixed order.
func fields(info Info) []Field {
	distro := pkgmgr.DetectDistro()
	kernelRelease, machine := uname()

	out := []Field{
		{"backend", backendLine(info)},
		{"mode", modeLine(info)},
		{"distro", distroLine(distro)},
		{"kernel", fallback(kernelRelease)},
		{"arch", fallback(firstNonEmpty(machine, runtime.GOARCH))},
		{"locale", fallback(localeLine())},
		{"term", fallback(termLine())},
		{"theme", fallback(oneLine(info.Theme))},
		{"sudo", sudoLine(info)},
		{"root", yesNo(os.Geteuid() == 0)},
		{"binary", binaryLine()},
	}
	for _, f := range info.Extra {
		key := oneLine(f.Key)
		if key == "" {
			continue
		}
		out = append(out, Field{key, fallback(oneLine(f.Value))})
	}
	return out
}

// backendLine names the backend and, when the probe read one, its version.
// A backend whose version could not be read carries the probe's own reason
// rather than a bare "unknown", because "ufw is not installed" and "ufw
// printed something we could not parse" are different bugs.
func backendLine(info Info) string {
	name := oneLine(info.Backend)
	if name == "" {
		return unknown
	}
	if v := oneLine(info.BackendVersion); v != "" {
		return name + " " + v
	}
	if d := oneLine(info.BackendDetail); d != "" {
		return name + " (version unknown: " + d + ")"
	}
	return name + " (version unknown)"
}

// modeLine says whether the numbers above came from the machine or from a
// fake. It is a line of its own, rather than a note on the backend, so it
// cannot be missed.
func modeLine(info Info) string {
	if info.Demo {
		return "demo (sample data, the system was not read)"
	}
	return "live"
}

// distroLine renders os-release as "id version (pretty name)", dropping the
// halves the file did not carry.
func distroLine(d pkgmgr.Distro) string {
	id := strings.TrimSpace(d.ID)
	version := strings.TrimSpace(d.VersionID)
	pretty := strings.TrimSpace(d.PrettyName)
	head := strings.TrimSpace(id + " " + version)
	switch {
	case head == "" && pretty == "":
		return unknown
	case head == "":
		return pretty
	case pretty == "":
		return head
	}
	return head + " (" + pretty + ")"
}

// localeLine reports the locale the parsers ran under: a tool that mis-reads a
// number or a date is usually a tool that met a locale nobody tested.
func localeLine() string {
	lang := oneLine(os.Getenv("LANG"))
	lcAll := oneLine(os.Getenv("LC_ALL"))
	switch {
	case lang == "" && lcAll == "":
		return ""
	case lcAll == "":
		return lang
	case lang == "":
		return "LC_ALL=" + lcAll
	}
	return lang + " (LC_ALL=" + lcAll + ")"
}

// termLine reports the terminal type and, when the emulator announces itself,
// which emulator it was. Rendering bugs are almost always one or the other.
func termLine() string {
	term := oneLine(os.Getenv("TERM"))
	program := oneLine(os.Getenv("TERM_PROGRAM"))
	switch {
	case term == "" && program == "":
		return ""
	case program == "":
		return term
	case term == "":
		return program
	}
	return term + " (" + program + ")"
}

// sudoLine reports the escalation prefix as configured. It is a configuration
// value, not a credential: it says how the tool would ask, never what it was
// answered.
func sudoLine(info Info) string {
	if s := oneLine(info.Sudo); s != "" {
		return s
	}
	return "(none)"
}

// binaryLine says where the running binary lives and whether that place looks
// like a package manager put it there. The distinction matters more than the
// path: a bug in a hand-built binary from an unknown commit is a different
// conversation from a bug in the published package.
//
// The path is printed only when it is outside the home directories, because a
// path under /home names the user. Nothing is executed to answer this — the
// package that owns the file would cost a process, and the location already
// answers the question that gets asked.
func binaryLine() string {
	path, err := os.Executable()
	if err != nil || path == "" {
		return unknown
	}
	origin := "not from a package"
	if packagedPath(path) {
		origin = "packaged"
	}
	return scrubPath(path) + " (" + origin + ")"
}

// packagedPath reports whether a path is where a distribution package would
// have installed the binary. /usr/local/bin is deliberately not on the list:
// that is where a manual install goes.
func packagedPath(path string) bool {
	return strings.HasPrefix(path, "/usr/bin/") || strings.HasPrefix(path, "/bin/")
}

// scrubPath drops a path that would name the user. Everything under /home and
// /root goes; a relative path goes too, because it is only meaningful next to
// a working directory this block will never carry.
func scrubPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		return redactedPath
	}
	if underDir(path, "/home") || underDir(path, "/root") {
		return redactedPath
	}
	return oneLine(path)
}

// underDir reports whether path is dir itself or something inside it.
func underDir(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+"/")
}

// KitVersion is the version of tui-kit the running binary was built against,
// read from the build information the linker embeds. A binary built with the
// module replaced or with build info stripped reports "unknown" rather than
// guessing.
func KitVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return unknown
	}
	if info.Main.Path == kitModule && info.Main.Version != "" {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path == kitModule {
			return firstNonEmpty(dep.Version, unknown)
		}
	}
	return unknown
}

// oneLine flattens a value to something that cannot break the block's shape:
// no newlines, no carriage returns, no tabs, no surrounding space. A caller
// that hands in a multi-line string gets it collapsed rather than a report
// with invented keys in it.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// fallback renders an empty fact as "unknown", so every key keeps a value.
func fallback(s string) string {
	return firstNonEmpty(s, unknown)
}

// firstNonEmpty returns the first of its arguments that is not empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// yesNo renders a boolean the way the block reads it.
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
