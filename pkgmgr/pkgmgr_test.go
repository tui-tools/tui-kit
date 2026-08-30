package pkgmgr

import (
	"context"
	"strings"
	"testing"
)

// Both implementations answer the same interface, which is what lets a UI
// hold one of them and --demo hold the other.
var (
	_ Interface = (*Real)(nil)
	_ Interface = (*Fake)(nil)
)

// installed builds an "is this binary here" test from a set of names.
func installed(bins ...string) func(string, ...string) bool {
	set := map[string]bool{}
	for _, bin := range bins {
		set[bin] = true
	}
	return func(bin string, _ ...string) bool { return set[bin] }
}

// TestDetect is the decision table: the binary has to be there, and among the
// ones that are, the distribution decides. The cases that matter are the
// mixed machines — an Ubuntu image carrying rpm, a Fedora image carrying apt —
// where the wrong answer would drive the machine with a manager that does not
// own it.
func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		what    string
		distro  Distro
		present []string
		want    Manager
		wantErr bool
	}{
		{"fedora", Distro{ID: "fedora"}, []string{"dnf", "apt-get"}, ManagerDNF, false},
		{"ubuntu with rpm", Distro{ID: "ubuntu", Like: []string{"debian"}},
			[]string{"apt-get", "dnf"}, ManagerAPT, false},
		{"omarchy server", Distro{ID: "omarchy-server",
			Like: []string{"omarchy", "arch"}}, []string{"pacman"},
			ManagerPacman, false},
		{"a derivative that only names its parent",
			Distro{ID: "steamos", Like: []string{"arch"}},
			[]string{"pacman", "apt-get"}, ManagerPacman, false},
		{"a distribution nobody has heard of",
			Distro{ID: "mydistro"}, []string{"apt-get"}, ManagerAPT, false},
		{"no os-release at all", Distro{}, []string{"dnf"}, ManagerDNF, false},
		{"nothing installed", Distro{ID: "fedora"}, nil, "", true},
	} {
		got, err := detect(tc.distro, installed(tc.present...))
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: no error, got %q", tc.what, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.what, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.what, got, tc.want)
		}
	}
}

// TestDistroMatches: the ID is a stronger claim than ID_LIKE, and both are
// consulted.
func TestDistroMatches(t *testing.T) {
	omarchy := ParseOSRelease(fixture(t, "os-release-omarchy.txt"))
	if !omarchy.Matches(ManagerPacman) {
		t.Errorf("Omarchy Server does not match pacman")
	}
	if omarchy.Matches(ManagerAPT) {
		t.Errorf("Omarchy Server matched apt")
	}
	if !ParseOSRelease(fixture(t, "os-release-ubuntu.txt")).Matches(ManagerAPT) {
		t.Errorf("Ubuntu does not match apt")
	}
	if (Distro{}).Matches(ManagerDNF) {
		t.Errorf("an empty distribution matched dnf")
	}
}

// TestRepoStatus reads the probe against fixture trees rather than the
// machine's own /etc, so what "configured" means is pinned rather than
// whatever the host happens to be.
func TestRepoStatus(t *testing.T) {
	for _, tc := range []struct {
		what    string
		manager Manager
		root    string
		want    bool
		path    string
	}{
		{"apt configured", ManagerAPT, "testdata/repo/apt", true,
			"/etc/apt/sources.list.d/tui-tools.list"},
		{"dnf configured", ManagerDNF, "testdata/repo/dnf", true,
			"/etc/yum.repos.d/tui-tools.repo"},
		{"pacman through an Include", ManagerPacman, "testdata/repo/pacman",
			true, "/etc/pacman.d/tui-tools.conf"},
		{"apt with only a commented line", ManagerAPT, "testdata/repo/plain",
			false, ""},
		{"dnf with other repositories", ManagerDNF, "testdata/repo/plain",
			false, ""},
		{"pacman with no section", ManagerPacman, "testdata/repo/plain",
			false, ""},
	} {
		got, err := repoStatus(tc.manager, RepoConfig{}, osDirFS{root: tc.root})
		if err != nil {
			t.Errorf("%s: %v", tc.what, err)
			continue
		}
		if got.Configured != tc.want {
			t.Errorf("%s: configured = %v, want %v (%s)",
				tc.what, got.Configured, tc.want, got.Detail)
		}
		if got.Configured && got.Path != tc.path {
			t.Errorf("%s: path = %q, want %q", tc.what, got.Path, tc.path)
		}
		// Every answer explains itself, because a screen that says "not
		// configured" and nothing else is a screen nobody can act on.
		if got.Detail == "" {
			t.Errorf("%s: no detail", tc.what)
		}
	}
	if _, err := repoStatus(Manager("apk"), RepoConfig{},
		osDirFS{root: "testdata/repo/apt"}); err == nil {
		t.Errorf("repoStatus accepted apk")
	}
	// A machine with no /etc at all is "not configured", not an error: the
	// launcher's next screen offers to set it up.
	missing, err := repoStatus(ManagerAPT, RepoConfig{}, osDirFS{root: "testdata/nope"})
	if err != nil || missing.Configured {
		t.Errorf("missing tree = %+v, %v", missing, err)
	}
}

