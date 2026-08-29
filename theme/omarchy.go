package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tui-tools/tui-kit/internal/kv"
)

// OmarchyColorsPath is the file Omarchy symlinks to the active theme palette.
const OmarchyColorsPath = "~/.config/omarchy/current/theme/colors.toml"

var hexRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// paletteFromKeys maps Omarchy color keys onto our palette, keeping the
// caller-supplied base for anything the theme file does not define.
func paletteFromKeys(name string, keys map[string]string, base Palette) Palette {
	p := base
	p.Name = name
	if mode, ok := keys["mode"]; ok {
		p.Dark = !strings.EqualFold(mode, "light")
	}

	set := func(dst *string, names ...string) {
		for _, n := range names {
			if v, ok := keys[n]; ok && hexRe.MatchString(v) {
				*dst = v
				return
			}
		}
	}

	set(&p.Background, "background")
	set(&p.AltBackground, "lighter_background", "dark_background", "background")
	set(&p.Foreground, "bright_foreground", "foreground")
	set(&p.MutedForeground, "muted", "dark_foreground")
	set(&p.Accent, "accent", "blue")
	set(&p.Selection, "selection")
	set(&p.Green, "green")
	set(&p.Red, "red")
	set(&p.Yellow, "yellow")
	set(&p.Blue, "blue")
	set(&p.Magenta, "magenta")
	set(&p.Cyan, "cyan")
	set(&p.Orange, "orange", "yellow")
	return p
}

// LoadPaletteFile reads an Omarchy-style colors.toml from disk.
func LoadPaletteFile(path string) (Palette, error) {
	full := kv.ExpandHome(path)
	// Lenient parsing: an exotic theme file should degrade to the defaults,
	// never take the tool down.
	keys, err := kv.ReadFile(full, false)
	if err != nil {
		return Palette{}, fmt.Errorf("theme: open %s: %w", full, err)
	}
	if len(keys) == 0 {
		return Palette{}, fmt.Errorf("theme: %s has no usable color keys", full)
	}
	name := "omarchy"
	// The theme directory name is the most useful label we have.
	if dir := filepath.Base(filepath.Dir(full)); dir != "" && dir != "." && dir != "theme" {
		name = "omarchy:" + dir
	}
	return paletteFromKeys(name, keys, TokyoNight()), nil
}

// ResolvePalette picks the palette to use, in priority order:
//  1. $TUI_THEME, when it points at a readable colors.toml;
//  2. the active Omarchy theme, when present;
//  3. the built-in Tokyo Night default.
//
// It never fails: a broken override falls back to the default and the reason
// is returned so the caller can surface it in a status line.
func ResolvePalette() (p Palette, warning string) {
	if custom := os.Getenv("TUI_THEME"); custom != "" {
		loaded, err := LoadPaletteFile(custom)
		if err == nil {
			return loaded, ""
		}
		return TokyoNight(), err.Error()
	}
	if loaded, err := LoadPaletteFile(OmarchyColorsPath); err == nil {
		return loaded, ""
	}
	return TokyoNight(), ""
}
