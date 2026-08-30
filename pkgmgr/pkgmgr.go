// Package pkgmgr answers the four questions a launcher for the tui-tools
// family has to ask the machine it is standing on: which package manager runs
// here, is the family's repository configured, which tui-* packages are
// installed or available, and what exactly would be run to change that.
//
// It is the shared half of what tui-update's own backend already does, lifted
// so the launcher does not grow a second copy of distro detection. What it
// adds on top is the family's own packages: validation of a tui-* name before
// it can reach an argv, the repository probe, and the repository setup steps
// mirrored from pkgs/install.sh and Omarchy Server's tui-tools addon.
//
//	apt      Debian, Ubuntu and their derivatives; dpkg-query and apt-cache.
//	dnf      Fedora, RHEL and their rebuilds; rpm and dnf repoquery.
//	pacman   Arch and Omarchy; pacman -Q and pacman -Si.
//
// Nothing here starts a process. Every command is built as a value, previewed
// by the caller and executed by the kit runner — the family's single exec
// site — so the command line in the dialog is the command line that runs.
// Reads are unprivileged wherever the manager allows one: telling a user what
// is installed must not need a password.
package pkgmgr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tui-tools/tui-kit/runner"
)

// ErrNotAvailable reports that no supported package manager was found on this
// machine. It is the runner's sentinel, so a tool that already treats that
// error as "explain and offer --demo" needs no second case.
var ErrNotAvailable = runner.ErrNotAvailable

// Manager names a package manager. It is a string so it can be written
// straight into a manifest, a --check payload or a log line.
type Manager string

// The managers this package drives. One of them is active on a machine.
const (
	ManagerAPT    Manager = "apt"
	ManagerDNF    Manager = "dnf"
	ManagerPacman Manager = "pacman"
)

// String returns the manager's name.
func (m Manager) String() string { return string(m) }

// Known reports whether a manager is one this package can build commands for.
func (m Manager) Known() bool {
	switch m {
	case ManagerAPT, ManagerDNF, ManagerPacman:
		return true
	}
	return false
}

// installHint is appended to the "not found" error, so the message names what
// the machine would have needed.
const installHint = "tui-tools drives apt, dnf or pacman; " +
	"or use --demo to explore the UI"

// osReleasePath is where the distribution identifies itself.
const osReleasePath = "/etc/os-release"

// managerIDs maps a manager to the /etc/os-release ID values that legitimately
// carry it. A machine whose ID is not listed still works — a derivative nobody
// has heard of is not a reason to refuse — but a machine carrying two managers
// is resolved by this table rather than by the order of a slice.
var managerIDs = map[Manager][]string{
	// Omarchy Server identifies itself as "omarchy-server", with ID_LIKE
	// "omarchy arch". The ID_LIKE fallback would find it anyway; it is listed
	// by its own name because detection should not depend on a derivative
	// remembering to name its parent.
	ManagerPacman: {"arch", "archarm", "omarchy", "omarchy-server",
		"endeavouros", "manjaro", "cachyos"},
	ManagerAPT: {"debian", "ubuntu", "raspbian", "linuxmint", "pop", "devuan"},
	ManagerDNF: {"fedora", "rhel", "centos", "rocky", "almalinux", "ol"},
}

// managerBinary is the binary whose presence makes a manager a candidate.
var managerBinary = map[Manager]string{
	ManagerPacman: "pacman",
	ManagerAPT:    "apt-get",
	ManagerDNF:    "dnf",
}

// managerOrder is the order candidates are considered in when /etc/os-release
// settles nothing.
var managerOrder = []Manager{ManagerPacman, ManagerAPT, ManagerDNF}

// searchPaths are the locations a non-root PATH commonly omits.
var searchPaths = map[string][]string{
	"pacman":      {"/usr/bin/pacman", "/bin/pacman"},
	"pacman-key":  {"/usr/bin/pacman-key", "/bin/pacman-key"},
	"apt-get":     {"/usr/bin/apt-get", "/bin/apt-get"},
	"apt-cache":   {"/usr/bin/apt-cache", "/bin/apt-cache"},
	"dpkg-query":  {"/usr/bin/dpkg-query", "/bin/dpkg-query"},
	"dnf":         {"/usr/bin/dnf", "/bin/dnf"},
	"rpm":         {"/usr/bin/rpm", "/bin/rpm"},
	"curl":        {"/usr/bin/curl", "/bin/curl"},
	"gpg":         {"/usr/bin/gpg", "/bin/gpg"},
	"tee":         {"/usr/bin/tee", "/bin/tee"},
	"install":     {"/usr/bin/install", "/bin/install"},
	"chmod":       {"/usr/bin/chmod", "/bin/chmod"},
	"mktemp":      {"/usr/bin/mktemp", "/bin/mktemp"},
	"pacman-conf": {"/usr/bin/pacman-conf", "/bin/pacman-conf"},
}

