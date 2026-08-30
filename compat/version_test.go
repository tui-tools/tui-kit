package compat

import (
	"slices"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		pattern string
		want    string
	}{
		{"ufw", "ufw 0.36.2", "", "0.36.2"},
		{"systemd", "systemd 257 (257.2-1-arch)\n+PAM +AUDIT", "", "257"},
		{"snapper", "snapper 0.11.2", "", "0.11.2"},
		{"two parts", "tool version 1.4", "", "1.4"},
		{"pre-release", "tool 2.0.0-rc1 built today", "", "2.0.0-rc1"},
		{"leading noise", "GNU tool (2026) 3.1.4", "", "2026"},
		{"explicit group", "GNU tool (2026) 3.1.4", `\) (\d+\.\d+\.\d+)`, "3.1.4"},
		{"whole match pattern", "build 42", `\d+`, "42"},
		{"broken pattern falls back", "tool 1.2.3", `([`, "1.2.3"},
		{"nothing", "no version here", "", ""},
		{"empty", "   ", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseVersion(tc.output, tc.pattern); got != tc.want {
				t.Errorf("ParseVersion(%q, %q) = %q, want %q",
					tc.output, tc.pattern, got, tc.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.36.2", "0.36.2", 0},
		{"0.36", "0.36.0", 0},
		{"0.36.1", "0.36.2", -1},
		{"0.37", "0.36.9", 1},
		{"250", "249", 1},
		{"250", "250.1", -1},
		{"v1.2.3", "1.2.3", 0},
		{"2.0.0-rc1", "2.0.0", -1},
		{"2.0.0", "2.0.0-rc1", 1},
		{"2.0.0-rc1", "2.0.0-rc2", -1},
		// A dash suffix is read as a pre-release, so a distro's package
		// revision sorts just before the plain release. That is the loose
		// part of "loosely": it never turns 257 into 256.
		{"257.2-1-arch", "257.2", -1},
		{"10.0", "9.9", 1},
	}
	for _, tc := range cases {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		version    string
		constraint string
		want       bool
	}{
		{"0.10.0", "<0.11", true},
		{"0.11.0", "<0.11", false},
		{"0.11.1", ">=0.11", true},
		{"249", ">=250", false},
		{"250", ">=250", true},
		{"0.36.2", "0.36.x", true},
		{"0.37.0", "0.36.x", false},
		{"0.36.2", "0.36.*", true},
		{"0.36", "0.36", true},
		{"0.36.1", "0.36", false},
		{"0.36.2", "", true},
		{"", "<0.11", false},
		{"0.38", ">=0.36,<0.40", true},
		{"0.41", ">=0.36,<0.40", false},
		{"0.36", "!=0.36", false},
		{"0.36", "nonsense", false},
		{"1.2.3", "*", true},
	}
	for _, tc := range cases {
		if got := Match(tc.version, tc.constraint); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v",
				tc.version, tc.constraint, got, tc.want)
		}
	}
}

func TestSortVersions(t *testing.T) {
	got := []string{"0.36.2", "0.11", "257", "0.36", "2.0.0-rc1", "2.0.0"}
	SortVersions(got)
	want := []string{"0.11", "0.36", "0.36.2", "2.0.0-rc1", "2.0.0", "257"}
	if !slices.Equal(got, want) {
		t.Errorf("SortVersions = %v, want %v", got, want)
	}
}
