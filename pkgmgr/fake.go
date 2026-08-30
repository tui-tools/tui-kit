package pkgmgr

import (
	"context"
	"sort"
	"strings"
)

// Fake is a package manager that touches nothing. It answers from an
// in-memory catalogue and applies to it whatever the previewed command would
// have done, which is what powers the two things every tool in the family
// has:
//
//   - `--demo`: every key works, every command is built and previewed for
//     real, and the machine is never reached;
//   - tests: assert on Fake.Ran to prove that a key produced exactly one
//     command, with exactly the argv the preview showed.
//
// The zero value is usable and behaves like an empty pacman machine; NewFake
// gives a more interesting one to look at.
type Fake struct {
	// Kind is the manager the fake pretends to be. Empty means pacman.
	Kind Manager
	// Machine is what Distro reports.
	Machine Distro
	// Repo is the repository the setup steps configure.
	Repo RepoConfig
	// Configured is what RepoStatus reports.
	Configured bool
	// InstalledPkgs and AvailablePkgs are the catalogue, keyed by package
	// name. Run moves entries between them the way a real install would.
	InstalledPkgs map[string]string
	AvailablePkgs map[string]string
	// Prefix is the escalation prefix shown in the preview of a privileged
	// step, so a demo looks like a real run.
	Prefix string
	// Err, when set, is returned by every Run instead of an answer.
	Err error
	// Ran records every command Run received, in order.
	Ran []Command
}

// NewFake returns a demo machine: Omarchy Server with the repository already
// configured, three of the family's tools installed and a newer version of one
// of them waiting.
func NewFake() *Fake {
	return &Fake{
		Kind: ManagerPacman,
		Machine: Distro{
			ID:         "omarchy-server",
			Like:       []string{"omarchy", "arch"},
			PrettyName: "Omarchy Server",
		},
		Configured: true,
		Prefix:     "sudo -n",
		InstalledPkgs: map[string]string{
			"tui-firewall": "0.2.1-1",
			"tui-systemd":  "0.1.1-1",
			"tui-update":   "0.1.1-1",
		},
		AvailablePkgs: map[string]string{
			"tui-firewall":   "0.2.2-1",
			"tui-systemd":    "0.1.1-1",
			"tui-update":     "0.1.1-1",
			"tui-disk":       "0.1.1-1",
			"tui-logs":       "0.1.1-1",
			"tui-users":      "0.1.1-1",
			"tui-containers": "0.1.1-1",
		},
	}
}

// Manager names the manager the fake pretends to be.
func (f *Fake) Manager() Manager {
	if f.Kind == "" {
		return ManagerPacman
	}
	return f.Kind
}

// Distro describes the fake machine.
func (f *Fake) Distro() Distro { return f.Machine }

// Describe is the header line, marked so nobody mistakes a demo for a machine.
func (f *Fake) Describe() string {
	describe := f.Manager().String() + " (demo)"
	if name := f.Machine.String(); name != "" {
		describe += " on " + name
	}
	return describe
}

// Preview renders the command the way the real thing would, prefix and all.
func (f *Fake) Preview(cmd Command) string {
	if !cmd.Privileged || f.Prefix == "" {
		return cmd.String()
	}
	return f.Prefix + " " + cmd.String()
}

// Run records the command and applies it to the in-memory catalogue, so the
// screen behind it changes exactly as it would on a real machine.
func (f *Fake) Run(_ context.Context, cmd Command) (string, error) {
	f.Ran = append(f.Ran, cmd)
	if f.Err != nil {
		return "", f.Err
	}
	f.apply(cmd)
	return "ok", nil
}

