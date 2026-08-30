// Package compat answers one question every tool in this family is asked and
// none of them could answer: which version of the backend am I driving, and
// has anyone ever run me against it?
//
// The facts live in the tool's manifest — `backends[]` in tool.json — so the
// README, the website and the running binary cannot disagree. At startup a
// tool probes the backend once (see Probe), gets a Result, and shows it in its
// header through ui.CompatFact:
//
//	ufw 0.36.2              tested
//	ufw 0.37.0 (untested)   never seen by the test lab
//	ufw 0.35 (below minimum 0.36)
//
// The same Result carries the notes that apply to that version and a Caps set
// built from the backend's declared features, so a tool gates a view on
// caps.Has("timers") instead of on a version number written into the code.
//
// Nothing here is allowed to fail a tool. A missing binary, a hung process or
// output nobody can parse all end as StatusUnknown, which the header renders
// as a plain backend name.
package compat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/tui-tools/tui-kit/runner"
)

// ProbeTimeout bounds the single `--version` call. It is short because it runs
// on the startup path, before the first frame is drawn.
const ProbeTimeout = 2 * time.Second

// Note is a caveat that applies to a range of backend versions: what the range
// is, and what it does to the tool.
type Note struct {
	// Range is a constraint in the small grammar Match understands
	// ("<0.11", ">=250", "0.36.x").
	Range string `json:"range"`
	// Impact is one sentence naming what changes for the user.
	Impact string `json:"impact"`
}

// Feature is a backend capability that appeared in a known version. Tools ask
// for it by name through Caps rather than comparing numbers inline.
type Feature struct {
	// Name is the identifier a tool passes to Caps.Has ("timers").
	Name string `json:"name"`
	// Since is the first backend version that has it ("250").
	Since string `json:"since"`
}

// Backend mirrors one entry of `backends[]` in the tool manifest.
type Backend struct {
	// Name is how the backend is known to the user ("ufw", "systemd").
	Name string `json:"name"`
	// Binary is the executable that is probed ("ufw", "systemctl").
	Binary string `json:"binary"`
	// VersionCommand is the argv that prints the version, including argv[0].
	VersionCommand []string `json:"versionCommand"`
	// VersionRegex optionally extracts the version from that output. Empty
	// takes the first version-shaped token.
	VersionRegex string `json:"versionRegex,omitempty"`
	// Minimum is the oldest version the tool claims to work with.
	Minimum string `json:"minimum,omitempty"`
	// Tested is the list of versions a real run passed against. It is
	// generated from compat/results.jsonl by tui-kit/tools/compat-sync.py.
	Tested []string `json:"tested,omitempty"`
	// Notes are the version-ranged caveats.
	Notes []Note `json:"notes,omitempty"`
	// Features are the capabilities Caps answers for.
	Features []Feature `json:"features,omitempty"`
	// SearchPaths are absolute fallbacks for a binary a plain PATH misses.
	SearchPaths []string `json:"searchPaths,omitempty"`
}

// Status is the verdict on the version that was found.
type Status int

// The four possible verdicts. Unknown is the zero value, because "we could not
// tell" is what an unprobed Result means.
const (
	// StatusUnknown: the binary is missing, the call failed, or the output
	// carried nothing version-shaped.
	StatusUnknown Status = iota
	// StatusTested: the version is in the manifest's tested list.
	StatusTested
	// StatusUntested: a usable version nobody has run the suite against.
	StatusUntested
	// StatusBelowMinimum: older than the manifest's minimum.
	StatusBelowMinimum
)

// String names the status the way the JSON of --check reports it.
func (s Status) String() string {
	switch s {
	case StatusTested:
		return "tested"
	case StatusUntested:
		return "untested"
	case StatusBelowMinimum:
		return "below-minimum"
	default:
		return "unknown"
	}
}

// Result is what a probe found.
type Result struct {
	// Backend is the backend's name, echoed so a Result stands alone.
	Backend string `json:"backend"`
	// Version is the detected version, empty when it could not be read.
	Version string `json:"version,omitempty"`
	// Status is the verdict.
	Status Status `json:"-"`
	// Minimum is the manifest's minimum, echoed for the badge.
	Minimum string `json:"minimum,omitempty"`
	// Notes are the manifest notes whose range covers Version.
	Notes []Note `json:"notes,omitempty"`
	// Detail is why the version is unknown, when it is. It is never fatal:
	// a tool shows it at most as a muted hint.
	Detail string `json:"detail,omitempty"`

	caps Caps
}

// MarshalJSON writes the status as its name, so `--check` output reads as
// "status": "untested" rather than an integer nobody can interpret.
func (r Result) MarshalJSON() ([]byte, error) {
	type alias Result
	return json.Marshal(struct {
		alias
		Status string `json:"status"`
	}{alias(r), r.Status.String()})
}

