package runner

import (
	"context"
	"fmt"
	"strings"
)

// Fake is a Runner that touches nothing. It records what it was asked to run
// and answers from a canned table, which is what powers two things every tool
// in the family has:
//
//   - `--demo`: the tool drives an in-memory backend built on a Fake, so every
//     key works and every command is built and previewed for real, with
//     nothing reaching the system;
//   - tests: assert on Fake.Ran to prove a key produced exactly one command,
//     with exactly the argv the preview showed.
//
// The zero value is usable and reports "ok" for everything.
type Fake struct {
	// Prefix is the escalation prefix shown in the preview, so a demo looks
	// like the real thing. Empty previews the bare command.
	Prefix string
	// Outputs maps a command's String() to the output Run should return.
	// A command with no entry returns Default.
	Outputs map[string]string
	// Default is returned for a command Outputs does not cover.
	Default string
	// Err, when set, is returned by every Run instead of an output.
	Err error
	// Ran records every command Run received, in order.
	Ran []Command
	// Hook, when set, runs before the canned answer, so a fake backend can
	// mutate its in-memory state the way the real command would.
	Hook func(cmd Command) (string, error)
}

// Preview renders the command the way the real Runner would.
func (f *Fake) Preview(cmd Command) string {
	if f.Prefix == "" {
		return cmd.String()
	}
	return f.Prefix + " " + cmd.String()
}

// Run records the command and returns the canned answer.
func (f *Fake) Run(_ context.Context, cmd Command) (string, error) {
	f.Ran = append(f.Ran, cmd)
	if f.Err != nil {
		return "", f.Err
	}
	if f.Hook != nil {
		return f.Hook(cmd)
	}
	if out, ok := f.Outputs[cmd.String()]; ok {
		return out, nil
	}
	if f.Default != "" {
		return f.Default, nil
	}
	return "ok", nil
}

// Reset clears the recorded commands between test cases.
func (f *Fake) Reset() { f.Ran = nil }

// Last returns the most recent command, and whether there was one.
func (f *Fake) Last() (Command, bool) {
	if len(f.Ran) == 0 {
		return Command{}, false
	}
	return f.Ran[len(f.Ran)-1], true
}

// Previews renders every recorded command, for a golden-file assertion.
func (f *Fake) Previews() []string {
	out := make([]string, 0, len(f.Ran))
	for _, cmd := range f.Ran {
		out = append(out, f.Preview(cmd))
	}
	return out
}

// String summarises the fake, for a test failure message.
func (f *Fake) String() string {
	return fmt.Sprintf("runner.Fake{ran: %s}", strings.Join(f.Previews(), "; "))
}
