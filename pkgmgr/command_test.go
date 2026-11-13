package pkgmgr

import (
	"errors"
	"strings"
	"testing"
)

// argv renders a command the way the preview shows it, for a table.
func argv(cmd Command) string { return cmd.String() }

// TestArgvTable pins the exact command line each manager gets. It is a table
// rather than three tests because the point is that they can be read side by
// side: a change to one of these is a change to what a user is shown, and it
// should be visible as one diff.
func TestArgvTable(t *testing.T) {
	names := []string{"tui-firewall", "tui-disk"}
	for _, tc := range []struct {
		what    string
		manager Manager
		build   func(Manager, []string) ([]Command, error)
		want    []string
	}{
		{"install", ManagerAPT, BuildInstall, []string{
			"DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get update",
			"DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get install -y tui-firewall tui-disk",
		}},
		{"install", ManagerDNF, BuildInstall, []string{
			"dnf install -y tui-firewall tui-disk",
		}},
		{"install", ManagerPacman, BuildInstall, []string{
			"pacman -Syu --needed --noconfirm tui-firewall tui-disk",
		}},
		{"remove", ManagerAPT, BuildRemove, []string{
			"DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get remove -y tui-firewall tui-disk",
		}},
		{"remove", ManagerDNF, BuildRemove, []string{
			"dnf remove -y tui-firewall tui-disk",
		}},
		{"remove", ManagerPacman, BuildRemove, []string{
			"pacman -R --noconfirm tui-firewall tui-disk",
		}},
		{"upgrade", ManagerAPT, BuildUpgrade, []string{
			"DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get update",
			"DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get install --only-upgrade -y tui-firewall tui-disk",
		}},
		{"upgrade", ManagerDNF, BuildUpgrade, []string{
			"dnf upgrade -y tui-firewall tui-disk",
		}},
		{"upgrade", ManagerPacman, BuildUpgrade, []string{
			"pacman -Syu --noconfirm tui-firewall tui-disk",
		}},
	} {
		got, err := tc.build(tc.manager, names)
		if err != nil {
			t.Errorf("%s on %s: %v", tc.what, tc.manager, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s on %s: %d steps, want %d", tc.what, tc.manager,
				len(got), len(tc.want))
			continue
		}
		for i := range got {
			if argv(got[i]) != tc.want[i] {
				t.Errorf("%s on %s step %d = %q, want %q",
					tc.what, tc.manager, i, argv(got[i]), tc.want[i])
			}
			if !got[i].Privileged {
				t.Errorf("%s on %s step %d is not marked privileged",
					tc.what, tc.manager, i)
			}
			if got[i].Explain == "" {
				t.Errorf("%s on %s step %d has no explanation",
					tc.what, tc.manager, i)
			}
		}
	}
}

// TestAPTMutationsRunNoninteractive pins the environment every apt mutation
// runs with, and how it reaches apt: through env(1) after the escalation
// prefix, where sudo cannot reset it, and in the preview, so the confirmed line
// is the line that runs (#32).
func TestAPTMutationsRunNoninteractive(t *testing.T) {
	names := []string{"tui-firewall"}
	var steps []Command
	for _, build := range []func(Manager, []string) ([]Command, error){
		BuildInstall, BuildUpgrade, BuildRemove,
	} {
		got, err := build(ManagerAPT, names)
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, got...)
	}
	setup, err := BuildRepoSetup(ManagerAPT, RepoConfig{},
		"0123456789ABCDEF0123456789ABCDEF01234567")
	if err != nil {
		t.Fatal(err)
	}
	steps = append(steps, setup.Steps...)

	want := []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a"}
	apt := 0
	for _, step := range steps {
		if step.Argv[0] != "apt-get" {
			if len(step.Env) != 0 {
				t.Errorf("%q is not apt and carries %q", step.Argv, step.Env)
			}
			continue
		}
		apt++
		if strings.Join(step.Env, " ") != strings.Join(want, " ") {
			t.Errorf("%q runs with %q, want %q", step.Argv, step.Env, want)
		}
		rc := step.runnerCommand()
		if strings.Join(rc.Env, " ") != strings.Join(want, " ") {
			t.Errorf("%q loses its env on the way to the runner: %q",
				step.Argv, rc.Env)
		}
	}
	if apt != 6 {
		t.Errorf("found %d apt-get steps, want 6 (install 2, upgrade 2, "+
			"remove 1, repository setup 1)", apt)
	}

	// The demo shows the line the real runner builds.
	fake := &Fake{Prefix: "sudo -n"}
	if got := fake.Preview(steps[1]); got != "sudo -n env "+
		"DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a "+
		"apt-get install -y tui-firewall" {
		t.Errorf("Fake.Preview = %q", got)
	}
	// Other managers are left as they were.
	for _, manager := range []Manager{ManagerDNF, ManagerPacman} {
		got, _ := BuildInstall(manager, names)
		for _, step := range got {
			if len(step.Env) != 0 {
				t.Errorf("%s step %q carries %q", manager, step.Argv, step.Env)
			}
		}
	}
}

