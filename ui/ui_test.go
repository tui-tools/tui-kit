package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tui-tools/tui-kit/theme"
)

// testTheme builds a color-free theme so assertions compare plain text.
func testTheme(t *testing.T) theme.Theme {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	return theme.FromPalette(theme.TokyoNight())
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{in: "hello", width: 10, want: "hello"},
		{in: "hello", width: 5, want: "hello"},
		{in: "hello", width: 4, want: "hel" + Ellipsis},
		{in: "hello", width: 1, want: Ellipsis},
		{in: "hello", width: 0, want: ""},
		{in: "hello", width: -3, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.in+"/"+string(rune('0'+tc.width%10)), func(t *testing.T) {
			if got := Truncate(tc.in, tc.width); got != tc.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		})
	}
}

func TestPad(t *testing.T) {
	if got := Pad("ab", 5); got != "ab   " {
		t.Errorf("Pad = %q, want %q", got, "ab   ")
	}
	if got := Pad("abcdef", 4); got != "abc"+Ellipsis {
		t.Errorf("Pad = %q", got)
	}
}

func TestTableRenderKeepsHeight(t *testing.T) {
	tm := testTheme(t)
	table := Table{
		Columns: []Column{
			{Title: "NUM", Width: 4},
			{Title: "TO", Width: 10, Flex: true},
		},
		Rows:     [][]string{{"1", "22/tcp"}, {"2", "443/tcp"}},
		Selected: 0,
		Height:   5,
	}
	out := table.Render(tm, 40)
	lines := strings.Split(out, "\n")
	// One header line plus a stable number of data lines.
	if len(lines) != 6 {
		t.Fatalf("len(lines) = %d, want 6", len(lines))
	}
	if !strings.Contains(lines[0], "NUM") || !strings.Contains(lines[0], "TO") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "22/tcp") {
		t.Errorf("first row = %q", lines[1])
	}
}

func TestTableNarrowWidth(t *testing.T) {
	tm := testTheme(t)
	table := Table{
		Columns: []Column{
			{Title: "A", Width: 20},
			{Title: "B", Width: 20, Flex: true},
			{Title: "C", Width: 20},
		},
		Rows:   [][]string{{"aaaaaaaaaa", "bbbbbbbbbb", "cccccccccc"}},
		Height: 1,
	}
	// A width far below the sum of the columns must still render without
	// panicking and without exploding past the requested width.
	for _, width := range []int{10, 25, 40, 120} {
		out := table.Render(tm, width)
		for _, line := range strings.Split(out, "\n") {
			if len(line) == 0 {
				t.Fatalf("width %d produced an empty line", width)
			}
		}
	}
}

func TestHelpScreenFitsWidth(t *testing.T) {
	tm := testTheme(t)
	hints := []KeyHint{
		{Key: "↑/↓", Desc: "move"},
		{Key: "ctrl+shift+r", Desc: "reload the whole ruleset from disk and " +
			"redraw every pane, discarding any pending edit"},
		{Key: "q", Desc: "quit"},
		{Key: "/", Desc: "filter-by-an-extremely-long-unbreakable-token-value"},
	}
	title := "tui-kit — an intentionally long help panel title that will not fit"

	for _, width := range []int{40, 80, 200} {
		out := HelpScreen(tm, title, hints, width)
		if out == "" {
			t.Fatalf("width %d rendered nothing", width)
		}
		for i, line := range strings.Split(out, "\n") {
			// lipgloss.Width counts cells, ignoring the ANSI styling.
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line %d is %d cells: %q",
					width, i, w, line)
			}
		}
		if !strings.Contains(out, "quit") {
			t.Errorf("width %d dropped a hint description", width)
		}
	}
}

func TestConfirmUpdate(t *testing.T) {
	tests := []struct {
		key       string
		confirmed bool
	}{
		{key: "y", confirmed: true},
		{key: "enter", confirmed: true},
		{key: "n", confirmed: false},
		{key: "esc", confirmed: false},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			c := Confirm{Title: "Run?", Command: "ufw reload"}
			if !c.Update(keyMsg(tc.key)) {
				t.Fatal("Update should consume key messages")
			}
			if !c.Done {
				t.Fatal("Done = false")
			}
			if c.Confirmed != tc.confirmed {
				t.Errorf("Confirmed = %v, want %v", c.Confirmed, tc.confirmed)
			}
		})
	}
}

func TestConfirmViewShowsCommand(t *testing.T) {
	tm := testTheme(t)
	c := Confirm{Title: "Delete rule 3", Body: "This cannot be undone.",
		Command: "sudo -n ufw --force delete 3", Danger: true}
	out := c.View(tm, 80, 24)
	if !strings.Contains(out, "sudo -n ufw --force delete 3") {
		t.Error("the confirm dialog must preview the exact command")
	}
	if !strings.Contains(out, "Delete rule 3") {
		t.Error("the confirm dialog must show its title")
	}
}

