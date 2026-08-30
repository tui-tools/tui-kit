package pkgmgr

import (
	"os"
	"path/filepath"
	"testing"
)

// fixture reads a captured command output.
//
// Every one of these is written by hand against the line shape the command
// documents, with neutral package names: a fixture is a pinned line shape, not
// a snapshot of somebody's machine. rpm-q-installed.txt keeps the "name.arch"
// key and the epoch that tui-update's own fixture exercised, because that
// parser's behaviour has to survive tui-update being switched onto this
// package. Every fixture is pinned by a test that names the shape it asserts.
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // the name is a literal in the tests, and testdata is in the repository
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(raw)
}

// TestParsePipedVersionsKeepsTUIUpdatesBehaviour is the ported assertion: the
// "name.arch" key tui-update's rpm query asks for, and the epoch that comes
// with it. This package asks the same query for a bare name, and one parser
// serves both, which is the whole reason the case is kept here.
func TestParsePipedVersionsKeepsTUIUpdatesBehaviour(t *testing.T) {
	installed := ParsePipedVersions(fixture(t, "rpm-q-installed.txt"))
	if got := installed["bash.x86_64"]; got != "5.2.37-1.fc42" {
		t.Errorf("bash installed = %q", got)
	}
	// rpm's %|EPOCH?{…}| conditional is what keeps the two sides comparable.
	if got := installed["tui-update.x86_64"]; got != "3:0.1.1-1.fc42" {
		t.Errorf("tui-update installed = %q, want the epoch", got)
	}
	// A noarch package is keyed the same way.
	if got := installed["tzdata.noarch"]; got != "2025b-1.fc42" {
		t.Errorf("tzdata installed = %q", got)
	}
	if len(installed) != 10 {
		t.Errorf("parsed %d packages, want 10", len(installed))
	}
}

func TestParsePipedVersionsRPM(t *testing.T) {
	installed := ParsePipedVersions(fixture(t, "rpm-q-tui.txt"))
	if got := installed["tui-firewall"]; got != "0.2.1-1.fc42" {
		t.Errorf("tui-firewall = %q", got)
	}
	if got := installed["tui-update"]; got != "3:0.1.1-1.fc42" {
		t.Errorf("tui-update = %q, want the epoch", got)
	}
	// "package tui-disk is not installed" is rpm's complaint, not an answer.
	if _, ok := installed["tui-disk"]; ok {
		t.Errorf("tui-disk was reported as installed")
	}
	if len(installed) != 3 {
		t.Errorf("parsed %d packages, want 3", len(installed))
	}
}

func TestParsePipedVersionsDpkg(t *testing.T) {
	installed := ParsePipedVersions(fixture(t, "dpkg-query-tui.txt"))
	if got := installed["tui-firewall"]; got != "0.2.1-1" {
		t.Errorf("tui-firewall = %q", got)
	}
	// dpkg-query's "no packages found matching" line shares the stream.
	if len(installed) != 2 {
		t.Errorf("parsed %d packages, want 2: %v", len(installed), installed)
	}
}

func TestParsePipedVersionsDNFRepoquery(t *testing.T) {
	available := ParsePipedVersions(fixture(t, "dnf-repoquery-tui.txt"))
	if got := available["tui-disk"]; got != "0.1.1-1.fc42" {
		t.Errorf("tui-disk = %q", got)
	}
}

func TestParsePacmanQuery(t *testing.T) {
	installed := ParsePacmanQuery(fixture(t, "pacman-q-tui.txt"))
	if got := installed["tui-systemd"]; got != "0.1.1-1" {
		t.Errorf("tui-systemd = %q", got)
	}
	if _, ok := installed["tui-disk"]; ok {
		t.Errorf("pacman's error line was read as a version")
	}
	if len(installed) != 2 {
		t.Errorf("parsed %d packages, want 2", len(installed))
	}
}

func TestParsePacmanSync(t *testing.T) {
	available := ParsePacmanSync(fixture(t, "pacman-si-tui.txt"))
	if got := available["tui-firewall"]; got != "0.2.2-1" {
		t.Errorf("tui-firewall = %q", got)
	}
	if got := available["tui-disk"]; got != "0.1.1-1" {
		t.Errorf("tui-disk = %q", got)
	}
	// The URL line carries a colon too, and is not a field this reads.
	if len(available) != 2 {
		t.Errorf("parsed %d packages, want 2: %v", len(available), available)
	}
}

func TestParseAPTPolicy(t *testing.T) {
	available := ParseAPTPolicy(fixture(t, "apt-cache-policy-tui.txt"))
	if got := available["tui-firewall"]; got != "0.2.2-1" {
		t.Errorf("tui-firewall candidate = %q", got)
	}
	if got := available["tui-disk"]; got != "0.1.1-1" {
		t.Errorf("tui-disk candidate = %q", got)
	}
	// A candidate of (none) means no repository here carries it, which the
	// caller reads as an absent key rather than an empty version.
	if _, ok := available["tui-nope"]; ok {
		t.Errorf("tui-nope was given a candidate")
	}
}

func TestParseKeyFingerprint(t *testing.T) {
	// The primary key's fingerprint is the first fpr: record; the subkey's
	// comes after it and must not win.
	got := ParseKeyFingerprint(fixture(t, "gpg-show-keys.txt"))
	if got != "1111222233334444555566667777888899990000" {
		t.Errorf("fingerprint = %q", got)
	}
	if ParseKeyFingerprint("gpg: no valid OpenPGP data found") != "" {
		t.Errorf("a failed read produced a fingerprint")
	}
}

func TestParseOSRelease(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		id      string
		like    []string
		pretty  string
	}{
		{"os-release-omarchy.txt", "omarchy-server",
			[]string{"omarchy", "arch"}, "Omarchy Server"},
		{"os-release-ubuntu.txt", "ubuntu", []string{"debian"},
			"Ubuntu 24.04.3 LTS"},
		{"os-release-fedora.txt", "fedora", nil,
			"Fedora Linux 42 (Server Edition)"},
	} {
		distro := ParseOSRelease(fixture(t, tc.fixture))
		if distro.ID != tc.id || distro.PrettyName != tc.pretty {
			t.Errorf("%s = %+v", tc.fixture, distro)
		}
		if len(distro.Like) != len(tc.like) {
			t.Errorf("%s ID_LIKE = %v, want %v", tc.fixture, distro.Like, tc.like)
			continue
		}
		for i := range tc.like {
			if distro.Like[i] != tc.like[i] {
				t.Errorf("%s ID_LIKE = %v, want %v", tc.fixture, distro.Like, tc.like)
			}
		}
	}
}
