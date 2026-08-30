package pkgmgr

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/tui-tools/tui-kit/runner"
)

// Command is one invocation the user is about to be shown. It is a value, and
// the same value is what the runner executes: the command line in the dialog
// is the command line that runs, which is the only promise this family makes.
type Command struct {
	// Argv is the command line, argv[0] first. It never contains a shell
	// construct, because nothing here reaches a shell.
	Argv []string
	// Privileged reports that the step changes the machine and needs the
	// escalation prefix. A read is left unprivileged, so listing what is
	// installed never raises a password prompt.
	Privileged bool
	// Explain is one line saying what the step is for, for the preview.
	Explain string
	// Stdin is written to the process's standard input and closed. It carries
	// the body of a repository file, which `tee` writes: a multi-line file
	// cannot be an argument, and a shell redirection is not available to a
	// family that builds argv values.
	Stdin string
}

// String renders the command the way the user reads it in the preview.
func (c Command) String() string { return strings.Join(c.Argv, " ") }

// runnerCommand converts a step into the value the kit runner takes.
func (c Command) runnerCommand() runner.Command {
	return runner.Command{
		Argv:        c.Argv,
		Description: c.Explain,
		Destructive: c.Destructive(),
		Stdin:       c.Stdin,
	}
}

// Destructive reports whether the step takes software off the machine, so a
// confirm dialog can paint itself in the danger colour.
func (c Command) Destructive() bool {
	if len(c.Argv) < 2 {
		return false
	}
	switch c.Argv[1] {
	case "remove", "-R", "-Rs", "-Rns":
		return true
	}
	return false
}

// ErrInvalidName reports a package name that is not a tui-tools package.
var ErrInvalidName = errors.New("pkgmgr: not a tui-tools package name")

// packageNameRe is the whole set of names this package will build a command
// from. The family's packages are `tui-` and lower-case letters, nothing else,
// and a name that comes from a config file, a URL or a keystroke has to be
// held to that before it can reach an argv.
var packageNameRe = regexp.MustCompile(`^tui-[a-z]+$`)

// ValidName reports whether a name is a tui-tools package name.
func ValidName(name string) bool { return packageNameRe.MatchString(name) }

// CheckName rejects anything that is not a tui-tools package name.
func CheckName(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return nil
}

// CheckNames rejects an empty set and any name in it that is not a tui-tools
// package name. Every builder starts here, so no command in this package can
// be assembled from input nobody validated.
func CheckNames(names []string) error {
	if len(names) == 0 {
		return errors.New("pkgmgr: no package named")
	}
	for _, name := range names {
		if err := CheckName(name); err != nil {
			return err
		}
	}
	return nil
}

// fingerprintRe is a full 40-character OpenPGP v4 fingerprint. A short key id
// is not accepted: it is what makes a pinned key not actually pinned.
var fingerprintRe = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)

// CheckFingerprint rejects anything that is not a full OpenPGP fingerprint,
// and returns it upper-cased, which is the form pacman-key and gpg print.
func CheckFingerprint(fingerprint string) (string, error) {
	clean := strings.ReplaceAll(fingerprint, " ", "")
	if !fingerprintRe.MatchString(clean) {
		return "", fmt.Errorf(
			"pkgmgr: %q is not a 40-character OpenPGP fingerprint", fingerprint)
	}
	return strings.ToUpper(clean), nil
}

// The repository defaults: the family's own, which is what every machine
// wants unless it is being pointed at a staging copy.
const (
	// DefaultRepoURL is where the family publishes its packages.
	DefaultRepoURL = "https://pkgs.tui.tools"
	// DefaultRepoName is the repository's name in every manager's
	// configuration, and the stem of every file the setup writes.
	DefaultRepoName = "tui-tools"
	// DefaultSuite is the apt suite.
	DefaultSuite = "stable"
	// DefaultComponent is the apt component.
	DefaultComponent = "main"
)

