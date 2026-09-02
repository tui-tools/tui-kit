package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tui-tools/tui-kit/theme"
)

// dialogMinContent is the narrowest content column a dialog lays out to. Below
// it a dialog stops shrinking and lets the terminal clip it: two cells of text
// per line is not a dialog anybody can read.
const dialogMinContent = 20

// dialogContentWidth returns the cells a dialog body has to work with, inside
// the border and the padding the dialog style adds and inside the two cells of
// margin the dialogs keep from the screen edge.
func dialogContentWidth(t theme.Theme, width int) int {
	return max(width-4-t.Dialog.GetHorizontalFrameSize(), dialogMinContent)
}

// fitBody clips body to the lines that fit in avail, starting at offset.
//
// A body that fits is returned whole with a maximum offset of zero, so a
// dialog that does not scroll renders exactly as it always did. One that does
// not fit gives up one more line to the scroll indicator the caller pins above
// the footer. avail of zero or less means the caller does not know the
// terminal height, and nothing is clipped.
func fitBody(body []string, offset, avail int) (lines []string, clamped, maxOffset int) {
	if avail <= 0 || len(body) <= avail {
		return body, 0, 0
	}
	window := max(avail-1, 1)
	maxOffset = len(body) - window
	clamped = min(max(offset, 0), maxOffset)
	return body[clamped : clamped+window], clamped, maxOffset
}

// place centers a rendered dialog box in the given area.
func place(box string, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// hintGap separates two key hints on a dialog's footer line.
const hintGap = "    "

// hintLines lays the footer hints out on as many lines as the content width
// requires. A dialog drops nothing: unlike the top-level help bar, its footer
// is the only place the keys are written down, so a narrow terminal gets two
// lines rather than a missing "esc cancel".
func hintLines(t theme.Theme, content int, hints []KeyHint) []string {
	var lines []string
	current, used := "", 0
	for _, h := range hints {
		plain := h.Key + " " + h.Desc
		cost := lipgloss.Width(plain)
		part := t.Key.Render(h.Key) + t.KeyDesc.Render(" "+h.Desc)
		switch {
		case current == "":
			current, used = part, cost
		case used+lipgloss.Width(hintGap)+cost <= content:
			current += t.KeyDesc.Render(hintGap) + part
			used += lipgloss.Width(hintGap) + cost
		default:
			lines = append(lines, current)
			current, used = part, cost
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// Confirm is a yes/no dialog that previews the exact command about to run.
// It is a value the host model stores and forwards key messages to.
type Confirm struct {
	Title string
	// Body is the explanation shown above the command preview.
	Body string
	// Command is the literal command line that will be executed; empty hides
	// the preview block.
	Command string
	// Danger paints the dialog in the danger color.
	Danger bool
	// Confirmed reports the user's answer once Done is true.
	Confirmed bool
	// Done reports that the dialog finished.
	Done bool
	// Payload carries whatever the host needs to act on the answer.
	Payload any
	// Scroll is the first visible body line. It only ever leaves zero on a
	// body too tall for the terminal, and View keeps it in range.
	Scroll int
	// maxScroll is what the last render measured, so scrolling knows where to
	// stop without the caller having to tell it the geometry.
	maxScroll int
}

// Update handles a key message. It returns true when the message was consumed.
func (c *Confirm) Update(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch strings.ToLower(key.String()) {
	case "y", "enter":
		c.Confirmed, c.Done = true, true
	case "n", "esc", "q", "ctrl+c":
		c.Confirmed, c.Done = false, true
	case "down", "j":
		c.Scroll = min(c.Scroll+1, c.maxScroll)
	case "up", "k":
		c.Scroll = max(c.Scroll-1, 0)
	case "pgdown":
		c.Scroll = min(c.Scroll+scrollPage, c.maxScroll)
	case "pgup":
		c.Scroll = max(c.Scroll-scrollPage, 0)
	case "home":
		c.Scroll = 0
	case "end":
		c.Scroll = c.maxScroll
	}
	return true
}

// scrollPage is how far pgup and pgdn move a dialog body.
const scrollPage = 10

// View renders the dialog centered in the given area.
//
// The body is wrapped to the dialog's inner width — prose on word boundaries,
// the command preview with an indented continuation — so nothing is cut. When
// the result is taller than the terminal the body scrolls under a pinned title
// and above a pinned footer, and a muted line says where in the body the
// window is.
//
// The receiver is a pointer because the render is what measures the body: it
// is where the scroll offset learns its bounds.
func (c *Confirm) View(t theme.Theme, width, height int) string {
	content := dialogContentWidth(t, width)

	titleStyle := t.Title
	if c.Danger {
		titleStyle = t.Danger
	}
	head := renderLines(Wrap(c.Title, content), titleStyle)

	var body []string
	if c.Body != "" {
		body = append(body, "")
		body = append(body, renderLines(WrapBody(c.Body, content), t.Base)...)
	}
	if c.Command != "" {
		body = append(body, "", t.Muted.Render("Command to run:"))
		body = append(body,
			renderLines(WrapBody("$ "+c.Command, content), t.Command)...)
	}

	foot := append([]string{""}, hintLines(t, content, []KeyHint{
		{Key: "y", Desc: "confirm"},
		{Key: "n", Desc: "cancel"},
	})...)

	lines, scroll, maxScroll := fitBody(body, c.Scroll,
		height-t.Dialog.GetVerticalFrameSize()-len(head)-len(foot))
	c.Scroll, c.maxScroll = scroll, maxScroll
	if maxScroll > 0 {
		lines = append(lines,
			positionLine(t, scroll, len(lines), len(body), content))
	}

	all := append(append(head, lines...), foot...)
	return place(t.Dialog.Render(strings.Join(all, "\n")), width, height)
}

// renderLines applies one style to every line of a wrapped block.
func renderLines(lines []string, style lipgloss.Style) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = style.Render(line)
	}
	return out
}

// positionLine tells the user which slice of the body is on screen and how to
// move it. It replaces the line fitBody gave up, so the dialog height is the
// same whether it scrolls or not.
func positionLine(t theme.Theme, offset, shown, total, content int) string {
	text := fmt.Sprintf("%d-%d of %d   j/k pgup/pgdn scroll",
		offset+1, offset+shown, total)
	return t.Muted.Render(Truncate(text, content))
}

// Input is a single-line prompt.
type Input struct {
	Title string
	Help  string
	Model textinput.Model
	// Done reports that the dialog finished; Accepted tells submit from cancel.
	Done     bool
	Accepted bool
	// Payload carries whatever the host needs to act on the answer.
	Payload any
}

// NewInput builds a focused single-line prompt.
func NewInput(title, placeholder, value string) Input {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.Focus()
	ti.CharLimit = 256
	return Input{Title: title, Model: ti}
}

// Update forwards the message to the text input and watches for submit and
// cancel. It returns a command to run and whether the message was consumed.
func (in *Input) Update(msg tea.Msg) (tea.Cmd, bool) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			in.Done, in.Accepted = true, true
			return nil, true
		case "esc", "ctrl+c":
			in.Done, in.Accepted = true, false
			return nil, true
		}
	}
	var cmd tea.Cmd
	in.Model, cmd = in.Model.Update(msg)
	return cmd, true
}