// TestReadArgvTable pins the read commands, and that none of them escalates:
// telling a user what is installed must never ask for a password.
func TestReadArgvTable(t *testing.T) {
	names := []string{"tui-firewall"}
	for _, tc := range []struct {
		manager Manager
		build   func(Manager, []string) (Command, error)
		want    string
	}{
		{ManagerAPT, BuildInstalled,
			"dpkg-query -W '-f=${Package}|${Version}|${db:Status-Status}\n' tui-firewall"},
		{ManagerPacman, BuildInstalled, "pacman -Q tui-firewall"},
		{ManagerAPT, BuildAvailable, "apt-cache policy tui-firewall"},
		{ManagerPacman, BuildAvailable, "pacman -Si tui-firewall"},
		{ManagerDNF, BuildAvailable,
			"dnf --quiet repoquery --latest-limit 1 --qf '%{name}|%{evr}\n' tui-firewall"},
	} {
		cmd, err := tc.build(tc.manager, names)
		if err != nil {
			t.Errorf("%s: %v", tc.manager, err)
			continue
		}
		if argv(cmd) != tc.want {
			t.Errorf("%s = %q, want %q", tc.manager, argv(cmd), tc.want)
		}
		if cmd.Privileged {
			t.Errorf("%s read is privileged: %q", tc.manager, argv(cmd))
		}
	}
	// rpm's conditional epoch is what keeps "installed" and "available"
	// comparable, so the format string is asserted rather than assumed.
	cmd, err := BuildInstalled(ManagerDNF, names)
	if err != nil {
		t.Fatalf("BuildInstalled dnf: %v", err)
	}
	if !strings.Contains(argv(cmd), "%|EPOCH?{%{EPOCH}:}|") {
		t.Errorf("rpm query lost the epoch conditional: %q", argv(cmd))
	}
}

// TestOnlyTUIPackages is the whole point of the name check: nothing else can
// become an argument, whichever door it comes in through.
func TestOnlyTUIPackages(t *testing.T) {
	rejected := []string{
		"", "tui", "tui-", "TUI-firewall", "tui-firewall2", "tui_firewall",
		"tui-fire wall", "tui-firewall;reboot", "../tui-firewall",
		"tui-firewall-extra", "openssh-server", "-rf", "--force",
		"tui-firewall\n", "tui-firewall ",
	}
	builders := map[string]func([]string) error{
		"BuildInstall": func(n []string) error {
			_, err := BuildInstall(ManagerAPT, n)
			return err
		},
		"BuildRemove": func(n []string) error {
			_, err := BuildRemove(ManagerDNF, n)
			return err
		},
		"BuildUpgrade": func(n []string) error {
			_, err := BuildUpgrade(ManagerPacman, n)
			return err
		},
		"BuildInstalled": func(n []string) error {
			_, err := BuildInstalled(ManagerAPT, n)
			return err
		},
		"BuildAvailable": func(n []string) error {
			_, err := BuildAvailable(ManagerPacman, n)
			return err
		},
	}
	for name, build := range builders {
		for _, bad := range rejected {
			if err := build([]string{bad}); err == nil {
				t.Errorf("%s accepted %q", name, bad)
			}
		}
		// One bad name in a good set poisons the whole set.
		if err := build([]string{"tui-firewall", "rm -rf /"}); err == nil {
			t.Errorf("%s accepted a mixed set", name)
		}
		if err := build(nil); err == nil {
			t.Errorf("%s accepted an empty set", name)
		}
	}
	if err := CheckName("nope"); !errors.Is(err, ErrInvalidName) {
		t.Errorf("CheckName error = %v, want ErrInvalidName", err)
	}
	for _, good := range []string{"tui-firewall", "tui-ssh", "tui-cert"} {
		if !ValidName(good) {
			t.Errorf("ValidName rejected %q", good)
		}
	}
}

