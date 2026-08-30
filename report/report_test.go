package report

import (
	"os"
	"strings"
	"testing"

	"github.com/tui-tools/tui-kit/pkgmgr"
)

// TestRenderShape pins the block's shape: a headline, then one `key: value`
// per line, in a fixed order. The order is the contract — a maintainer reading
// a hundred pasted reports reads them by position.
func TestRenderShape(t *testing.T) {
	setEnv(t, map[string]string{
		"LANG": "en_US.UTF-8", "LC_ALL": "", "TERM": "xterm-256color",
		"TERM_PROGRAM": "ghostty",
	})

	out := Render(Info{
		Tool: "tui-firewall", Version: "0.2.2",
		Backend: "ufw", BackendVersion: "0.36.2",
		Sudo: "sudo -n", Theme: "tokyo-night",
		Extra: []Field{{"selected zone", "public"}},
	})

	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if !strings.HasSuffix(out, "\n") {
		t.Error("the block must end with a newline")
	}
	if got := lines[0]; !strings.HasPrefix(got, "tui-firewall 0.2.2 (kit ") {
		t.Errorf("headline = %q, want it to start with the tool and version", got)
	}

	wantKeys := []string{"backend", "mode", "distro", "kernel", "arch",
		"locale", "term", "theme", "sudo", "root", "binary", "selected zone"}
	if len(lines[1:]) != len(wantKeys) {
		t.Fatalf("got %d body lines, want %d:\n%s", len(lines[1:]), len(wantKeys), out)
	}
	for i, want := range wantKeys {
		key, value, ok := strings.Cut(lines[i+1], ": ")
		if !ok {
			t.Fatalf("line %q is not a key: value pair", lines[i+1])
		}
		if key != want {
			t.Errorf("key %d = %q, want %q", i, key, want)
		}
		if value == "" {
			t.Errorf("key %q has an empty value; every fact must render", key)
		}
	}
}

// TestRenderValues checks the lines the caller controls, including the ones
// that are assembled from more than one fact.
func TestRenderValues(t *testing.T) {
	tests := []struct {
		name string
		info Info
		env  map[string]string
		want []string
		deny []string
	}{
		{
			name: "backend with a probed version",
			info: Info{Backend: "ufw", BackendVersion: "0.36.2"},
			want: []string{"backend: ufw 0.36.2", "mode: live"},
		},
		{
			name: "backend whose version could not be read",
			info: Info{Backend: "firewalld", BackendDetail: "firewall-cmd not found"},
			want: []string{"backend: firewalld (version unknown: firewall-cmd not found)"},
		},
		{
			name: "backend with neither a version nor a reason",
			info: Info{Backend: "ufw"},
			want: []string{"backend: ufw (version unknown)"},
		},
		{
			name: "no backend at all",
			info: Info{},
			want: []string{"backend: unknown"},
		},
		{
			name: "demo says so on its own line",
			info: Info{Backend: "demo", Demo: true},
			want: []string{"backend: demo", "mode: demo (sample data, the system was not read)"},
		},
		{
			name: "escalation disabled",
			info: Info{Sudo: ""},
			want: []string{"sudo: (none)"},
		},
		{
			name: "locale falls back to LC_ALL alone",
			info: Info{},
			env:  map[string]string{"LANG": "", "LC_ALL": "C"},
			want: []string{"locale: LC_ALL=C"},
		},
		{
			name: "both locale variables are shown",
			info: Info{},
			env:  map[string]string{"LANG": "pt_BR.UTF-8", "LC_ALL": "C"},
			want: []string{"locale: pt_BR.UTF-8 (LC_ALL=C)"},
		},
		{
			name: "no locale at all",
			info: Info{},
			env:  map[string]string{"LANG": "", "LC_ALL": ""},
			want: []string{"locale: unknown"},
		},
		{
			name: "terminal without an emulator name",
			info: Info{},
			env:  map[string]string{"TERM": "linux", "TERM_PROGRAM": ""},
			want: []string{"term: linux"},
		},
		{
			name: "an unnamed tool still renders",
			info: Info{},
			want: []string{"unknown unknown (kit "},
		},
		{
			name: "an empty extra key is dropped rather than printed bare",
			info: Info{Extra: []Field{{"", "value"}, {"zone", ""}}},
			want: []string{"zone: unknown"},
			deny: []string{": value"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"LANG": "en_US.UTF-8", "LC_ALL": "",
				"TERM": "xterm-256color", "TERM_PROGRAM": ""}
			for k, v := range tc.env {
				env[k] = v
			}
			setEnv(t, env)

			out := Render(tc.info)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("report is missing %q:\n%s", want, out)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(out, deny) {
					t.Errorf("report contains %q, which it must not:\n%s", deny, out)
				}
			}
		})
	}
}

