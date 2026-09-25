package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// debconfTranscript is what `sudo -n dpkg-reconfigure tzdata` printed with
// no controlling terminal, captured in an ubuntu:24.04 container: whiptail's
// screen, drawn with cursor moves and the DEC line-drawing set, then the one
// line that says why it stopped (tui-kit#35).
func debconfTranscript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "debconf-whiptail-no-terminal.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const debconfReason = "Failed to open terminal.debconf: whiptail output the above errors, giving up!"

func TestStatusLineReadsATerminalProgramsFailure(t *testing.T) {
	if got := StatusLine(debconfTranscript(t)); got != debconfReason {
		t.Errorf("StatusLine = %q, want %q", got, debconfReason)
	}
}

func TestStatusLineIsPlainText(t *testing.T) {
	got := StatusLine(debconfTranscript(t))
	for _, r := range got {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			t.Fatalf("control character %U left in %q", r, got)
		}
	}
}

func TestStatusLine(t *testing.T) {
	for _, tc := range []struct{ what, in, want string }{
		{"plain single line", "ERROR: Could not find a profile matching 'x'",
			"ERROR: Could not find a profile matching 'x'"},
		{"plain single line keeps its spacing", "Status:  inactive", "Status:  inactive"},
		{"first line that reads like an error", "Reading package lists...\n" +
			"E: Unable to locate package tui-nope\nE: second", "E: Unable to locate package tui-nope"},
		{"else the last line", "Usage: tool [options]\n  --help  show this", "--help  show this"},
		{"blank lines skipped", "\n\n  ufw: not enabled  \n\n", "ufw: not enabled"},
		{"colours stripped", "\x1b[1;31merror:\x1b[0m bad value", "error: bad value"},
		{"carriage return ends a line", "10%\r50%\r100%\ndone", "done"},
		{"osc title and bell", "\x1b]0;title\x07ok\x07", "ok"},
		{"line drawing dropped", "\x1b(0lqqk\x1b(B\x1b[2;1Hpermission denied", "permission denied"},
		{"only escapes", "\x1b[2J\x1b[H\x1b[?25h", ""},
		{"invalid utf-8 dropped", "bad \xff byte", "bad  byte"},
		{"empty", "", ""},
	} {
		if got := StatusLine(tc.in); got != tc.want {
			t.Errorf("%s: StatusLine(%q) = %q, want %q", tc.what, tc.in, got, tc.want)
		}
	}
}

func TestStatusLineIsCapped(t *testing.T) {
	got := StatusLine("fatal: " + strings.Repeat("é", 500))
	if n := utf8.RuneCountInString(got); n != MaxStatusLine {
		t.Errorf("%d runes, want %d", n, MaxStatusLine)
	}
	if !strings.HasSuffix(got, "...") || !utf8.ValidString(got) {
		t.Errorf("capped line = %q", got)
	}
}

// TestRunFailureFromATerminalProgramIsReadable: the error a failed step
// returns carries the readable reason, not whiptail's escape codes.
func TestRunFailureFromATerminalProgramIsReadable(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "screen")
	if err := os.WriteFile(transcript, []byte(debconfTranscript(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := fakeBin(t, "dpkg-reconfigure", `cat "`+transcript+`"; exit 255`)
	r := &Runner{Name: "dpkg-reconfigure", Bin: filepath.Join(dir, "dpkg-reconfigure")}
	_, err := r.Run(context.Background(), Command{Argv: []string{"dpkg-reconfigure", "tzdata"}})
	if err == nil {
		t.Fatal("the step succeeded")
	}
	want := "`dpkg-reconfigure tzdata` failed: " + debconfReason
	if err.Error() != want {
		t.Errorf("err = %q\nwant  %q", err, want)
	}
}