// Value returns the trimmed text the user typed.
func (in Input) Value() string { return strings.TrimSpace(in.Model.Value()) }

// View renders the prompt centered in the given area. The help text wraps to
// the dialog's inner width instead of being clipped by it.
func (in Input) View(t theme.Theme, width, height int) string {
	inner := min(max(width-8, 24), 70)
	content := max(inner-t.Dialog.GetHorizontalFrameSize(), dialogMinContent)

	lines := append(renderLines(Wrap(in.Title, content), t.Title),
		"", in.Model.View())
	if in.Help != "" {
		lines = append(lines, "")
		lines = append(lines, renderLines(WrapBody(in.Help, content), t.Muted)...)
	}
	lines = append(lines, "")
	lines = append(lines, hintLines(t, content, []KeyHint{
		{Key: "enter", Desc: "ok"},
		{Key: "esc", Desc: "cancel"},
	})...)

	return place(t.Dialog.Width(inner).Render(strings.Join(lines, "\n")),
		width, height)
}

// Picker is a single-choice list dialog (app profiles, log levels, policies).
//
// It carries a type-ahead filter: printable characters narrow the list to the
// options that contain what was typed, case-insensitively. That is what makes
// a three-hundred entry list — every systemd unit on a server, say — usable
// with the arrow keys.
type Picker struct {
	Title   string
	Options []string
	// Cursor indexes the currently visible options, which is Options itself
	// while Filter is empty.
	Cursor int
	// Filter is the type-ahead query. Typing appends to it, backspace edits
	// it, esc clears it before it cancels the dialog.
	Filter string
	// Done reports that the dialog finished; Accepted tells submit from cancel.
	Done     bool
	Accepted bool
	// Payload carries whatever the host needs to act on the answer.
	Payload any
	// maxVisible bounds the rendered window; zero means all options.
	maxVisible int
}

// NewPicker builds a picker positioned on the current value when it matches
// one of the options.
func NewPicker(title string, options []string, current string) Picker {
	p := Picker{Title: title, Options: options, maxVisible: 12}
	for i, o := range options {
		if o == current {
			p.Cursor = i
			break
		}
	}
	return p
}

// Visible returns the options the current filter keeps, in their original
// order. With no filter it is Options itself, so Cursor means the same thing
// it always did.
func (p Picker) Visible() []string {
	if p.Filter == "" {
		return p.Options
	}
	needle := strings.ToLower(p.Filter)
	out := make([]string, 0, len(p.Options))
	for _, o := range p.Options {
		if strings.Contains(strings.ToLower(o), needle) {
			out = append(out, o)
		}
	}
	return out
}

// setFilter applies a new filter, keeping the cursor on the option it was on
// when that option survives, and inside the list when it does not.
func (p *Picker) setFilter(filter string) {
	selected := p.Selected()
	p.Filter = filter

	options := p.Visible()
	p.Cursor = 0
	for i, o := range options {
		if o == selected {
			p.Cursor = i
			return
		}
	}
}

