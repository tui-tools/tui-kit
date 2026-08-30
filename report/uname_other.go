//go:build !linux

package report

// uname has nothing to read off Linux. The family targets Linux; this file
// exists so the package still builds when someone runs `go vet` from a Mac.
func uname() (release, machine string) { return "", "" }