// TestUnknownManagerIsRefused: a manager nothing here knows produces an error
// rather than a plausible-looking command for the wrong machine.
func TestUnknownManagerIsRefused(t *testing.T) {
	names := []string{"tui-firewall"}
	if _, err := BuildInstall(Manager("zypper"), names); err == nil {
		t.Errorf("BuildInstall accepted zypper")
	}
	if _, err := BuildInstalled(Manager("apk"), names); err == nil {
		t.Errorf("BuildInstalled accepted apk")
	}
	if _, err := BuildRefresh(Manager("")); err == nil {
		t.Errorf("BuildRefresh accepted an empty manager")
	}
	if _, err := BuildRepoSetup(Manager("apk"), RepoConfig{}, testFingerprint); err == nil {
		t.Errorf("BuildRepoSetup accepted apk")
	}
	if Manager("zypper").Known() {
		t.Errorf("zypper reported itself known")
	}
}

// testFingerprint stands in for the family's own key. The real one belongs to
// the caller: a fingerprint compiled into a library is one that cannot be
// rotated without a release.
const testFingerprint = "1111222233334444555566667777888899990000"

func TestCheckFingerprint(t *testing.T) {
	got, err := CheckFingerprint(
		"1111 2222 3333 4444 5555  6666 7777 8888 9999 0000")
	if err != nil || got != testFingerprint {
		t.Errorf("spaced fingerprint = %q, %v", got, err)
	}
	if got, err := CheckFingerprint(strings.ToLower(testFingerprint)); err != nil ||
		got != testFingerprint {
		t.Errorf("lower-case fingerprint = %q, %v", got, err)
	}
	for _, bad := range []string{
		"", "DEADBEEF", "1111222233334444555566667777888899990000A",
		"1111222233334444555566667777888899990Z00", "$(id)",
	} {
		if _, err := CheckFingerprint(bad); err == nil {
			t.Errorf("CheckFingerprint accepted %q", bad)
		}
	}
}

// TestRepoSetupPinsTheKey asserts the shape every manager's setup shares: the
// key is downloaded, its fingerprint is read before anything imports it, and
// the verify step is the unprivileged one.
func TestRepoSetupPinsTheKey(t *testing.T) {
	for _, manager := range []Manager{ManagerAPT, ManagerDNF, ManagerPacman} {
		setup, err := BuildRepoSetup(manager, RepoConfig{}, testFingerprint)
		if err != nil {
			t.Fatalf("%s: %v", manager, err)
		}
		verify := setup.Steps[setup.Verify]
		if !strings.HasPrefix(argv(verify), "gpg --show-keys --with-colons ") {
			t.Errorf("%s verify step = %q", manager, argv(verify))
		}
		if verify.Privileged {
			t.Errorf("%s reads a fingerprint with escalation", manager)
		}
		// Nothing may import the key before it has been checked.
		for i, step := range setup.Steps {
			imports := strings.Contains(argv(step), "--dearmor") ||
				strings.Contains(argv(step), "rpm --import") ||
				strings.Contains(argv(step), "pacman-key --add")
			if imports && i < setup.Verify {
				t.Errorf("%s imports the key at step %d, before the check at %d",
					manager, i, setup.Verify)
			}
		}
		// The key is read where it was downloaded, and that is not where
		// anything trusts a key from.
		staged := verify.Argv[len(verify.Argv)-1]
		if !strings.HasPrefix(staged, stagingDir(RepoConfig{}.normalize())+"/") {
			t.Errorf("%s reads the key at %q, not in the staging directory",
				manager, staged)
		}
		if !setup.Match(fixtureFingerprintOutput) {
			t.Errorf("%s did not match its own pinned fingerprint", manager)
		}
		if setup.Match("fpr:::::::::AAAABBBBCCCCDDDDEEEEFFFF00001111222233334:") {
			t.Errorf("%s matched a different key", manager)
		}
		// The last step refreshes, so a package can be installed right after.
		last := setup.Steps[len(setup.Steps)-1]
		refresh, _ := BuildRefresh(manager)
		if argv(last) != argv(refresh) {
			t.Errorf("%s ends with %q, want the refresh %q",
				manager, argv(last), argv(refresh))
		}
	}
}