// Caps returns the capability set for the detected version.
func (r Result) Caps() Caps { return r.caps }

// ExecFunc runs the version command and returns its output. It exists so the
// probe can be tested without a real process; production code uses the
// runner, which is the only place in the kit allowed to exec.
type ExecFunc func(ctx context.Context, argv []string) (string, error)

// Probe runs the backend's version command once and classifies what came back.
// It never returns an error: everything that can go wrong is a Result with
// StatusUnknown and a Detail explaining it.
func Probe(ctx context.Context, b Backend) Result {
	return ProbeWith(ctx, b, RunnerExec(b.SearchPaths))
}

// ProbeWith is Probe against an explicit runner, for tests and for a tool that
// already holds a resolved runner for the same binary.
func ProbeWith(ctx context.Context, b Backend, exec ExecFunc) Result {
	result := Result{Backend: b.Name, Minimum: b.Minimum}
	result.caps = Caps{features: b.Features}

	if exec == nil || len(b.VersionCommand) == 0 {
		result.Detail = "no version command is declared for this backend"
		return result
	}

	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	out, err := exec(ctx, b.VersionCommand)
	if err != nil && strings.TrimSpace(out) == "" {
		result.Detail = runner.FirstLine(err.Error())
		return result
	}
	// Some tools print their version and exit non-zero (a usage banner on an
	// unknown flag); the output is still what we want, so it is parsed either
	// way and only an empty one is a failure.
	version := ParseVersion(out, b.VersionRegex)
	if version == "" {
		result.Detail = "could not read a version from `" +
			strings.Join(b.VersionCommand, " ") + "`"
		return result
	}

	result.Version = version
	result.caps.version = version
	result.Status = classify(version, b)
	result.Notes = notesFor(version, b.Notes)
	return result
}

// classify decides the verdict for a detected version.
func classify(version string, b Backend) Status {
	if b.Minimum != "" && Compare(version, b.Minimum) < 0 {
		return StatusBelowMinimum
	}
	for _, tested := range b.Tested {
		if Compare(version, tested) == 0 {
			return StatusTested
		}
	}
	return StatusUntested
}

// notesFor keeps the notes whose range covers the detected version.
func notesFor(version string, notes []Note) []Note {
	var out []Note
	for _, n := range notes {
		if Match(version, n.Range) {
			out = append(out, n)
		}
	}
	return out
}

// RunnerExec is the production ExecFunc: one unprivileged read through the
// kit's runner, which resolves the binary on PATH and in the declared
// fallbacks. Reading a version never escalates.
func RunnerExec(searchPaths []string) ExecFunc {
	return func(ctx context.Context, argv []string) (string, error) {
		if len(argv) == 0 {
			return "", errNoCommand
		}
		unprivileged := false
		r, err := runner.New(runner.Options{
			Bin:             argv[0],
			SearchPaths:     searchPaths,
			Timeout:         ProbeTimeout,
			PrivilegedReads: &unprivileged,
		})
		if err != nil {
			return "", err
		}
		return r.Read(ctx, argv...)
	}
}

// errNoCommand is returned for an empty version command, which a manifest
// should never carry but a hand-built Backend might.
var errNoCommand = errors.New("no version command")

// Caps is the capability set of one backend version. Build one through Probe;
// the zero value answers true for everything, which is the right default for a
// tool whose manifest declares no features.
type Caps struct {
	version  string
	features []Feature
}

// NewCaps builds a capability set by hand, for tests and for a backend a tool
// configures without a manifest.
func NewCaps(version string, features []Feature) Caps {
	return Caps{version: version, features: features}
}

// Has reports whether the running backend has a feature.
//
// A feature the manifest does not declare is assumed present: the manifest
// lists what is *version-gated*, not everything the backend can do. An unknown
// version is also treated as capable, because hiding a working view on a
// version we failed to parse is worse than letting the backend refuse the
// command with its own message.
func (c Caps) Has(name string) bool {
	for _, f := range c.features {
		if f.Name != name {
			continue
		}
		if f.Since == "" || c.version == "" {
			return true
		}
		return Compare(c.version, f.Since) >= 0
	}
	return true
}

// Since returns the version a feature appeared in, and whether it is declared.
// It is what a tool puts in the message explaining why a view is unavailable.
func (c Caps) Since(name string) (string, bool) {
	for _, f := range c.features {
		if f.Name == name {
			return f.Since, f.Since != ""
		}
	}
	return "", false
}

// Version is the backend version the set was built for, empty when unknown.
func (c Caps) Version() string { return c.version }
