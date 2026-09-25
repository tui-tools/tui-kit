package pkgmgr

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A companion is a package of the family that is not a terminal UI, or an
// upstream project a tool drives: headscale (mirrored in the family
// repository), tailscale (from its own repository). Its name is the project's
// own, so the tui-[a-z]+ rule every tool builder holds a name to does not
// apply. The companion builders below give such a name the same argv shapes
// the tools get, including the Omarchy exception, behind a check of their
// own: a name that comes from a catalog, a config file or a keystroke still
// has to be held to a pattern before it can reach an argv.

// ErrInvalidCompanionName reports a name that is not a companion package name.
var ErrInvalidCompanionName = errors.New("pkgmgr: not a companion package name")

// companionNameRe is the shape a companion package name may have: lower-case
// words of letters and digits joined by single hyphens, starting with a
// letter. Nothing a shell or a package manager would read as an option, a
// glob, a version constraint or a path fits it.
var companionNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// MaxCompanionName bounds a companion package name.
const MaxCompanionName = 64

// ValidCompanionName reports whether a name is a companion package name.
// A tool's own name (tui-<word>) is one too.
func ValidCompanionName(name string) bool {
	return len(name) <= MaxCompanionName && companionNameRe.MatchString(name)
}

// CheckCompanionName rejects anything that is not a companion package name.
// It takes the bare name; a repository-qualified target goes through
// CheckCompanionTarget.
func CheckCompanionName(name string) error {
	if !ValidCompanionName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidCompanionName, name)
	}
	return nil
}

// CheckCompanionNames rejects an empty set and any name in it that is not a
// companion package name.
func CheckCompanionNames(names []string) error {
	if len(names) == 0 {
		return errors.New("pkgmgr: no companion package named")
	}
	for _, name := range names {
		if err := CheckCompanionName(name); err != nil {
			return err
		}
	}
	return nil
}

// SplitCompanionTarget reads an install or upgrade target: a companion name,
// optionally qualified with the repository it has to come from, as pacman
// writes it ("tui-tools/headscale"). The repository is "" for a bare name.
// Both halves are validated: the repository as a repository name
// (RepoConfig.Name), the name as a companion name.
func SplitCompanionTarget(target string) (repo, name string, err error) {
	repo, name, qualified := strings.Cut(target, "/")
	if !qualified {
		repo, name = "", target
	} else if !repoNameRe.MatchString(repo) {
		return "", "", fmt.Errorf("%w: %q (repository %q)",
			ErrInvalidCompanionName, target, repo)
	}
	if !ValidCompanionName(name) {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidCompanionName, target)
	}
	return repo, name, nil
}

// CheckCompanionTarget rejects anything SplitCompanionTarget does not accept.
func CheckCompanionTarget(target string) error {
	_, _, err := SplitCompanionTarget(target)
	return err
}

// companionTargets validates install or upgrade targets and returns them in
// the form the manager's argv takes. pacman names a repository in the target
// itself, which matters for a mirror: headscale is also in Arch's own
// repositories, listed before the family's in pacman.conf, so a bare name
// would install the distribution's build while the dialog promised the
// family's. apt and dnf get the bare name: apt reads "name/x" as a release,
// not a repository, and a dnf --repo would also hide the distribution's
// repositories from dependency resolution; there the repository setup's own
// priority decides, as it does for the tools.
func companionTargets(manager Manager, targets []string) ([]string, error) {
	if len(targets) == 0 {
		return nil, errors.New("pkgmgr: no companion package named")
	}
	out := make([]string, len(targets))
	for i, target := range targets {
		_, name, err := SplitCompanionTarget(target)
		if err != nil {
			return nil, err
		}
		out[i] = name
		if manager == ManagerPacman {
			out[i] = target
		}
	}
	return out, nil
}

// BuildCompanionInstalled is BuildInstalled for companion names (bare: the
// local database knows no repository).
func BuildCompanionInstalled(manager Manager, names []string) (Command, error) {
	if err := CheckCompanionNames(names); err != nil {
		return Command{}, err
	}
	return installedCmd(manager, names)
}

// BuildCompanionAvailable is BuildAvailable for companion names (bare).
func BuildCompanionAvailable(manager Manager, names []string) (Command, error) {
	if err := CheckCompanionNames(names); err != nil {
		return Command{}, err
	}
	return availableCmd(manager, names)
}

// BuildCompanionInstallOn is BuildInstallOn for companion targets, each a
// name or a repository-qualified name (SplitCompanionTarget). The steps are
// the tools' own: a refresh then `apt-get install -y` with the apt
// environment, `dnf install -y`, `pacman -Syu --needed` on Arch and `pacman
// -S --needed` on Omarchy. The qualifier reaches pacman's argv and is dropped
// for apt and dnf (companionTargets says why).
func BuildCompanionInstallOn(manager Manager, distro Distro, targets []string) ([]Command, error) {
	args, err := companionTargets(manager, targets)
	if err != nil {
		return nil, err
	}
	if manager == ManagerPacman && distro.Omarchy() {
		return omarchySteps("Install", args), nil
	}
	return installSteps(manager, args)
}

// BuildCompanionUpgradeOn is BuildUpgradeOn for companion targets, with the
// same qualifier rule as BuildCompanionInstallOn.
func BuildCompanionUpgradeOn(manager Manager, distro Distro, targets []string) ([]Command, error) {
	args, err := companionTargets(manager, targets)
	if err != nil {
		return nil, err
	}
	if manager == ManagerPacman && distro.Omarchy() {
		return omarchySteps("Upgrade", args), nil
	}
	return upgradeSteps(manager, args)
}

// BuildCompanionRemove is BuildRemove for companion names (bare: removing
// needs no repository).
func BuildCompanionRemove(manager Manager, names []string) ([]Command, error) {
	if err := CheckCompanionNames(names); err != nil {
		return nil, err
	}
	return removeSteps(manager, names)
}
