// Package config loads a tui-tools tool's configuration from the two files
// every tool in the family reads, plus the environment.
//
// For a tool named `tui-firewall` the sources are, in increasing precedence:
//
//  1. the defaults the tool passes in Options.Defaults;
//  2. /etc/tui-firewall/config.toml;
//  3. ~/.config/tui-firewall/config.toml;
//  4. environment variables prefixed TUI_FIREWALL_ (TUI_FIREWALL_SUDO=…);
//  5. command line flags, which the tool applies with Set after loading.
//
// The file format is the small `key = "value"` subset of TOML described in
// internal/kv. Values stay untyped strings: a tool declares the keys it knows
// through Options.Defaults and reads them with String, Bool or Int, and an
// unknown key in a file is ignored so a newer config never breaks an older
// binary. Use Validate to reject the keys a tool does care about.
package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/tui-tools/tui-kit/internal/kv"
)

// Keys every tool in the family shares. A tool is free to ignore them, but
// when it supports the behaviour it must use these names.
const (
	// KeySudo is the privilege escalation prefix, as a command line
	// ("sudo -n"). Empty disables escalation.
	KeySudo = "sudo"
	// KeyTheme is a path to an Omarchy-style colors.toml; empty autodetects.
	KeyTheme = "theme"
)

// Options describes where a tool's configuration lives.
type Options struct {
	// Tool is the binary name, which is also the config directory name:
	// "tui-firewall" reads /etc/tui-firewall/config.toml. Required.
	Tool string
	// SystemPath and UserPath override the derived locations; empty uses
	// SystemPathFor(Tool) and UserPathFor(Tool). Tests point them at a
	// temporary directory.
	SystemPath string
	UserPath   string
	// EnvPrefix overrides the derived environment prefix; empty uses
	// EnvPrefixFor(Tool). Set it to "-" to disable environment lookups.
	EnvPrefix string
	// Defaults seeds the values, and declares which keys are read from the
	// environment (a key absent here is never taken from the environment,
	// so an unrelated variable cannot leak into the configuration).
	Defaults map[string]string
}

// Config is a resolved configuration: a flat key/value map plus the list of
// files that were actually read.
type Config struct {
	// Tool is the tool name the configuration was loaded for.
	Tool string
	// Values holds the resolved keys. Never nil after Load.
	Values map[string]string
	// Sources lists the files that were read, in the order they were applied.
	Sources []string
}

// SystemPathFor is the machine-wide configuration file of a tool.
func SystemPathFor(tool string) string { return "/etc/" + tool + "/config.toml" }

// UserPathFor is the per-user configuration file of a tool, which overrides
// the machine-wide one.
func UserPathFor(tool string) string { return "~/.config/" + tool + "/config.toml" }

// EnvPrefixFor derives the environment prefix of a tool: "tui-firewall"
// becomes "TUI_FIREWALL_".
func EnvPrefixFor(tool string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(tool)) + "_"
}

// envName is the variable a key is read from: prefix + upper-cased key.
func envName(prefix, key string) string {
	return prefix + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
}

// Load reads the two standard files and then the environment.
func Load(opts Options) (Config, error) {
	if opts.Tool == "" {
		return Config{}, fmt.Errorf("config: Options.Tool is required")
	}
	system := opts.SystemPath
	if system == "" {
		system = SystemPathFor(opts.Tool)
	}
	user := opts.UserPath
	if user == "" {
		user = UserPathFor(opts.Tool)
	}
	return LoadFrom(opts, system, user)
}

// LoadFrom reads the given paths in order, each overriding the previous one,
// and then applies the environment. A missing file is not an error; a
// malformed one is reported with its line number.
func LoadFrom(opts Options, paths ...string) (Config, error) {
	cfg := Config{Tool: opts.Tool, Values: map[string]string{}}
	for key, value := range opts.Defaults {
		cfg.Values[key] = value
	}

	for _, path := range paths {
		keys, err := kv.ReadFile(path, true)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cfg, fmt.Errorf("config: %w", err)
		}
		for key, value := range keys {
			cfg.Values[key] = value
		}
		cfg.Sources = append(cfg.Sources, kv.ExpandHome(path))
	}

	cfg.applyEnv(opts)
	return cfg, nil
}

// applyEnv overrides the declared keys from the environment.
func (c *Config) applyEnv(opts Options) {
	prefix := opts.EnvPrefix
	if prefix == "-" {
		return
	}
	if prefix == "" {
		prefix = EnvPrefixFor(opts.Tool)
	}
	// Only declared keys are considered, so the environment cannot introduce
	// a key the tool never asked for.
	for key := range opts.Defaults {
		if value, ok := os.LookupEnv(envName(prefix, key)); ok {
			c.Values[key] = value
		}
	}
}

// Set overrides one key, which is how a tool folds its command line in. It
// is safe on a zero Config.
func (c *Config) Set(key, value string) {
	if c.Values == nil {
		c.Values = map[string]string{}
	}
	c.Values[key] = value
}

// Has reports whether a key is present.
func (c Config) Has(key string) bool {
	_, ok := c.Values[key]
	return ok
}

// String returns a key's value, or fallback when it is absent.
func (c Config) String(key, fallback string) string {
	if v, ok := c.Values[key]; ok {
		return v
	}
	return fallback
}

// Bool reads a key as a boolean, accepting the forms strconv.ParseBool does.
// An unparsable value falls back rather than failing: use Validate to reject
// it at startup when it matters.
func (c Config) Bool(key string, fallback bool) bool {
	v, ok := c.Values[key]
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return parsed
}

// Int reads a key as an integer, falling back when it is absent or unparsable.
func (c Config) Int(key string, fallback int) int {
	v, ok := c.Values[key]
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return parsed
}

// Keys returns the configured keys, sorted, for a help or debug screen.
func (c Config) Keys() []string {
	keys := make([]string, 0, len(c.Values))
	for key := range c.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// SudoPrefix splits the `sudo` key into an argv prefix, and returns nil when
// escalation is disabled.
func (c Config) SudoPrefix() []string {
	fields := strings.Fields(c.String(KeySudo, ""))
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// Theme returns the configured palette path; empty means autodetect.
func (c Config) Theme() string { return c.String(KeyTheme, "") }

// OneOf validates that a key holds one of the allowed values, so a typo fails
// loudly at startup instead of silently selecting a default.
func (c Config) OneOf(key string, allowed ...string) error {
	value := c.String(key, "")
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("config: unknown %s %q (want %s)", key, value,
		strings.Join(allowed, ", "))
}
