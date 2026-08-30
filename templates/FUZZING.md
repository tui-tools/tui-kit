# Fuzzing in the tui-tools family

Every tool in this family reads output it did not write. `ufw status`,
`systemctl list-units`, `pacman -Si`, `/etc/os-release`: bytes produced by
another program, on a machine we have never seen, parsed into the values the
dialog shows and the commands the tool then offers to run. A parser that
returns nonsense on an unexpected line is how a tool ends up showing a rule
that is not there, or building a command around a name it never really read.

So: **every package that parses command output carries at least one Go native
fuzz test.**

## The rule

- Name it after what it parses: `func FuzzParseUFWStatus(f *testing.F)`, in
  the same package as the parser, in a `_test.go` file.
- **Seed it from `testdata`.** The seed corpus is the same captured output the
  table tests use, plus the shapes a real capture never has: the empty string,
  a lone separator, a truncated line. The fuzzer mutates from real line shapes
  instead of guessing them.
- **Assert an invariant, not an output.** A fuzz target is not a golden test:
  the input is unknown, so the assertion has to be something that holds for
  every input. What callers are allowed to assume — no blank name, no blank
  version, a fingerprint that is a full 40 hex digits or nothing at all, a
  field that is a bare word. Not panicking is the floor, not the goal.
- Anything the parser's result is fed to and that can itself fail — a
  `String()` the UI prints, a `Matches()` a decision reads — belongs inside the
  target too.

`tui-kit/pkgmgr/fuzz_test.go` is the worked example: six targets over the six
parsers, seeded from `pkgmgr/testdata`.

## What CI runs, and why it is only the seeds

CI runs `go test ./...`, and that already executes **every seed of every
`Fuzz` function** as an ordinary test case. A fixture that stops parsing, or an
invariant a change breaks on a known input, fails the build like any other
test.

CI does **not** run `go test -fuzz`. That is a deliberate limit, and the reason
is the toolchain rather than the budget: `-fuzz` accepts exactly one target in
exactly one package per invocation. `go test -fuzz=. ./...` is refused with
"cannot use -fuzz flag with multiple packages", and `-fuzz=.` inside one
package is refused with "matches more than one fuzz test". Fuzzing the family's
parsers in CI would therefore mean one hand-maintained job step per target,
each spending its ten seconds re-deriving what the previous run already found
and then throwing the corpus away, because the generated corpus lives in the
build cache and not in the repository. The cost grows with every parser and
the coverage does not.

Continuous fuzzing is a thing you run, not a thing a pull request waits on:

```sh
go test -run=^$ -fuzz=FuzzParseKeyFingerprint -fuzztime=5m ./pkgmgr/
```

A crash writes its input to `testdata/fuzz/<FuzzName>/<hash>`. **Commit that
file.** From then on it is a seed, `go test` replays it on every commit, and
the bug cannot come back quietly.