// Distro is what /etc/os-release says about the machine.
type Distro struct {
	// ID is the os-release ID field ("fedora", "omarchy-server").
	ID string
	// Like are the ID_LIKE fields, in order, for a derivative that names its
	// parent.
	Like []string
	// PrettyName is the human-readable name, for a header line.
	PrettyName string
}

// String renders the distribution the way a header shows it.
func (d Distro) String() string {
	if d.PrettyName != "" {
		return d.PrettyName
	}
	return d.ID
}

// Matches reports whether this distribution is one that legitimately carries a
// manager, by ID first and then by ID_LIKE.
func (d Distro) Matches(m Manager) bool {
	if matchesID(m, d.ID) {
		return true
	}
	for _, like := range d.Like {
		if matchesID(m, like) {
			return true
		}
	}
	return false
}

// matchesID reports whether a distribution id belongs to a manager.
func matchesID(m Manager, id string) bool {
	if id == "" {
		return false
	}
	for _, known := range managerIDs[m] {
		if strings.EqualFold(known, id) {
			return true
		}
	}
	return false
}

// DetectDistro reads /etc/os-release. An unreadable file is not an error: it
// only means the binary search decides the manager alone.
func DetectDistro() Distro {
	raw, err := os.ReadFile(osReleasePath)
	if err != nil {
		return Distro{}
	}
	return ParseOSRelease(string(raw))
}

// ParseOSRelease reads the ID, ID_LIKE and PRETTY_NAME fields of an
// os-release file. It is exported because the parser is what the tests can
// hold still; the file it reads on a real machine cannot be.
func ParseOSRelease(text string) Distro {
	var d Distro
	for _, line := range splitLines(text) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "ID":
			d.ID = value
		case "ID_LIKE":
			d.Like = strings.Fields(value)
		case "PRETTY_NAME":
			d.PrettyName = value
		}
	}
	return d
}

// Detect picks the package manager this machine runs.
//
// The binary has to be there — a manager that is not installed is not the
// machine's manager, whatever /etc/os-release claims. Among the ones that are,
// the distribution decides: Ubuntu images ship an `rpm` and Fedora images can
// carry an `apt`, and the tool that installs software here is the one the
// distribution itself says it is.
func Detect() (Manager, Distro, error) {
	distro := DetectDistro()
	manager, err := detect(distro, runner.Available)
	return manager, distro, err
}

// detect is Detect with the "is this binary here" test injected, so the
// decision table can be tested without a container per distribution.
func detect(distro Distro, available func(string, ...string) bool) (Manager, error) {
	var installed []Manager
	for _, manager := range managerOrder {
		bin := managerBinary[manager]
		if available(bin, searchPaths[bin]...) {
			installed = append(installed, manager)
		}
	}
	if len(installed) == 0 {
		return "", fmt.Errorf(
			"pkgmgr: no supported package manager found: %w (%s)",
			ErrNotAvailable, installHint)
	}
	// The ID is a stronger claim than ID_LIKE, so every candidate is offered
	// the ID before any of them is offered a parent name.
	for _, manager := range installed {
		if matchesID(manager, distro.ID) {
			return manager, nil
		}
	}
	for _, like := range distro.Like {
		for _, manager := range installed {
			if matchesID(manager, like) {
				return manager, nil
			}
		}
	}
	return installed[0], nil
}