// TestRepoStatusHonoursTheConfiguredHost: a machine pointed at a staging copy
// is not configured for the family's own repository.
func TestRepoStatusHonoursTheConfiguredHost(t *testing.T) {
	staging := RepoConfig{URL: "https://staging.tui.tools"}
	got, err := repoStatus(ManagerAPT, staging, osDirFS{root: "testdata/repo/apt"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Configured {
		t.Errorf("the production sources file counted as the staging one")
	}
}

// TestFakeInstalls proves the demo backend behaves like the real one: it
// validates the same names, previews the same argv, and the catalogue behind
// the screen changes the way an install would change a machine.
func TestFakeInstalls(t *testing.T) {
	ctx := context.Background()
	fake := NewFake()

	before, err := fake.Installed(ctx, []string{"tui-disk"})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Errorf("tui-disk was already installed: %v", before)
	}

	steps, err := fake.Install([]string{"tui-disk"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if _, runErr := fake.Run(ctx, step); runErr != nil {
			t.Fatalf("run %q: %v", step, runErr)
		}
	}
	after, err := fake.Installed(ctx, []string{"tui-disk"})
	if err != nil {
		t.Fatal(err)
	}
	if after["tui-disk"] != "0.1.1-1" {
		t.Errorf("tui-disk after install = %v", after)
	}
	if len(fake.Ran) != len(steps) {
		t.Errorf("ran %d commands, want %d", len(fake.Ran), len(steps))
	}
	// The preview carries the escalation prefix a privileged step would run
	// with, and an unprivileged one carries none.
	previews := fake.Previews()
	if !strings.HasPrefix(previews[0], "sudo -n pacman -Sy") {
		t.Errorf("preview = %q", previews[0])
	}

	// Removing it puts the machine back.
	remove, err := fake.Remove([]string{"tui-disk"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fake.Run(ctx, remove[0]); err != nil {
		t.Fatal(err)
	}
	gone, err := fake.Installed(ctx, []string{"tui-disk"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 0 {
		t.Errorf("tui-disk survived the remove: %v", gone)
	}
}

// TestFakeValidatesLikeTheRealThing: a fake that accepts what the machine
// would reject is a fake that hides a bug.
func TestFakeValidatesLikeTheRealThing(t *testing.T) {
	ctx := context.Background()
	fake := NewFake()
	if _, err := fake.Installed(ctx, []string{"openssh-server"}); err == nil {
		t.Errorf("the fake accepted a name the builders reject")
	}
	if _, err := fake.Install([]string{"tui-firewall; reboot"}); err == nil {
		t.Errorf("the fake built a command from an invalid name")
	}
	if _, err := fake.RepoSetup("DEADBEEF"); err == nil {
		t.Errorf("the fake accepted a short key id as a fingerprint")
	}
}

// TestFakeRepoStatus: the demo machine can be shown either way round.
func TestFakeRepoStatus(t *testing.T) {
	fake := NewFake()
	got, err := fake.RepoStatus()
	if err != nil || !got.Configured {
		t.Errorf("demo machine = %+v, %v", got, err)
	}
	fake.Configured = false
	if got, _ = fake.RepoStatus(); got.Configured {
		t.Errorf("the demo machine stayed configured")
	}
	// Writing a repository file is what configures it, in the demo as on a
	// machine.
	setup, err := fake.RepoSetup(testFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range setup.Steps {
		if _, runErr := fake.Run(context.Background(), step); runErr != nil {
			t.Fatal(runErr)
		}
	}
	if got, _ = fake.RepoStatus(); !got.Configured {
		t.Errorf("the setup did not configure the demo machine")
	}
}

// TestFakeCatalogue keeps the demo list sorted and complete.
func TestFakeCatalogue(t *testing.T) {
	names := NewFake().Catalogue()
	if len(names) != 7 {
		t.Errorf("catalogue = %v", names)
	}
	for i := range names {
		if !ValidName(names[i]) {
			t.Errorf("catalogue holds %q", names[i])
		}
		if i > 0 && names[i-1] >= names[i] {
			t.Errorf("catalogue is not sorted: %v", names)
		}
	}
}

// TestNotInstalledOnly: the managers' "no such package" complaints are an
// answer, and only a real failure is one.
func TestNotInstalledOnly(t *testing.T) {
	for _, out := range []string{
		"",
		"package tui-disk is not installed",
		"dpkg-query: no packages found matching tui-disk",
		"error: package 'tui-disk' was not found",
	} {
		if !notInstalledOnly(out) {
			t.Errorf("%q was read as a failure", out)
		}
	}
	for _, out := range []string{
		"sudo: a password is required",
		"error: rpmdb: BDB0113 Thread died in Berkeley DB library",
	} {
		if notInstalledOnly(out) {
			t.Errorf("%q was read as an answer", out)
		}
	}
}

// TestFakeDescribesItselfAsADemo: nobody should mistake a demo for a machine.
func TestFakeDescribesItselfAsADemo(t *testing.T) {
	fake := NewFake()
	if !strings.Contains(fake.Describe(), "(demo)") {
		t.Errorf("Describe = %q", fake.Describe())
	}
	if fake.Distro().ID != "omarchy-server" {
		t.Errorf("Distro = %+v", fake.Distro())
	}
	if fake.Manager() != ManagerPacman {
		t.Errorf("Manager = %q", fake.Manager())
	}
	// The zero value is usable, and behaves like an empty pacman machine.
	var empty Fake
	if empty.Manager() != ManagerPacman || empty.Describe() == "" {
		t.Errorf("the zero Fake is not usable: %q", empty.Describe())
	}
	if _, err := empty.Run(context.Background(),
		Command{Argv: []string{"pacman", "-S", "tui-disk"}}); err != nil {
		t.Errorf("zero Fake Run: %v", err)
	}
}

// TestFakeAvailable answers from the catalogue, and only for what was asked.
func TestFakeAvailable(t *testing.T) {
	available, err := NewFake().Available(context.Background(),
		[]string{"tui-firewall", "tui-nope"})
	if err != nil {
		t.Fatal(err)
	}
	if available["tui-firewall"] != "0.2.2-1" {
		t.Errorf("tui-firewall available = %v", available)
	}
	if len(available) != 1 {
		t.Errorf("available = %v, want tui-firewall alone", available)
	}
}

// TestRunnerCommandCarriesTheWholeStep: the value the kit runner executes has
// to be the value that was previewed, body and all.
func TestRunnerCommandCarriesTheWholeStep(t *testing.T) {
	setup, err := BuildRepoSetup(ManagerPacman, RepoConfig{}, testFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	step := findStep(t, setup, "tee /etc/pacman.d/tui-tools.conf")
	got := step.runnerCommand()
	if got.String() != step.String() {
		t.Errorf("argv = %q, want %q", got.String(), step.String())
	}
	if got.Stdin != step.Stdin {
		t.Errorf("the file body did not survive the conversion")
	}
	if got.Description != step.Explain {
		t.Errorf("description = %q, want %q", got.Description, step.Explain)
	}
	remove, err := BuildRemove(ManagerPacman, []string{"tui-disk"})
	if err != nil {
		t.Fatal(err)
	}
	if !remove[0].runnerCommand().Destructive {
		t.Errorf("a remove reaches the runner without its danger marking")
	}
}
