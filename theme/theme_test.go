package theme

import (
	"os"
	"path/filepath"
	"testing"
)

const tokyoNightTOML = `mode = "dark"

accent = "#7aa2f7"
selection = "#292e42"
muted = "#414868"

background = "#1a1b26"
lighter_background = "#24283b"

foreground = "#a9b1d6"
dark_foreground = "#565f89"
bright_foreground = "#c0caf5"

red = "#f7768e"
yellow = "#e0af68"
orange = "#eb927b"
green = "#9ece6a"
cyan = "#449dab"
blue = "#7aa2f7"
magenta = "#ad8ee6"
`

const lightTOML = `mode = "light"
accent = "#6e6e6e"
selection = "#c0c0c0"
background = "#ffffff"
foreground = "#000000"
red = "#2a2a2a"
`

func writeTheme(t *testing.T, dir, content string) string {
	t.Helper()
	themeDir := filepath.Join(t.TempDir(), dir)
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(themeDir, "colors.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadPaletteFile(t *testing.T) {
	tests := []struct {
		name    string
		dir     string
		content string
		want    Palette
	}{
		{
			name:    "omarchy tokyo night",
			dir:     "tokyo-night",
			content: tokyoNightTOML,
			want: Palette{
				Name: "omarchy:tokyo-night", Dark: true,
				Background: "#1a1b26", AltBackground: "#24283b",
				Foreground: "#c0caf5", MutedForeground: "#414868",
				Accent: "#7aa2f7", Selection: "#292e42",
				Green: "#9ece6a", Red: "#f7768e", Yellow: "#e0af68",
				Blue: "#7aa2f7", Magenta: "#ad8ee6", Cyan: "#449dab",
				Orange: "#eb927b",
			},
		},
		{
			name:    "light theme falls back for missing keys",
			dir:     "white",
			content: lightTOML,
			want: Palette{
				Name: "omarchy:white", Dark: false,
				Background: "#ffffff", AltBackground: "#ffffff",
				Foreground: "#000000", MutedForeground: "#565f89",
				Accent: "#6e6e6e", Selection: "#c0c0c0",
				Green: "#9ece6a", Red: "#2a2a2a", Yellow: "#e0af68",
				Blue: "#7aa2f7", Magenta: "#bb9af7", Cyan: "#7dcfff",
				Orange: "#e0af68",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTheme(t, tc.dir, tc.content)
			got, err := LoadPaletteFile(path)
			if err != nil {
				t.Fatalf("LoadPaletteFile: %v", err)
			}
			if got != tc.want {
				t.Errorf("palette mismatch\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

func TestLoadPaletteFileErrors(t *testing.T) {
	if _, err := LoadPaletteFile(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	empty := writeTheme(t, "empty", "# only a comment\n\n")
	if _, err := LoadPaletteFile(empty); err == nil {
		t.Fatal("expected an error for a file without color keys")
	}
}

func TestResolvePaletteEnvOverride(t *testing.T) {
	path := writeTheme(t, "tokyo-night", tokyoNightTOML)
	t.Setenv("TUI_THEME", path)

	got, warning := ResolvePalette()
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if got.Name != "omarchy:tokyo-night" {
		t.Errorf("Name = %q, want omarchy:tokyo-night", got.Name)
	}

	t.Setenv("TUI_THEME", filepath.Join(t.TempDir(), "nope.toml"))
	got, warning = ResolvePalette()
	if warning == "" {
		t.Error("expected a warning for an unreadable TUI_THEME")
	}
	if got != TokyoNight() {
		t.Error("expected a fallback to the default palette")
	}
}

func TestFromPaletteNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	tm := FromPalette(TokyoNight())
	if !tm.NoColor {
		t.Fatal("NoColor should be true when NO_COLOR is set")
	}
	if got := tm.Danger.Render("x"); got != "x" {
		t.Errorf("expected unstyled output with NO_COLOR, got %q", got)
	}
}
