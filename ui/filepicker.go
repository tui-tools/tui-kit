package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tui-tools/tui-kit/theme"
)

// FileSystem is what a FilePicker reads to list directories and to check a
// typed path. Names are absolute, slash-separated paths.
//
// The real filesystem is OSFileSystem. Tests and --demo screenshots inject a
// fake tree instead, usually an fstest.MapFS through FileSystemFromFS, so a
// screenshot never shows the machine it was rendered on.
type FileSystem interface {
	ReadDir(name string) ([]fs.DirEntry, error)
	// Stat follows symbolic links, like os.Stat: a link to a directory is a
	// directory to the picker.
	Stat(name string) (fs.FileInfo, error)
}

// OSFileSystem reads the machine's own filesystem. Listing a directory is a
// read, like reading a keystroke; the family's exec-site rule is about
// mutations, and the picker performs none.
type OSFileSystem struct{}

// ReadDir lists a directory.
func (OSFileSystem) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }

// Stat describes a path, following symbolic links.
func (OSFileSystem) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }

// FileSystemFromFS adapts an fs.FS rooted at "/" to a FileSystem, so a
// fstest.MapFS of "etc/ssl/cert.pem" answers for "/etc/ssl/cert.pem".
func FileSystemFromFS(fsys fs.FS) FileSystem { return rootedFS{fsys: fsys} }

type rootedFS struct{ fsys fs.FS }

// rel turns an absolute path into the unrooted form io/fs expects.
func (r rootedFS) rel(name string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(name)), "/")
	if rel == "" {
		return "."
	}
	return rel
}

func (r rootedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(r.fsys, r.rel(name))
}

func (r rootedFS) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(r.fsys, r.rel(name))
}

// FilePickerOptions configures NewFilePicker. The zero value picks an
// existing file of any type, starting in the working directory.
type FilePickerOptions struct {
	Title string
	// Help is a line or two under the list: what the path is for, what the
	// tool will check about it.
	Help string
	// Start is where the picker opens: a directory, or a file, in which case
	// the picker opens in its directory with the file highlighted. That makes
	// the current value of a setting the natural start. A path that does not
	// exist opens its nearest existing parent. Empty is the working directory
	// on the real filesystem, and "/" on an injected one.
	Start string
	// Extensions limits the files listed and accepted, as ".pem" or "pem",
	// compared case-insensitively. Directories are always listed. Empty
	// means any file.
	Extensions []string
	// ShowHidden lists dot files from the start; "." toggles it either way.
	ShowHidden bool
	// DirsOnly picks a directory instead of a file.
	DirsOnly bool
	// NewFile accepts a typed path that does not exist yet, as long as its
	// directory does: an export target, a key to generate. The zero value
	// requires the path to exist.
	NewFile bool
	// InitialPath opens the picker with the path field focused and holding
	// this text, as typed: a suggested destination such as "~/ca.crt" that
	// the user only confirms with enter or edits first. esc goes back to the
	// listing as usual. When Start is empty, the listing opens where the
	// path points (its directory, or its nearest existing parent).
	InitialPath string
	// Home expands a typed "~". Empty means the user's home directory on the
	// real filesystem, and no expansion on an injected one.
	Home string
	// FS is the filesystem to read. Nil means OSFileSystem.
	FS FileSystem
}

// fileEntry is one row of the listing.
type fileEntry struct {
	name string
	dir  bool
	// self is the "./" row a directory picker starts with, which selects the
	// directory being listed.
	self bool
}

// FilePicker is a dialog that picks a path: a directory listing with a path
// field on top that doubles as the place to type or paste one.
//
// Like the other dialogs it is a value the host model stores and forwards key
// messages to. Once Done is true, Accepted tells a choice from a cancel and
// Value returns the chosen absolute, cleaned path. The picker checks only
// what it needs to list and select; the tool keeps validating the result
// (readable by the service account, the right kind of file) as it did before.
type FilePicker struct {
	Title string
	Help  string
	// Dir is the directory being listed: absolute and clean.
	Dir string
	// Cursor indexes the listed entries.
	Cursor int
	// ShowHidden lists the entries whose name starts with a dot.
	ShowHidden bool
	// Done reports that the dialog finished; Accepted tells select from cancel.
	Done     bool
	Accepted bool
	// Payload carries whatever the host needs to act on the answer.
	Payload any

	extensions []string
	dirsOnly   bool
	newFile    bool
	home       string
	fsys       FileSystem

	entries []fileEntry
	// readErr is why Dir could not be listed. It is shown in place of the
	// listing rather than closing the dialog.
	readErr error
	// message is feedback on the last key: a typed path that does not exist,
	// a file of the wrong type. The next key clears it.
	message string

	path   textinput.Model
	typing bool
	chosen string

	offset     int
	maxVisible int
}

