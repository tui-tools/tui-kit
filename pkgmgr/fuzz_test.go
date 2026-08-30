package pkgmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The parsers in this package are the one place where output the tool did not
// write becomes data the tool acts on: a package manager's stdout arrives as
// bytes and leaves as a version the caller compares, a fingerprint the caller
// trusts, or a distribution name. `go test` runs the seeds below on every
// commit, and `go test -fuzz=FuzzParseKeyFingerprint ./pkgmgr` explores past
// them locally — see templates/FUZZING.md for the family rule.
//
// The seeds are the same captured fixtures the table tests use, so the corpus
// starts on the real line shapes and mutates from there instead of guessing
// them.

// seed adds every named testdata file to the corpus, plus the shapes a real
// fixture never contains: nothing, a lone separator, a truncated line.
func seed(f *testing.F, names ...string) {
	f.Helper()
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // the name is a literal in the tests, and testdata is in the repository
		if err != nil {
			f.Fatalf("read fixture %s: %v", name, err)
		}
		f.Add(string(raw))
	}
	f.Add("")
	f.Add("\n\n\n")
	f.Add("|")
	f.Add(":")
}

// checkVersions asserts what every caller of a version map is allowed to
// assume: a key it can name and a version it can compare, never a blank.
func checkVersions(t *testing.T, versions map[string]string) {
	t.Helper()
	for name, version := range versions {
		if name == "" || version == "" {
			t.Fatalf("blank entry %q => %q", name, version)
		}
		if strings.ContainsAny(name, " \t\n") {
			t.Fatalf("package name carries whitespace: %q", name)
		}
	}
}

func FuzzParsePipedVersions(f *testing.F) {
	seed(f, "dpkg-query-tui.txt", "rpm-q-tui.txt", "rpm-q-installed.txt", "dnf-repoquery-tui.txt")
	f.Fuzz(func(t *testing.T, out string) {
		checkVersions(t, ParsePipedVersions(out))
	})
}

func FuzzParsePacmanQuery(f *testing.F) {
	seed(f, "pacman-q-tui.txt")
	f.Fuzz(func(t *testing.T, out string) {
		checkVersions(t, ParsePacmanQuery(out))
	})
}

func FuzzParsePacmanSync(f *testing.F) {
	seed(f, "pacman-si-tui.txt")
	f.Fuzz(func(t *testing.T, out string) {
		checkVersions(t, ParsePacmanSync(out))
	})
}

func FuzzParseAPTPolicy(f *testing.F) {
	seed(f, "apt-cache-policy-tui.txt")
	f.Fuzz(func(t *testing.T, out string) {
		checkVersions(t, ParseAPTPolicy(out))
	})
}

// FuzzParseKeyFingerprint is the one that matters most: whatever it returns
// non-empty is the key the tool is about to trust, so the shape of that
// answer has to hold for any input at all.
func FuzzParseKeyFingerprint(f *testing.F) {
	seed(f, "gpg-show-keys.txt")
	f.Fuzz(func(t *testing.T, out string) {
		got := ParseKeyFingerprint(out)
		if got == "" {
			return
		}
		if !fingerprintRe.MatchString(got) {
			t.Fatalf("returned something that is not a full fingerprint: %q", got)
		}
		if got != strings.ToUpper(got) {
			t.Fatalf("fingerprint is not upper case: %q", got)
		}
	})
}

func FuzzParseOSRelease(f *testing.F) {
	seed(f, "os-release-ubuntu.txt", "os-release-fedora.txt", "os-release-omarchy.txt")
	f.Fuzz(func(t *testing.T, text string) {
		d := ParseOSRelease(text)
		for _, like := range d.Like {
			if like == "" || strings.ContainsAny(like, " \t") {
				t.Fatalf("ID_LIKE entry is not a bare word: %q", like)
			}
		}
		// String() is what the UI prints, so it has to survive any file.
		_ = d.String()
		for _, m := range []Manager{ManagerPacman, ManagerAPT, ManagerDNF} {
			_ = d.Matches(m)
		}
	})
}