// RepoConfig describes the repository the setup steps configure and the probe
// looks for. The zero value is the family's own.
type RepoConfig struct {
	// URL is the repository root, without a trailing slash. It must be
	// https: an unsigned transport for a signing key is not a transport.
	URL string
	// Name is the repository's name, and the stem of the files written.
	Name string
	// Suite and Component are apt's, and unused by the other two managers.
	Suite, Component string
}

// normalize fills in the family's own repository for every empty field.
func (c RepoConfig) normalize() RepoConfig {
	if c.URL == "" {
		c.URL = DefaultRepoURL
	}
	c.URL = strings.TrimRight(c.URL, "/")
	if c.Name == "" {
		c.Name = DefaultRepoName
	}
	if c.Suite == "" {
		c.Suite = DefaultSuite
	}
	if c.Component == "" {
		c.Component = DefaultComponent
	}
	return c
}

// repoURLRe is the shape a repository URL may have. It ends up in an argv and
// in a file every package on the machine is then trusted from, so it is
// checked rather than assumed.
var repoURLRe = regexp.MustCompile(`^https://[a-z0-9.-]+(:[0-9]{1,5})?(/[A-Za-z0-9._~/-]*)?$`)

// repoNameRe is the shape a repository name may have: it becomes a file name
// and a section header.
var repoNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Validate rejects a repository that could not safely be written into a
// manager's configuration.
func (c RepoConfig) Validate() error {
	c = c.normalize()
	if !repoURLRe.MatchString(c.URL) {
		return fmt.Errorf("pkgmgr: %q is not an https repository URL", c.URL)
	}
	if !repoNameRe.MatchString(c.Name) {
		return fmt.Errorf("pkgmgr: %q is not a repository name", c.Name)
	}
	for _, field := range []string{c.Suite, c.Component} {
		if !repoNameRe.MatchString(field) {
			return fmt.Errorf("pkgmgr: %q is not an apt suite or component", field)
		}
	}
	return nil
}

// host is the repository's host name, which is what the probe looks for in a
// configuration file: the path may differ between a mirror and the original,
// the host is what identifies the repository.
func (c RepoConfig) host() string {
	rest := strings.TrimPrefix(c.normalize().URL, "https://")
	host, _, _ := strings.Cut(rest, "/")
	return host
}

// ---------------------------------------------------------------- reads ---

// BuildInstalled asks the machine which of the named packages are installed,
// and at which version. Every one of them is a local database query: no
// metadata is refreshed, nothing is downloaded, and no privilege is needed.
func BuildInstalled(manager Manager, names []string) (Command, error) {
	if err := CheckNames(names); err != nil {
		return Command{}, err
	}
	switch manager {
	case ManagerAPT:
		return Command{
			Argv: append([]string{
				"dpkg-query", "-W", "-f=${Package}|${Version}\n",
			}, names...),
			Explain: "Read the installed versions from the dpkg database",
		}, nil
	case ManagerDNF:
		return Command{
			Argv: append([]string{
				"rpm", "-q", "--qf",
				`%{NAME}|%|EPOCH?{%{EPOCH}:}|%{VERSION}-%{RELEASE}` + "\n",
			}, names...),
			Explain: "Read the installed versions from the rpm database",
		}, nil
	case ManagerPacman:
		return Command{
			Argv:    append([]string{"pacman", "-Q"}, names...),
			Explain: "Read the installed versions from the pacman database",
		}, nil
	default:
		return Command{}, fmt.Errorf("%w %q", errUnknownManager, manager)
	}
}

