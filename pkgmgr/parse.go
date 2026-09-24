package pkgmgr

import (
	"strings"
)

// splitLines splits command output into lines, dropping the empty element a
// trailing newline produces.
func splitLines(text string) []string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// ParsePipedVersions reads the `key|version` shape `rpm -q --qf` is asked for
// here, and that `dnf repoquery --qf` prints too. dpkg-query needs a status
// field on top of it, which ParseDpkgStatus reads.
//
// The key is whatever the format string put first: this package asks for the
// bare name, and tui-update asks the same query for "name.arch". Neither is
// interpreted, so one parser serves both.
func ParsePipedVersions(out string) map[string]string {
	versions := map[string]string{}
	for _, line := range splitLines(out) {
		key, version, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok || key == "" || version == "" {
			continue
		}
		// dpkg-query and rpm write their "no such package" complaints to the
		// same stream the runner reads. A complaint is not an answer.
		if strings.ContainsAny(key, " \t") {
			continue
		}
		versions[key] = version
	}
	return versions
}

// ParseDpkgStatus reads the `name|version|status` shape BuildInstalled asks
// dpkg-query for, and keeps only the packages whose status is `installed`.
//
// dpkg answers every name its database knows, not only the installed ones: a
// name it learnt from the Suggests: of an installed package comes back as
// `name||not-installed`, and a package removed but not purged as
// `name|0.1.2-1|config-files`, a version and all. The status is what tells
// them apart, so a line without one is not read as installed either.
func ParseDpkgStatus(out string) map[string]string {
	versions := map[string]string{}
	for _, line := range splitLines(out) {
		key, version, status, ok := cutDpkgLine(line)
		if !ok || version == "" || status != dpkgInstalled {
			continue
		}
		versions[key] = version
	}
	return versions
}

// dpkgInstalled is the ${db:Status-Status} of a package that is installed.
const dpkgInstalled = "installed"

// cutDpkgLine splits one `name|version|status` line. ok is false for a line of
// any other shape, a dpkg-query complaint among them: those carry spaces in
// what would be the name.
func cutDpkgLine(line string) (key, version, status string, ok bool) {
	fields := strings.Split(strings.TrimSpace(line), "|")
	if len(fields) != 3 || fields[0] == "" || strings.ContainsAny(fields[0], " \t") {
		return "", "", "", false
	}
	return fields[0], fields[1], fields[2], true
}

// ParsePacmanQuery reads `pacman -Q`, whose every line is a name and a
// version separated by a space.
func ParsePacmanQuery(out string) map[string]string {
	versions := map[string]string{}
	for _, line := range splitLines(out) {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		// `error: package 'tui-nope' was not found` has more than two fields,
		// so it never reaches here; a warning with exactly two would, which
		// is what this rejects.
		if strings.HasSuffix(fields[0], ":") {
			continue
		}
		versions[fields[0]] = fields[1]
	}
	return versions
}

// ParsePacmanSync reads `pacman -Si`, which prints one `Field : value` block
// per package. Only Name and Version are kept, and a block is recorded when
// its Version line arrives, so a truncated last block is dropped rather than
// half-reported.
func ParsePacmanSync(out string) map[string]string {
	versions := map[string]string{}
	var name string
	for _, line := range splitLines(out) {
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		field = strings.TrimSpace(field)
		value = strings.TrimSpace(value)
		switch field {
		case "Name":
			name = value
		case "Version":
			if name != "" && value != "" {
				versions[name] = value
			}
			name = ""
		}
	}
	return versions
}

// ParseAPTPolicy reads `apt-cache policy`, whose block per package is
//
//	tui-firewall:
//	  Installed: 0.2.1
//	  Candidate: 0.2.2
//
// The candidate is the version an install would fetch. `(none)` means no
// repository on this machine carries the package, which the caller reads as
// the key being absent rather than as an empty version.
func ParseAPTPolicy(out string) map[string]string {
	versions := map[string]string{}
	var name string
	for _, line := range splitLines(out) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// A package header is flush left and ends in a colon.
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(trimmed, ":") {
			name = strings.TrimSuffix(trimmed, ":")
			continue
		}
		value, ok := strings.CutPrefix(trimmed, "Candidate:")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if name == "" || value == "" || value == "(none)" {
			continue
		}
		versions[name] = value
	}
	return versions
}

// parseInstalled reads the output of BuildInstalled for a manager.
func parseInstalled(manager Manager, out string) map[string]string {
	switch manager {
	case ManagerAPT:
		return ParseDpkgStatus(out)
	case ManagerPacman:
		return ParsePacmanQuery(out)
	default:
		return ParsePipedVersions(out)
	}
}

// parseAvailable reads the output of BuildAvailable for a manager.
func parseAvailable(manager Manager, out string) map[string]string {
	switch manager {
	case ManagerAPT:
		return ParseAPTPolicy(out)
	case ManagerPacman:
		return ParsePacmanSync(out)
	default:
		return ParsePipedVersions(out)
	}
}

// ParseKeyFingerprint reads the fingerprint out of
// `gpg --show-keys --with-colons`, which prints it as the tenth field of the
// first `fpr:` record — the primary key's. It returns "" when there is none,
// and the caller treats that as a key it must not import.
func ParseKeyFingerprint(out string) string {
	for _, line := range splitLines(out) {
		if !strings.HasPrefix(line, "fpr:") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 10 {
			continue
		}
		fpr := strings.TrimSpace(fields[9])
		if !fingerprintRe.MatchString(fpr) {
			continue
		}
		return strings.ToUpper(fpr)
	}
	return ""
}