// Update handles filtering, navigation and selection. It returns true when the
// message was consumed.
//
// Every printable character goes to the filter, so the vi-style j/k/g/G that
// an earlier kit accepted here are gone: a picker you can type into cannot
// also spend letters on navigation. Arrows, home/end, pgup/pgdn, enter and esc
// are the navigation keys.
func (p *Picker) Update(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	// Typing comes first: key.String on a rune key is the rune itself, so the
	// navigation switch below would otherwise swallow "j", "q" and friends.
	if !key.Alt {
		switch key.Type {
		case tea.KeyRunes:
			p.setFilter(p.Filter + string(key.Runes))
			return true
		case tea.KeySpace:
			// A filter never starts with a space; inside one it is a real
			// character, because unit and profile names contain spaces.
			if p.Filter != "" {
				p.setFilter(p.Filter + " ")
			}
			return true
		}
	}

	last := max(len(p.Visible())-1, 0)
	switch key.String() {
	case "up":
		if p.Cursor > 0 {
			p.Cursor--
		}
	case "down":
		if p.Cursor < last {
			p.Cursor++
		}
	case "pgup":
		p.Cursor = max(p.Cursor-p.page(), 0)
	case "pgdown":
		p.Cursor = min(p.Cursor+p.page(), last)
	case "home":
		p.Cursor = 0
	case "end":
		p.Cursor = last
	case "backspace":
		if runes := []rune(p.Filter); len(runes) > 0 {
			p.setFilter(string(runes[:len(runes)-1]))
		}
	case "ctrl+u":
		p.setFilter("")
	case "enter":
		p.Done, p.Accepted = true, len(p.Visible()) > 0
	case "esc":
		// The filter is the first thing esc undoes: a mistyped query should
		// not cost the user the dialog.
		if p.Filter != "" {
			p.setFilter("")
			return true
		}
		p.Done, p.Accepted = true, false
	case "ctrl+c":
		p.Done, p.Accepted = true, false
	}
	return true
}

// page is how far pgup and pgdn move the cursor.
func (p Picker) page() int {
	if p.maxVisible > 0 {
		return p.maxVisible
	}
	return scrollPage
}

// Selected returns the highlighted option, or "" when nothing is listed.
func (p Picker) Selected() string {
	options := p.Visible()
	if p.Cursor < 0 || p.Cursor >= len(options) {
		return ""
	}
	return options[p.Cursor]
}

// View renders the picker centered in the given area.
//
// The window grows to whatever the terminal height allows, up to the picker's
// own bound, and the option labels are padded to the widest one that fits the
// dialog rather than to the widest one there is.
func (p *Picker) View(t theme.Theme, width, height int) string {
	content := dialogContentWidth(t, width)
	options := p.Visible()

	head := renderLines(Wrap(p.Title, content), t.Title)
	if p.Filter != "" {
		head = append(head, t.Muted.Render("filter: ")+
			t.Accent.Render(Truncate(p.Filter, max(content-8, 1))))
	}
	head = append(head, "")

	foot := append([]string{""}, hintLines(t, content, []KeyHint{
		{Key: "↑/↓", Desc: "move"},
		{Key: "type", Desc: "filter"},
		{Key: "enter", Desc: "select"},
		{Key: "esc", Desc: "cancel"},
	})...)

	if len(options) == 0 {
		empty := "(nothing to choose from)"
		if p.Filter != "" {
			empty = "(no option matches the filter)"
		}
		all := append(append(head, t.Muted.Render(empty)), foot...)
		return place(t.Dialog.Render(strings.Join(all, "\n")), width, height)
	}

	// The window is the smaller of the picker's bound and what the terminal
	// leaves between the pinned title and the pinned footer. One line is kept
	// for the "and more" marker.
	window := len(options)
	if p.maxVisible > 0 {
		window = min(window, p.maxVisible)
	}
	if height > 0 {
		room := height - t.Dialog.GetVerticalFrameSize() - len(head) - len(foot) - 1
		window = min(window, max(room, 1))
	}

	// Scroll the window so the cursor stays visible.
	p.Cursor = min(max(p.Cursor, 0), len(options)-1)
	start := 0
	if p.Cursor >= window {
		start = p.Cursor - window + 1
	}
	end := min(start+window, len(options))

	labelWidth := 0
	for _, o := range options {
		if w := lipgloss.Width(o); w > labelWidth {
			labelWidth = w
		}
	}
	// "> " prefix plus the row style's own padding.
	labelWidth = min(labelWidth, max(content-2-t.Row.GetHorizontalFrameSize(), 1))

	lines := head
	for i := start; i < end; i++ {
		label := Pad(options[i], labelWidth)
		if i == p.Cursor {
			lines = append(lines, t.SelRow.Render("> "+label))
			continue
		}
		lines = append(lines, t.Row.Render("  "+label))
	}
	if len(options) > window {
		lines = append(lines, t.Muted.Render(Truncate(
			fmt.Sprintf("%d-%d of %d", start+1, end, len(options)), content)))
	}

	return place(t.Dialog.Render(strings.Join(append(lines, foot...), "\n")),
		width, height)
}