// NewFilePicker builds a picker from its options and lists its start
// directory.
func NewFilePicker(opts FilePickerOptions) FilePicker {
	p := FilePicker{
		Title:      opts.Title,
		Help:       opts.Help,
		ShowHidden: opts.ShowHidden,
		dirsOnly:   opts.DirsOnly,
		newFile:    opts.NewFile,
		home:       opts.Home,
		fsys:       opts.FS,
		maxVisible: 12,
	}
	onDisk := p.fsys == nil
	if onDisk {
		p.fsys = OSFileSystem{}
		if p.home == "" {
			p.home, _ = os.UserHomeDir()
		}
	}
	for _, ext := range opts.Extensions {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		p.extensions = append(p.extensions, ext)
	}

	ti := textinput.New()
	ti.Prompt = "path: "
	ti.CharLimit = 4096
	p.path = ti

	initial := strings.TrimSpace(opts.InitialPath)
	start := strings.TrimSpace(opts.Start)
	if start == "" {
		start = initial
	}
	if start == "" && onDisk {
		start, _ = os.Getwd()
	}
	if start == "" {
		start = "/"
	}
	start = p.resolve(start, onDisk)
	dir, focus := p.startDir(start)
	p.navigate(dir, focus)
	if initial != "" {
		// The blink command Focus returns is dropped: the field is focused
		// and shows its cursor without it, as NewInput's does.
		_ = p.startTyping(initial)
	}
	return p
}

