package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tui-tools/tui-kit/compat"
	"github.com/tui-tools/tui-kit/theme"
)

// plainTheme drops every colour, so the assertions below are about the text
// the badge renders and not about escape sequences.
func plainTheme(t *testing.T) theme.Theme {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	return theme.FromPalette(theme.TokyoNight())
}

// probe builds a Result the way a tool does, from a canned version output.
func probe(output string, err error) compat.Result {
	backend := compat.Backend{
		Name:           "snapper",
		VersionCommand: []string{"snapper", "--version"},
		Minimum:        "0.10.0",
		Tested:         []string{"0.13.1"},
	}
	return compat.ProbeWith(context.Background(), backend,
		func(context.Context, []string) (string, error) { return output, err })
}

func TestCompatBadge(t *testing.T) {
	th := plainTheme(t)

	cases := []struct {
		name   string
		result compat.Result
		want   string
	}{
		{"tested", probe("snapper 0.13.1", nil), "snapper 0.13.1"},
		{"untested", probe("snapper 0.14.0", nil), "snapper 0.14.0 (untested)"},
		{"below minimum", probe("snapper 0.9.9", nil),
			"snapper 0.9.9 (below minimum 0.10.0)"},
		{"unknown", probe("", errors.New("not found")),
			"snapper (version unknown)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompatBadge(th, tc.result); got != tc.want {
				t.Errorf("CompatBadge = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompatFact(t *testing.T) {
	th := plainTheme(t)

	tested := CompatFact(th, probe("snapper 0.13.1", nil))
	if tested.Label != "backend" || tested.Value != "snapper 0.13.1" {
		t.Errorf("tested fact = %+v", tested)
	}
	if tested.Style != nil {
		t.Error("a tested version uses the theme's base style")
	}

	untested := CompatFact(th, probe("snapper 0.14.0", nil))
	if untested.Value != "snapper 0.14.0 (untested)" {
		t.Errorf("untested value = %q", untested.Value)
	}
	if untested.Style == nil {
		t.Error("an untested version is styled")
	}

	// The header must render it without panicking on any of the shapes.
	for _, result := range []compat.Result{
		probe("snapper 0.13.1", nil),
		probe("snapper 0.9.0", nil),
		probe("", errors.New("nope")),
	} {
		out := Header{Title: "tui-snapper", Facts: []Fact{CompatFact(th, result)}}.
			Render(th, 60)
		if !strings.Contains(out, "backend") {
			t.Errorf("header lost the backend fact: %q", out)
		}
	}
}

func TestCompatNotes(t *testing.T) {
	th := plainTheme(t)
	backend := compat.Backend{
		Name:           "snapper",
		VersionCommand: []string{"snapper", "--version"},
		Notes: []compat.Note{
			{Range: "<0.11", Impact: "no --machine-readable, output is parsed as text"},
		},
	}
	old := compat.ProbeWith(context.Background(), backend,
		func(context.Context, []string) (string, error) { return "snapper 0.10.2", nil })
	lines := CompatNotes(th, old)
	if len(lines) != 1 || !strings.Contains(lines[0], "machine-readable") {
		t.Errorf("notes = %v, want the <0.11 caveat", lines)
	}

	current := compat.ProbeWith(context.Background(), backend,
		func(context.Context, []string) (string, error) { return "snapper 0.13.1", nil })
	if lines := CompatNotes(th, current); len(lines) != 0 {
		t.Errorf("a current version has no caveats, got %v", lines)
	}
}
