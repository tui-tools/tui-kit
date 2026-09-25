package pkgmgr

import (
	"context"
	"strings"
	"testing"
)

// TestDistroOmarchy pins what makes a machine Omarchy: an os-release ID or
// ID_LIKE starting with "omarchy" (Omarchy Server and the desktop Omarchy),
// or the guard hook being installed on a machine whose os-release still says
// arch. Plain Arch, and every other distribution, is not.
func TestDistroOmarchy(t *testing.T) {
	for _, tc := range []struct {
		what   string
		distro Distro
		want   bool
	}{
		{"omarchy server", ParseOSRelease(fixture(t, "os-release-omarchy.txt")), true},
		{"omarchy desktop", ParseOSRelease(fixture(t, "os-release-omarchy-desktop.txt")), true},
		{"arch", ParseOSRelease(fixture(t, "os-release-arch.txt")), false},
		{"arch with the guard hook", Distro{ID: "arch", UpdateGuard: true}, true},
		{"ubuntu", ParseOSRelease(fixture(t, "os-release-ubuntu.txt")), false},
		{"fedora", ParseOSRelease(fixture(t, "os-release-fedora.txt")), false},
		{"unknown", Distro{}, false},
	} {
		if got := tc.distro.Omarchy(); got != tc.want {
			t.Errorf("%s: Omarchy() = %v, want %v", tc.what, got, tc.want)
		}
	}
}

// guardRefuses is the decision Omarchy's omarchy-update-pacman-guard makes on
// a pacman command line: a sync together with a sysupgrade, short or long
// form, is a direct system upgrade and is refused. It is restated here so the
// argv this package builds can be held against it.
func guardRefuses(argv []string) bool {
	var sync, sysupgrade bool
	for _, arg := range argv[1:] {
		switch {
		case arg == "--sync":
			sync = true
		case arg == "--sysupgrade":
			sysupgrade = true
		case strings.HasPrefix(arg, "--"):
		case strings.HasPrefix(arg, "-"):
			sync = sync || strings.Contains(arg, "S")
			sysupgrade = sysupgrade || strings.Contains(arg, "u")
		}
	}
	return sync && sysupgrade
}

// TestOmarchyRefusalFixture holds the refusal captured from a real Omarchy
// Server 4.0.1 guest, where the family's Arch install (`pacman -Syu --needed
// --noconfirm tui-cert`) was aborted by the guard hook. It is what the
// Omarchy plan exists to avoid.
func TestOmarchyRefusalFixture(t *testing.T) {
	refusal := fixture(t, "pacman-omarchy-guard-refusal.txt")
	for _, want := range []string{
		"Checking Omarchy update entrypoint",
		"This looks like a direct pacman system upgrade",
		"omarchy update",
		"failed to run transaction hooks",
		"no packages were upgraded",
	} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the captured refusal no longer says %q", want)
		}
	}
	if !guardRefuses(strings.Fields("pacman -Syu --needed --noconfirm tui-cert")) {
		t.Errorf("the restated guard lets through the command it refused")
	}
}

// TestInstallOnOmarchyAndArch builds both plans from the same names: Omarchy
// gets one `-S --needed` step that its guard lets through and that says why,
// and plain Arch keeps the `-Syu` the family rule gives it.
func TestInstallOnOmarchyAndArch(t *testing.T) {
	names := []string{"tui-firewall", "tui-disk"}
	omarchy := ParseOSRelease(fixture(t, "os-release-omarchy.txt"))
	arch := ParseOSRelease(fixture(t, "os-release-arch.txt"))
	for _, tc := range []struct {
		what   string
		distro Distro
		build  func(Manager, Distro, []string) ([]Command, error)
		want   string
		guard  bool
	}{
		{"install on omarchy", omarchy, BuildInstallOn,
			"pacman -S --needed --noconfirm tui-firewall tui-disk", false},
		{"upgrade on omarchy", omarchy, BuildUpgradeOn,
			"pacman -S --needed --noconfirm tui-firewall tui-disk", false},
		{"install on arch", arch, BuildInstallOn,
			"pacman -Syu --needed --noconfirm tui-firewall tui-disk", true},
		{"upgrade on arch", arch, BuildUpgradeOn,
			"pacman -Syu --noconfirm tui-firewall tui-disk", true},
	} {
		steps, err := tc.build(ManagerPacman, tc.distro, names)
		if err != nil {
			t.Fatalf("%s: %v", tc.what, err)
		}
		if len(steps) != 1 {
			t.Fatalf("%s: %d steps, want 1", tc.what, len(steps))
		}
		step := steps[0]
		if step.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.what, step, tc.want)
		}
		if !step.Privileged {
			t.Errorf("%s is not privileged", tc.what)
		}
		if got := guardRefuses(step.Argv); got != tc.guard {
			t.Errorf("%s: Omarchy's guard refuses it = %v, want %v",
				tc.what, got, tc.guard)
		}
		saysOmarchy := strings.Contains(step.Explain, "omarchy update")
		if saysOmarchy != tc.distro.Omarchy() {
			t.Errorf("%s: explanation %q mentions omarchy update = %v",
				tc.what, step.Explain, saysOmarchy)
		}
	}
}

// TestOmarchyOnlyChangesPacman: the Omarchy plan is a pacman plan, so a
// distribution claiming Omarchy on another manager changes nothing, and the
// names are validated the same way.
func TestOmarchyOnlyChangesPacman(t *testing.T) {
	omarchy := Distro{ID: "omarchy-server"}
	got, err := BuildInstallOn(ManagerAPT, omarchy, []string{"tui-disk"})
	if err != nil {
		t.Fatal(err)
	}
	if got[len(got)-1].String() != "DEBIAN_FRONTEND=noninteractive "+
		"NEEDRESTART_MODE=a apt-get install -y tui-disk" {
		t.Errorf("apt install on an omarchy id = %q", got[len(got)-1])
	}
	for _, build := range []func(Manager, Distro, []string) ([]Command, error){
		BuildInstallOn, BuildUpgradeOn,
	} {
		if _, err := build(ManagerPacman, omarchy, []string{"firefox"}); err == nil {
			t.Errorf("a non-family name was accepted on Omarchy")
		}
	}
}

// TestFakeOnOmarchy: the demo machine is Omarchy Server, so --demo previews
// the plan a real Omarchy would run, and the catalogue still changes with it.
func TestFakeOnOmarchy(t *testing.T) {
	ctx := context.Background()
	fake := NewFake()
	steps, err := fake.Upgrade([]string{"tui-firewall"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].String() != "pacman -S --needed --noconfirm tui-firewall" {
		t.Fatalf("demo upgrade = %v", steps)
	}
	if _, err = fake.Run(ctx, steps[0]); err != nil {
		t.Fatal(err)
	}
	got, err := fake.Installed(ctx, []string{"tui-firewall"})
	if err != nil {
		t.Fatal(err)
	}
	if got["tui-firewall"] != "0.2.2-1" {
		t.Errorf("tui-firewall after the demo upgrade = %v", got)
	}
}
