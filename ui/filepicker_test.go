package ui

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fakeTree is the tree every FilePicker test walks. It stands for a host
// where a tool asks for a certificate, a key or a share directory.
func fakeTree() fstest.MapFS {
	file := &fstest.MapFile{Data: []byte("x"), Mode: 0o644}
	dir := &fstest.MapFile{Mode: fs.ModeDir | 0o755}
	return fstest.MapFS{
		"etc/ssl/certs/ca.pem":       file,
		"etc/ssl/certs/server.CRT":   file,
		"etc/ssl/certs/README":       file,
		"etc/ssl/private/server.key": file,
		"etc/ssl/.hidden.pem":        file,
		"etc/ssl/openssl.cnf":        file,
		"etc/secret/key.pem":         file,
		"home/admin/.ssh/id_ed25519": file,
		"home/admin/notes.txt":       file,
		"srv/share":                  dir,
		"srv/empty":                  dir,
	}
}

// deniedFS answers like the fake tree, except that the listed directories
// cannot be read, as a root-only directory is for an unprivileged tool.
type deniedFS struct {
	FileSystem
	denied map[string]bool
}

func (d deniedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if d.denied[name] {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return d.FileSystem.ReadDir(name)
}

func newTestPicker(opts FilePickerOptions) FilePicker {
	if opts.FS == nil {
		opts.FS = deniedFS{
			FileSystem: FileSystemFromFS(fakeTree()),
			denied:     map[string]bool{"/etc/secret": true},
		}
	}
	return NewFilePicker(opts)
}

// listed returns the names on screen, the way the picker labels them.
func listed(p FilePicker) []string {
	var names []string
	for _, e := range p.entries {
		switch {
		case e.self:
			names = append(names, "./")
		case e.dir:
			names = append(names, e.name+"/")
		default:
			names = append(names, e.name)
		}
	}
	return names
}

// press sends named keys; it knows the few keyMsg does not.
func press(p *FilePicker, keys ...string) {
	for _, k := range keys {
		switch k {
		case "tab":
			p.Update(tea.KeyMsg{Type: tea.KeyTab})
		case "right":
			p.Update(tea.KeyMsg{Type: tea.KeyRight})
		case "left":
			p.Update(tea.KeyMsg{Type: tea.KeyLeft})
		default:
			p.Update(keyMsg(k))
		}
	}
}

func typeText(p *FilePicker, text string) {
	for _, r := range text {
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestFilePickerStartsWhereItIsTold(t *testing.T) {
	tests := []struct {
		name, start, dir, highlighted string
	}{
		{name: "a directory", start: "/etc/ssl", dir: "/etc/ssl",
			highlighted: "/etc/ssl/certs"},
		{name: "a file highlights it", start: "/etc/ssl/certs/ca.pem",
			dir: "/etc/ssl/certs", highlighted: "/etc/ssl/certs/ca.pem"},
		{name: "a missing path opens its nearest parent",
			start: "/etc/ssl/gone/cert.pem", dir: "/etc/ssl",
			highlighted: "/etc/ssl/certs"},
		{name: "nothing at all opens the root", start: "/nope/nada", dir: "/",
			highlighted: "/etc"},
		{name: "unclean input is cleaned", start: "/etc//ssl/./certs/../",
			dir: "/etc/ssl", highlighted: "/etc/ssl/certs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPicker(FilePickerOptions{Start: tc.start})
			if p.Dir != tc.dir {
				t.Errorf("Dir = %q, want %q", p.Dir, tc.dir)
			}
			if got := p.Highlighted(); got != tc.highlighted {
				t.Errorf("Highlighted = %q, want %q", got, tc.highlighted)
			}
		})
	}
}

func TestFilePickerNavigation(t *testing.T) {
	p := newTestPicker(FilePickerOptions{Start: "/etc/ssl"})
	if got := strings.Join(listed(p), " "); got != "certs/ private/ openssl.cnf" {
		t.Fatalf("listing = %q", got)
	}

	// enter on a directory opens it in a file picker.
	press(&p, "enter")
	if p.Dir != "/etc/ssl/certs" || p.Done {
		t.Fatalf("enter on certs/: Dir = %q, Done = %v", p.Dir, p.Done)
	}
	if got := strings.Join(listed(p), " "); got != "ca.pem README server.CRT" {
		t.Errorf("listing = %q, want case-insensitive name order", got)
	}

	// backspace goes up with the cursor on the directory just left.
	press(&p, "backspace")
	if p.Dir != "/etc/ssl" || p.Highlighted() != "/etc/ssl/certs" {
		t.Errorf("after backspace: Dir = %q, Highlighted = %q", p.Dir, p.Highlighted())
	}
	press(&p, "h", "h")
	if p.Dir != "/" {
		t.Errorf("h twice from /etc/ssl: Dir = %q, want /", p.Dir)
	}
	press(&p, "h")
	if p.Dir != "/" {
		t.Errorf("up from / must stay at /, got %q", p.Dir)
	}

	// Arrows move and stop at the ends; enter on a file chooses it.
	p = newTestPicker(FilePickerOptions{Start: "/etc/ssl/certs"})
	press(&p, "up", "down", "down", "down", "down")
	if got := p.Highlighted(); got != "/etc/ssl/certs/server.CRT" {
		t.Errorf("Highlighted = %q after running past the end", got)
	}
	press(&p, "home")
	if got := p.Highlighted(); got != "/etc/ssl/certs/ca.pem" {
		t.Errorf("home: Highlighted = %q", got)
	}
	press(&p, "enter")
	if !p.Done || !p.Accepted || p.Value() != "/etc/ssl/certs/ca.pem" {
		t.Errorf("enter on a file: Done=%v Accepted=%v Value=%q",
			p.Done, p.Accepted, p.Value())
	}
}

func TestFilePickerEscAndCtrlCCancel(t *testing.T) {
	for _, k := range []tea.KeyMsg{keyMsg("esc"), {Type: tea.KeyCtrlC}} {
		p := newTestPicker(FilePickerOptions{Start: "/etc/ssl/certs"})
		p.Update(k)
		if !p.Done || p.Accepted || p.Value() != "" {
			t.Errorf("%s: Done=%v Accepted=%v Value=%q",
				k.String(), p.Done, p.Accepted, p.Value())
		}
	}
}

func TestFilePickerExtensionFilter(t *testing.T) {
	p := newTestPicker(FilePickerOptions{
		Start:      "/etc/ssl/certs",
		Extensions: []string{"pem", ".crt"},
	})
	// Case-insensitive, with or without the dot; README is filtered out.
	if got := strings.Join(listed(p), " "); got != "ca.pem server.CRT" {
		t.Errorf("listing = %q", got)
	}
	// Directories are always listed, so the user can still walk the tree.
	press(&p, "backspace")
	if got := strings.Join(listed(p), " "); got != "certs/ private/" {
		t.Errorf("listing of /etc/ssl = %q", got)
	}
}

func TestFilePickerHiddenToggle(t *testing.T) {
	p := newTestPicker(FilePickerOptions{Start: "/etc/ssl"})
	for _, name := range listed(p) {
		if strings.HasPrefix(name, ".") {
			t.Fatalf("hidden entry listed by default: %q", name)
		}
	}
	press(&p, "down") // private/
	press(&p, ".")
	if !p.ShowHidden {
		t.Fatal(". should show hidden entries")
	}
	if got := strings.Join(listed(p), " "); got != "certs/ private/ .hidden.pem openssl.cnf" {
		t.Errorf("listing with hidden = %q", got)
	}
	if got := p.Highlighted(); got != "/etc/ssl/private" {
		t.Errorf("the toggle must keep the cursor on its entry, got %q", got)
	}
	press(&p, ".")
	if p.ShowHidden {
		t.Error(". again should hide them")
	}

	p = newTestPicker(FilePickerOptions{Start: "/home/admin", ShowHidden: true})
	if got := strings.Join(listed(p), " "); got != ".ssh/ notes.txt" {
		t.Errorf("ShowHidden from the start: listing = %q", got)
	}
}

func TestFilePickerDirsOnly(t *testing.T) {
	p := newTestPicker(FilePickerOptions{Start: "/srv", DirsOnly: true})
	if got := strings.Join(listed(p), " "); got != "./ empty/ share/" {
		t.Fatalf("listing = %q", got)
	}

	// The "./" row chooses the directory being listed.
	press(&p, "enter")
	if !p.Accepted || p.Value() != "/srv" {
		t.Errorf("enter on ./: Accepted=%v Value=%q", p.Accepted, p.Value())
	}

	// enter chooses a subdirectory; right opens it instead.
	p = newTestPicker(FilePickerOptions{Start: "/srv", DirsOnly: true})
	press(&p, "down", "down", "right")
	if p.Dir != "/srv/share" || p.Done {
		t.Fatalf("right on share/: Dir=%q Done=%v", p.Dir, p.Done)
	}
	if got := strings.Join(listed(p), " "); got != "./" {
		t.Errorf("an empty directory still offers itself, got %q", got)
	}
	// Back up lands on share/, so empty/ is one row above it.
	press(&p, "backspace", "up", "enter")
	if !p.Accepted || p.Value() != "/srv/empty" {
		t.Errorf("enter on empty/: Accepted=%v Value=%q", p.Accepted, p.Value())
	}

	// No files are listed at all.
	p = newTestPicker(FilePickerOptions{Start: "/etc/ssl/certs", DirsOnly: true})
	if got := strings.Join(listed(p), " "); got != "./" {
		t.Errorf("files listed in a directory picker: %q", got)
	}
}

func TestFilePickerTypedPath(t *testing.T) {
	// "/" opens the path field on the listed directory.
	p := newTestPicker(FilePickerOptions{Start: "/etc/ssl"})
	press(&p, "/")
	if !p.Typing() || p.path.Value() != "/etc/ssl/" {
		t.Fatalf("after /: Typing=%v field=%q", p.Typing(), p.path.Value())
	}
	// Keys that mean something to the listing are text in the field.
	typeText(&p, "certs/../private/server.key")
	if p.Dir != "/etc/ssl" || p.ShowHidden {
		t.Fatal("typing must not drive the listing")
	}
	press(&p, "enter")
	if !p.Accepted || p.Value() != "/etc/ssl/private/server.key" {
		t.Errorf("typed file: Accepted=%v Value=%q", p.Accepted, p.Value())
	}

	// A typed directory is opened, and the focus goes back to the list.
	p = newTestPicker(FilePickerOptions{Start: "/"})
	press(&p, "/")
	typeText(&p, "home/admin")
	press(&p, "enter")
	if p.Done || p.Typing() || p.Dir != "/home/admin" {
		t.Errorf("typed dir: Done=%v Typing=%v Dir=%q", p.Done, p.Typing(), p.Dir)
	}

	// A relative path is taken from the listed directory.
	p = newTestPicker(FilePickerOptions{Start: "/etc/ssl"})
	press(&p, "tab", "ctrl+u")
	typeText(&p, "certs/ca.pem")
	press(&p, "enter")
	if p.Value() != "/etc/ssl/certs/ca.pem" {
		t.Errorf("relative typed path: Value=%q", p.Value())
	}

	// esc leaves the field without cancelling the dialog.
	p = newTestPicker(FilePickerOptions{Start: "/etc/ssl"})
	press(&p, "/", "esc")
	if p.Typing() || p.Done {
		t.Errorf("esc in the field: Typing=%v Done=%v", p.Typing(), p.Done)
	}

	// A paste lands in the field, wherever the focus was.
	p = newTestPicker(FilePickerOptions{Start: "/srv"})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" /etc/ssl/certs/ca.pem\n"), Paste: true})
	if !p.Typing() || p.path.Value() != "/etc/ssl/certs/ca.pem" {
		t.Fatalf("paste: Typing=%v field=%q", p.Typing(), p.path.Value())
	}
	press(&p, "enter")
	if p.Value() != "/etc/ssl/certs/ca.pem" {
		t.Errorf("pasted path: Value=%q", p.Value())
	}

	// "~" expands to the home directory it was given.
	p = newTestPicker(FilePickerOptions{Start: "/", Home: "/home/admin"})
	press(&p, "~")
	typeText(&p, "notes.txt")
	press(&p, "enter")
	if p.Value() != "/home/admin/notes.txt" {
		t.Errorf("~ path: Value=%q", p.Value())
	}
}

