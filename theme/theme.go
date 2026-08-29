package theme

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Theme is the palette plus the Lip Gloss styles shared by every tui-tools
// binary. Build one with New and pass it down to the widgets in pkg/ui.
type Theme struct {
	Palette Palette
	// NoColor reports that colors were disabled (NO_COLOR is set). Styles are
	// still valid, they simply carry no color attributes.
	NoColor bool
	// Warning is a non-fatal message produced while resolving the palette
	// (for example: $TUI_THEME points at a file that cannot be read).
	Warning string

	Title      lipgloss.Style
	Subtitle   lipgloss.Style
	Accent     lipgloss.Style
	Muted      lipgloss.Style
	Danger     lipgloss.Style
	OK         lipgloss.Style
	Warn       lipgloss.Style
	Info       lipgloss.Style
	Base       lipgloss.Style
	Header     lipgloss.Style
	Footer     lipgloss.Style
	Row        lipgloss.Style
	SelRow     lipgloss.Style
	TableHead  lipgloss.Style
	Border     lipgloss.Style
	Dialog     lipgloss.Style
	Key        lipgloss.Style
	KeyDesc    lipgloss.Style
	StatusLine lipgloss.Style
	Command    lipgloss.Style
}

// noColorEnabled reports whether the NO_COLOR convention asks us to drop all
// color output. Any non-empty value counts, per https://no-color.org.
func noColorEnabled() bool {
	return os.Getenv("NO_COLOR") != ""
}

// New builds a Theme from the resolved palette (see ResolvePalette).
func New() Theme {
	p, warning := ResolvePalette()
	t := FromPalette(p)
	t.Warning = warning
	return t
}

// FromPalette builds the style set for an explicit palette. Useful in tests
// and for tools that want to pin a palette regardless of the environment.
func FromPalette(p Palette) Theme {
	t := Theme{Palette: p, NoColor: noColorEnabled()}

	// With NO_COLOR we keep layout (padding, borders, bold) but drop every
	// foreground/background so the output stays readable on any terminal.
	color := func(hex string) lipgloss.TerminalColor {
		if t.NoColor {
			return lipgloss.NoColor{}
		}
		return lipgloss.Color(hex)
	}

	t.Base = lipgloss.NewStyle().Foreground(color(p.Foreground))
	t.Title = lipgloss.NewStyle().Bold(true).Foreground(color(p.Accent))
	t.Subtitle = lipgloss.NewStyle().Foreground(color(p.Cyan))
	t.Accent = lipgloss.NewStyle().Foreground(color(p.Accent))
	t.Muted = lipgloss.NewStyle().Foreground(color(p.MutedForeground))
	t.Danger = lipgloss.NewStyle().Bold(true).Foreground(color(p.Red))
	t.OK = lipgloss.NewStyle().Bold(true).Foreground(color(p.Green))
	t.Warn = lipgloss.NewStyle().Foreground(color(p.Yellow))
	t.Info = lipgloss.NewStyle().Foreground(color(p.Blue))

	t.Header = lipgloss.NewStyle().Padding(0, 1)
	t.Footer = lipgloss.NewStyle().Padding(0, 1).Foreground(color(p.MutedForeground))
	t.Row = lipgloss.NewStyle().Padding(0, 1).Foreground(color(p.Foreground))
	t.SelRow = lipgloss.NewStyle().Padding(0, 1).
		Foreground(color(p.Foreground)).
		Background(color(p.Selection)).
		Bold(true)
	t.TableHead = lipgloss.NewStyle().Padding(0, 1).Bold(true).
		Foreground(color(p.Magenta))

	t.Border = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color(p.MutedForeground))
	t.Dialog = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color(p.Accent)).
		Padding(1, 2)

	t.Key = lipgloss.NewStyle().Bold(true).Foreground(color(p.Yellow))
	t.KeyDesc = lipgloss.NewStyle().Foreground(color(p.MutedForeground))
	t.StatusLine = lipgloss.NewStyle().Padding(0, 1)
	t.Command = lipgloss.NewStyle().Foreground(color(p.Green)).Bold(true)

	return t
}
