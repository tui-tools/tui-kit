package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procRoot is where process status is read from; tests point it at a fixture.
var procRoot = "/proc"

// stopTimeout bounds the `sudo -n kill` that stops a detached command. It
// either acts or is refused at once; it never asks for anything.
const stopTimeout = 5 * time.Second

// stopDetached asks an escalated mutation started through setsid(1) to stop.
//
// Signalling sudo is not enough there. Under sudo's pty (use_pty) the command
// leads a process group, so setsid(1) has to fork to start the new session,
// and it relays no signal: sudo's SIGTERM ends setsid(1) and sudo, the command
// keeps running detached, and the first line it writes once the runner has
// stopped reading kills it with SIGPIPE, halfway through whatever it was
// doing. That is exactly the end a package manager must not meet.
//
// So the SIGTERM goes to the command's own process group, which is its session
// (setsid makes it the leader of both), through the same escalation prefix:
// the ticket that let the command start lets the kill through. When the
// command cannot be found or the kill is refused, sudo gets the SIGTERM, as
// for any other mutation.
func (r *Runner) stopDetached(sudo *os.Process) error {
	if leader, ok := detachedCommand(procRoot, sudo.Pid); ok {
		killBin := "kill"
		if path, err := exec.LookPath("kill"); err == nil {
			killBin = path
		}
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		args := append(append([]string{}, r.Privilege[1:]...),
			killBin, "-TERM", "--", "-"+strconv.Itoa(leader))
		kill := exec.CommandContext(ctx, r.Privilege[0], args...) //nolint:gosec // argv is built here, never from a shell string
		// In the TUI's session, like the command it stops, so sudo finds the
		// same ticket.
		kill.Env = append(os.Environ(), r.env...)
		if err := kill.Run(); err == nil {
			return nil
		}
	}
	return sudo.Signal(syscall.SIGTERM)
}

// procStat is the part of /proc/<pid>/stat stopDetached needs.
type procStat struct {
	pid, ppid, sid int
	comm           string
}

// detachedCommand finds the command an escalated `sudo ... setsid -w` started:
// the nearest descendant of sudo that leads its own session and is not sudo
// itself. sudo's pty monitor also leads a session, and it is a fork of sudo,
// so it carries sudo's name and is passed over. Without a pty, setsid(1) does
// not fork and the command is sudo's direct child; it is found the same way.
func detachedCommand(root string, sudoPid int) (int, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, false
	}
	children := map[int][]procStat{}
	var sudo procStat
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		st, ok := readStat(root, pid)
		if !ok {
			continue
		}
		if pid == sudoPid {
			sudo = st
		}
		children[st.ppid] = append(children[st.ppid], st)
	}
	if sudo.pid == 0 {
		return 0, false
	}
	queue := children[sudoPid]
	for len(queue) > 0 {
		st := queue[0]
		queue = queue[1:]
		if st.sid == st.pid && st.comm != sudo.comm {
			return st.pid, true
		}
		queue = append(queue, children[st.pid]...)
	}
	return 0, false
}

// readStat parses /proc/<pid>/stat. The name is in parentheses and may itself
// contain spaces and parentheses, so the fields are counted from the last ')'.
func readStat(root string, pid int) (procStat, bool) {
	data, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat")) //nolint:gosec // a numeric pid under /proc, never user input
	if err != nil {
		return procStat{}, false
	}
	text := string(data)
	open, end := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
	if open < 0 || end < open {
		return procStat{}, false
	}
	// After the name: state ppid pgrp session ...
	fields := strings.Fields(text[end+1:])
	if len(fields) < 4 {
		return procStat{}, false
	}
	ppid, err1 := strconv.Atoi(fields[1])
	sid, err2 := strconv.Atoi(fields[3])
	if err1 != nil || err2 != nil {
		return procStat{}, false
	}
	return procStat{pid: pid, ppid: ppid, sid: sid, comm: text[open+1 : end]}, true
}