// TestFilePickerInitialPath: a suggested path opens in the focused field, as
// typed, so the user confirms it with enter or edits it (tui-kit#37).
func TestFilePickerInitialPath(t *testing.T) {
	tm := testTheme(t)
	opts := FilePickerOptions{InitialPath: "~/ca.crt", Home: "/home/admin",
		NewFile: true}

	// Confirmed as suggested: "~" expands, the new file is accepted.
	p := newTestPicker(opts)
	if !p.Typing() || p.path.Value() != "~/ca.crt" || !p.path.Focused() {
		t.Fatalf("opened: Typing=%v field=%q focused=%v",
			p.Typing(), p.path.Value(), p.path.Focused())
	}
	if p.Dir != "/home/admin" {
		t.Errorf("without Start the listing opens where the path points: Dir=%q", p.Dir)
	}
	if out := p.View(tm, 80, 24); !strings.Contains(out, "~/ca.crt") {
		t.Errorf("the view must show the suggested path:\n%s", out)
	}
	press(&p, "enter")
	if !p.Done || !p.Accepted || p.Value() != "/home/admin/ca.crt" {
		t.Errorf("confirmed: Done=%v Accepted=%v Value=%q", p.Done, p.Accepted, p.Value())
	}

	// Edited before confirming: the cursor sits at the end of the text.
	p = newTestPicker(opts)
	press(&p, "backspace", "backspace", "backspace")
	typeText(&p, "pem")
	press(&p, "enter")
	if p.Value() != "/home/admin/ca.pem" {
		t.Errorf("edited: Value=%q", p.Value())
	}

	// esc goes back to the listing, not out of the dialog.
	p = newTestPicker(opts)
	press(&p, "esc")
	if p.Done || p.Typing() {
		t.Errorf("esc: Done=%v Typing=%v", p.Done, p.Typing())
	}

	// Start still decides the listing; the field keeps the suggestion.
	p = newTestPicker(FilePickerOptions{Start: "/etc/ssl/certs/ca.pem",
		InitialPath: "/srv/share/ca.pem", NewFile: true})
	if p.Dir != "/etc/ssl/certs" || p.Highlighted() != "/etc/ssl/certs/ca.pem" {
		t.Errorf("with Start: Dir=%q Highlighted=%q", p.Dir, p.Highlighted())
	}
	if !p.Typing() || p.path.Value() != "/srv/share/ca.pem" {
		t.Errorf("with Start: Typing=%v field=%q", p.Typing(), p.path.Value())
	}

	// The options still rule: a suggestion in a missing directory is
	// refused inline and stays in the field to fix.
	p = newTestPicker(FilePickerOptions{InitialPath: "/nope/ca.crt", NewFile: true})
	press(&p, "enter")
	if p.Done || !p.Typing() || p.Message() == "" {
		t.Errorf("refused: Done=%v Typing=%v Message=%q", p.Done, p.Typing(), p.Message())
	}

	// Without InitialPath nothing changes: the listing has the focus.
	if p := newTestPicker(FilePickerOptions{Start: "/etc/ssl"}); p.Typing() {
		t.Error("a picker without InitialPath must open on the listing")
	}
}

