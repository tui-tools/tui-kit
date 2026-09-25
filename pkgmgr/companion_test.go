package pkgmgr

import (
	"errors"
	"strings"
	"testing"
)

// steps renders a plan the way the preview shows it, one line per step.
func steps(t *testing.T, cmds []Command, err error) []string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(cmds))
	for i, cmd := range cmds {
		if !cmd.Privileged {
			t.Errorf("%q is not privileged", cmd.Argv)
		}
		out[i] = cmd.String()
	}
	return out
}

// TestCompanionNames holds the companion rule the launcher used to keep for
// itself: lower-case words joined by single hyphens, 64 characters at most.
func TestCompanionNames(t *testing.T) {
	for _, name := range []string{
		"headscale", "tailscale", "tui-tools-lab", "tui-disk", "k3s", "a",
		strings.Repeat("a", MaxCompanionName),
	} {
		if err := CheckCompanionName(name); err != nil {
			t.Errorf("CheckCompanionName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{
		"", "-y", "--noconfirm", "Headscale", "3proxy", "head--scale",
		"headscale-", "head scale", "head;scale", "headscale*", "../headscale",
		"tui-tools/headscale", "headscale=1.0", "headscale>=1", "héadscale",
		strings.Repeat("a", MaxCompanionName+1),
	} {
		if err := CheckCompanionName(name); !errors.Is(err, ErrInvalidCompanionName) {
			t.Errorf("CheckCompanionName(%q) = %v, want ErrInvalidCompanionName", name, err)
		}
	}
	if err := CheckCompanionNames(nil); err == nil {
		t.Error("an empty set must be refused")
	}
	if err := CheckCompanionNames([]string{"headscale", "-y"}); err == nil {
		t.Error("one bad name refuses the set")
	}
}

func TestSplitCompanionTarget(t *testing.T) {
	for _, tc := range []struct{ target, repo, name string }{
		{"headscale", "", "headscale"},
		{"tui-tools/headscale", "tui-tools", "headscale"},
		{"extra/tailscale", "extra", "tailscale"},
	} {
		repo, name, err := SplitCompanionTarget(tc.target)
		if err != nil || repo != tc.repo || name != tc.name {
			t.Errorf("SplitCompanionTarget(%q) = %q, %q, %v", tc.target, repo, name, err)
		}
	}
	for _, target := range []string{
		"", "/headscale", "tui-tools/", "tui-tools//headscale",
		"a/b/headscale", "-y/headscale", "Tui/headscale", "tui-tools/-y",
		"tui tools/headscale",
	} {
		if err := CheckCompanionTarget(target); !errors.Is(err, ErrInvalidCompanionName) {
			t.Errorf("CheckCompanionTarget(%q) = %v, want ErrInvalidCompanionName", target, err)
		}
	}
}

// TestCompanionInstallAndUpgradeOn: a companion gets the tools' plans on
// every distribution, the repository qualifier reaches pacman only, and
// Omarchy keeps its guard-safe `-S --needed`.
func TestCompanionInstallAndUpgradeOn(t *testing.T) {
	ubuntu := ParseOSRelease(fixture(t, "os-release-ubuntu.txt"))
	fedora := ParseOSRelease(fixture(t, "os-release-fedora.txt"))
	arch := ParseOSRelease(fixture(t, "os-release-arch.txt"))
	omarchy := ParseOSRelease(fixture(t, "os-release-omarchy.txt"))
	desktop := ParseOSRelease(fixture(t, "os-release-omarchy-desktop.txt"))
	targets := []string{"tui-tools/headscale", "tailscale"}
	aptEnv := "env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a "
	for _, tc := range []struct {
		what    string
		manager Manager
		distro  Distro
		build   func(Manager, Distro, []string) ([]Command, error)
		want    []string
	}{
		{"install on ubuntu", ManagerAPT, ubuntu, BuildCompanionInstallOn, []string{
			aptEnv + "apt-get update",
			aptEnv + "apt-get install -y headscale tailscale"}},
		{"upgrade on ubuntu", ManagerAPT, ubuntu, BuildCompanionUpgradeOn, []string{
			aptEnv + "apt-get update",
			aptEnv + "apt-get install --only-upgrade -y headscale tailscale"}},
		{"install on fedora", ManagerDNF, fedora, BuildCompanionInstallOn, []string{
			"dnf install -y headscale tailscale"}},
		{"upgrade on fedora", ManagerDNF, fedora, BuildCompanionUpgradeOn, []string{
			"dnf upgrade -y headscale tailscale"}},
		{"install on arch", ManagerPacman, arch, BuildCompanionInstallOn, []string{
			"pacman -Syu --needed --noconfirm tui-tools/headscale tailscale"}},
		{"upgrade on arch", ManagerPacman, arch, BuildCompanionUpgradeOn, []string{
			"pacman -Syu --noconfirm tui-tools/headscale tailscale"}},
		{"install on omarchy server", ManagerPacman, omarchy, BuildCompanionInstallOn, []string{
			"pacman -S --needed --noconfirm tui-tools/headscale tailscale"}},
		{"upgrade on omarchy server", ManagerPacman, omarchy, BuildCompanionUpgradeOn, []string{
			"pacman -S --needed --noconfirm tui-tools/headscale tailscale"}},
		{"install on omarchy desktop", ManagerPacman, desktop, BuildCompanionInstallOn, []string{
			"pacman -S --needed --noconfirm tui-tools/headscale tailscale"}},
	} {
		cmds, err := tc.build(tc.manager, tc.distro, targets)
		got := steps(t, cmds, err)
		// The preview the confirm dialog shows, escalation and env included.
		fake := &Fake{Prefix: "sudo -n"}
		for i, cmd := range cmds {
			got[i] = strings.TrimPrefix(fake.Preview(cmd), "sudo -n ")
		}
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s:\n got %q\nwant %q", tc.what, got, tc.want)
		}
		last := cmds[len(cmds)-1]
		if tc.manager == ManagerPacman {
			if guardRefuses(last.Argv) == tc.distro.Omarchy() {
				t.Errorf("%s: Omarchy's guard refuses %q = %v", tc.what,
					last.Argv, guardRefuses(last.Argv))
			}
			if strings.Contains(last.Explain, "omarchy update") != tc.distro.Omarchy() {
				t.Errorf("%s: explanation %q", tc.what, last.Explain)
			}
		}
	}
}

// TestCompanionBuildersMatchTheToolBuilders: for a name both rules accept, the
// companion plan is the tool plan, so the two can never drift apart.
func TestCompanionBuildersMatchTheToolBuilders(t *testing.T) {
	names := []string{"tui-disk"}
	for _, manager := range []Manager{ManagerAPT, ManagerDNF, ManagerPacman} {
		for _, distro := range []Distro{{ID: "arch"}, {ID: "omarchy-server"}} {
			pairs := []struct {
				tool, companion func(Manager, Distro, []string) ([]Command, error)
			}{
				{BuildInstallOn, BuildCompanionInstallOn},
				{BuildUpgradeOn, BuildCompanionUpgradeOn},
			}
			for _, pair := range pairs {
				tool, err := pair.tool(manager, distro, names)
				if err != nil {
					t.Fatal(err)
				}
				companion, err := pair.companion(manager, distro, names)
				if err != nil {
					t.Fatal(err)
				}
				if a, b := planString(tool), planString(companion); a != b {
					t.Errorf("%s on %s: tool %q, companion %q", manager, distro.ID, a, b)
				}
			}
		}
		for _, pair := range []struct {
			tool, companion func(Manager, []string) (Command, error)
		}{
			{BuildInstalled, BuildCompanionInstalled},
			{BuildAvailable, BuildCompanionAvailable},
		} {
			tool, _ := pair.tool(manager, names)
			companion, err := pair.companion(manager, names)
			if err != nil || tool.String() != companion.String() || companion.Privileged {
				t.Errorf("%s read: tool %q, companion %q (%v)", manager, tool, companion, err)
			}
		}
		tool, _ := BuildRemove(manager, names)
		companion, err := BuildCompanionRemove(manager, names)
		if err != nil || planString(tool) != planString(companion) {
			t.Errorf("%s remove: tool %q, companion %q (%v)", manager,
				planString(tool), planString(companion), err)
		}
	}
}

// planString flattens a plan into one comparable string, env included.
func planString(cmds []Command) string {
	var lines []string
	for _, cmd := range cmds {
		lines = append(lines, strings.Join(cmd.Env, " ")+" "+cmd.String())
	}
	return strings.Join(lines, "\n")
}

// TestCompanionBuildersRefuseBadInput: every companion builder validates
// before it builds, and the reads and the removal take bare names only.
func TestCompanionBuildersRefuseBadInput(t *testing.T) {
	arch := Distro{ID: "arch"}
	for _, bad := range [][]string{nil, {}, {"-y"}, {"headscale", "--noconfirm"},
		{"tui-tools/../x"}, {"tui-tools/headscale/x"}} {
		if _, err := BuildCompanionInstallOn(ManagerPacman, arch, bad); err == nil {
			t.Errorf("install %q was built", bad)
		}
		if _, err := BuildCompanionUpgradeOn(ManagerAPT, arch, bad); err == nil {
			t.Errorf("upgrade %q was built", bad)
		}
	}
	qualified := []string{"tui-tools/headscale"}
	if _, err := BuildCompanionInstalled(ManagerPacman, qualified); err == nil {
		t.Error("a read takes bare names")
	}
	if _, err := BuildCompanionAvailable(ManagerPacman, qualified); err == nil {
		t.Error("a read takes bare names")
	}
	if _, err := BuildCompanionRemove(ManagerPacman, qualified); err == nil {
		t.Error("a removal takes bare names")
	}
	if _, err := BuildCompanionInstallOn(Manager("zypper"), arch, []string{"headscale"}); err == nil {
		t.Error("an unknown manager must be refused")
	}
}
