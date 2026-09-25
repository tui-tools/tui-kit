package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A mutation has no wall-clock timeout (see runner.DefaultTimeout), so an
// install that downloads for minutes is expected. What a tool owes the user in
// the meantime is proof that it has not frozen: the status line says what is
// running and for how long, refreshed once a second. The success or failure
// message that replaces it when the command returns is the tool's usual one.
//
// The pattern, in a tool's Update:
//
//	case startMsg:
//	    a.running, a.since = "Install tui-cert", time.Now()
//	    return a, tea.Batch(runIt, ui.RunningTick())
//	case ui.RunningTickMsg:
//	    if a.running == "" {
//	        return a, nil // the command returned; let the tick die
//	    }
//	    return a, ui.RunningTick()
//
// and in View, while a.running is set:
//
//	ui.StatusLine(t, ui.StatusInfo, ui.RunningMessage(a.running, time.Since(a.since)), "", width)

// RunningTickMsg is delivered once a second by RunningTick, so the view can
// redraw the elapsed time.
type RunningTickMsg time.Time

// RunningTick schedules the next RunningTickMsg, one second from now.
func RunningTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return RunningTickMsg(t)
	})
}

// Elapsed renders a duration the way a status line counts it: whole seconds,
// "7s", "1m05s", "1h02m03s".
func Elapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d / time.Second)
	h, m, sec := s/3600, s/60%60, s%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm%02ds", h, m, sec)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, sec)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

// RunningMessage is the status line while a command runs:
// "Install tui-cert: running… 1m05s".
func RunningMessage(what string, elapsed time.Duration) string {
	return what + ": running" + Ellipsis + " " + Elapsed(elapsed)
}
