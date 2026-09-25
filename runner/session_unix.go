//go:build unix

package runner

import "syscall"

// detachedSession starts the child as the leader of a new session, which
// leaves it without a controlling terminal (see Runner.exec).
func detachedSession() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