// resolve expands "~" and makes a path absolute and clean. A relative path is
// taken from the directory being listed, or from the working directory while
// there is none yet.
func (p FilePicker) resolve(path string, onDisk bool) string {
	if p.home != "" && (path == "~" || strings.HasPrefix(path, "~/")) {
		path = filepath.Join(p.home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		base := p.Dir
		if base == "" && onDisk {
			base, _ = os.Getwd()
		}
		if base == "" {
			base = "/"
		}
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}

// startDir finds the directory to open for a start path, and the entry to
// highlight in it.
func (p FilePicker) startDir(start string) (dir, focus string) {
	if info, err := p.fsys.Stat(start); err == nil {
		if info.IsDir() {
			return start, ""
		}
		return filepath.Dir(start), filepath.Base(start)
	}
	// Walk up to the nearest parent that is there: a setting whose file was
	// deleted still opens somewhere useful.
	for dir = filepath.Dir(start); dir != "/"; dir = filepath.Dir(dir) {
		if info, err := p.fsys.Stat(dir); err == nil && info.IsDir() {
			return dir, ""
		}
	}
	return "/", ""
}

// navigate lists dir and puts the cursor on focus when it is listed there.
func (p *FilePicker) navigate(dir, focus string) {
	p.Dir = dir
	p.Cursor, p.offset = 0, 0
	p.load()
	for i, e := range p.entries {
		if e.name == focus && !e.self {
			p.Cursor = i
			break
		}
	}
}

// load reads Dir into entries: directories first, then files, each sorted by
// name, with the hidden, the filtered-out and (for a directory picker) the
// files left out.
func (p *FilePicker) load() {
	p.entries, p.readErr = nil, nil
	if p.dirsOnly {
		p.entries = append(p.entries, fileEntry{name: ".", dir: true, self: true})
	}

	list, err := p.fsys.ReadDir(p.Dir)
	if err != nil {
		p.readErr = err
		p.clampCursor()
		return
	}

	var dirs, files []fileEntry
	for _, e := range list {
		name := e.Name()
		if !p.ShowHidden && strings.HasPrefix(name, ".") {
			continue
		}
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			// A link is what it points at; a dangling one is listed as a file
			// and fails the tool's own validation, like any other bad path.
			if info, err := p.fsys.Stat(filepath.Join(p.Dir, name)); err == nil {
				isDir = info.IsDir()
			}
		}
		switch {
		case isDir:
			dirs = append(dirs, fileEntry{name: name, dir: true})
		case !p.dirsOnly && p.extensionOK(name):
			files = append(files, fileEntry{name: name})
		}
	}
	byName := func(list []fileEntry) {
		sort.SliceStable(list, func(i, j int) bool {
			a, b := strings.ToLower(list[i].name), strings.ToLower(list[j].name)
			if a == b {
				return list[i].name < list[j].name
			}
			return a < b
		})
	}
	byName(dirs)
	byName(files)
	p.entries = append(append(p.entries, dirs...), files...)
	p.clampCursor()
}

// extensionOK reports whether a file name passes the extension filter.
func (p FilePicker) extensionOK(name string) bool {
	if len(p.extensions) == 0 {
		return true
	}
	lower := strings.ToLower(name)
	for _, ext := range p.extensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func (p *FilePicker) clampCursor() {
	p.Cursor = min(max(p.Cursor, 0), max(len(p.entries)-1, 0))
}

// Value returns the chosen path, absolute and clean, once the dialog was
// accepted; "" otherwise.
func (p FilePicker) Value() string { return p.chosen }

// Highlighted returns the absolute path of the entry under the cursor, or ""
// when nothing is listed.
func (p FilePicker) Highlighted() string {
	if p.Cursor < 0 || p.Cursor >= len(p.entries) {
		return ""
	}
	e := p.entries[p.Cursor]
	if e.self {
		return p.Dir
	}
	return filepath.Join(p.Dir, e.name)
}

// Typing reports whether keys go to the path field rather than the listing.
func (p FilePicker) Typing() bool { return p.typing }

// Message returns the inline feedback on the last key, if any.
func (p FilePicker) Message() string { return p.message }

// ReadError returns why the listed directory could not be read, if it could
// not.
func (p FilePicker) ReadError() error { return p.readErr }

// Update handles a message. It returns a command to run (the path field's
// cursor blink) and whether the message was consumed.
func (p *FilePicker) Update(msg tea.Msg) (tea.Cmd, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		if !p.typing {
			return nil, false
		}
		var cmd tea.Cmd
		p.path, cmd = p.path.Update(msg)
		return cmd, true
	}

	p.message = ""
	if key.String() == "ctrl+c" {
		p.finish("", false)
		return nil, true
	}
	if p.typing {
		return p.updateTyping(key), true
	}
	return p.updateList(key), true
}

// updateList handles a key while the listing has the focus.
func (p *FilePicker) updateList(key tea.KeyMsg) tea.Cmd {
	// A paste lands in the path field, whatever the focus was: pasting a
	// path is the fastest way to one.
	if key.Paste && key.Type == tea.KeyRunes {
		return p.startTyping(strings.TrimSpace(string(key.Runes)))
	}

	last := max(len(p.entries)-1, 0)
	switch key.String() {
	case "up", "k":
		p.Cursor = max(p.Cursor-1, 0)
	case "down", "j":
		p.Cursor = min(p.Cursor+1, last)
	case "pgup":
		p.Cursor = max(p.Cursor-p.maxVisible, 0)
	case "pgdown":
		p.Cursor = min(p.Cursor+p.maxVisible, last)
	case "home", "g":
		p.Cursor = 0
	case "end", "G":
		p.Cursor = last
	case "enter":
		p.selectHighlighted()
	case "right", "l":
		p.openHighlighted()
	case "backspace", "h", "left":
		p.up()
	case ".":
		focus := ""
		if p.Cursor < len(p.entries) {
			focus = p.entries[p.Cursor].name
		}
		p.ShowHidden = !p.ShowHidden
		p.navigate(p.Dir, focus)
	case "/", "tab":
		return p.startTyping(p.dirPrefix())
	case "~":
		if p.home != "" {
			return p.startTyping("~/")
		}
	case "esc":
		p.finish("", false)
	}
	return nil
}

// selectHighlighted is enter on the listing: it opens a directory in a file
// picker and chooses everything else.
func (p *FilePicker) selectHighlighted() {
	if p.Cursor >= len(p.entries) {
		if p.readErr != nil {
			p.message = "this directory cannot be read; backspace goes up"
		}
		return
	}
	e := p.entries[p.Cursor]
	switch {
	case e.self:
		p.finish(p.Dir, true)
	case e.dir && p.dirsOnly:
		p.finish(filepath.Join(p.Dir, e.name), true)
	case e.dir:
		p.navigate(filepath.Join(p.Dir, e.name), "")
	default:
		p.finish(filepath.Join(p.Dir, e.name), true)
	}
}

// openHighlighted enters the directory under the cursor.
func (p *FilePicker) openHighlighted() {
	if p.Cursor >= len(p.entries) {
		return
	}
	if e := p.entries[p.Cursor]; e.dir && !e.self {
		p.navigate(filepath.Join(p.Dir, e.name), "")
	}
}

// up lists the parent directory with the cursor on the one just left.
func (p *FilePicker) up() {
	if p.Dir == "/" {
		return
	}
	p.navigate(filepath.Dir(p.Dir), filepath.Base(p.Dir))
}

// dirPrefix is what the path field starts from: the listed directory, ready
// for a name to be typed after it.
func (p FilePicker) dirPrefix() string {
	if p.Dir == "/" {
		return "/"
	}
	return p.Dir + "/"
}

func (p *FilePicker) startTyping(value string) tea.Cmd {
	p.typing = true
	p.path.SetValue(value)
	p.path.CursorEnd()
	return p.path.Focus()
}

func (p *FilePicker) stopTyping() {
	p.typing = false
	p.path.Blur()
}

// updateTyping handles a key while the path field has the focus.
func (p *FilePicker) updateTyping(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc", "tab":
		p.stopTyping()
		return nil
	case "enter":
		p.submitTyped()
		return nil
	}
	var cmd tea.Cmd
	p.path, cmd = p.path.Update(key)
	return cmd
}

