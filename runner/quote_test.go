package runner

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// quoteCases are the argvs the family has really previewed wrong (#31), and
// the edges of the quoting rule.
var quoteCases = []struct {
	name string
	argv []string
	want string
}{
	{name: "plain words stay bare",
		argv: []string{"ufw", "--force", "delete", "1"},
		want: "ufw --force delete 1"},
	{name: "safe punctuation stays bare",
		argv: []string{"curl", "--proto", "=https", "-o", "/etc/apt/keyrings/a.asc",
			"https://pkgs.tui.tools/pubkey.asc", "user@host:22", "50%", "a,b", "x_y+z"},
		want: "curl --proto =https -o /etc/apt/keyrings/a.asc " +
			"https://pkgs.tui.tools/pubkey.asc user@host:22 50% a,b x_y+z"},
	{name: "a comment with spaces and parentheses is one argument",
		argv: []string{"ufw", "allow", "19443/tcp", "comment",
			"headscale control (tailnet)"},
		want: "ufw allow 19443/tcp comment 'headscale control (tailnet)'"},
	{name: "an sh -c script keeps its && and redirect inside the quotes",
		argv: []string{"sh", "-c",
			"cp -p /etc/headscale/config.yaml /etc/headscale/config.yaml.bak " +
				"&& cat > /etc/headscale/config.yaml"},
		want: "sh -c 'cp -p /etc/headscale/config.yaml " +
			"/etc/headscale/config.yaml.bak && cat > /etc/headscale/config.yaml'"},
	{name: "empty arguments are kept",
		argv: []string{"usermod", "-c", "", "alice", ""},
		want: "usermod -c '' alice ''"},
	{name: "a single quote is closed, escaped and reopened",
		argv: []string{"echo", "it's"},
		want: `echo 'it'"'"'s'`},
	{name: "shell metacharacters are quoted",
		argv: []string{"printf", "$HOME", "*", "a;b", "a|b", "~", "#x", "`id`", `a\b`,
			"a\"b", "{x}", "!", "a\tb"},
		want: `printf '$HOME' '*' 'a;b' 'a|b' '~' '#x' '` + "`id`" + `' 'a\b' 'a"b' ` +
			`'{x}' '!' '` + "a\tb'"},
	{name: "a newline stays inside the quotes",
		argv: []string{"dpkg-query", "-W", "-f=${Package}\n"},
		want: "dpkg-query -W '-f=${Package}\n'"},
	{name: "non-ASCII is quoted",
		argv: []string{"echo", "café"},
		want: "echo 'café'"},
}

func TestJoin(t *testing.T) {
	for _, tc := range quoteCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Join(tc.argv); got != tc.want {
				t.Errorf("Join(%q)\n got %s\nwant %s", tc.argv, got, tc.want)
			}
			if got := (Command{Argv: tc.argv}).String(); got != tc.want {
				t.Errorf("Command.String() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestJoinRoundTripsThroughAShell is the promise itself: the preview, pasted
// into a POSIX shell, is the same argv. The shell parses the joined line and
// prints each argument it got, NUL-terminated.
func TestJoinRoundTripsThroughAShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this machine")
	}
	for _, tc := range quoteCases {
		t.Run(tc.name, func(t *testing.T) {
			script := "set -- " + Join(tc.argv) + `; for a do printf '%s\0' "$a"; done`
			out, err := exec.Command(sh, "-c", script).Output() //nolint:gosec // the test's own script
			if err != nil {
				t.Fatalf("sh: %v", err)
			}
			got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
			if !reflect.DeepEqual(got, tc.argv) {
				t.Errorf("the shell read %q, want %q", got, tc.argv)
			}
		})
	}
}

// TestPreviewQuotesArguments covers the whole line a user reads:
// the escalation prefix and the command.
func TestPreviewQuotesArguments(t *testing.T) {
	r := &Runner{Name: "ufw", Privilege: []string{"/usr/bin/sudo", "-n"}}
	cmd := Command{Argv: []string{"ufw", "allow", "19443/tcp", "comment",
		"headscale control (tailnet)"}}
	want := "/usr/bin/sudo -n ufw allow 19443/tcp comment 'headscale control (tailnet)'"
	if got := r.Preview(cmd); got != want {
		t.Errorf("Preview = %s, want %s", got, want)
	}
}
