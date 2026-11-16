// Package trust decides whether a file is safe for a process running as root
// to use: a binary the runner is about to execute with privilege, or a
// configuration file a root process is about to read.
//
// The rule is the one sudo's secure_path and OpenSSH's StrictModes apply. The
// file and every directory above it must be owned by root and writable by
// root alone: not by others, and by their group only when that group is root.
// Anything else lets an ordinary account replace what root runs or reads, by
// writing the file or by renaming another one into its place.
package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Info is the part of a file's metadata the rule looks at.
type Info struct {
	UID  uint32
	GID  uint32
	Mode fs.FileMode
}

// StatFunc reads Info without following a final symlink. Tests hand Check a
// fake one; Lstat is the real thing.
type StatFunc func(path string) (Info, error)

// ErrUntrusted is wrapped by every refusal, so a caller can tell "unsafe"
// from "missing".
var ErrUntrusted = errors.New("not trusted for use as root")

// Lstat is the StatFunc of the real filesystem.
func Lstat(path string) (Info, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Info{}, err
	}
	uid, gid, ok := owner(info)
	if !ok {
		return Info{}, fmt.Errorf("%s: no owner information on this platform", path)
	}
	return Info{UID: uid, GID: gid, Mode: info.Mode()}, nil
}

// Check applies the rule to path, which must be absolute and free of symlinks
// (CheckFile resolves them first). The file itself must be a regular file.
func Check(path string, stat StatFunc) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %s is not an absolute path", ErrUntrusted, path)
	}
	path = filepath.Clean(path)
	info, err := stat(path)
	if err != nil {
		return err
	}
	if !info.Mode.IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrUntrusted, path)
	}
	for p := path; ; p = filepath.Dir(p) {
		if p != path {
			if info, err = stat(p); err != nil {
				return err
			}
		}
		if err := checkOne(p, info); err != nil {
			return err
		}
		if p == "/" {
			return nil
		}
	}
}

// checkOne applies the ownership and permission rule to one path.
func checkOne(path string, info Info) error {
	if info.UID != 0 {
		return fmt.Errorf("%w: %s is owned by uid %d, not root", ErrUntrusted, path, info.UID)
	}
	perm := info.Mode.Perm()
	if perm&0o002 != 0 {
		return fmt.Errorf("%w: %s is writable by every user", ErrUntrusted, path)
	}
	if perm&0o020 != 0 && info.GID != 0 {
		return fmt.Errorf("%w: %s is writable by group %d", ErrUntrusted, path, info.GID)
	}
	return nil
}

// CheckFile resolves the symlinks in path and applies Check to the file they
// lead to, on the real filesystem. A missing file is reported as such
// (errors.Is(err, fs.ErrNotExist)), not as a refusal.
func CheckFile(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	return Check(resolved, Lstat)
}
