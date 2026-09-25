package runner

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxStatusLine caps the text StatusLine returns, in runes.
const MaxStatusLine = 200

// meaningfulRe marks the line of a command's output that says what went
// wrong, as opposed to progress, banners or a dialog's leftovers.
var meaningfulRe = regexp.MustCompile(
	`(?i)error|failed|fatal|refused|denied|cannot|can't|unable|giving up|not found|no such`)

// StatusLine turns a command's output into the one line worth putting in a
// status line.
//
// The output of a failed step is not always text. A program that draws on a
// terminal (whiptail for debconf, a pager, a progress bar) leaves escape
// sequences and cursor moves behind, and since the runner gives no child a
// terminal (see Runner.exec) such a program fails, with its screen in the
// output and the reason on the last line. The raw first line of that is a
// run of escape codes that would garble the TUI drawing it.
//
// So the output is cleaned first: escape sequences and control characters
// go, a cursor move or a carriage return ends a line, the text a program
// draws in the DEC line-drawing set (a dialog's frame) goes, and lines that
// are blank afterwards are dropped. Of what is left, the first line that
// reads like an error (meaningfulRe) wins, else the last one, which is where
// a program says why it stopped; the result is capped at MaxStatusLine.
// Output that is plain single-line text comes back unchanged.
func StatusLine(output string) string {
	lines := cleanLines(output)
	if len(lines) == 0 {
		return ""
	}
	line := lines[len(lines)-1]
	for _, l := range lines {
		if meaningfulRe.MatchString(l) {
			line = l
			break
		}
	}
	if utf8.RuneCountInString(line) > MaxStatusLine {
		runes := []rune(line)
		line = strings.TrimSpace(string(runes[:MaxStatusLine-3])) + "..."
	}
	return line
}

// cleanLines strips terminal control from output and splits it into the
// non-blank lines a person would read, trimmed.
func cleanLines(output string) []string {
	var (
		lines    []string
		cur      strings.Builder
		graphics bool // inside ESC ( 0: the DEC line-drawing set
	)
	flush := func() {
		if l := strings.TrimSpace(cur.String()); l != "" {
			lines = append(lines, l)
		}
		cur.Reset()
	}
	for i := 0; i < len(output); {
		r, size := utf8.DecodeRuneInString(output[i:])
		switch {
		case r == 0x1b:
			n, brk, charset := escape(output[i:])
			if charset != 0 {
				graphics = charset == '0'
			}
			if brk {
				flush()
			}
			i += n
			continue
		case r == '\n' || r == '\r' || r == '\f' || r == '\v':
			flush()
		case r == '\t':
			cur.WriteByte(' ')
		case r == utf8.RuneError && size == 1, unicode.IsControl(r):
			// Other C0 and C1 controls, DEL and bytes that are not UTF-8.
		case graphics:
			// A frame drawn as "lqqqk": not text.
		default:
			cur.WriteRune(r)
		}
		i += size
	}
	flush()
	return lines
}

// escape measures the escape sequence at the start of s. It reports its
// length, whether it moves the cursor to another line (so the text on
// either side is not one line), and, for a character set designation of
// G0, the set it selects.
func escape(s string) (n int, lineBreak bool, charset byte) {
	if len(s) < 2 {
		return len(s), false, 0
	}
	switch s[1] {
	case '[': // CSI: parameters and intermediates, then a final byte.
		for j := 2; j < len(s); j++ {
			if c := s[j]; c >= 0x40 && c <= 0x7e {
				return j + 1, strings.IndexByte("ABEFHfdJ", c) >= 0, 0
			}
		}
		return len(s), false, 0
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC: up to BEL or ST.
		for j := 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1, false, 0
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2, false, 0
			}
		}
		return len(s), false, 0
	case '(', ')', '*', '+': // Character set designation: one more byte.
		if len(s) < 3 {
			return len(s), false, 0
		}
		if s[1] == '(' {
			return 3, false, s[2]
		}
		return 3, false, 0
	case 'E', 'D', 'M': // NEL, IND, RI: a new line.
		return 2, true, 0
	default: // ESC 7, ESC 8, ESC =, ESC > and the like.
		return 2, false, 0
	}
}
