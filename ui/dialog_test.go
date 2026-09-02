package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestWrapWidthEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{name: "empty", in: "", width: 10, want: []string{""}},
		{name: "fits", in: "one two", width: 10, want: []string{"one two"}},
		{name: "exact", in: "one two", width: 7, want: []string{"one two"}},
		{name: "breaks on the space", in: "one two", width: 6,
			want: []string{"one", "two"}},
		{name: "splits an unbreakable word", in: "aaaaaa", width: 4,
			want: []string{"aaaa", "aa"}},
		{name: "width of one", in: "ab", width: 1, want: []string{"a", "b"}},
		{name: "width of zero", in: "hello", width: 0, want: []string{""}},
		{name: "negative width", in: "hello", width: -5, want: []string{""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Wrap(tc.in, tc.width)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("Wrap(%q, %d) = %q, want %q",
					tc.in, tc.width, got, tc.want)
			}
		})
	}
}

// TestWrapCountsCellsNotRunes is the reason the wrap goes through the same
// width helper the rest of the package uses: a CJK ideograph and an emoji each
// take two cells, so a line of four of them is eight cells wide, not four.
func TestWrapCountsCellsNotRunes(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
	}{
		{name: "cjk", in: "設定を保存しますか本当に", width: 8},
		{name: "emoji", in: "🔥🔥🔥🔥🔥🔥🔥🔥", width: 6},
		{name: "mixed prose", in: "delete 規則 3 now 🔥 for good", width: 9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := Wrap(tc.in, tc.width)
			if len(lines) < 2 {
				t.Fatalf("Wrap(%q, %d) = %q, want it split",
					tc.in, tc.width, lines)
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w > tc.width {
					t.Errorf("line %d is %d cells, want at most %d: %q",
						i, w, tc.width, line)
				}
			}
		})
	}
}

func TestWrapBodyKeepsBlankLinesAndIndentsContinuations(t *testing.T) {
	body := "First paragraph that is long enough to need two lines.\n" +
		"\n" +
		"  an indented block whose text also has to wrap somewhere"
	lines := WrapBody(body, 24)

	if len(lines) < 4 {
		t.Fatalf("WrapBody produced %d lines: %q", len(lines), lines)
	}
	blank := false
	for _, line := range lines {
		if line == "" {
			blank = true
		}
		if w := lipgloss.Width(line); w > 24 {
			t.Errorf("line %q is %d cells", line, w)
		}
	}
	if !blank {
		t.Error("the paragraph break was dropped")
	}

	// Every line of the indented block keeps the indent, so the block still
	// reads as one thing.
	var block []string
	for _, line := range lines {
		if strings.HasPrefix(line, "  ") {
			block = append(block, line)
		}
	}
	if len(block) < 2 {
		t.Fatalf("the indented block did not wrap with its indent: %q", lines)
	}
}

// TestWrapBodyWrapsCommandsRatherThanClippingThem is the decision the dialogs
// make about a "$ " line: the command preview is the family's trust boundary,
// so a long command is shown whole, with its continuations indented under it,
// and never cut.
func TestWrapBodyWrapsCommandsRatherThanClippingThem(t *testing.T) {
	command := "$ sudo -n systemctl --no-pager --full show " +
		"nginx.service --property=ExecStart --property=Environment"
	lines := WrapBody(command, 30)

	if len(lines) < 3 {
		t.Fatalf("the command did not wrap: %q", lines)
	}
	if !strings.HasPrefix(lines[0], "$ ") {
		t.Errorf("the first line lost its prompt: %q", lines[0])
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("continuation %q is not indented", line)
		}
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > 30 {
			t.Errorf("line %d is %d cells: %q", i, w, line)
		}
	}
	// Nothing was lost on the way: the words are all still there, in order.
	joined := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	if joined != command {
		t.Errorf("rejoined = %q, want %q", joined, command)
	}
}