func TestFilePickerTypedPathIsChecked(t *testing.T) {
	tests := []struct {
		name  string
		opts  FilePickerOptions
		typed string
		want  string // accepted value, or "" when refused
		msg   string // part of the inline message when refused
	}{
		{name: "missing file refused by default",
			typed: "/etc/ssl/new.pem", msg: "no such file"},
		{name: "new file accepted when allowed",
			opts: FilePickerOptions{NewFile: true}, typed: "/etc/ssl/new.pem",
			want: "/etc/ssl/new.pem"},
		{name: "new file needs its directory",
			opts: FilePickerOptions{NewFile: true}, typed: "/etc/nowhere/new.pem",
			msg: "directory does not exist: /etc/nowhere"},
		{name: "wrong extension refused",
			opts: FilePickerOptions{Extensions: []string{".pem"}}, typed: "/etc/ssl/openssl.cnf",
			msg: "expects a .pem file"},
		{name: "wrong extension refused for a new file too",
			opts:  FilePickerOptions{NewFile: true, Extensions: []string{".pem"}},
			typed: "/etc/ssl/new.txt", msg: "expects a .pem file"},
		{name: "a file is not a directory",
			opts: FilePickerOptions{DirsOnly: true}, typed: "/etc/ssl/openssl.cnf",
			msg: "not a directory"},
		{name: "a typed directory is chosen in a directory picker",
			opts: FilePickerOptions{DirsOnly: true}, typed: "/srv/share/",
			want: "/srv/share"},
		{name: "a new directory under an existing one",
			opts:  FilePickerOptions{DirsOnly: true, NewFile: true},
			typed: "/srv/new-share", want: "/srv/new-share"},
		{name: "an empty field is refused", typed: "", msg: "type a path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Start = "/etc/ssl"
			p := newTestPicker(tc.opts)
			press(&p, "/", "ctrl+u")
			typeText(&p, tc.typed)
			press(&p, "enter")
			if tc.want != "" {
				if !p.Accepted || p.Value() != tc.want {
					t.Errorf("Accepted=%v Value=%q, want %q (message %q)",
						p.Accepted, p.Value(), tc.want, p.Message())
				}
				return
			}
			if p.Done {
				t.Fatalf("refused path finished the dialog with %q", p.Value())
			}
			if !p.Typing() {
				t.Error("a refused path must keep the field open to fix it")
			}
			if !strings.Contains(p.Message(), tc.msg) {
				t.Errorf("Message = %q, want it to mention %q", p.Message(), tc.msg)
			}
			// The next key clears the message.
			typeText(&p, "x")
			if p.Message() != "" {
				t.Errorf("message survived the next key: %q", p.Message())
			}
		})
	}
}