// TestSetupMatchWantsExactlyThePinnedKey: the downloaded file has to hold one
// primary key and that key has to be the pinned one. A file carrying the
// pinned key first and another after it is the attack the pin exists for:
// apt's keyring would take both, and either could then sign the repository.
func TestSetupMatchWantsExactlyThePinnedKey(t *testing.T) {
	setup, err := BuildRepoSetup(ManagerAPT, RepoConfig{}, testFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		out  string
		want bool
	}{
		{"the pinned key alone", fixture(t, "gpg-show-keys.txt"), true},
		{"the pinned key and another", fixture(t, "gpg-show-keys-two.txt"), false},
		{"the pinned key and one gpg marks invalid",
			fixture(t, "gpg-show-keys-hidden.txt"), false},
		{"another key", fixture(t, "gpg-show-keys-other.txt"), false},
		{"another key and then the pinned one",
			fixture(t, "gpg-show-keys-other.txt") + fixture(t, "gpg-show-keys.txt"), false},
		{"no key", "gpg: no valid OpenPGP data found", false},
		{"a fingerprint without a key", "fpr:::::::::" + testFingerprint + ":\n", false},
	} {
		if got := setup.Match(tc.out); got != tc.want {
			t.Errorf("%s: Match = %v, want %v", tc.name, got, tc.want)
		}
	}
	// A Setup carrying nothing but the fingerprint is how tools that copy
	// the plan into their own type call it; the rule is the same.
	bare := Setup{Fingerprint: testFingerprint}
	if bare.Match(fixture(t, "gpg-show-keys-two.txt")) {
		t.Errorf("a bare Setup matched a file carrying two keys")
	}
	if (Setup{}).Match(fixture(t, "gpg-show-keys.txt")) {
		t.Errorf("a Setup without a fingerprint matched a key")
	}
}

// TestRepoSetupRefusalLeavesNothingTrusted runs every manager's setup against
// a model of the files each step writes, refuses the key at the check, and
// asserts that nothing was written outside the staging directory and nothing
// was imported: a refused key never sits where a repository configuration
// would trust it. The accepted run is the control, showing the model does see
// the trusted files when the key is the right one.
func TestRepoSetupRefusalLeavesNothingTrusted(t *testing.T) {
	for _, manager := range []Manager{ManagerAPT, ManagerDNF, ManagerPacman} {
		setup, err := BuildRepoSetup(manager, RepoConfig{}, testFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		staging := stagingDir(RepoConfig{}.normalize()) + "/"
		for _, tc := range []struct {
			name    string
			out     string
			refused bool
		}{
			{"two keys", fixture(t, "gpg-show-keys-two.txt"), true},
			{"another key", fixture(t, "gpg-show-keys-other.txt"), true},
			{"the pinned key", fixture(t, "gpg-show-keys.txt"), false},
		} {
			written, imported, stopped := simulateSetup(t, setup, tc.out)
			if stopped != tc.refused {
				t.Errorf("%s/%s: stopped = %v, want %v", manager, tc.name, stopped, tc.refused)
				continue
			}
			if !tc.refused {
				// apt trusts the keyring file it is pointed at; dnf and
				// pacman trust what was imported.
				trusted := len(imported)
				for _, path := range written {
					if !strings.HasPrefix(path, staging) {
						trusted++
					}
				}
				if trusted == 0 {
					t.Errorf("%s/%s: the accepted key never left staging", manager, tc.name)
				}
				continue
			}
			if len(imported) != 0 {
				t.Errorf("%s/%s: imported %q after a refusal", manager, tc.name, imported)
			}
			for _, path := range written {
				if !strings.HasPrefix(path, staging) {
					t.Errorf("%s/%s: %s was written before the key was checked",
						manager, tc.name, path)
				}
			}
		}
	}
}

// simulateSetup walks the steps the way a caller does, stopping after the
// verify step when Match refuses its output, and returns the files the steps
// that ran would have written and the keys they would have imported. It reads
// the argv shapes this package builds; an unknown privileged step fails the
// test, so a new step cannot slip past the model.
func simulateSetup(t *testing.T, setup Setup, verifyOut string) (written, imported []string, stopped bool) {
	t.Helper()
	for i, step := range setup.Steps {
		a := step.Argv
		switch {
		case a[0] == "install" && len(a) >= 2 && a[1] == "-d":
			// A directory is not a key.
		case a[0] == "curl":
			written = append(written, argAfter(t, a, "-o"))
		case a[0] == "gpg" && hasArgv(a, "--show-keys"):
		case a[0] == "gpg" && hasArgv(a, "--dearmor"):
			written = append(written, argAfter(t, a, "-o"))
		case a[0] == "install":
			written = append(written, a[len(a)-1])
		case a[0] == "chmod", a[0] == "rm":
		case a[0] == "rpm" && hasArgv(a, "--import"),
			a[0] == "pacman-key":
			imported = append(imported, a[len(a)-1])
		case a[0] == "tee":
			written = append(written, a[len(a)-1])
		case a[0] == "apt-get", a[0] == "dnf", a[0] == "pacman":
		default:
			t.Fatalf("the model does not know the step %q", argv(step))
		}
		if i == setup.Verify && !setup.Match(verifyOut) {
			return written, imported, true
		}
	}
	return written, imported, false
}

// argAfter returns the argument that follows a flag.
func argAfter(t *testing.T, argv []string, flag string) string {
	t.Helper()
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag {
			return argv[i+1]
		}
	}
	t.Fatalf("no %s in %q", flag, argv)
	return ""
}