func TestConfirmViewWrapsInsteadOfTruncating(t *testing.T) {
	tm := testTheme(t)
	c := Confirm{
		Title: "Replace the certificate for api.example.com",
		Body: "The current certificate was issued by an internal CA and " +
			"expires in three days. Replacing it restarts every service " +
			"that reads it.",
		Command: "sudo -n certbot certonly --non-interactive --agree-tos " +
			"--webroot -w /var/www/html -d api.example.com",
	}
	for _, width := range []int{40, 60, 100} {
		out := c.View(tm, width, 40)
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line %d is %d cells: %q",
					width, i, w, line)
			}
		}
		// The tail of the body and of the command survive the wrap. The box
		// border is stripped first, so what is compared is the text.
		flat := strings.Join(strings.Fields(stripBorder(out)), " ")
		if !strings.Contains(flat, "that reads it.") {
			t.Errorf("width %d truncated the body: %s", width, flat)
		}
		if !strings.Contains(flat, "-d api.example.com") {
			t.Errorf("width %d truncated the command: %s", width, flat)
		}
	}
}

func TestConfirmScrollsATallBodyAndKeepsTheFooter(t *testing.T) {
	tm := testTheme(t)
	var paragraphs []string
	for i := 0; i < 40; i++ {
		paragraphs = append(paragraphs, "line-"+string(rune('a'+i%26))+
			" of a body far taller than the terminal it is shown in")
	}
	c := Confirm{Title: "Apply 40 changes", Body: strings.Join(paragraphs, "\n")}

	const height = 20
	first := c.View(tm, 70, height)
	if lines := strings.Count(first, "\n") + 1; lines > height {
		t.Fatalf("the dialog is %d lines tall in a %d line terminal",
			lines, height)
	}
	if !strings.Contains(first, "confirm") || !strings.Contains(first, "cancel") {
		t.Error("the footer hints must stay visible while the body scrolls")
	}
	if c.maxScroll == 0 {
		t.Fatal("a body this tall must be scrollable")
	}
	if !strings.Contains(first, "of ") {
		t.Error("a scrolling dialog must say where in the body it is")
	}

	// Scrolling moves the window and stops at both ends.
	c.Update(keyMsg("j"))
	if c.Scroll != 1 {
		t.Fatalf("Scroll = %d after j, want 1", c.Scroll)
	}
	if second := c.View(tm, 70, height); second == first {
		t.Error("j did not move the window")
	}
	for i := 0; i < 100; i++ {
		c.Update(keyMsg("j"))
	}
	if c.Scroll != c.maxScroll {
		t.Errorf("Scroll = %d, want it clamped to %d", c.Scroll, c.maxScroll)
	}
	c.Update(keyMsg("pgup"))
	if c.Scroll != c.maxScroll-scrollPage {
		t.Errorf("Scroll = %d after pgup, want %d",
			c.Scroll, c.maxScroll-scrollPage)
	}
	for i := 0; i < 100; i++ {
		c.Update(keyMsg("k"))
	}
	if c.Scroll != 0 {
		t.Errorf("Scroll = %d, want 0", c.Scroll)
	}

	// Scrolling never answers the dialog.
	if c.Done {
		t.Error("scrolling finished the dialog")
	}
	c.Update(keyMsg("y"))
	if !c.Done || !c.Confirmed {
		t.Error("y must still confirm after scrolling")
	}
}

// TestConfirmShortBodyDoesNotScroll pins the promise that a dialog that always
// fitted still renders exactly as it did, with no scroll indicator.
func TestConfirmShortBodyDoesNotScroll(t *testing.T) {
	tm := testTheme(t)
	c := Confirm{Title: "Delete rule 3", Body: "This cannot be undone.",
		Command: "ufw --force delete 3"}
	out := c.View(tm, 80, 24)
	if c.maxScroll != 0 {
		t.Errorf("maxScroll = %d, want 0", c.maxScroll)
	}
	if strings.Contains(out, "scroll") {
		t.Errorf("a dialog that fits must not offer to scroll:\n%s", out)
	}
	c.Update(keyMsg("j"))
	if c.Scroll != 0 {
		t.Errorf("Scroll = %d, want 0 on a body that fits", c.Scroll)
	}
}

