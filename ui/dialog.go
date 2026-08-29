package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/edimarlnx/tui-tools/pkg/theme"
)

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
	}
	return true
}

// View renders the dialog centered in the given area.
func (c Confirm) View(t theme.Theme, width, height int) string {
	titleStyle := t.Title
	if c.Danger {
		titleStyle = t.Danger
	}
	lines := []string{titleStyle.Render(c.Title)}
	if c.Body != "" {
		lines = append(lines, "", t.Base.Render(c.Body))
	}
	if c.Command != "" {
		lines = append(lines, "",
			t.Muted.Render("Command to run:"),
			t.Command.Render("$ "+c.Command))
	}
	lines = append(lines, "",
		t.Key.Render("y")+t.KeyDesc.Render(" confirm    ")+
			t.Key.Render("n")+t.KeyDesc.Render(" cancel"))

	box := t.Dialog.MaxWidth(max(width-4, 20)).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
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

// View renders the prompt centered in the given area.
func (in Input) View(t theme.Theme, width, height int) string {
	lines := []string{t.Title.Render(in.Title), "", in.Model.View()}
	if in.Help != "" {
		lines = append(lines, "", t.Muted.Render(in.Help))
	}
	lines = append(lines, "",
		t.Key.Render("enter")+t.KeyDesc.Render(" ok    ")+
			t.Key.Render("esc")+t.KeyDesc.Render(" cancel"))

	box := t.Dialog.Width(min(max(width-8, 24), 70)).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// Picker is a single-choice list dialog (app profiles, log levels, policies).
type Picker struct {
	Title   string
	Options []string
	Cursor  int
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

// Update handles navigation and selection. It returns true when the message
// was consumed.
func (p *Picker) Update(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch key.String() {
	case "up", "k":
		if p.Cursor > 0 {
			p.Cursor--
		}
	case "down", "j":
		if p.Cursor < len(p.Options)-1 {
			p.Cursor++
		}
	case "home", "g":
		p.Cursor = 0
	case "end", "G":
		p.Cursor = max(len(p.Options)-1, 0)
	case "enter":
		p.Done, p.Accepted = true, len(p.Options) > 0
	case "esc", "q", "ctrl+c":
		p.Done, p.Accepted = true, false
	}
	return true
}

// Selected returns the highlighted option, or "" when the list is empty.
func (p Picker) Selected() string {
	if p.Cursor < 0 || p.Cursor >= len(p.Options) {
		return ""
	}
	return p.Options[p.Cursor]
}

// View renders the picker centered in the given area.
func (p Picker) View(t theme.Theme, width, height int) string {
	lines := []string{t.Title.Render(p.Title), ""}
	if len(p.Options) == 0 {
		lines = append(lines, t.Muted.Render("(nothing to choose from)"))
	}

	// Scroll the window so the cursor stays visible.
	start := 0
	if p.maxVisible > 0 && p.Cursor >= p.maxVisible {
		start = p.Cursor - p.maxVisible + 1
	}
	end := len(p.Options)
	if p.maxVisible > 0 && start+p.maxVisible < end {
		end = start + p.maxVisible
	}

	longest := 0
	for _, o := range p.Options {
		if w := lipgloss.Width(o); w > longest {
			longest = w
		}
	}
	for i := start; i < end; i++ {
		label := Pad(p.Options[i], longest)
		if i == p.Cursor {
			lines = append(lines, t.SelRow.Render("> "+label))
			continue
		}
		lines = append(lines, t.Row.Render("  "+label))
	}
	if end < len(p.Options) {
		lines = append(lines, t.Muted.Render("  … and more"))
	}

	lines = append(lines, "",
		t.Key.Render("↑/↓")+t.KeyDesc.Render(" move    ")+
			t.Key.Render("enter")+t.KeyDesc.Render(" select    ")+
			t.Key.Render("esc")+t.KeyDesc.Render(" cancel"))

	box := t.Dialog.MaxWidth(max(width-4, 20)).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
