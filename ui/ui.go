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

// HelpScreenMinContent is the narrowest content area the help panel will use.
// Below it the panel stops shrinking, because a one or two cell wide column of
// text is less useful than a panel the terminal clips.
const HelpScreenMinContent = 16

// helpKeyGap separates the key column from its description.
const helpKeyGap = "  "

// HelpScreen renders the full key list as a bordered panel that fits within
// width.
//
// The panel is drawn through the dialog style, which adds a border and
// padding, so the content is laid out against width minus that frame rather
// than against the full terminal width. Descriptions wrap onto continuation
// lines aligned under the first one; a key column wider than half the content
// area is truncated so the descriptions keep room to breathe.
func HelpScreen(t theme.Theme, title string, hints []KeyHint, width int) string {
	content := max(width-t.Dialog.GetHorizontalFrameSize(), HelpScreenMinContent)

	keyWidth := 0
	for _, h := range hints {
		if w := lipgloss.Width(h.Key); w > keyWidth {
			keyWidth = w
		}
	}
	keyWidth = min(keyWidth, max(content/2, 1))

	descWidth := max(content-keyWidth-lipgloss.Width(helpKeyGap), 1)
	indent := strings.Repeat(" ", keyWidth+lipgloss.Width(helpKeyGap))

	lines := []string{t.Title.Render(Truncate(title, content)), ""}
	for _, h := range hints {
		desc := Wrap(h.Desc, descWidth)
		lines = append(lines, t.Key.Render(Pad(h.Key, keyWidth))+helpKeyGap+
			t.KeyDesc.Render(desc[0]))
		for _, extra := range desc[1:] {
			lines = append(lines, indent+t.KeyDesc.Render(extra))
		}
	}
	return t.Dialog.Render(strings.Join(lines, "\n"))
}

// Wrap breaks s on spaces into lines of at most width cells, hard-splitting any
// single word that is wider than the line. Widths are counted in terminal
// cells, so a CJK ideograph or an emoji costs two. It always returns at least
// one line, so callers can index the first one.
func Wrap(s string, width int) []string {
	return wrapIndented(s, "", width)
}

// preIndentFor reports the continuation indent a pre-formatted line wants, and
// whether the line is pre-formatted at all.
//
// Three shapes are recognised, and they are the ones the family's dialogs
// produce: a command preview starting with "$ ", an indented block starting
// with two spaces, and a unified-diff line ("+ ", "- ", "@@", "+++ ", "--- ")
// such as a configuration change previewed before it is written. All of them
// are wrapped rather than clipped — the command preview is the trust boundary
// of the whole family, and a command line the user cannot read to its end is a
// command line they cannot check — but their continuations are indented so the
// eye still sees one logical line, and their inner spacing is kept as written.
func preIndentFor(line string) (indent string, pre bool) {
	if strings.HasPrefix(line, "$ ") {
		return "  ", true
	}
	if lead := leadingSpace(line); lead != "" && strings.HasPrefix(line, "  ") {
		return lead + "  ", true
	}
	if isDiffLine(line) {
		return "  ", true
	}
	return "", false
}

// isDiffLine reports whether a line reads as a unified-diff line: an added or
// removed line ("+ ", "- ", or the marker alone), a hunk header ("@@") or a file
// header ("+++ ", "--- "). In a diff of a YAML file the spaces after the marker
// are the file's own indentation, which is its meaning.
func isDiffLine(line string) bool {
	switch {
	case strings.HasPrefix(line, "@@"),
		strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
		return true
	case line == "+", line == "-":
		return true
	case strings.HasPrefix(line, "+ "), strings.HasPrefix(line, "- "),
		strings.HasPrefix(line, "+\t"), strings.HasPrefix(line, "-\t"):
		return true
	}
	return false
}