// Interface is the whole contract, so a UI can hold either the real thing or
// the Fake.
type Interface interface {
	// Manager names the package manager in use.
	Manager() Manager
	// Distro describes the machine.
	Distro() Distro
	// Describe is the one-line summary for a header.
	Describe() string
	// Preview renders the exact command line Run will execute.
	Preview(cmd Command) string
	// Run executes a previously previewed command.
	Run(ctx context.Context, cmd Command) (string, error)
	// Installed reports the installed version of each named package.
	Installed(ctx context.Context, names []string) (map[string]string, error)
	// Available reports the version the repositories would install.
	Available(ctx context.Context, names []string) (map[string]string, error)
	// RepoStatus reports whether the tui-tools repository is configured.
	RepoStatus() (RepoStatus, error)
	// Install builds the previewable steps that install the named packages.
	Install(names []string) ([]Command, error)
	// Remove builds the steps that remove them.
	Remove(names []string) ([]Command, error)
	// Upgrade builds the steps that upgrade them.
	Upgrade(names []string) ([]Command, error)
	// RepoSetup builds the steps that add the repository, pinning the key by
	// the fingerprint the caller passes in.
	RepoSetup(fingerprint string) (Setup, error)
}

// Options describes the machine a Real drives.
type Options struct {
	// SudoPrefix is the escalation command line, split into argv
	// ("sudo", "-n"), as the tool's configuration carries it. Empty runs the
	// commands directly.
	SudoPrefix []string
	// Repo overrides the repository the setup steps configure and the probe
	// looks for. The zero value is the family's own.
	Repo RepoConfig
	// Root prefixes every path RepoStatus reads. It exists so the probe can
	// be tested against a directory tree instead of the machine's own /etc;
	// leave it empty everywhere else.
	Root string
}

// Real drives the machine's package manager through the kit runner.
type Real struct {
	manager Manager
	distro  Distro
	repo    RepoConfig
	root    string
	// runners holds one runner per binary, keyed by the name that appears in
	// a Command's Argv[0]. A binary the machine does not have is absent, and
	// the code that wanted it says so where its answer would have been.
	runners map[string]*runner.Runner
}

// New detects the manager, locates the binaries and, when not running as
// root, validates the configured privilege prefix — so a machine that cannot
// escalate says so at startup rather than halfway through an install.
func New(opts Options) (*Real, error) {
	manager, distro, err := Detect()
	if err != nil {
		return nil, err
	}
	r := &Real{
		manager: manager,
		distro:  distro,
		repo:    opts.Repo.normalize(),
		root:    opts.Root,
		runners: map[string]*runner.Runner{},
	}
	// Every binary here is registered as an unprivileged reader: the reads
	// this package makes answer to any user from the metadata already on
	// disk, and only Run — the install, the remove, the repository write —
	// escalates.
	unprivileged := false
	for bin := range searchPaths {
		run, newErr := runner.New(runner.Options{
			Bin:             bin,
			SearchPaths:     searchPaths[bin],
			SudoPrefix:      opts.SudoPrefix,
			InstallHint:     installHint,
			PrivilegedReads: &unprivileged,
		})
		if newErr != nil {
			continue
		}
		r.runners[bin] = run
	}
	if r.runners[managerBinary[manager]] == nil {
		return nil, fmt.Errorf("pkgmgr: %s was detected but cannot be run: %w",
			manager, ErrNotAvailable)
	}
	return r, nil
}

// Manager names the package manager in use.
func (r *Real) Manager() Manager { return r.manager }

// Distro describes the machine.
func (r *Real) Distro() Distro { return r.distro }

// Describe names the manager and how it is reached, for the header.
func (r *Real) Describe() string {
	describe := r.runners[managerBinary[r.manager]].Describe()
	if name := r.distro.String(); name != "" {
		describe += " on " + name
	}
	return describe
}

// Has reports whether a binary a step needs is available on this machine.
func (r *Real) Has(bin string) bool { return r.runners[bin] != nil }

// runnerFor picks the runner that owns a command, by its argv[0].
func (r *Real) runnerFor(cmd Command) *runner.Runner {
	if len(cmd.Argv) == 0 {
		return nil
	}
	return r.runners[cmd.Argv[0]]
}

// Preview renders the exact command line Run will execute. Every command goes
// through the runner of its own binary, so the preview carries the privilege
// prefix that binary will really be called with.
func (r *Real) Preview(cmd Command) string {
	run := r.runnerFor(cmd)
	if run == nil {
		return cmd.String()
	}
	if !cmd.Privileged {
		// An unprivileged step shows no prefix, because none will be added.
		return cmd.String()
	}
	return run.Preview(cmd.runnerCommand())
}