// BuildAvailable asks which version the repositories would install.
//
// It answers from the metadata already on disk — `apt-cache policy` reads the
// apt lists, `dnf repoquery` its cache, `pacman -Si` the sync database — so a
// machine whose repository was just added, and whose lists have not been
// refreshed since, honestly reports nothing rather than refreshing behind the
// user's back. The refresh is a privileged write, and it is a previewed step
// of its own.
func BuildAvailable(manager Manager, names []string) (Command, error) {
	if err := CheckNames(names); err != nil {
		return Command{}, err
	}
	switch manager {
	case ManagerAPT:
		return Command{
			Argv:    append([]string{"apt-cache", "policy"}, names...),
			Explain: "Read the candidate versions from the apt lists on disk",
		}, nil
	case ManagerDNF:
		return Command{
			Argv: append([]string{
				"dnf", "--quiet", "repoquery", "--latest-limit", "1",
				"--qf", `%{name}|%{evr}` + "\n",
			}, names...),
			Explain: "Read the available versions from the dnf cache",
		}, nil
	case ManagerPacman:
		return Command{
			Argv:    append([]string{"pacman", "-Si"}, names...),
			Explain: "Read the available versions from the pacman sync database",
		}, nil
	default:
		return Command{}, fmt.Errorf("%w %q", errUnknownManager, manager)
	}
}

// -------------------------------------------------------------- mutations ---

// BuildRefresh is the metadata refresh. It is a privileged write to the
// manager's cache, which is why it is a step of the previewed sequence rather
// than something a read path does behind the user's back.
func BuildRefresh(manager Manager) (Command, error) {
	switch manager {
	case ManagerAPT:
		return Command{
			Argv:       []string{"apt-get", "update"},
			Privileged: true,
			Explain:    "Refresh the apt package lists",
		}, nil
	case ManagerDNF:
		return Command{
			Argv:       []string{"dnf", "-q", "makecache"},
			Privileged: true,
			Explain:    "Refresh the dnf metadata cache",
		}, nil
	case ManagerPacman:
		return Command{
			Argv:       []string{"pacman", "-Sy"},
			Privileged: true,
			Explain:    "Refresh the pacman sync databases",
		}, nil
	default:
		return Command{}, fmt.Errorf("%w %q", errUnknownManager, manager)
	}
}

// BuildInstall builds the steps that install the named packages.
//
// apt is given a refresh first, because a repository that was added a moment
// ago is invisible to it until its lists are fetched; dnf refreshes an expired
// cache on its own; pacman refreshes and upgrades in the one step Arch
// supports. Every step is privileged, every step is previewed, and the
// sequence stops at the first one that fails.
func BuildInstall(manager Manager, names []string) ([]Command, error) {
	if err := CheckNames(names); err != nil {
		return nil, err
	}
	refresh, err := BuildRefresh(manager)
	if err != nil {
		return nil, err
	}
	switch manager {
	case ManagerAPT:
		return []Command{refresh, {
			Argv:       append([]string{"apt-get", "install", "-y"}, names...),
			Privileged: true,
			Explain:    "Install " + strings.Join(names, ", "),
		}}, nil
	case ManagerDNF:
		return []Command{{
			Argv:       append([]string{"dnf", "install", "-y"}, names...),
			Privileged: true,
			Explain:    "Install " + strings.Join(names, ", "),
		}}, nil
	case ManagerPacman:
		// Not `-Sy` then `-S`: on Arch a refreshed database followed by a
		// plain install is the partial upgrade the distribution warns
		// against, since the new package is linked against libraries the
		// machine has not updated. `-Syu` with the names is the supported
		// form, and the preview shows it for what it is: an install that
		// brings the rest of the system along.
		return []Command{{
			Argv: append([]string{
				"pacman", "-Syu", "--needed", "--noconfirm",
			}, names...),
			Privileged: true,
			Explain:    "Install " + strings.Join(names, ", ") + ", upgrading the system with them",
		}}, nil
	default:
		return nil, fmt.Errorf("%w %q", errUnknownManager, manager)
	}
}