// units is the shape of the list the filter exists for: far more entries than
// any window can show.
func units() []string {
	names := []string{"nginx.service", "postgresql.service", "sshd.service",
		"systemd-journald.service", "systemd-logind.service",
		"docker.service", "containerd.service", "cron.service",
		"NetworkManager.service", "logrotate.timer", "fstrim.timer"}
	for len(names) < 327 {
		names = append(names, "filler-"+string(rune('a'+len(names)%26))+
			".service")
	}
	return names
}

func TestPickerTypeAheadFilter(t *testing.T) {
	p := NewPicker("Unit", units(), "nginx.service")
	if got := len(p.Visible()); got != 327 {
		t.Fatalf("Visible = %d options, want all 327", got)
	}

	for _, r := range "syst" {
		p.Update(keyMsg(string(r)))
	}
	if p.Filter != "syst" {
		t.Fatalf("Filter = %q, want syst", p.Filter)
	}
	visible := p.Visible()
	if len(visible) == 0 || len(visible) >= 327 {
		t.Fatalf("the filter kept %d of 327 options", len(visible))
	}
	for _, o := range visible {
		if !strings.Contains(strings.ToLower(o), "syst") {
			t.Errorf("%q does not match the filter", o)
		}
	}
	if p.Cursor < 0 || p.Cursor >= len(visible) {
		t.Errorf("Cursor = %d, outside the %d visible options",
			p.Cursor, len(visible))
	}

	// The filter is case-insensitive and matches anywhere in the option.
	p = NewPicker("Unit", units(), "")
	for _, r := range "JOURNALD" {
		p.Update(keyMsg(string(r)))
	}
	if got := p.Visible(); len(got) != 1 || got[0] != "systemd-journald.service" {
		t.Errorf("Visible = %q, want the one journald unit", got)
	}
	p.Update(keyMsg("enter"))
	if !p.Accepted || p.Selected() != "systemd-journald.service" {
		t.Errorf("Selected = %q, Accepted = %v", p.Selected(), p.Accepted)
	}
}

func TestPickerBackspaceEditsTheFilter(t *testing.T) {
	p := NewPicker("Unit", units(), "")
	for _, r := range "nginz" {
		p.Update(keyMsg(string(r)))
	}
	if len(p.Visible()) != 0 {
		t.Fatalf("a typo should match nothing, got %q", p.Visible())
	}
	p.Update(keyMsg("backspace"))
	p.Update(keyMsg("x"))
	if p.Filter != "nginx" {
		t.Fatalf("Filter = %q, want nginx", p.Filter)
	}
	if got := p.Visible(); len(got) != 1 || got[0] != "nginx.service" {
		t.Errorf("Visible = %q", got)
	}

	// Backspacing an empty filter is a no-op, not a cancel.
	p = NewPicker("Unit", units(), "")
	p.Update(keyMsg("backspace"))
	if p.Filter != "" || p.Done {
		t.Errorf("Filter = %q, Done = %v", p.Filter, p.Done)
	}
}

func TestPickerEscClearsTheFilterBeforeItCancels(t *testing.T) {
	p := NewPicker("Unit", units(), "")
	for _, r := range "docker" {
		p.Update(keyMsg(string(r)))
	}
	p.Update(keyMsg("esc"))
	if p.Filter != "" {
		t.Fatalf("the first esc must clear the filter, got %q", p.Filter)
	}
	if p.Done {
		t.Fatal("the first esc must not cancel the dialog")
	}
	if len(p.Visible()) != 327 {
		t.Errorf("clearing the filter must restore every option, got %d",
			len(p.Visible()))
	}
	p.Update(keyMsg("esc"))
	if !p.Done || p.Accepted {
		t.Errorf("the second esc must cancel, Done=%v Accepted=%v",
			p.Done, p.Accepted)
	}

	// ctrl+c cancels whatever the filter holds; it is the panic key.
	p = NewPicker("Unit", units(), "")
	p.Update(keyMsg("d"))
	p.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !p.Done || p.Accepted {
		t.Errorf("ctrl+c must cancel, Done=%v Accepted=%v", p.Done, p.Accepted)
	}
}