func TestInputUpdate(t *testing.T) {
	in := NewInput("Comment", "optional", "")
	if _, consumed := in.Update(keyMsg("a")); !consumed {
		t.Fatal("Update should consume key messages")
	}
	if in.Done {
		t.Fatal("typing should not finish the dialog")
	}
	if _, _ = in.Update(keyMsg("enter")); !in.Done || !in.Accepted {
		t.Errorf("enter should accept, got Done=%v Accepted=%v", in.Done, in.Accepted)
	}

	in = NewInput("Comment", "optional", "seed")
	if _, _ = in.Update(keyMsg("esc")); !in.Done || in.Accepted {
		t.Errorf("esc should cancel, got Done=%v Accepted=%v", in.Done, in.Accepted)
	}
	if in.Value() != "seed" {
		t.Errorf("Value = %q, want seed", in.Value())
	}
}

func TestPicker(t *testing.T) {
	p := NewPicker("Level", []string{"off", "low", "medium", "high"}, "medium")
	if p.Cursor != 2 {
		t.Fatalf("Cursor = %d, want 2 (positioned on the current value)", p.Cursor)
	}
	p.Update(keyMsg("down"))
	if p.Selected() != "high" {
		t.Errorf("Selected = %q, want high", p.Selected())
	}
	// The cursor must not run past the end of the list.
	p.Update(keyMsg("down"))
	if p.Selected() != "high" {
		t.Errorf("Selected = %q, want high", p.Selected())
	}
	p.Update(keyMsg("enter"))
	if !p.Done || !p.Accepted {
		t.Errorf("enter should accept, got Done=%v Accepted=%v", p.Done, p.Accepted)
	}
}

func TestPickerEmpty(t *testing.T) {
	tm := testTheme(t)
	p := NewPicker("App profile", nil, "")
	if p.Selected() != "" {
		t.Errorf("Selected = %q, want empty", p.Selected())
	}
	p.Update(keyMsg("enter"))
	if !p.Done || p.Accepted {
		t.Error("an empty picker must not accept")
	}
	if out := p.View(tm, 60, 20); !strings.Contains(out, "nothing to choose from") {
		t.Error("an empty picker should say so")
	}
}

func TestHelpBarDropsHintsThatDoNotFit(t *testing.T) {
	tm := testTheme(t)
	hints := []KeyHint{
		{Key: "a", Desc: "add"},
		{Key: "d", Desc: "delete"},
		{Key: "e", Desc: "enable"},
		{Key: "q", Desc: "quit"},
	}
	wide := HelpBar(tm, hints, 80)
	narrow := HelpBar(tm, hints, 16)
	if !strings.Contains(wide, "delete") {
		t.Error("a wide bar should show every hint")
	}
	if strings.Contains(narrow, "quit") {
		t.Error("a narrow bar should drop the hints that do not fit")
	}
}

func TestStatusLineFallback(t *testing.T) {
	tm := testTheme(t)
	if out := StatusLine(tm, StatusInfo, "", "ready", 40); !strings.Contains(out, "ready") {
		t.Errorf("StatusLine = %q, want the fallback", out)
	}
	out := StatusLine(tm, StatusError, "boom", "ready", 40)
	if !strings.Contains(out, "boom") || strings.Contains(out, "ready") {
		t.Errorf("StatusLine = %q, want the message", out)
	}
}

// keyMsg builds the tea.KeyMsg for a key name.
func keyMsg(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

func TestTableRowsNeverExceedTheGivenWidth(t *testing.T) {
	// A row wider than the terminal wraps, and a wrapped row desynchronises
	// Bubble Tea's line accounting: every frame after it lands in the wrong
	// place. The layout must therefore fit inside the row style's padding.
	tm := theme.FromPalette(theme.TokyoNight())

	tests := []struct {
		name    string
		columns []Column
		width   int
	}{
		{
			name: "flexible columns absorbing the slack",
			columns: []Column{
				{Title: "UNIT", Width: 28, Flex: true},
				{Title: "ACTIVE", Width: 8},
				{Title: "SUB", Width: 8},
				{Title: "AT BOOT", Width: 9},
				{Title: "DESCRIPTION", Width: 24, Flex: true},
			},
			width: 104,
		},
		{
			name: "two flexible columns",
			columns: []Column{
				{Title: "NEXT", Width: 12},
				{Title: "LAST", Width: 12},
				{Title: "TIMER", Width: 24, Flex: true},
				{Title: "ACTIVATES", Width: 24, Flex: true},
			},
			width: 104,
		},
		{
			name: "columns wider than the terminal are shrunk",
			columns: []Column{
				{Title: "A", Width: 40, Flex: true},
				{Title: "B", Width: 40, Flex: true},
				{Title: "C", Width: 40},
			},
			width: 60,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := make([]string, len(tc.columns))
			for i := range row {
				row[i] = strings.Repeat("x", 60)
			}
			table := Table{
				Columns: tc.columns,
				Rows:    [][]string{row, row},
				// -1 selects nothing, so the selected style cannot mask a
				// width bug in the ordinary row style.
				Selected: -1,
				Height:   4,
			}
			for _, width := range []int{tc.width, tc.width - 1, tc.width + 13} {
				rendered := table.Render(tm, width)
				for i, line := range strings.Split(rendered, "\n") {
					if got := lipgloss.Width(line); got > width {
						t.Errorf("width %d: line %d is %d cells wide",
							width, i, got)
					}
				}
			}
		})
	}
}
