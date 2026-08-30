package compat

import (
	"regexp"
	"strconv"
	"strings"
)

// versionRe matches the version token a `--version` line carries: two or three
// numeric parts, optionally followed by a pre-release or build suffix. It is
// deliberately loose, because the binaries this family drives do not agree on
// a format: `ufw 0.36.2`, `systemd 257 (257.2-1-arch)`, `snapper 0.11.2`.
var versionRe = regexp.MustCompile(`\d+(?:\.\d+){0,2}(?:[-+][0-9A-Za-z.]+)?`)

// ParseVersion pulls the version out of a `--version` line. pattern is an
// optional regular expression: when it has a capturing group the group is the
// version, otherwise the whole match is. An empty pattern takes the first
// version-shaped token, which is what nearly every tool prints.
//
// It returns an empty string when nothing matched, which the caller reports as
// StatusUnknown rather than guessing.
func ParseVersion(output, pattern string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	re := versionRe
	if pattern != "" {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			// A broken regex in a manifest must not break the tool: fall back
			// to the default token.
			compiled = versionRe
		}
		re = compiled
	}
	match := re.FindStringSubmatch(output)
	if match == nil {
		return ""
	}
	if len(match) > 1 && match[1] != "" {
		return strings.TrimSpace(match[1])
	}
	return strings.TrimSpace(match[0])
}

// parsed is a version split into its numeric parts and its suffix.
type parsed struct {
	parts  []int
	suffix string
}

// split breaks a version string into numbers and a trailing suffix. Anything
// that is not a number ends the numeric run: "257.2-1-arch" is 257.2 with the
// suffix "-1-arch".
func split(version string) parsed {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")

	var out parsed
	for i := 0; i < len(version); {
		if version[i] == '.' && len(out.parts) > 0 {
			i++
			continue
		}
		j := i
		for j < len(version) && version[j] >= '0' && version[j] <= '9' {
			j++
		}
		if j == i {
			out.suffix = version[i:]
			break
		}
		n, err := strconv.Atoi(version[i:j])
		if err != nil {
			out.suffix = version[i:]
			break
		}
		out.parts = append(out.parts, n)
		i = j
		if i < len(version) && version[i] != '.' {
			out.suffix = version[i:]
			break
		}
	}
	return out
}

// part returns the i-th numeric component, treating a missing one as zero so
// that "0.36" and "0.36.0" compare equal.
func (p parsed) part(i int) int {
	if i < len(p.parts) {
		return p.parts[i]
	}
	return 0
}

// prerelease reports whether the suffix marks a pre-release ("-rc1"). A build
// suffix that starts with anything else ("+deb", "257.2-1-arch" reached
// through the numeric path) is not treated as one.
func (p parsed) prerelease() bool {
	return strings.HasPrefix(p.suffix, "-") && p.suffix != "-"
}

// Compare orders two versions: -1 when a sorts before b, 0 when they are the
// same release, 1 when a sorts after b.
//
// It is loose on purpose. Numeric parts are compared left to right with the
// missing ones read as zero, so "250" < "250.1" is false and "250" == "250.0"
// is true. A pre-release suffix sorts before the plain release with the same
// numbers, and two different suffixes are compared as strings, which is enough
// to keep a sorted list stable without pretending to implement semver.
func Compare(a, b string) int {
	pa, pb := split(a), split(b)
	n := max(len(pa.parts), len(pb.parts))
	for i := range n {
		switch {
		case pa.part(i) < pb.part(i):
			return -1
		case pa.part(i) > pb.part(i):
			return 1
		}
	}
	switch {
	case pa.prerelease() && !pb.prerelease():
		return -1
	case !pa.prerelease() && pb.prerelease():
		return 1
	case pa.suffix == pb.suffix:
		return 0
	case pa.suffix < pb.suffix:
		return -1
	default:
		return 1
	}
}

// Match reports whether version satisfies a constraint. The grammar is the
// small one a manifest note needs, not a dependency solver:
//
//	"0.36"          exactly that release ("0.36" == "0.36.0")
//	"<0.11"         also <=, >, >=, =, ==, != with the same operand
//	"0.36.x"        a wildcard in any position, "*" accepted for the same
//	">=0.36,<0.40"  a comma-separated list, every part must hold
//
// An empty constraint matches everything; an unparsable one matches nothing,
// so a typo in a manifest hides a note instead of showing it everywhere.
func Match(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return true
	}
	if version == "" {
		return false
	}
	for _, part := range strings.Split(constraint, ",") {
		if !matchOne(version, strings.TrimSpace(part)) {
			return false
		}
	}
	return true
}

// matchOne evaluates a single constraint term.
func matchOne(version, constraint string) bool {
	if constraint == "" {
		return true
	}
	for _, op := range []string{">=", "<=", "==", "!=", ">", "<", "="} {
		if !strings.HasPrefix(constraint, op) {
			continue
		}
		operand := strings.TrimSpace(strings.TrimPrefix(constraint, op))
		if operand == "" {
			return false
		}
		cmp := Compare(version, operand)
		switch op {
		case ">=":
			return cmp >= 0
		case "<=":
			return cmp <= 0
		case ">":
			return cmp > 0
		case "<":
			return cmp < 0
		case "!=":
			return cmp != 0
		default: // "=" and "=="
			return cmp == 0
		}
	}
	if strings.ContainsAny(constraint, "x*") {
		return matchWildcard(version, constraint)
	}
	return Compare(version, constraint) == 0
}

// matchWildcard compares the numeric parts up to the first wildcard: "0.36.x"
// holds for every 0.36 release.
func matchWildcard(version, constraint string) bool {
	got := split(version)
	for i, field := range strings.Split(constraint, ".") {
		field = strings.TrimSpace(field)
		if field == "x" || field == "X" || field == "*" {
			return true
		}
		want, err := strconv.Atoi(field)
		if err != nil {
			return false
		}
		if got.part(i) != want {
			return false
		}
	}
	return true
}

// SortVersions orders a list of versions oldest first, in place.
func SortVersions(versions []string) {
	// A simple insertion sort: these lists are the handful of releases a tool
	// was tested against, never more.
	for i := 1; i < len(versions); i++ {
		for j := i; j > 0 && Compare(versions[j-1], versions[j]) > 0; j-- {
			versions[j-1], versions[j] = versions[j], versions[j-1]
		}
	}
}