// TestScrubPath is the privacy rule in table form: a path that would name the
// user never reaches the block.
func TestScrubPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"a packaged binary is printed", "/usr/bin/tui-firewall", "/usr/bin/tui-firewall"},
		{"so is one in /bin", "/bin/tui-firewall", "/bin/tui-firewall"},
		{"and a manual install outside home", "/usr/local/bin/tui-firewall", "/usr/local/bin/tui-firewall"},
		{"and an opt install", "/opt/tui/tui-firewall", "/opt/tui/tui-firewall"},
		{"a home path is redacted", "/home/alice/go/bin/tui-firewall", redactedPath},
		{"including the directory itself", "/home", redactedPath},
		{"root's home too", "/root/tui-firewall", redactedPath},
		{"and /root itself", "/root", redactedPath},
		{"a relative path says nothing", "./tui-firewall", redactedPath},
		{"a bare name says nothing either", "tui-firewall", redactedPath},
		{"a lookalike outside home is kept", "/homebrew/bin/tui-firewall", "/homebrew/bin/tui-firewall"},
		{"and one outside root", "/rootfs/bin/tui-firewall", "/rootfs/bin/tui-firewall"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scrubPath(tc.path); got != tc.want {
				t.Errorf("scrubPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestPackagedPath separates "a package manager put this here" from "someone
// built this", which is the distinction the binary line exists for.
func TestPackagedPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/usr/bin/tui-firewall", true},
		{"/bin/tui-firewall", true},
		{"/usr/local/bin/tui-firewall", false},
		{"/home/alice/go/bin/tui-firewall", false},
		{"/opt/tui/tui-firewall", false},
		{"/usr/bindings/tui-firewall", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := packagedPath(tc.path); got != tc.want {
				t.Errorf("packagedPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestOneLine covers the other half of the scrubbing: a caller's value can
// never add a line, and so can never invent a key.
func TestOneLine(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"a plain value is untouched", "tokyo-night", "tokyo-night"},
		{"surrounding space goes", "  sudo -n  ", "sudo -n"},
		{"a newline cannot open a new key", "ufw\nroot: yes", "ufw root: yes"},
		{"nor a carriage return", "ufw\r\nroot: yes", "ufw root: yes"},
		{"a tab collapses", "a\tb", "a b"},
		{"runs of space collapse", "a    b", "a b"},
		{"control characters are dropped", "ufw\x1b[31m", "ufw[31m"},
		{"an empty value stays empty", "   ", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := oneLine(tc.value); got != tc.want {
				t.Errorf("oneLine(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestRenderNeverLeaksTheEnvironment is the guard the package comment
// promises: an environment full of things a public issue must not carry, and a
// block that carries none of them.
func TestRenderNeverLeaksTheEnvironment(t *testing.T) {
	setEnv(t, map[string]string{
		"LANG": "en_US.UTF-8", "TERM": "xterm-256color",
		"HOSTNAME": "workstation", "USER": "alice", "HOME": "/home/alice",
		"AWS_SECRET_ACCESS_KEY": "s3cr3t", "SSH_CONNECTION": "10.0.0.2 22",
	})

	out := Render(Info{Tool: "tui-firewall", Version: "0.2.2", Backend: "ufw"})
	for _, forbidden := range []string{"alice", "workstation", "s3cr3t", "10.0.0.2", "/home/"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the report leaked %q:\n%s", forbidden, out)
		}
	}
}

// TestDistroLine renders os-release the way it comes off real machines,
// including the halves a distribution leaves out.
func TestDistroLine(t *testing.T) {
	tests := []struct {
		name string
		in   pkgmgr.Distro
		want string
	}{
		{
			name: "a released distribution",
			in:   pkgmgr.Distro{ID: "fedora", VersionID: "42", PrettyName: "Fedora Linux 42 (Workstation Edition)"},
			want: "fedora 42 (Fedora Linux 42 (Workstation Edition))",
		},
		{
			name: "a rolling one has no version",
			in:   pkgmgr.Distro{ID: "arch", PrettyName: "Arch Linux"},
			want: "arch (Arch Linux)",
		},
		{
			name: "no pretty name",
			in:   pkgmgr.Distro{ID: "omarchy-server", VersionID: "1"},
			want: "omarchy-server 1",
		},
		{
			name: "nothing but a pretty name",
			in:   pkgmgr.Distro{PrettyName: "Something Linux"},
			want: "Something Linux",
		},
		{
			name: "an unreadable os-release",
			in:   pkgmgr.Distro{},
			want: unknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := distroLine(tc.in); got != tc.want {
				t.Errorf("distroLine(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestKitVersion checks only what can be true in a test binary: the module is
// the kit itself, built without a version, so the answer is "unknown" rather
// than an invented one.
func TestKitVersion(t *testing.T) {
	if got := KitVersion(); got == "" {
		t.Error("KitVersion must always answer something")
	}
}

// setEnv pins the environment variables the report reads for the duration of
// a test. An empty value means "not set at all", which is a case the renderer
// has to handle: t.Setenv is called first regardless, because that is what
// registers the restore, and the variable is then removed.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for key, value := range env {
		t.Setenv(key, value)
		if value == "" {
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
}
