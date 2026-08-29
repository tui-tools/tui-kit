// Package kv reads the tiny subset of TOML the tui-tools family uses for its
// configuration and palette files: top-level `key = "value"` pairs, `#`
// comments and blank lines. It is deliberately not a TOML library — the
// format is a contract with the user, not with a spec, and keeping it in
// forty lines is what lets every tool ship with three dependencies.
package kv

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome turns a leading "~" into the user home directory. A home that
// cannot be resolved leaves the path untouched, so the caller reports a
// missing file rather than a surprising one.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

// Parse reads key/value pairs from r.
//
// With strict set, a line that is neither blank, a comment, a table header nor
// a `key = value` pair is an error naming the line number: a typo in a config
// file should fail loudly at startup. Without it, such a line is skipped, which
// is what a palette file wants — an exotic theme should degrade to the
// defaults instead of taking the whole tool down.
func Parse(r io.Reader, name string, strict bool) (map[string]string, error) {
	keys := map[string]string{}
	scanner := bufio.NewScanner(r)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") || strings.HasPrefix(text, "[") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			if strict {
				return nil, fmt.Errorf("%s:%d: expected `key = \"value\"`", name, line)
			}
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Drop a trailing inline comment when the value is quoted.
		if i := strings.LastIndex(value, `"`); i > 0 {
			value = value[:i+1]
		}
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		keys[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return keys, nil
}

// ReadFile parses one file, expanding a leading "~" in the path. A missing
// file returns an error satisfying os.IsNotExist, so callers can treat it as
// "not configured" rather than as a failure.
func ReadFile(path string, strict bool) (map[string]string, error) {
	full := ExpandHome(path)
	f, err := os.Open(full) //nolint:gosec // the path is a config location chosen by the user
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return Parse(f, full, strict)
}
