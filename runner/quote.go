package runner

import "strings"

// Quote renders one argument the way a POSIX shell reads it back as that same
// single argument. An argument made only of characters no shell treats
// specially is returned as it is; anything else, including the empty string,
// is wrapped in single quotes, with each embedded single quote written as
// '"'"'. This is the rule Python's shlex.quote follows.
//
// It exists for the preview. A tool never runs a shell, but the preview is the
// command line a person reads, checks and may paste into a terminal, and an
// argument with a space or a parenthesis in it has to read as one argument
// there too.
func Quote(arg string) string {
	if arg == "" {
		return "''"
	}
	if strings.IndexFunc(arg, needsQuote) < 0 {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
}

// Join quotes each argument that needs it and joins them with single spaces:
// pasted into a POSIX shell, the result is the same argv.
func Join(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = Quote(arg)
	}
	return strings.Join(quoted, " ")
}

// needsQuote reports whether a rune falls outside the set a shell reads
// literally in an unquoted word: ASCII letters and digits and @%+=:,./-_.
func needsQuote(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	case strings.ContainsRune("@%+=:,./-_", r):
		return false
	}
	return true
}