// BuildRemove builds the steps that take the named packages off the machine.
// Nothing else is removed with them: the dependencies a tool pulled in are
// left alone, because an autoremove decided by a launcher is how an unrelated
// package disappears.
func BuildRemove(manager Manager, names []string) ([]Command, error) {
	if err := CheckNames(names); err != nil {
		return nil, err
	}
	switch manager {
	case ManagerAPT:
		return []Command{{
			Argv:       append([]string{"apt-get", "remove", "-y"}, names...),
			Privileged: true,
			Explain:    "Remove " + strings.Join(names, ", "),
		}}, nil
	case ManagerDNF:
		return []Command{{
			Argv:       append([]string{"dnf", "remove", "-y"}, names...),
			Privileged: true,
			Explain:    "Remove " + strings.Join(names, ", "),
		}}, nil
	case ManagerPacman:
		return []Command{{
			Argv:       append([]string{"pacman", "-R", "--noconfirm"}, names...),
			Privileged: true,
			Explain:    "Remove " + strings.Join(names, ", "),
		}}, nil
	default:
		return nil, fmt.Errorf("%w %q", errUnknownManager, manager)
	}
}

// BuildUpgrade builds the steps that upgrade the named packages.
//
// On pacman that is `-Syu` with the names: Arch has no supported way to
// upgrade one package against a refreshed database without upgrading the
// machine with it, and pretending otherwise is how a partial upgrade breaks a
// system. The other two upgrade only what was asked for.
func BuildUpgrade(manager Manager, names []string) ([]Command, error) {
	if err := CheckNames(names); err != nil {
		return nil, err
	}
	refresh, err := BuildRefresh(manager)
	if err != nil {
		return nil, err
	}
	switch manager {
	case ManagerAPT:
		return []Command{refresh, {
			Argv: append([]string{
				"apt-get", "install", "--only-upgrade", "-y",
			}, names...),
			Privileged: true,
			Explain:    "Upgrade " + strings.Join(names, ", "),
		}}, nil
	case ManagerDNF:
		return []Command{{
			Argv:       append([]string{"dnf", "upgrade", "-y"}, names...),
			Privileged: true,
			Explain:    "Upgrade " + strings.Join(names, ", "),
		}}, nil
	case ManagerPacman:
		return []Command{{
			Argv:       append([]string{"pacman", "-Syu", "--noconfirm"}, names...),
			Privileged: true,
			Explain: "Upgrade " + strings.Join(names, ", ") +
				" (pacman upgrades the machine with them: a partial upgrade " +
				"is not supported on Arch)",
		}}, nil
	default:
		return nil, fmt.Errorf("%w %q", errUnknownManager, manager)
	}
}

// ------------------------------------------------------------ repository ---

// Setup is the previewed sequence that adds the tui-tools repository, plus the
// one thing a sequence of commands cannot express on its own: which step's
// output has to be compared against the pinned fingerprint before the rest of
// it is allowed to run.
//
// A caller that skips that check has imported whatever the network handed
// over, which is the failure the pinning exists to prevent.
type Setup struct {
	// Steps are the commands, in order.
	Steps []Command
	// Verify is the index in Steps of the `gpg --show-keys` call whose output
	// ParseKeyFingerprint reads.
	Verify int
	// Fingerprint is what that output has to be, upper-cased.
	Fingerprint string
}

// Match reports whether the output of Steps[Verify] carries the pinned key. A
// caller runs the step, hands the output here, and stops the sequence when
// this is false.
func (s Setup) Match(out string) bool {
	got := ParseKeyFingerprint(out)
	return got != "" && got == s.Fingerprint
}