func TestFilePickerUnreadableDirectory(t *testing.T) {
	tm := testTheme(t)
	// /etc lists secret/ first, and it cannot be read.
	p := newTestPicker(FilePickerOptions{Start: "/etc"})
	press(&p, "enter")
	if p.Dir != "/etc/secret" {
		t.Fatalf("Dir = %q, want /etc/secret", p.Dir)
	}
	if p.Done {
		t.Fatal("an unreadable directory must not end the dialog")
	}
	if p.ReadError() == nil {
		t.Fatal("ReadError should say why the directory is empty")
	}
	out := p.View(tm, 80, 24)
	if !strings.Contains(out, "cannot read this directory: permission denied") {
		t.Errorf("the view must report the error inline:\n%s", out)
	}
	press(&p, "enter")
	if p.Done || !strings.Contains(p.Message(), "cannot be read") {
		t.Errorf("enter in an unreadable directory: Done=%v Message=%q", p.Done, p.Message())
	}
	press(&p, "backspace")
	if p.Dir != "/etc" || p.ReadError() != nil {
		t.Errorf("backspace out of it: Dir=%q err=%v", p.Dir, p.ReadError())
	}

	// A path inside it can still be typed: the listing failed, not the stat.
	p = newTestPicker(FilePickerOptions{Start: "/etc/secret"})
	if p.ReadError() == nil {
		t.Fatal("starting in an unreadable directory should report it too")
	}
	press(&p, "/")
	typeText(&p, "key.pem")
	press(&p, "enter")
	if p.Value() != "/etc/secret/key.pem" {
		t.Errorf("typed path in an unreadable directory: Value=%q", p.Value())
	}
}