// submitTyped acts on the path in the field: a directory is opened (or, in a
// directory picker, chosen), a file is chosen, and anything the options rule
// out is reported inline with the field kept as typed.
func (p *FilePicker) submitTyped() {
	raw := strings.TrimSpace(p.path.Value())
	if raw == "" {
		p.message = "type a path, or esc to go back to the list"
		return
	}
	target := p.resolve(raw, false)

	info, err := p.fsys.Stat(target)
	switch {
	case err == nil && info.IsDir():
		if p.dirsOnly {
			p.finish(target, true)
			return
		}
		p.stopTyping()
		p.navigate(target, "")
	case err == nil:
		if p.dirsOnly {
			p.message = "not a directory: " + target
			return
		}
		if !p.extensionOK(target) {
			p.message = p.extensionMessage()
			return
		}
		p.finish(target, true)
	case errors.Is(err, fs.ErrNotExist):
		p.submitNew(target)
	default:
		p.message = fmt.Sprintf("cannot read %s: %s", target, describeErr(err))
	}
}

// submitNew handles a typed path that does not exist.
func (p *FilePicker) submitNew(target string) {
	if !p.newFile {
		p.message = "no such file or directory: " + target
		return
	}
	parent := filepath.Dir(target)
	if info, err := p.fsys.Stat(parent); err != nil || !info.IsDir() {
		p.message = "the directory does not exist: " + parent
		return
	}
	if !p.dirsOnly && !p.extensionOK(target) {
		p.message = p.extensionMessage()
		return
	}
	p.finish(target, true)
}

func (p FilePicker) extensionMessage() string {
	return "expects a " + strings.Join(p.extensions, ", ") + " file"
}

func (p *FilePicker) finish(path string, accepted bool) {
	p.stopTyping()
	p.chosen = ""
	if accepted {
		p.chosen = path
	}
	p.Done, p.Accepted = true, accepted
}