// The files the setup writes. They are named here rather than inline because
// the probe, the setup and a smoke test all have to agree on them.
const (
	// APTKeyringDir is where a Debian machine keeps repository keys today.
	APTKeyringDir = "/etc/apt/keyrings"
	// APTSourcesDir holds one sources file per repository.
	APTSourcesDir = "/etc/apt/sources.list.d"
	// DNFKeyDir is where an rpm machine keeps repository keys.
	DNFKeyDir = "/etc/pki/rpm-gpg"
	// DNFReposDir holds one .repo file per repository.
	DNFReposDir = "/etc/yum.repos.d"
	// PacmanConf is the configuration every pacman repository is reachable
	// from, directly or through an Include.
	PacmanConf = "/etc/pacman.conf"
	// PacmanConfDir is where the family's own section is written, so
	// pacman.conf gains one Include line rather than a block that a later
	// setup would have to find again.
	PacmanConfDir = "/etc/pacman.d"
)

// BuildRepoSetup returns the steps that add the tui-tools repository to this
// manager, with the signing key pinned by fingerprint.
//
// They mirror what pkgs/install.sh and Omarchy Server's `tui-tools` addon
// already do on real machines, with one difference: nothing here is piped
// through a shell, so the key is downloaded to a root-owned directory as its
// own step and the repository files are written by `tee` from standard input.
//
// The fingerprint is the caller's. This package has no business carrying the
// family's key material, and a fingerprint compiled into a library is one
// that cannot be rotated without a release.
func BuildRepoSetup(manager Manager, repo RepoConfig, fingerprint string) (Setup, error) {
	repo = repo.normalize()
	if err := repo.Validate(); err != nil {
		return Setup{}, err
	}
	fpr, err := CheckFingerprint(fingerprint)
	if err != nil {
		return Setup{}, err
	}
	switch manager {
	case ManagerAPT:
		return setupAPT(repo, fpr), nil
	case ManagerDNF:
		return setupDNF(repo, fpr), nil
	case ManagerPacman:
		return setupPacman(repo, fpr), nil
	default:
		return Setup{}, fmt.Errorf("%w %q", errUnknownManager, manager)
	}
}

// fetchKey downloads the armoured public key into a root-owned directory.
// --proto and --tlsv1.2 are there so a redirect cannot move the download onto
// a transport that a signing key has no business travelling over.
func fetchKey(repo RepoConfig, dest string) Command {
	return Command{
		Argv: []string{
			"curl", "-fsSL", "--proto", "=https", "--tlsv1.2", "--retry", "3",
			"-o", dest, repo.URL + "/pubkey.asc",
		},
		Privileged: true,
		Explain:    "Download the repository signing key to " + dest,
	}
}

// showKey is the step whose output is compared against the pinned
// fingerprint. It reads a file and prints what is in it, so it needs no
// privilege.
func showKey(dest, fpr string) Command {
	return Command{
		Argv:    []string{"gpg", "--show-keys", "--with-colons", dest},
		Explain: "Read the downloaded key's fingerprint; it must be " + fpr,
	}
}

// writeFile is a repository file written by `tee` from standard input, which
// is how a multi-line file is created without a shell redirection.
func writeFile(path, body, explain string) Command {
	return Command{
		Argv:       []string{"tee", path},
		Privileged: true,
		Explain:    explain,
		Stdin:      body,
	}
}

// setupAPT mirrors install.sh's apt path: a keyring directory, the dearmoured
// key beside it, one sources file naming it, and a refresh.
func setupAPT(repo RepoConfig, fpr string) Setup {
	armoured := APTKeyringDir + "/" + repo.Name + ".asc"
	keyring := APTKeyringDir + "/" + repo.Name + ".gpg"
	line := fmt.Sprintf("deb [signed-by=%s] %s/deb %s %s\n",
		keyring, repo.URL, repo.Suite, repo.Component)
	refresh, _ := BuildRefresh(ManagerAPT)
	return Setup{
		Verify:      2,
		Fingerprint: fpr,
		Steps: []Command{
			{
				Argv:       []string{"install", "-d", "-m", "0755", APTKeyringDir},
				Privileged: true,
				Explain:    "Make sure " + APTKeyringDir + " exists",
			},
			fetchKey(repo, armoured),
			showKey(armoured, fpr),
			{
				Argv: []string{
					"gpg", "--batch", "--yes", "--dearmor", "-o", keyring, armoured,
				},
				Privileged: true,
				Explain:    "Convert the key to the binary form apt reads",
			},
			{
				Argv:       []string{"chmod", "0644", keyring},
				Privileged: true,
				Explain:    "Let apt read the keyring as any user",
			},
			writeFile(APTSourcesDir+"/"+repo.Name+".list", line,
				"Add the repository, signed by that key alone"),
			refresh,
		},
	}
}

