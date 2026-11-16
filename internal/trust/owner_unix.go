//go:build unix

package trust

import (
	"io/fs"
	"syscall"
)

// owner reads the numeric owner and group of a file.
func owner(info fs.FileInfo) (uid, gid uint32, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Uid, st.Gid, true
}
