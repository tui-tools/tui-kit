// Package config loads the fwall configuration file. The format is the tiny
// subset of TOML fwall needs: top-level `key = "value"` pairs.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The backend selection values accepted by the `backend` key.
const (
	BackendAuto      = "auto"
	BackendUFW       = "ufw"
	BackendFirewalld = "firewalld"
)

// SystemPath is the machine-wide configuration file.
const SystemPath = "/etc/fwall/config.toml"

// UserPath is the per-user configuration file, which overrides SystemPath.
const UserPath = "~/.config/fwall/config.toml"

// Config is the resolved fwall configuration.
type Config struct {
	// Backend is "auto", "ufw" or "firewalld".
	Backend string
	// Sudo is the privilege-escalation prefix, as a command line
	// ("sudo -n"). Empty disables escalation entirely.
	Sudo string
	// Theme is a path to an Omarchy-style colors.toml; empty means autodetect.
	Theme string
	// Sources lists the files that were actually read, for the help screen.
	Sources []string
}

// Default returns the configuration used when no file is present.
func Default() Config {
	return Config{Backend: BackendAuto, Sudo: "sudo -n"}
}

// SudoPrefix splits Sudo into an argv prefix. It returns nil when escalation
// is disabled.
func (c Config) SudoPrefix() []string {
	fields := strings.Fields(c.Sudo)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// Validate rejects unknown values so a typo fails loudly at startup.
func (c Config) Validate() error {
	switch c.Backend {
	case BackendAuto, BackendUFW, BackendFirewalld:
		return nil
	default:
		return fmt.Errorf("config: unknown backend %q (want auto, ufw or firewalld)",
			c.Backend)
	}
}

// expandHome turns a leading "~" into the user home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

// Load reads SystemPath then UserPath, each overriding the previous one. A
// missing file is not an error; a malformed one is reported.
func Load() (Config, error) {
	return LoadFrom(SystemPath, UserPath)
}

// LoadFrom reads the given paths in order, later files overriding earlier
// ones. Exported so tests can point at a temporary directory.
func LoadFrom(paths ...string) (Config, error) {
	cfg := Default()
	for _, path := range paths {
		full := expandHome(path)
		keys, err := readFile(full)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cfg, err
		}
		applyKeys(&cfg, keys)
		cfg.Sources = append(cfg.Sources, full)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// applyKeys copies the recognized keys onto cfg. Unknown keys are ignored so
// a newer config file does not break an older binary.
func applyKeys(cfg *Config, keys map[string]string) {
	if v, ok := keys["backend"]; ok {
		cfg.Backend = strings.ToLower(v)
	}
	if v, ok := keys["sudo"]; ok {
		cfg.Sudo = v
	}
	if v, ok := keys["theme"]; ok {
		cfg.Theme = v
	}
}

// readFile parses one config file.
func readFile(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // the path is a fixed config location
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	keys := map[string]string{}
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") || strings.HasPrefix(text, "[") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return nil, fmt.Errorf("config: %s:%d: expected `key = \"value\"`", path, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Drop a trailing inline comment when the value is quoted.
		if i := strings.LastIndex(value, `"`); i > 0 {
			value = value[:i+1]
		}
		keys[key] = strings.Trim(value, `"'`)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return keys, nil
}
