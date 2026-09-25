package ui

import (
	"testing"
	"time"
)

func TestElapsed(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{0, "0s"},
		{999 * time.Millisecond, "0s"},
		{7 * time.Second, "7s"},
		{65 * time.Second, "1m05s"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1h02m03s"},
	} {
		if got := Elapsed(tc.in); got != tc.want {
			t.Errorf("Elapsed(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRunningMessage(t *testing.T) {
	got := RunningMessage("Install tui-cert", 65*time.Second)
	if want := "Install tui-cert: running… 1m05s"; got != want {
		t.Errorf("RunningMessage = %q, want %q", got, want)
	}
}

func TestRunningTick(t *testing.T) {
	if RunningTick() == nil {
		t.Fatal("RunningTick returned no command")
	}
}