// setupDNF mirrors install.sh's dnf path, with the .repo file written from
// the known fields rather than downloaded: the file that says which key to
// trust should not itself arrive over the network.
func setupDNF(repo RepoConfig, fpr string) Setup {
	key := DNFKeyDir + "/RPM-GPG-KEY-" + repo.Name
	body := fmt.Sprintf(`[%s]
name=%s
baseurl=%s/rpm/$basearch
enabled=1
# The package signatures.
gpgcheck=1
# The repository metadata signature, repomd.xml.asc. Checking both means a
# tampered index is caught before a package is even downloaded.
repo_gpgcheck=1
gpgkey=file://%s
metadata_expire=1h
`, repo.Name, repo.Name, repo.URL, key)
	refresh, _ := BuildRefresh(ManagerDNF)
	return Setup{
		Verify:      2,
		Fingerprint: fpr,
		Steps: []Command{
			{
				Argv:       []string{"install", "-d", "-m", "0755", DNFKeyDir},
				Privileged: true,
				Explain:    "Make sure " + DNFKeyDir + " exists",
			},
			fetchKey(repo, key),
			showKey(key, fpr),
			{
				Argv:       []string{"rpm", "--import", key},
				Privileged: true,
				Explain:    "Import the key into the rpm keyring",
			},
			writeFile(DNFReposDir+"/"+repo.Name+".repo", body,
				"Add the repository, with both the packages and the index "+
					"signature checked"),
			refresh,
		},
	}
}

// setupPacman mirrors Omarchy Server's tui-tools addon, which is the path
// that has been through an ISO build and an install: add the key, sign it
// locally — without that every package from the repository is rejected as
// untrusted — write the section to its own file, and include that file once.
func setupPacman(repo RepoConfig, fpr string) Setup {
	key := PacmanConfDir + "/" + repo.Name + ".pubkey.asc"
	conf := PacmanConfDir + "/" + repo.Name + ".conf"
	// $arch is pacman's own variable, expanded by pacman per architecture,
	// which is why one file serves every machine.
	body := fmt.Sprintf(`# The %s repository (%s), written by tui-tools.
[%s]
SigLevel = Required TrustedOnly
Server = %s/arch/$arch
`, repo.Name, repo.URL, repo.Name, repo.URL)
	include := fmt.Sprintf("\n# Added by tui-tools.\nInclude = %s\n", conf)
	refresh, _ := BuildRefresh(ManagerPacman)
	return Setup{
		Verify:      2,
		Fingerprint: fpr,
		Steps: []Command{
			{
				Argv:       []string{"install", "-d", "-m", "0755", PacmanConfDir},
				Privileged: true,
				Explain:    "Make sure " + PacmanConfDir + " exists",
			},
			fetchKey(repo, key),
			showKey(key, fpr),
			{
				Argv:       []string{"pacman-key", "--add", key},
				Privileged: true,
				Explain:    "Add the key to pacman's keyring",
			},
			{
				Argv:       []string{"pacman-key", "--lsign-key", fpr},
				Privileged: true,
				Explain: "Sign the key locally; without this every package " +
					"from the repository is rejected as untrusted",
			},
			writeFile(conf, body,
				"Write the repository section, requiring a trusted signature"),
			{
				Argv:       []string{"tee", "-a", PacmanConf},
				Privileged: true,
				Explain:    "Include that section from " + PacmanConf,
				Stdin:      include,
			},
			refresh,
		},
	}
}