func TestPickerFilterKeepsTheSelectionWhenItSurvives(t *testing.T) {
	p := NewPicker("Unit", []string{"alpha", "beta", "gamma", "beta-two"}, "beta")
	if p.Selected() != "beta" {
		t.Fatalf("Selected = %q", p.Selected())
	}
	p.Update(keyMsg("b"))
	if p.Selected() != "beta" {
		t.Errorf("Selected = %q, want the filter to keep the selection",
			p.Selected())
	}
	// A filter that drops the selection puts the cursor on the first match.
	p.Update(keyMsg("backspace"))
	for _, r := range "gam" {
		p.Update(keyMsg(string(r)))
	}
	if p.Selected() != "gamma" {
		t.Errorf("Selected = %q, want gamma", p.Selected())
	}
}

func TestPickerFilterAcceptsSpacesInsideOnly(t *testing.T) {
	p := NewPicker("Reason", []string{"a b c", "abc"}, "")
	p.Update(keyMsg("space"))
	if p.Filter != "" {
		t.Errorf("a filter must not start with a space, got %q", p.Filter)
	}
	p.Update(keyMsg("a"))
	p.Update(keyMsg("space"))
	p.Update(keyMsg("b"))
	if p.Filter != "a b" {
		t.Fatalf("Filter = %q, want %q", p.Filter, "a b")
	}
	if got := p.Visible(); len(got) != 1 || got[0] != "a b c" {
		t.Errorf("Visible = %q", got)
	}
}

func TestPickerViewShowsTheFilterAndFitsTheScreen(t *testing.T) {
	tm := testTheme(t)
	p := NewPicker("Follow which unit?", units(), "")
	for _, r := range "system" {
		p.Update(keyMsg(string(r)))
	}

	const height = 20
	for _, width := range []int{40, 80, 120} {
		out := p.View(tm, width, height)
		if !strings.Contains(out, "filter:") || !strings.Contains(out, "system") {
			t.Errorf("width %d does not show the filter:\n%s", width, out)
		}
		if !strings.Contains(out, "select") || !strings.Contains(out, "cancel") {
			t.Errorf("width %d lost the footer hints", width)
		}
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line %d is %d cells: %q",
					width, i, w, line)
			}
		}
		if lines := strings.Count(out, "\n") + 1; lines > height {
			t.Errorf("width %d: the picker is %d lines in a %d line terminal",
				width, lines, height)
		}
	}

	// A filter that matches nothing says so instead of showing an empty box.
	for _, r := range "zzzz" {
		p.Update(keyMsg(string(r)))
	}
	if out := p.View(tm, 60, height); !strings.Contains(out, "no option matches") {
		t.Errorf("an unmatched filter must say so:\n%s", out)
	}
}

// TestPickerLongOptionsStayInsideTheDialog covers the other half of the
// truncation bug: a unit name longer than the dialog must not push the box
// past the terminal.
func TestPickerLongOptionsStayInsideTheDialog(t *testing.T) {
	tm := testTheme(t)
	options := []string{
		"short.service",
		strings.Repeat("very-long-unit-name-", 8) + ".service",
		"設定を保存しますか本当に設定を保存しますか本当に設定を保存しますか",
	}
	p := NewPicker("Unit", options, "")
	for _, width := range []int{30, 60, 100} {
		out := p.View(tm, width, 24)
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line %d is %d cells: %q",
					width, i, w, line)
			}
		}
	}
}

// stripBorder removes the dialog's box-drawing characters, so a test can read
// the wrapped text as one paragraph.
func stripBorder(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("╭╮╰╯│─", r) {
			return ' '
		}
		return r
	}, s)
}
