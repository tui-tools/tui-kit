//go:build !unix

package runner

import "syscall"

// detachedSession has nothing to detach from on a platform without POSIX
// sessions; the family only targets Linux, this keeps the kit compiling.
func detachedSession() *syscall.SysProcAttr { return nil }