// apply is the fake's whole model of a package manager: a step that installs
// takes the available version, a step that removes drops it, and a step that
// writes a repository file makes the repository configured.
func (f *Fake) apply(cmd Command) {
	if len(cmd.Argv) < 2 {
		return
	}
	if cmd.Argv[0] == "tee" {
		f.Configured = true
		return
	}
	names, action := f.classify(cmd)
	for _, name := range names {
		switch action {
		case "install":
			if version, ok := f.AvailablePkgs[name]; ok {
				f.set(&f.InstalledPkgs, name, version)
			}
		case "remove":
			delete(f.InstalledPkgs, name)
		}
	}
}

// classify reads an action and its package names off a command built here. It
// is deliberately shallow: it recognises the argv this package produces, and
// ignores anything else.
func (f *Fake) classify(cmd Command) (names []string, action string) {
	for _, arg := range cmd.Argv[1:] {
		if ValidName(arg) {
			names = append(names, arg)
		}
	}
	switch cmd.Argv[1] {
	case "install", "-S", "-Syu":
		return names, "install"
	case "remove", "-R":
		return names, "remove"
	}
	return nil, ""
}

// set writes into a map that may not exist yet, so the zero Fake is usable.
func (f *Fake) set(m *map[string]string, name, version string) {
	if *m == nil {
		*m = map[string]string{}
	}
	(*m)[name] = version
}

// Installed reports the installed version of each named package.
func (f *Fake) Installed(_ context.Context, names []string) (map[string]string, error) {
	return f.lookup(names, f.InstalledPkgs)
}

// Available reports the version the fake repository would install.
func (f *Fake) Available(_ context.Context, names []string) (map[string]string, error) {
	return f.lookup(names, f.AvailablePkgs)
}

// lookup validates the names the same way the real path does — a fake that
// accepts what the machine would reject is a fake that hides a bug — and
// answers from the catalogue.
func (f *Fake) lookup(names []string, from map[string]string) (map[string]string, error) {
	if err := CheckNames(names); err != nil {
		return nil, err
	}
	if f.Err != nil {
		return nil, f.Err
	}
	found := map[string]string{}
	for _, name := range names {
		if version, ok := from[name]; ok {
			found[name] = version
		}
	}
	return found, nil
}

// RepoStatus reports the fake machine's repository state.
func (f *Fake) RepoStatus() (RepoStatus, error) {
	repo := f.Repo.normalize()
	if f.Configured {
		return RepoStatus{
			Configured: true,
			Path:       "(demo)",
			Detail:     "the demo machine has " + repo.host() + " configured",
		}, nil
	}
	return RepoStatus{
		Detail: "the demo machine does not have " + repo.host() + " configured",
	}, nil
}

// Install builds the steps that install the named packages.
func (f *Fake) Install(names []string) ([]Command, error) {
	return BuildInstall(f.Manager(), names)
}

// Remove builds the steps that remove them.
func (f *Fake) Remove(names []string) ([]Command, error) {
	return BuildRemove(f.Manager(), names)
}

// Upgrade builds the steps that upgrade them.
func (f *Fake) Upgrade(names []string) ([]Command, error) {
	return BuildUpgrade(f.Manager(), names)
}

// RepoSetup builds the steps that would add the repository.
func (f *Fake) RepoSetup(fingerprint string) (Setup, error) {
	return BuildRepoSetup(f.Manager(), f.Repo, fingerprint)
}

// Catalogue lists every package the fake repository carries, sorted, so a
// demo has something to show without the caller writing the list twice.
func (f *Fake) Catalogue() []string {
	names := make([]string, 0, len(f.AvailablePkgs))
	for name := range f.AvailablePkgs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Reset clears the recorded commands between test cases.
func (f *Fake) Reset() { f.Ran = nil }

// Previews renders every recorded command, for a golden-file assertion.
func (f *Fake) Previews() []string {
	out := make([]string, 0, len(f.Ran))
	for _, cmd := range f.Ran {
		out = append(out, f.Preview(cmd))
	}
	return out
}

// String summarises the fake, for a test failure message.
func (f *Fake) String() string {
	return "pkgmgr.Fake{ran: " + strings.Join(f.Previews(), "; ") + "}"
}
