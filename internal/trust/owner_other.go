//go:build !unix

package trust

import "io/fs"

// owner has no POSIX ownership to read on this platform. The family only
// targets Linux; this keeps the kit compiling, and every check fails closed.
func owner(fs.FileInfo) (uid, gid uint32, ok bool) { return 0, 0, false }