// Run executes a previewed command.
func (r *Real) Run(ctx context.Context, cmd Command) (string, error) {
	run := r.runnerFor(cmd)
	if run == nil {
		return "", fmt.Errorf("pkgmgr: %q is not available on this machine: %w",
			firstArg(cmd), ErrNotAvailable)
	}
	if !cmd.Privileged {
		return run.Read(ctx, cmd.Argv...)
	}
	return run.Run(ctx, cmd.runnerCommand())
}

// firstArg names the binary a command wanted, for an error message.
func firstArg(cmd Command) string {
	if len(cmd.Argv) == 0 {
		return "(empty command)"
	}
	return cmd.Argv[0]
}

// Installed reports the installed version of each named package, keyed by
// name. A package that is not installed is simply absent from the map, which
// is the answer rather than an error.
//
// The query commands all exit non-zero when one of the names is unknown, and
// print the versions of the ones that are known anyway, so the output decides
// and the exit status is only consulted when there is nothing to read.
func (r *Real) Installed(ctx context.Context, names []string) (map[string]string, error) {
	cmd, err := BuildInstalled(r.manager, names)
	if err != nil {
		return nil, err
	}
	out, err := r.Run(ctx, cmd)
	versions := parseInstalled(r.manager, out)
	if len(versions) == 0 && err != nil && !notInstalledOnly(out) {
		return nil, err
	}
	return versions, nil
}

// Available reports the version the repositories would install, keyed by
// name. A package no repository carries is absent from the map.
func (r *Real) Available(ctx context.Context, names []string) (map[string]string, error) {
	cmd, err := BuildAvailable(r.manager, names)
	if err != nil {
		return nil, err
	}
	out, err := r.Run(ctx, cmd)
	versions := parseAvailable(r.manager, out)
	if len(versions) == 0 && err != nil && !notInstalledOnly(out) {
		return nil, err
	}
	return versions, nil
}

// notInstalledOnly reports whether the output is nothing but the managers'
// "no such package" complaints. Those are an answer — the package is not
// there — and the caller reads it from the absent map key.
func notInstalledOnly(out string) bool {
	lines := splitLines(out)
	if len(lines) == 0 {
		return true
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.Contains(line, "is not installed"),
			strings.Contains(line, "no packages found matching"),
			strings.Contains(line, "not installed and no information"),
			strings.Contains(line, "was not found"),
			strings.Contains(line, "No matching Packages"),
			strings.Contains(line, "package not found"),
			strings.Contains(line, "error: package"),
			strings.Contains(line, "No package"):
		default:
			return false
		}
	}
	return true
}

// Install builds the steps that install the named packages.
func (r *Real) Install(names []string) ([]Command, error) {
	return BuildInstall(r.manager, names)
}

// Remove builds the steps that remove the named packages.
func (r *Real) Remove(names []string) ([]Command, error) {
	return BuildRemove(r.manager, names)
}

// Upgrade builds the steps that upgrade the named packages.
func (r *Real) Upgrade(names []string) ([]Command, error) {
	return BuildUpgrade(r.manager, names)
}

// RepoSetup builds the steps that add the tui-tools repository, pinning the
// signing key by the fingerprint the caller passes in.
func (r *Real) RepoSetup(fingerprint string) (Setup, error) {
	return BuildRepoSetup(r.manager, r.repo, fingerprint)
}

// RepoStatus reports whether the tui-tools repository is configured for this
// manager. It reads files rather than asking the manager, because the answer
// has to be available before anything is run and a `dnf repolist` on a
// machine with no metadata is a network call.
func (r *Real) RepoStatus() (RepoStatus, error) {
	return repoStatus(r.manager, r.repo, osDirFS{root: r.fsRoot()})
}

// fsRoot is the directory RepoStatus reads under: the machine's own root,
// unless a test pointed it somewhere else.
func (r *Real) fsRoot() string {
	if r.root == "" {
		return "/"
	}
	return r.root
}

// RepoStatus is what the repository probe found.
type RepoStatus struct {
	// Configured reports whether this manager would fetch tui-* packages
	// from the family's repository.
	Configured bool
	// Path is the file that configures it, when there is one.
	Path string
	// Detail is one line saying how the answer was reached, for a screen
	// that has to explain itself rather than show a checkbox.
	Detail string
}

