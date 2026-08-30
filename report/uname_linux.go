//go:build linux

package report

import "syscall"

// uname returns the kernel release and the machine type, the two fields of the
// uname syscall a bug report needs — `uname -r` and `uname -m`, without
// starting a process. The other fields are not read: nodename is the hostname,
// which this package will not print.
//
// A syscall that fails leaves both empty, and the block says "unknown".
func uname() (release, machine string) {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return "", ""
	}
	return charsToString(u.Release[:]), charsToString(u.Machine[:])
}

// charsToString turns one of Utsname's fixed C char arrays into a Go string,
// stopping at the first NUL. It is generic because the element type of those
// arrays is signed on some Linux architectures and unsigned on others.
func charsToString[T int8 | byte](chars []T) string {
	buf := make([]byte, 0, len(chars))
	for _, c := range chars {
		if c == 0 {
			break
		}
		buf = append(buf, byte(c))
	}
	return string(buf)
}
