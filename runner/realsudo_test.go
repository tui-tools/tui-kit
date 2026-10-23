package runner

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRealSudo drives the real sudo (or sudo-rs) of the machine it runs on. It
// only runs when TUI_KIT_REAL_SUDO=1, because it needs what CI does not have:
// an ordinary user whose sudo asks for a password, a terminal, and a ticket
// made in that terminal just before. In a throwaway container or VM:
//
//	go test -c -o runner.test ./runner
//	sudo -v && TUI_KIT_REAL_SUDO=1 ./runner.test -test.run RealSudo -test.v
//
// and, for the message, once more after `sudo -k`, with TUI_KIT_REAL_SUDO=nopass.
func TestRealSudo(t *testing.T) {
	mode := os.Getenv("TUI_KIT_REAL_SUDO")
	if mode == "" {
		t.Skip("set TUI_KIT_REAL_SUDO=1 after `sudo -v` in a terminal (see the comment)")
	}
	if os.Geteuid() == 0 {
		t.Fatal("run it as an ordinary user: root never escalates")
	}
	sh, err := New(Options{Bin: "sh", SudoPrefix: []string{"sudo", "-n"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(sh.Detach) == 0 {
		t.Fatal("no setsid(1) found: this is the fallback path, not the one under test")
	}
	run := func(ctx context.Context, script string) (string, error) {
		return sh.Run(ctx, Command{Argv: []string{"sh", "-c", script}})
	}

	if mode == "nopass" {
		_, err := run(context.Background(), "id -u")
		if err == nil || !strings.Contains(err.Error(), "run `sudo -v` in this terminal") {
			t.Fatalf("without a ticket: %v", err)
		}
		t.Logf("without a ticket: %v", err)
		return
	}

	t.Run("the ticket from sudo -v is used", func(t *testing.T) {
		out, err := run(context.Background(), "id -u")
		if err != nil || out != "0" {
			t.Fatalf("id -u = %q, %v", out, err)
		}
		t.Logf("preview: %s", sh.Preview(Command{Argv: []string{"sh", "-c", "id -u"}}))
	})

	t.Run("the command has no terminal", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		start := time.Now()
		out, err := run(ctx, "read -r answer </dev/tty && echo answered")
		if err == nil || ctx.Err() != nil {
			t.Fatalf("a command reading /dev/tty: %q, %v after %s", out, err, time.Since(start))
		}
		t.Logf("failed at once (%s): %v", time.Since(start).Round(time.Millisecond), err)
	})

	t.Run("cancel reaches the command", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		start := time.Now()
		out, err := run(ctx, `trap 'echo cleaned up; exit 3' TERM
i=0; while [ $i -lt 30 ]; do echo "step $i"; i=$((i+1)); sleep 1; done`)
		if err == nil || !strings.Contains(out, "cleaned up") {
			t.Fatalf("cancelled command: %q, %v", out, err)
		}
		if took := time.Since(start); took > MutationGrace {
			t.Errorf("cancel took %s", took)
		}
		t.Logf("output: %q", out)
	})
}