// repoStatus is RepoStatus against an arbitrary filesystem, so the probe can
// be tested against a directory tree.
func repoStatus(manager Manager, repo RepoConfig, fsys fs) (RepoStatus, error) {
	repo = repo.normalize()
	host := repo.host()
	switch manager {
	case ManagerAPT:
		return probeDir(fsys, "etc/apt/sources.list.d", host,
			"an apt sources file naming "+host)
	case ManagerDNF:
		return probeDir(fsys, "etc/yum.repos.d", host,
			"a dnf .repo file naming "+host)
	case ManagerPacman:
		return probePacman(fsys, repo)
	default:
		return RepoStatus{}, fmt.Errorf("pkgmgr: %w %q", errUnknownManager, manager)
	}
}

// errUnknownManager is a manager this build has no commands for. It is a
// programming error rather than a fact about the machine.
var errUnknownManager = errors.New("pkgmgr: unknown package manager")

// probeDir looks for a file under dir that mentions the repository host. The
// name of the file is not what decides — an administrator is free to call it
// anything — so the content is what is read.
func probeDir(fsys fs, dir, host, what string) (RepoStatus, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return RepoStatus{
			Detail: dir + " could not be read, so the repository is assumed " +
				"not to be configured",
		}, nil
	}
	for _, name := range entries {
		body, readErr := fsys.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			continue
		}
		if !mentionsHost(string(body), host) {
			continue
		}
		return RepoStatus{
			Configured: true,
			Path:       "/" + filepath.Join(dir, name),
			Detail:     "/" + filepath.Join(dir, name) + " is " + what,
		}, nil
	}
	return RepoStatus{
		Detail: "no file under /" + dir + " names " + host,
	}, nil
}

// mentionsHost reports whether a repository file points at the host, ignoring
// the lines that are commented out: a sources file whose only mention of the
// repository is a `#` line does not configure anything.
func mentionsHost(body, host string) bool {
	for _, line := range splitLines(body) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, host) {
			return true
		}
	}
	return false
}

// probePacman looks for a [tui-tools] section reachable from
// /etc/pacman.conf, which on a real machine is usually one Include away:
// pkgs/install.sh appends the section itself, and Omarchy Server's addon
// writes /etc/pacman.d/tui-tools.conf and includes it.
func probePacman(fsys fs, repo RepoConfig) (RepoStatus, error) {
	const conf = "etc/pacman.conf"
	body, err := fsys.ReadFile(conf)
	if err != nil {
		return RepoStatus{
			Detail: "/" + conf + " could not be read, so the repository is " +
				"assumed not to be configured",
		}, nil
	}
	section := "[" + repo.Name + "]"
	if hasSection(string(body), section) {
		return RepoStatus{
			Configured: true,
			Path:       "/" + conf,
			Detail:     "/" + conf + " has a " + section + " section",
		}, nil
	}
	for _, include := range includedFiles(string(body)) {
		included, readErr := fsys.ReadFile(strings.TrimPrefix(include, "/"))
		if readErr != nil {
			continue
		}
		if hasSection(string(included), section) {
			return RepoStatus{
				Configured: true,
				Path:       include,
				Detail: include + " has a " + section + " section, included " +
					"from /" + conf,
			}, nil
		}
	}
	return RepoStatus{
		Detail: "no " + section + " section is reachable from /" + conf,
	}, nil
}

// hasSection reports whether a pacman configuration declares a section, on a
// line that is not commented out.
func hasSection(body, section string) bool {
	for _, line := range splitLines(body) {
		if strings.TrimSpace(line) == section {
			return true
		}
	}
	return false
}

// includedFiles lists the absolute paths a pacman configuration includes. A
// glob is not expanded: the family's own include is a single named file, and
// walking a wildcard is more filesystem than a probe needs.
func includedFiles(body string) []string {
	var files []string
	for _, line := range splitLines(body) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(key) != "Include" {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsAny(value, "*?[") {
			continue
		}
		files = append(files, value)
	}
	return files
}

// fs is the sliver of a filesystem the repository probe reads. It is an
// interface so the probe can be pointed at a fixture tree.
type fs interface {
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]string, error)
}

// osDirFS reads under a directory on the real filesystem.
type osDirFS struct{ root string }

// ReadFile reads one file under the root. os.DirFS would do as well, but its
// ReadDir returns entries where this probe only wants names, so the two calls
// it makes are spelled out here instead.
func (o osDirFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(o.root, name)) //nolint:gosec // a fixed path under a configured root
}

// ReadDir lists the names of a directory's entries, sorted.
func (o osDirFS) ReadDir(name string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(o.root, name))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}
