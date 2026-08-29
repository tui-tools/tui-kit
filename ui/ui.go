// Package ui holds the widgets shared by every tui-tools binary: a header, a
// help/footer bar, a table, confirm and input dialogs and a status line. They
// are plain render helpers over a theme.Theme, not Bubble Tea models, so each
// tool keeps full control of its own update loop.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/tui-tools/tui-kit/theme"
)

// Ellipsis is appended to values truncated to fit a column.
const Ellipsis = "…"

// Truncate shortens s to width cells, appending an ellipsis when it had to
// cut. A width of zero or less returns an empty string.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return Ellipsis
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + Ellipsis
}

// Pad right-pads s with spaces to exactly width cells, truncating when longer.
func Pad(s string, width int) string {
	s = Truncate(s, width)
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// Header renders the top bar: the tool name, a subtitle and any number of
// "label: value" facts, wrapped to the available width.
type Header struct {
	Title    string
	Subtitle string
	Facts    []Fact
}

// Fact is one "label: value" pair in the header.
type Fact struct {
	Label string
	Value string
	// Style overrides the value style; nil uses the theme's base style.
	Style *lipgloss.Style
}

// Render draws the header.
func (h Header) Render(t theme.Theme, width int) string {
	title := t.Title.Render(h.Title)
	if h.Subtitle != "" {
		title += t.Muted.Render("  " + h.Subtitle)
	}

	lines := []string{title}
	if facts := renderFacts(t, h.Facts, width); facts != "" {
		lines = append(lines, facts)
	}
	return t.Header.Width(width).Render(strings.Join(lines, "\n"))
}

// renderFacts lays the facts out on as many lines as the width requires.
func renderFacts(t theme.Theme, facts []Fact, width int) string {
	const separator = "   "
	var lines []string
	var current string
	for _, f := range facts {
		style := t.Base
		if f.Style != nil {
			style = *f.Style
		}
		part := t.Muted.Render(f.Label+": ") + style.Render(f.Value)
		switch {
		case current == "":
			current = part
		case lipgloss.Width(current)+lipgloss.Width(separator)+lipgloss.Width(part) <= width-2:
			current += separator + part
		default:
			lines = append(lines, current)
			current = part
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return strings.Join(lines, "\n")
}

// KeyHint is one "key description" pair of the help bar.
type KeyHint struct {
	Key  string
	Desc string
}

// HelpBar renders a single-line hint bar, dropping the hints that do not fit.
func HelpBar(t theme.Theme, hints []KeyHint, width int) string {
	const separator = "  "
	var parts []string
	used := 0
	for _, h := range hints {
		part := t.Key.Render(h.Key) + " " + t.KeyDesc.Render(h.Desc)
		cost := lipgloss.Width(part)
		if len(parts) > 0 {
			cost += lipgloss.Width(separator)
		}
		if used+cost > width-2 {
			break
		}
		parts = append(parts, part)
		used += cost
	}
	return t.Footer.Width(width).Render(strings.Join(parts, separator))
}

// HelpScreen renders the full key list as a bordered panel.
func HelpScreen(t theme.Theme, title string, hints []KeyHint, width int) string {
	keyWidth := 0
	for _, h := range hints {
		if w := lipgloss.Width(h.Key); w > keyWidth {
			keyWidth = w
		}
	}
	lines := []string{t.Title.Render(title), ""}
	for _, h := range hints {
		lines = append(lines, t.Key.Render(Pad(h.Key, keyWidth))+"  "+
			t.KeyDesc.Render(h.Desc))
	}
	return t.Dialog.Render(strings.Join(lines, "\n"))
}

// StatusKind selects the color of a status line message.
type StatusKind int

// The status line message kinds.
const (
	StatusInfo StatusKind = iota
	StatusOK
	StatusWarn
	StatusError
)

// StatusLine renders the bottom message line. An empty message renders the
// fallback in the muted style.
func StatusLine(t theme.Theme, kind StatusKind, message, fallback string,
	width int) string {
	style := t.Muted
	text := fallback
	if message != "" {
		text = message
		switch kind {
		case StatusOK:
			style = t.OK
		case StatusWarn:
			style = t.Warn
		case StatusError:
			style = t.Danger
		case StatusInfo:
			style = t.Info
		}
	}
	return t.StatusLine.Width(width).Render(style.Render(Truncate(text, width-2)))
}

// Column describes one column of a Table.
type Column struct {
	Title string
	// Width is the fixed width in cells; when Flex is true it is the minimum
	// and the column absorbs the leftover space.
	Width int
	Flex  bool
}

// Table renders a fixed-width row table with a highlighted selection. It is
// deliberately simple: tools own their data and scrolling.
type Table struct {
	Columns []Column
	// Rows holds the cell values, one slice per row, aligned with Columns.
	Rows [][]string
	// Styles optionally overrides the style of a whole row (nil entries use
	// the theme default). Its length, when set, must match Rows.
	Styles []*lipgloss.Style
	// Selected is the index of the highlighted row; -1 for none.
	Selected int
	// Offset is the first visible row.
	Offset int
	// Height is the number of data rows to draw (excluding the header).
	Height int
}

// layout distributes width across the columns, growing the flexible ones.
func (tb Table) layout(width int) []int {
	widths := make([]int, len(tb.Columns))
	fixed, flexCount := 0, 0
	for i, c := range tb.Columns {
		widths[i] = c.Width
		fixed += c.Width + 1 // one padding space between columns
		if c.Flex {
			flexCount++
		}
	}
	extra := width - fixed
	if extra > 0 && flexCount > 0 {
		share := extra / flexCount
		for i, c := range tb.Columns {
			if c.Flex {
				widths[i] += share
			}
		}
	}
	if extra < 0 {
		// Shrink the flexible columns first, then the rest, never below 3.
		for pass := 0; pass < 2 && extra < 0; pass++ {
			for i := len(widths) - 1; i >= 0 && extra < 0; i-- {
				if pass == 0 && !tb.Columns[i].Flex {
					continue
				}
				if reducible := widths[i] - 3; reducible > 0 {
					cut := min(reducible, -extra)
					widths[i] -= cut
					extra += cut
				}
			}
		}
	}
	return widths
}

// Render draws the table, including its header row.
//
// The columns are laid out against the row style's content width, not the full
// terminal width. Rows are drawn through a padded style, so laying out against
// the full width overflows every row by exactly the padding, and a wrapped row
// does more than look wrong: it desynchronises Bubble Tea's line accounting, so
// every frame after it is drawn in the wrong place.
func (tb Table) Render(t theme.Theme, width int) string {
	inner := max(width-t.Row.GetHorizontalFrameSize(), 1)
	widths := tb.layout(inner)

	var head []string
	for i, c := range tb.Columns {
		head = append(head, Pad(c.Title, widths[i]))
	}
	lines := []string{t.TableHead.Render(strings.Join(head, " "))}

	end := min(tb.Offset+tb.Height, len(tb.Rows))
	for idx := tb.Offset; idx < end; idx++ {
		var cells []string
		for i := range tb.Columns {
			value := ""
			if i < len(tb.Rows[idx]) {
				value = tb.Rows[idx][i]
			}
			cells = append(cells, Pad(value, widths[i]))
		}
		line := strings.Join(cells, " ")

		style := t.Row
		if idx < len(tb.Styles) && tb.Styles[idx] != nil {
			style = *tb.Styles[idx]
		}
		if idx == tb.Selected {
			style = t.SelRow
		}
		lines = append(lines, style.Width(width).Render(line))
	}

	// Pad to a stable height so the layout does not jump as rows are added.
	for i := end - tb.Offset; i < tb.Height; i++ {
		lines = append(lines, t.Row.Width(width).Render(""))
	}
	return strings.Join(lines, "\n")
}

// EmptyState renders a centered hint for an empty table.
func EmptyState(t theme.Theme, message string, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		t.Muted.Render(message))
}