// leadingSpace returns the run of spaces and tabs that opens s.
func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// WrapBody wraps a multi-line dialog body to width cells.
//
// Prose lines are word-wrapped, their runs of spaces folded into one.
// Pre-formatted lines — a "$ " command preview, a two-space indented block or a
// diff line — are wrapped too, with their continuations indented under the
// first line, so nothing in a dialog is ever silently cut; their spacing is
// kept exactly as written, because in a diff of a YAML file the indentation is
// the content. Empty lines are kept, because they are the paragraph breaks the
// body author wrote. A width of zero or less returns nothing.
func WrapBody(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		indent, pre := preIndentFor(line)
		if pre {
			out = append(out, wrapPreformatted(line, indent, width)...)
			continue
		}
		out = append(out, wrapIndented(line, indent, width)...)
	}
	return out
}

// wrapIndented wraps the words of line into lines of at most width cells,
// prefixing every line after the first with indent. The leading whitespace of
// line opens the first output line, so an indented block keeps its shape.
// Inside the line, words are separated by single spaces: this is the prose
// wrap.
func wrapIndented(line, indent string, width int) []string {
	lead := leadingSpace(line)
	var pieces []piece
	for _, word := range strings.Fields(line) {
		pieces = append(pieces, piece{sep: " ", word: word})
	}
	return wrapPieces(pieces, lead, indent, width)
}

// wrapPreformatted wraps a pre-formatted line without touching its spacing:
// every run of spaces between two words is kept as it was, and a line is only
// broken at such a run, which is dropped at the break the way a word wrap drops
// the space it breaks on.
func wrapPreformatted(line, indent string, width int) []string {
	lead := leadingSpace(line)
	return wrapPieces(splitPieces(line[len(lead):]), lead, indent, width)
}

// piece is one word of a line and the whitespace written before it.
type piece struct {
	sep, word string
}

// splitPieces cuts s, which starts with a word, into its words, each with the
// run of whitespace that precedes it in s.
func splitPieces(s string) []piece {
	var pieces []piece
	for s != "" {
		rest := strings.TrimLeft(s, " \t")
		sep := s[:len(s)-len(rest)]
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			end = len(rest)
		}
		pieces = append(pieces, piece{sep: sep, word: rest[:end]})
		s = rest[end:]
	}
	return pieces
}

// wrapPieces lays pieces out into lines of at most width cells. The first line
// opens with lead, every later one with indent; a piece that starts a line
// drops its separator, and a word wider than the line is hard-split.
func wrapPieces(pieces []piece, lead, indent string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if lipgloss.Width(indent) >= width {
		indent = ""
	}
	if lipgloss.Width(lead) >= width {
		lead = ""
	}

	var lines []string
	current := ""
	// budget is what is left on the line being filled: the first one carries
	// the original indentation, the rest carry the continuation indent.
	budget := func() int {
		prefix := indent
		if len(lines) == 0 {
			prefix = lead
		}
		return max(width-lipgloss.Width(prefix), 1)
	}
	flush := func() {
		if len(lines) == 0 {
			lines = append(lines, lead+current)
		} else {
			lines = append(lines, indent+current)
		}
		current = ""
	}

	for _, p := range pieces {
		word := p.word
		for lipgloss.Width(word) > budget() {
			if current != "" {
				flush()
				continue
			}
			head, tail := splitAt(word, budget())
			if head == "" {
				break
			}
			current = head
			flush()
			word = tail
		}
		switch {
		case current == "":
			current = word
		case lipgloss.Width(current)+lipgloss.Width(p.sep)+lipgloss.Width(word) <= budget():
			current += p.sep + word
		default:
			flush()
			current = word
		}
	}
	if current != "" || len(lines) == 0 {
		flush()
	}
	return lines
}

// splitAt cuts s into a head of at most width cells and the remaining tail.
func splitAt(s string, width int) (head, tail string) {
	runes := []rune(s)
	for i := range runes {
		if lipgloss.Width(string(runes[:i+1])) > width {
			return string(runes[:i]), string(runes[i:])
		}
	}
	return s, ""
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