// hasArgv reports whether an argv carries an argument.
func hasArgv(argv []string, want string) bool {
	for _, arg := range argv {
		if arg == want {
			return true
		}
	}
	return false
}

// fixtureFingerprintOutput is the gpg record carrying testFingerprint.
const fixtureFingerprintOutput = "" +
	"pub:-:255:22:0A1B2C3D4E5F6071:1756512000:::-:::scESC:::::ed25519:::0:\n" +
	"fpr:::::::::1111222233334444555566667777888899990000:\n"

// TestRepoSetupMirrorsTheInstaller pins the parts of the setup that have to
// keep matching pkgs/install.sh and Omarchy Server's tui-tools addon, because
// a machine set up by one and read by the other has to agree.
func TestRepoSetupMirrorsTheInstaller(t *testing.T) {
	apt, err := BuildRepoSetup(ManagerAPT, RepoConfig{}, testFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	sources := findStep(t, apt, "tee /etc/apt/sources.list.d/tui-tools.list")
	want := "deb [signed-by=/etc/apt/keyrings/tui-tools.gpg] " +
		"https://pkgs.tui.tools/deb stable main\n"
	if sources.Stdin != want {
		t.Errorf("apt sources line = %q, want %q", sources.Stdin, want)
	}

	dnf, err := BuildRepoSetup(ManagerDNF, RepoConfig{}, testFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	repo := findStep(t, dnf, "tee /etc/yum.repos.d/tui-tools.repo")
	for _, line := range []string{
		"[tui-tools]",
		"baseurl=https://pkgs.tui.tools/rpm/$basearch",
		"gpgcheck=1",
		"repo_gpgcheck=1",
	} {
		if !strings.Contains(repo.Stdin, line) {
			t.Errorf("dnf .repo is missing %q:\n%s", line, repo.Stdin)
		}
	}

	pacman, err := BuildRepoSetup(ManagerPacman, RepoConfig{}, testFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	conf := findStep(t, pacman, "tee /etc/pacman.d/tui-tools.conf")
	for _, line := range []string{
		"[tui-tools]",
		"SigLevel = Required TrustedOnly",
		"Server = https://pkgs.tui.tools/arch/$arch",
	} {
		if !strings.Contains(conf.Stdin, line) {
			t.Errorf("pacman conf is missing %q:\n%s", line, conf.Stdin)
		}
	}
	// Local signing is what moves the key from known to trusted; without it
	// every package from the repository is rejected.
	lsign := findStep(t, pacman, "pacman-key --lsign-key "+testFingerprint)
	if !lsign.Privileged {
		t.Errorf("pacman-key --lsign-key is not privileged")
	}
	include := findStep(t, pacman, "tee -a /etc/pacman.conf")
	if !strings.Contains(include.Stdin, "Include = /etc/pacman.d/tui-tools.conf") {
		t.Errorf("pacman.conf include = %q", include.Stdin)
	}
}

// findStep returns the step with that command line, failing when there is
// none.
func findStep(t *testing.T, setup Setup, want string) Command {
	t.Helper()
	for _, step := range setup.Steps {
		if argv(step) == want {
			return step
		}
	}
	t.Fatalf("no step %q in %v", want, setupArgv(setup))
	return Command{}
}

// setupArgv renders a setup for a failure message.
func setupArgv(setup Setup) []string {
	out := make([]string, 0, len(setup.Steps))
	for _, step := range setup.Steps {
		out = append(out, argv(step))
	}
	return out
}

// TestRepoConfigIsValidated: the URL and the name end up in an argv and in a
// file every package on the machine is then trusted from.
func TestRepoConfigIsValidated(t *testing.T) {
	for _, bad := range []RepoConfig{
		{URL: "http://pkgs.tui.tools"},
		{URL: "https://pkgs.tui.tools/$(id)"},
		{URL: "https://pkgs.tui.tools/a b"},
		{URL: "ftp://pkgs.tui.tools"},
		{Name: "tui tools"},
		{Name: "../../etc/passwd"},
		{Suite: "stable main"},
		{Component: "main;reboot"},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("Validate accepted %+v", bad)
		}
		if _, err := BuildRepoSetup(ManagerAPT, bad, testFingerprint); err == nil {
			t.Errorf("BuildRepoSetup accepted %+v", bad)
		}
	}
	if err := (RepoConfig{}).Validate(); err != nil {
		t.Errorf("the family's own repository is invalid: %v", err)
	}
	if host := (RepoConfig{}).host(); host != "pkgs.tui.tools" {
		t.Errorf("host = %q", host)
	}
	// A staging copy is a legitimate override.
	staging := RepoConfig{URL: "https://staging.tui.tools/pkgs/"}
	if err := staging.Validate(); err != nil {
		t.Errorf("staging repository rejected: %v", err)
	}
	setup, err := BuildRepoSetup(ManagerAPT, staging, testFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	sources := findStep(t, setup, "tee /etc/apt/sources.list.d/tui-tools.list")
	if !strings.Contains(sources.Stdin, "https://staging.tui.tools/pkgs/deb") {
		t.Errorf("staging sources line = %q", sources.Stdin)
	}
}

// TestDestructive marks the commands a confirm dialog should paint red.
func TestDestructive(t *testing.T) {
	names := []string{"tui-firewall"}
	for _, manager := range []Manager{ManagerAPT, ManagerDNF, ManagerPacman} {
		remove, err := BuildRemove(manager, names)
		if err != nil {
			t.Fatal(err)
		}
		if !remove[0].Destructive() {
			t.Errorf("%s remove is not destructive: %q", manager, argv(remove[0]))
		}
		install, err := BuildInstall(manager, names)
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range install {
			if step.Destructive() {
				t.Errorf("%s install step %q is marked destructive",
					manager, argv(step))
			}
		}
	}
}

// TestNoShellConstructs: nothing built here reaches a shell, so nothing built
// here may contain one. A repository file's body goes through stdin, which is
// why the check is on the argv alone.
func TestNoShellConstructs(t *testing.T) {
	names := []string{"tui-firewall", "tui-disk"}
	var all []Command
	for _, manager := range []Manager{ManagerAPT, ManagerDNF, ManagerPacman} {
		for _, build := range []func(Manager, []string) ([]Command, error){
			BuildInstall, BuildRemove, BuildUpgrade,
		} {
			steps, err := build(manager, names)
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, steps...)
		}
		setup, err := BuildRepoSetup(manager, RepoConfig{}, testFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, setup.Steps...)
	}
	for _, cmd := range all {
		for _, arg := range cmd.Argv {
			// `$basearch` and `$arch` are the managers' own variables, and
			// they live in a file body rather than in an argv.
			if strings.ContainsAny(arg, "$;&|><`") && !strings.HasPrefix(arg, "-f=") &&
				!strings.HasPrefix(arg, "%") {
				t.Errorf("shell construct in argv: %q (%q)", arg, argv(cmd))
			}
			if strings.Contains(arg, "\n") && !strings.HasPrefix(arg, "-f=") &&
				!strings.HasPrefix(arg, "%") {
				t.Errorf("newline in argv: %q (%q)", arg, argv(cmd))
			}
		}
	}
}
