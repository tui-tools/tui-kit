package kv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "pairs, comments and blank lines",
			input: `# a comment

backend = "ufw"
sudo = "sudo -n"
`,
			want: map[string]string{"backend": "ufw", "sudo": "sudo -n"},
		},
		{
			name:  "a trailing inline comment is dropped from a quoted value",
			input: `backend = "ufw"  # the user file wins`,
			want:  map[string]string{"backend": "ufw"},
		},
		{
			name:  "table headers are ignored",
			input: "[section]\nkey = \"value\"\n",
			want:  map[string]string{"key": "value"},
		},
		{
			name:  "single quotes and bare values",
			input: "a = 'one'\nb = two\n",
			want:  map[string]string{"a": "one", "b": "two"},
		},
		{
			name:    "a malformed line is an error in strict mode",
			input:   "backend ufw\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tc.input), "test.toml", true)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if !strings.Contains(err.Error(), "test.toml:1") {
					t.Errorf("error should name the file and line, got %q", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseLenientSkipsBadLines(t *testing.T) {
	// A palette file must degrade to the defaults, never fail the tool.
	got, err := Parse(strings.NewReader("garbage\nred = \"#f7768e\"\n"),
		"colors.toml", false)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]string{"red": "#f7768e"}) {
		t.Errorf("got %v", got)
	}
}

func TestReadFileMissing(t *testing.T) {
	_, err := ReadFile(filepath.Join(t.TempDir(), "absent.toml"), true)
	if !os.IsNotExist(err) {
		t.Fatalf("err = %v, want a not-exist error the caller can skip", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory on this host")
	}
	tests := []struct {
		in   string
		want string
	}{
		{in: "~/.config/x", want: filepath.Join(home, ".config/x")},
		{in: "~", want: home},
		{in: "/etc/x", want: "/etc/x"},
		{in: "relative/x", want: "relative/x"},
		// "~name" is another user's home, which we deliberately do not expand.
		{in: "~other/x", want: "~other/x"},
	}
	for _, tc := range tests {
		if got := ExpandHome(tc.in); got != tc.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
