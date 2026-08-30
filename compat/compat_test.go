package compat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeExec answers a probe from a canned output, the way a fake backend does.
func fakeExec(out string, err error) ExecFunc {
	return func(_ context.Context, _ []string) (string, error) {
		return out, err
	}
}

// ufwBackend is the manifest block tui-firewall declares, used as the fixture
// for the whole probe path.
func ufwBackend() Backend {
	return Backend{
		Name:           "ufw",
		Binary:         "ufw",
		VersionCommand: []string{"ufw", "--version"},
		Minimum:        "0.36",
		Tested:         []string{"0.36.1", "0.36.2"},
		Notes: []Note{
			{Range: "<0.36", Impact: "no numbered status, rules cannot be deleted by number"},
			{Range: "0.36.x", Impact: "the app profile column is absent"},
		},
	}
}

func TestProbeTested(t *testing.T) {
	got := ProbeWith(context.Background(), ufwBackend(),
		fakeExec("ufw 0.36.2\n", nil))

	if got.Version != "0.36.2" {
		t.Errorf("version = %q, want 0.36.2", got.Version)
	}
	if got.Status != StatusTested {
		t.Errorf("status = %v, want tested", got.Status)
	}
	if len(got.Notes) != 1 || !strings.Contains(got.Notes[0].Impact, "app profile") {
		t.Errorf("notes = %+v, want only the 0.36.x one", got.Notes)
	}
}

func TestProbeUntested(t *testing.T) {
	got := ProbeWith(context.Background(), ufwBackend(),
		fakeExec("ufw 0.37.0", nil))
	if got.Status != StatusUntested {
		t.Errorf("status = %v, want untested", got.Status)
	}
	if len(got.Notes) != 0 {
		t.Errorf("0.37.0 matches no note, got %+v", got.Notes)
	}
}

func TestProbeBelowMinimum(t *testing.T) {
	got := ProbeWith(context.Background(), ufwBackend(),
		fakeExec("ufw 0.35", nil))
	if got.Status != StatusBelowMinimum {
		t.Errorf("status = %v, want below-minimum", got.Status)
	}
	if got.Minimum != "0.36" {
		t.Errorf("minimum = %q, want 0.36", got.Minimum)
	}
	if len(got.Notes) != 1 || !strings.Contains(got.Notes[0].Impact, "numbered") {
		t.Errorf("notes = %+v, want the <0.36 one", got.Notes)
	}
}

func TestProbeMissingBinary(t *testing.T) {
	got := ProbeWith(context.Background(), ufwBackend(),
		fakeExec("", errors.New("command not available: the ufw command was not found")))

	if got.Status != StatusUnknown {
		t.Errorf("status = %v, want unknown", got.Status)
	}
	if got.Version != "" {
		t.Errorf("version = %q, want empty", got.Version)
	}
	if !strings.Contains(got.Detail, "not found") {
		t.Errorf("detail = %q, want the exec failure", got.Detail)
	}
}

func TestProbeUnparsableOutput(t *testing.T) {
	got := ProbeWith(context.Background(), ufwBackend(),
		fakeExec("ufw: unrecognised option", nil))
	if got.Status != StatusUnknown {
		t.Errorf("status = %v, want unknown", got.Status)
	}
	if !strings.Contains(got.Detail, "ufw --version") {
		t.Errorf("detail = %q, want it to name the command", got.Detail)
	}
}

// A binary that prints its version and exits non-zero is still readable, and a
// probe must not throw the output away over the exit code.
func TestProbeOutputDespiteError(t *testing.T) {
	got := ProbeWith(context.Background(), ufwBackend(),
		fakeExec("ufw 0.36.1", errors.New("exit status 1")))
	if got.Status != StatusTested || got.Version != "0.36.1" {
		t.Errorf("got %+v, want the version read despite the error", got)
	}
}

func TestProbeNoVersionCommand(t *testing.T) {
	got := ProbeWith(context.Background(), Backend{Name: "none"}, fakeExec("x", nil))
	if got.Status != StatusUnknown || got.Detail == "" {
		t.Errorf("got %+v, want unknown with a detail", got)
	}
}

func TestCaps(t *testing.T) {
	backend := Backend{
		Name:           "systemd",
		VersionCommand: []string{"systemctl", "--version"},
		Features: []Feature{
			{Name: "timers", Since: "250"},
			{Name: "boot-blame", Since: "230"},
		},
	}

	caps := ProbeWith(context.Background(), backend,
		fakeExec("systemd 257 (257.2-1-arch)", nil)).Caps()
	if !caps.Has("timers") {
		t.Error("257 has timers")
	}
	if !caps.Has("anything-undeclared") {
		t.Error("an undeclared feature is assumed present")
	}

	old := ProbeWith(context.Background(), backend,
		fakeExec("systemd 249", nil)).Caps()
	if old.Has("timers") {
		t.Error("249 has no JSON timers")
	}
	if !old.Has("boot-blame") {
		t.Error("249 still has boot-blame")
	}
	if since, ok := old.Since("timers"); !ok || since != "250" {
		t.Errorf("Since(timers) = %q, %v; want 250, true", since, ok)
	}

	// An unreadable version must not hide working views.
	unknown := ProbeWith(context.Background(), backend,
		fakeExec("", errors.New("nope"))).Caps()
	if !unknown.Has("timers") {
		t.Error("an unknown version is treated as capable")
	}
}

func TestResultJSONCarriesTheStatusName(t *testing.T) {
	got := ProbeWith(context.Background(), ufwBackend(), fakeExec("ufw 0.37", nil))
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["status"] != "untested" {
		t.Errorf("status = %v, want untested", decoded["status"])
	}
	if decoded["version"] != "0.37" {
		t.Errorf("version = %v, want 0.37", decoded["version"])
	}
}
