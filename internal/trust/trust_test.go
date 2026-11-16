package trust

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fakeFS is a filesystem described by its metadata alone.
type fakeFS map[string]Info

func (f fakeFS) stat(path string) (Info, error) {
	info, ok := f[path]
	if !ok {
		return Info{}, &fs.PathError{Op: "lstat", Path: path, Err: fs.ErrNotExist}
	}
	return info, nil
}

// system is a sane tree: everything root's, nothing writable by anyone else.
func system() fakeFS {
	dir := fs.ModeDir | 0o755
	return fakeFS{
		"/":                       {Mode: dir},
		"/usr":                    {Mode: dir},
		"/usr/sbin":               {Mode: dir},
		"/usr/sbin/ufw":           {Mode: 0o755},
		"/etc":                    {Mode: dir},
		"/etc/tui-x":              {Mode: dir},
		"/etc/tui-x/config.toml":  {Mode: 0o644},
		"/home":                   {Mode: dir},
		"/home/u":                 {UID: 1000, GID: 1000, Mode: fs.ModeDir | 0o700},
		"/home/u/.config":         {UID: 1000, GID: 1000, Mode: dir},
		"/home/u/.config/x.toml":  {Mode: 0o644},
		"/tmp":                    {Mode: fs.ModeDir | fs.ModeSticky | 0o777},
		"/tmp/ufw":                {Mode: 0o755},
		"/usr/local":              {GID: 50, Mode: fs.ModeDir | 0o775},
		"/usr/local/bin":          {Mode: dir},
		"/usr/local/bin/tool":     {Mode: 0o755},
		"/opt":                    {Mode: fs.ModeDir | 0o775},
		"/opt/tool":               {Mode: 0o755},
		"/usr/sbin/group-written": {GID: 10, Mode: 0o775},
		"/usr/sbin/world-written": {Mode: 0o757},
		"/usr/sbin/user-owned":    {UID: 1000, Mode: 0o755},
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		path string
		ok   bool
	}{
		{"/usr/sbin/ufw", true},
		{"/etc/tui-x/config.toml", true},
		// A root-owned file in a directory an ordinary user owns can be
		// renamed away and replaced by that user.
		{"/home/u/.config/x.toml", false},
		// The sticky bit does not make a world-writable directory safe for
		// root to resolve a binary in: a user can still create the name.
		{"/tmp/ufw", false},
		// Writable by a group that is not root, two levels up.
		{"/usr/local/bin/tool", false},
		// Writable by root's own group only: still root's.
		{"/opt/tool", true},
		{"/usr/sbin/group-written", false},
		{"/usr/sbin/world-written", false},
		{"/usr/sbin/user-owned", false},
		{"usr/sbin/ufw", false},
	}
	for _, tc := range tests {
		err := Check(tc.path, system().stat)
		if (err == nil) != tc.ok {
			t.Errorf("Check(%s) = %v, want ok=%v", tc.path, err, tc.ok)
		}
		if err != nil && !errors.Is(err, ErrUntrusted) {
			t.Errorf("Check(%s) = %v, not an ErrUntrusted", tc.path, err)
		}
	}
}

func TestCheckRejectsADirectory(t *testing.T) {
	if err := Check("/usr/sbin", system().stat); !errors.Is(err, ErrUntrusted) {
		t.Errorf("a directory passed as a file: %v", err)
	}
}

func TestCheckMissingIsNotARefusal(t *testing.T) {
	err := Check("/usr/sbin/missing", system().stat)
	if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrUntrusted) {
		t.Errorf("missing file = %v", err)
	}
}

// TestCheckFileOnTheRealFilesystem uses what every Linux host has: /bin/sh,
// root's, reached through the /bin symlink on a merged-/usr system; and a file
// in the test's temp dir, which belongs to whoever runs the test.
func TestCheckFileOnTheRealFilesystem(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	if err := CheckFile("/bin/sh"); err != nil {
		t.Errorf("CheckFile(/bin/sh) = %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: the temp dir is root's")
	}
	path := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckFile(path); !errors.Is(err, ErrUntrusted) {
		t.Errorf("CheckFile(user file) = %v, want a refusal", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckFile(link); !errors.Is(err, ErrUntrusted) {
		t.Errorf("CheckFile(link to a user file) = %v, want a refusal", err)
	}
	if err := CheckFile(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("CheckFile(missing) = %v, want not-exist", err)
	}
}