// describeErr drops the path an *fs.PathError repeats, since the dialog
// already shows it: "permission denied" rather than the whole sentence.
func describeErr(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

// hints is the footer for the current focus.
func (p FilePicker) hints() []KeyHint {
	if p.typing {
		return []KeyHint{
			{Key: "enter", Desc: "use path"},
			{Key: "tab", Desc: "list"},
			{Key: "esc", Desc: "back"},
		}
	}
	hints := []KeyHint{{Key: "↑/↓", Desc: "move"}}
	if p.dirsOnly {
		hints = append(hints,
			KeyHint{Key: "enter", Desc: "select"},
			KeyHint{Key: "→/l", Desc: "open"})
	} else {
		hints = append(hints, KeyHint{Key: "enter", Desc: "open/select"})
	}
	return append(hints,
		KeyHint{Key: "⌫/h", Desc: "up"},
		KeyHint{Key: ".", Desc: "hidden"},
		KeyHint{Key: "/", Desc: "type path"},
		KeyHint{Key: "esc", Desc: "cancel"},
	)
}

// truncateLeft keeps the end of a path, which is the part that tells two
// deep directories apart, and marks what was cut with an ellipsis.
func truncateLeft(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return Truncate(s, width)
	}
	runes := []rune(s)
	for i := range runes {
		if tail := string(runes[i:]); lipgloss.Width(tail) <= width-1 {
			return "…" + tail
		}
	}
	return "…"
}

// View renders the picker centered in the given area.
//
// The dialog keeps one width while the user moves between directories, like
// Input, so the frame does not jump with the longest name on screen. The
// listing takes whatever height the terminal leaves between the pinned
// title and path field and the pinned footer.
func (p *FilePicker) View(t theme.Theme, width, height int) string {
	inner := min(max(width-8, 24), 76)
	content := max(inner-t.Dialog.GetHorizontalFrameSize(), dialogMinContent)

	head := renderLines(Wrap(p.Title, content), t.Title)
	if p.typing {
		// The prompt is what tells the field from the listed directory it
		// replaces; the cell after the text is the cursor's.
		p.path.PromptStyle = t.Muted
		p.path.Width = max(content-lipgloss.Width(p.path.Prompt)-1, 1)
		head = append(head, p.path.View())
	} else {
		head = append(head, t.Accent.Render(truncateLeft(p.Dir, content)))
	}
	head = append(head, "")

	var tail []string
	if p.message != "" {
		tail = append(tail, "")
		tail = append(tail, renderLines(Wrap(p.message, content), t.Warn)...)
	}
	if p.Help != "" {
		tail = append(tail, "")
		tail = append(tail, renderLines(WrapBody(p.Help, content), t.Muted)...)
	}
	tail = append(tail, "")
	tail = append(tail, hintLines(t, content, p.hints())...)

	body := p.listing(t, content, height-t.Dialog.GetVerticalFrameSize()-len(head)-len(tail))

	all := append(append(head, body...), tail...)
	return place(t.Dialog.Width(inner).Render(strings.Join(all, "\n")), width, height)
}

// listing renders the entries that fit in room lines, the read error when
// the directory could not be listed, or a note when it lists nothing.
func (p *FilePicker) listing(t theme.Theme, content, room int) []string {
	var lines []string
	if p.readErr != nil {
		lines = append(lines, renderLines(Wrap(
			"cannot read this directory: "+describeErr(p.readErr), content), t.Danger)...)
	}
	if len(p.entries) == 0 {
		if p.readErr == nil {
			empty := "(empty directory)"
			if len(p.extensions) > 0 {
				empty = "(no " + strings.Join(p.extensions, ", ") + " file here)"
			}
			lines = append(lines, t.Muted.Render(Truncate(empty, content)))
		}
		return lines
	}

	// One line is kept for the position marker.
	window := min(len(p.entries), p.maxVisible)
	if room > 0 {
		window = min(window, max(room-len(lines)-1, 1))
	}
	p.clampCursor()
	if p.Cursor < p.offset {
		p.offset = p.Cursor
	}
	if p.Cursor >= p.offset+window {
		p.offset = p.Cursor - window + 1
	}
	p.offset = min(max(p.offset, 0), max(len(p.entries)-window, 0))
	end := min(p.offset+window, len(p.entries))

	labelWidth := max(content-2-t.Row.GetHorizontalFrameSize(), 1)
	for i := p.offset; i < end; i++ {
		e := p.entries[i]
		label := e.name
		switch {
		case e.self:
			label = "./ (this directory)"
		case e.dir:
			label += "/"
		}
		label = Pad(label, labelWidth)
		if i == p.Cursor {
			lines = append(lines, t.SelRow.Render("> "+label))
			continue
		}
		lines = append(lines, t.Row.Render("  "+label))
	}
	if len(p.entries) > window {
		lines = append(lines, t.Muted.Render(Truncate(
			fmt.Sprintf("%d-%d of %d", p.offset+1, end, len(p.entries)), content)))
	}
	return lines
}
