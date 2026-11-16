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
//
// A tool started as root holds its configuration to a stricter standard,
// because what it reads decides what root runs (which smb.conf to manage,
// which directory to create). Every file it reads must be owned by root and
// writable by root alone, and so must every directory above it; a file that
// is not stops the load with an error naming it. "~" is root's home from the
// account database, never $HOME, so `sudo -E` cannot point the tool at the
// invoking user's dotfiles. And when root was reached from another account
// the TUI_<TOOL>_* variables are refused: they come from that account, through
// `sudo -E`, a sudoers policy that keeps them, or `su` without "-", which
// keeps the whole environment. Such a session is recognised by SUDO_USER,
// DOAS_USER or PKEXEC_UID, and otherwise by the kernel's login uid
// (/proc/self/loginuid), which su and every other switch leave pointing at
// the account that logged in.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tui-tools/tui-kit/internal/kv"
	"github.com/tui-tools/tui-kit/internal/trust"
)

// Hooks the tests replace: who the process runs as, how a file is checked
// before root reads it, and where root's home is.
var (
	geteuid   = os.Geteuid
	checkFile = trust.CheckFile
	homeOf    = func(uid int) (string, error) {
		u, err := user.LookupId(strconv.Itoa(uid))
		if err != nil {
			return "", err
		}
		return u.HomeDir, nil
	}
	loginUID = readLoginUID
)

// unsetLoginUID is what /proc/self/loginuid holds for a process no login
// started: a boot service, a container without audit, a CI runner.
const unsetLoginUID = 4294967295

// readLoginUID reads the audit login uid of this process. ok is false when it
// cannot be read or was never set.
func readLoginUID() (uid int, ok bool) {
	raw, err := os.ReadFile("/proc/self/loginuid")
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
	// The upper bound also excludes the unset marker; a uid above it is not
	// one a login is given, and int is only guaranteed 32 bits.
	if err != nil || value == unsetLoginUID || value > math.MaxInt32 {
		return 0, false
	}
	return int(value), true
}

// invokedBy names the account a root process was reached from, or "" when
// root logged in as itself. The switch tools that say so are believed first;
// the login uid catches the ones that do not (`su` keeps no such variable).
func invokedBy() string {
	for _, name := range []string{"SUDO_USER", "DOAS_USER"} {
		if who := os.Getenv(name); who != "" {
			return who
		}
	}
	if uid := os.Getenv("PKEXEC_UID"); uid != "" {
		return "uid " + uid
	}
	if uid, ok := loginUID(); ok && uid != 0 {
		if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
			return u.Username
		}
		return "uid " + strconv.Itoa(uid)
	}
	return ""
}

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
// malformed one is reported with its line number. As root, a file that root
// alone does not control is an error too (see the package documentation).
func LoadFrom(opts Options, paths ...string) (Config, error) {
	cfg := Config{Tool: opts.Tool, Values: map[string]string{}}
	for key, value := range opts.Defaults {
		cfg.Values[key] = value
	}
	root := geteuid() == 0

	for _, path := range paths {
		full, err := expandHome(path, root)
		if err != nil {
			return cfg, fmt.Errorf("config: %w", err)
		}
		if root {
			if err := checkFile(full); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return cfg, fmt.Errorf("config: refusing %s: %w; running as root, "+
					"a configuration file must be owned by root and writable by "+
					"root alone, and so must every directory above it", full, err)
			}
		}
		keys, err := kv.ReadFile(full, true)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cfg, fmt.Errorf("config: %w", err)
		}
		for key, value := range keys {
			cfg.Values[key] = value
		}
		cfg.Sources = append(cfg.Sources, full)
	}

	if err := cfg.applyEnv(opts, root); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// expandHome turns a leading "~" into a home directory: the caller's $HOME
// normally, and as root the effective user's home from the account database,
// because $HOME is whatever the invoking user had under `sudo -E`.
func expandHome(path string, root bool) (string, error) {
	if !root || (path != "~" && !strings.HasPrefix(path, "~/")) {
		return kv.ExpandHome(path), nil
	}
	home, err := homeOf(geteuid())
	if err != nil || home == "" {
		return "", fmt.Errorf("cannot find root's home directory to expand %s", path)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// applyEnv overrides the declared keys from the environment. As root reached
// from another account (sudo, doas, pkexec, su) it refuses them instead: they
// belong to that account, not to root.
func (c *Config) applyEnv(opts Options, root bool) error {
	prefix := opts.EnvPrefix
	if prefix == "-" {
		return nil
	}
	if prefix == "" {
		prefix = EnvPrefixFor(opts.Tool)
	}
	invoker := ""
	if root {
		invoker = invokedBy()
	}
	var refused []string
	// Only declared keys are considered, so the environment cannot introduce
	// a key the tool never asked for.
	for key := range opts.Defaults {
		name := envName(prefix, key)
		value, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		if invoker != "" {
			refused = append(refused, name)
			continue
		}
		c.Values[key] = value
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		return fmt.Errorf("config: %s set in a root session started by %s; "+
			"running as root, the configuration is read from files root owns: "+
			"unset it (sudo without -E, su with -), or put the key in %s",
			strings.Join(refused, ", "), invoker, SystemPathFor(opts.Tool))
	}
	return nil
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