func TestFilePickerViewFitsANarrowTerminal(t *testing.T) {
	tm := testTheme(t)
	p := newTestPicker(FilePickerOptions{
		Title: "Certificate for the TLS listener",
		Help:  "PEM, readable by the service account.",
		Start: "/etc/ssl/certs/ca.pem",
	})
	for _, typing := range []bool{false, true} {
		if typing {
			press(&p, "/")
			typeText(&p, "a-very-long-directory-name/and-a-long-file-name.pem")
		}
		out := p.View(tm, 40, 20)
		lines := strings.Split(out, "\n")
		if len(lines) > 20 {
			t.Errorf("typing=%v: %d lines, want at most 20", typing, len(lines))
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > 40 {
				t.Errorf("typing=%v: line %d is %d cells wide: %q", typing, i, w, line)
			}
		}
		for _, want := range []string{"Certificate for", "esc"} {
			if !strings.Contains(out, want) {
				t.Errorf("typing=%v: view lacks %q:\n%s", typing, want, out)
			}
		}
		if !typing && !strings.Contains(out, "ca.pem") {
			t.Errorf("the highlighted file must be on screen:\n%s", out)
		}
	}
}

func TestFilePickerViewScrollsALongDirectory(t *testing.T) {
	tm := testTheme(t)
	tree := fstest.MapFS{}
	for i := range 40 {
		tree[filepath.Join("logs", strings.Repeat("x", 3)+string(rune('a'+i%26))+
			strings.Repeat("y", i/26)+".log")] = &fstest.MapFile{Data: []byte("x")}
	}
	p := NewFilePicker(FilePickerOptions{Start: "/logs", FS: FileSystemFromFS(tree)})
	press(&p, "end")
	out := p.View(tm, 80, 18)
	if n := len(strings.Split(out, "\n")); n > 18 {
		t.Errorf("view is %d lines, want at most 18", n)
	}
	if !strings.Contains(out, "of 40") {
		t.Errorf("a long listing needs a position marker:\n%s", out)
	}
	if !strings.Contains(out, "> "+filepath.Base(p.Highlighted())) {
		t.Errorf("the cursor row must stay visible after end:\n%s", out)
	}
}

func TestFilePickerLongDirectoryKeepsItsTail(t *testing.T) {
	if got := truncateLeft("/srv/projects/customer/releases", 16); got != "…stomer/releases" {
		t.Errorf("truncateLeft = %q", got)
	}
	if got := truncateLeft("/etc", 16); got != "/etc" {
		t.Errorf("truncateLeft on a short path = %q", got)
	}
}

func TestFileSystemFromFSMapsAbsolutePaths(t *testing.T) {
	fsys := FileSystemFromFS(fakeTree())
	for _, name := range []string{"/", "/etc", "/etc/ssl/"} {
		if _, err := fsys.ReadDir(name); err != nil {
			t.Errorf("ReadDir(%q): %v", name, err)
		}
	}
	info, err := fsys.Stat("/etc/ssl/certs/ca.pem")
	if err != nil || info.IsDir() {
		t.Errorf("Stat of a file: %v, %v", info, err)
	}
}
