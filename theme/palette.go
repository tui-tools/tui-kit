// Package theme provides the shared visual identity for every tui-tools
// binary: a color palette (Tokyo Night by default), optional loading of the
// active Omarchy desktop theme, and a set of ready-made Lip Gloss styles.
package theme

// Palette holds the raw colors used by every tui-tools binary. Values are hex
// strings ("#rrggbb"). Keeping them as strings (instead of lipgloss colors)
// makes the palette trivially serializable and comparable in tests.
type Palette struct {
	// Name identifies the palette source ("tokyo-night", "omarchy:gruvbox", …).
	Name string
	// Dark reports whether the palette is meant for a dark terminal.
	Dark bool

	Background      string
	AltBackground   string
	Foreground      string
	MutedForeground string

	// Accent drives titles and focused borders.
	Accent string
	// Selection is the background of the highlighted row.
	Selection string

	Green   string
	Red     string
	Yellow  string
	Blue    string
	Magenta string
	Cyan    string
	Orange  string
}

// TokyoNight is the default palette, matching the Omarchy tokyo-night theme.
func TokyoNight() Palette {
	return Palette{
		Name:            "tokyo-night",
		Dark:            true,
		Background:      "#1a1b26",
		AltBackground:   "#24283b",
		Foreground:      "#c0caf5",
		MutedForeground: "#565f89",
		Accent:          "#7aa2f7",
		Selection:       "#33467c",
		Green:           "#9ece6a",
		Red:             "#f7768e",
		Yellow:          "#e0af68",
		Blue:            "#7aa2f7",
		Magenta:         "#bb9af7",
		Cyan:            "#7dcfff",
		Orange:          "#e0af68",
	}
}
