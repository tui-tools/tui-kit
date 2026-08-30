<img src="assets/branding/logo-kit-dark-320.png" alt="tui-kit" width="240">

The shared foundation of the [tui-tools](https://github.com/tui-tools) family:
the palette, the widgets, the configuration loader and the command runner that
make every tool in the family look and behave the same.

```go
import (
    "github.com/tui-tools/tui-kit/config"
    "github.com/tui-tools/tui-kit/runner"
    "github.com/tui-tools/tui-kit/theme"
    "github.com/tui-tools/tui-kit/ui"
)
```

This is a library, not a tool. Install it as a dependency:

```sh
go get github.com/tui-tools/tui-kit@v0.1.3
```

## What is in it

| Package | What it gives you |
| --- | --- |
| `theme` | The Tokyo Night palette, Omarchy theme detection, `NO_COLOR`, and a ready-made set of Lip Gloss styles |
| `ui` | Header, table, help bar, help screen, status line, and the confirm / input / picker dialogs |
| `config` | `/etc/<tool>/config.toml` + `~/.config/<tool>/config.toml` + environment, in that order |
| `runner` | Preview → confirm → run, including privilege escalation, timeouts and a fake for `--demo` and tests |
| `manifest` | Reads the tool.json a tool embeds, so the manifest is the single source at runtime too |
| `compat` | Probes the backend's version at startup, classifies it against the manifest, and answers `caps.Has("timers")` |
| `pkgmgr` | Detects the distribution and its package manager, reports whether the tui-tools repository is configured and which `tui-*` packages are installed, and builds the install / remove / upgrade / repository-setup commands |

Plus the scripts in `tools/`: `render-screenshots.py` renders a tool's README
screenshots from the real binary, `render-install.py` and `render-compat.py`
generate the README sections that come from the manifest, `compat-sync.py`
rebuilds the tested-version lists from a tool's `compat/results.jsonl`,
`check-nfpm.py` asserts that the .deb/.rpm/pacman metadata in a tool's
`.goreleaser.yaml` still matches its `tool.json` — GoReleaser cannot read the
manifest, so the description is a copy, and this is what stops the copy from
drifting — and `check-exec.sh` asserts the exec boundary — `os/exec` in `runner` and in a
tool's `internal/<backend>/`, nowhere else. `templates/` holds what a new tool
starts from — the CI workflow, the Scorecard and CodeQL workflows, the
GoReleaser configuration, the shared `golangci.yml` lint bar, the
`gitleaks.toml` secret scanning rules, the `dependabot.yml` that keeps its
dependencies current and `FUZZING.md`, the family's rule for the parsers —
and
[`schema/tool.schema.json`](schema/tool.schema.json) is the manifest every tool
carries at its root so the family website can describe it — see
[`docs/tool-manifest.md`](docs/tool-manifest.md) and
[`docs/compatibility.md`](docs/compatibility.md).

Dependencies are deliberately small: Bubble Tea, Bubbles and Lip Gloss, nothing
else. Configuration and palette files are read by a forty-line parser rather
than a TOML library.

## The contract: preview, confirm, run

Every tool in the family makes the same promise, and `runner` is where it is
kept. A tool never assembles a shell string. It builds a `runner.Command` — an
argv plus a description — shows it with `ui.Confirm`, and hands that same value
back to the runner once the user answered yes.

```go
r, err := runner.New(runner.Options{
    Bin:         "ufw",
    SearchPaths: []string{"/usr/sbin/ufw"},
    SudoPrefix:  cfg.SudoPrefix(), // "sudo -n"
    InstallHint: "install it with `apt install ufw`",
})

cmd := runner.Command{
    Argv:        []string{"ufw", "--force", "delete", "1"},
    Description: "Delete rule 1",
    Destructive: true,
}

dialog := ui.Confirm{
    Title:   cmd.Description,
    Command: r.Preview(cmd), // sudo -n /usr/sbin/ufw --force delete 1
    Danger:  cmd.Destructive,
    Payload: cmd,
}
// … the user presses y …
out, err := r.Run(ctx, cmd)
```

Because `Preview` and `Run` consume the same value, the text in the dialog is
guaranteed to be what executes. That is the whole trust boundary.

Reads go through `Read`, which escalates only when the tool says its reads need
it: `ufw status` does, `systemctl list-units` does not.

## Demo mode and tests

`runner.Fake` records what it was asked to run and answers from a canned table.
It is what makes `--demo` honest: the tool builds and previews every command for
real, and nothing reaches the system.

```go
fake := &runner.Fake{Prefix: "sudo -n", Hook: func(cmd runner.Command) (string, error) {
    return sample.apply(cmd) // mutate the in-memory state the way the real command would
}}
```

The same type is the assertion in tests: press a key, then check that
`fake.Ran` holds exactly one command with exactly the argv the preview showed.

## Theme rules

The default palette is **Tokyo Night**. If the machine runs
[Omarchy](https://omarchy.org), the tools read the active desktop theme from
`~/.config/omarchy/current/theme/colors.toml` and follow it, so switching the
desktop theme switches every tool. Any Omarchy-format `colors.toml` works.

Precedence:

1. `TUI_THEME=/path/to/colors.toml`, or the tool's own `--theme` flag;
2. the active Omarchy theme, when that file exists;
3. the built-in Tokyo Night palette.

`NO_COLOR` is respected, per [no-color.org](https://no-color.org): any non-empty
value keeps layout, borders and emphasis and drops every color.

A palette file that cannot be read never takes a tool down. `theme.New` returns
a `Warning` string the tool shows in its status line, and falls back to the
default.

## Configuration

`config.Load` reads, in increasing precedence:

1. the defaults the tool declares;
2. `/etc/<tool>/config.toml`;
3. `~/.config/<tool>/config.toml`;
4. `TUI_<TOOL>_<KEY>` environment variables — only for keys the tool declared,
   so an unrelated variable can never leak in;
5. command line flags, which the tool folds in with `cfg.Set`.

```go
cfg, err := config.Load(config.Options{
    Tool: "tui-firewall",
    Defaults: map[string]string{
        "backend":        "auto",
        config.KeySudo:   "sudo -n",
        config.KeyTheme:  "",
    },
})
backend := cfg.String("backend", "auto")
```

Values stay untyped strings, read back with `String`, `Bool` or `Int`. An
unknown key in a file is kept but ignored, so a newer config never breaks an
older binary; `cfg.OneOf` rejects the values a tool does care about.

## Packages: `pkgmgr`

`pkgmgr` is what a launcher needs to know about the machine it was started on:
which package manager runs here, whether the family's repository is configured,
which `tui-*` packages are installed or available, and what exactly would be run
to change that.

```go
pm, err := pkgmgr.New(pkgmgr.Options{SudoPrefix: []string{"sudo", "-n"}})
status, _ := pm.RepoStatus()          // configured? which file says so?
have, _ := pm.Installed(ctx, names)   // version per package, unprivileged
want, _ := pm.Available(ctx, names)
steps, _ := pm.Install([]string{"tui-firewall"})
for _, step := range steps {
        // ui.Confirm(pm.Preview(step)) → pm.Run(ctx, step)
}
```

The contract, in short:

- **Detection.** `Detect` returns the manager and the distribution. The binary
  has to be there — a manager that is not installed is not this machine's
  manager, whatever `/etc/os-release` claims — and among the ones that are, the
  distribution decides, so an Ubuntu image carrying `rpm` is still driven by
  apt. `apt`, `dnf` and `pacman`, with `ParseOSRelease` exported so the decision
  table can be tested without a container per distribution.
- **Names.** Anything that reaches an argv is validated against
  `^tui-[a-z]+$` first. There is no builder that skips the check and no way to
  pass a name through it, so no command in this package can be assembled from
  input nobody looked at.
- **Commands are values.** `Command{Argv, Privileged, Explain, Stdin}` is
  previewed and then handed back to be run, exactly as `runner` does — the
  command line in the dialog is the command line that runs. A read is never
  marked privileged: listing what is installed must not raise a password
  prompt.
- **Repository.** `RepoStatus` reports whether `pkgs.tui.tools` is configured —
  an apt sources file naming it, a dnf `.repo`, or a `[tui-tools]` section
  reachable from `/etc/pacman.conf`, including through an `Include`.
  `RepoSetup(fingerprint)` returns the steps that add it, mirroring
  `pkgs/install.sh` and Omarchy Server's `tui-tools` addon, with the key pinned
  by the caller's fingerprint: the returned `Setup` names the step whose output
  must match before anything imports the key. The fingerprint is never
  compiled in — one that lives in a library is one that cannot be rotated
  without a release.
- **Exec.** Nothing in `pkgmgr` starts a process. Every command is executed by
  `runner`, which is the family's sanctioned exec site, so `check-exec.sh` stays
  satisfied without a tool having to wrap this package in an `internal/` of its
  own.
- **`pkgmgr.Fake`** is the `--demo` machine: the same validation, the same
  previews, and a catalogue that changes the way an install would change a
  real one.

## Naming

Every tool in the family is `tui-<target>`: the repository, the Go package and
the installed binary all carry that one name, with no aliases. `tui-firewall`,
`tui-systemd`. Use `tui-<name>-<solution>` only when a target needs
disambiguating.

## Branding

The family assets live in [`assets/branding/`](assets/branding) and are
regenerated with `make branding`. See [`docs/branding.md`](docs/branding.md) for
the colors and how to use them.

## Verifying a release

Every release is built by the tool's own CI workflow on a `v*` tag, and a tag
only becomes a release once the `security` job is green. That job runs
`govulncheck` over the code the build actually reaches, and `gitleaks` over
both the working tree and the whole git history — a secret that was committed
once stays reachable long after the commit that removed it. `gosec` runs a job
earlier, inside the lint bar.

What ships beside the binaries: a CycloneDX SBOM per archive, a keyless cosign
signature over `checksums.txt`, and SLSA build provenance for every archive,
package and the checksum file. None of it needs a key to be distributed: both
signatures are keyless, tied to the workflow identity GitHub issued at build
time.

Downloading `tui-firewall_0.2.2_linux_amd64.tar.gz` and its `.deb`, say:

```sh
gh attestation verify tui-firewall_0.2.2_linux_amd64.tar.gz -R tui-tools/tui-firewall
gh attestation verify tui-firewall_0.2.2_amd64.deb -R tui-tools/tui-firewall
```

That answers "was this file built by that repository's workflow". To check the
signature over the release as a whole, take `checksums.txt` and its bundle:

```sh
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp \
    '^https://github.com/tui-tools/tui-firewall/\.github/workflows/ci\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

The identity regexp is the part that matters: it says the signature has to come
from that repository's `ci.yml`, running on a tag. Without it, `cosign` would
accept anything signed by anyone. Then check what you downloaded against the
file you just verified:

```sh
sha256sum -c --ignore-missing checksums.txt
```

Replace `tui-firewall` with the tool you are verifying; everything else is the
same for every tool in the family.

Each repository also runs [OpenSSF
Scorecard](https://github.com/ossf/scorecard) weekly and publishes the result,
so its supply-chain posture is public. A tool's README carries it as a badge:

```md
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/tui-tools/tui-firewall/badge)](https://scorecard.dev/viewer/?uri=github.com/tui-tools/tui-firewall)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/14368/badge)](https://www.bestpractices.dev/projects/14368)
```

## Development

```sh
make check       # gofmt, go vet, the exec boundary and the tests: what CI runs
make test
make check-exec  # only runner may start a process
make branding
```

### Fuzzing the parsers

Every package that parses command output carries at least one Go native fuzz
test, seeded from its `testdata`. `make check` runs the seed corpus of each
one like any other test; exploring past the seeds is something you run when
you touch a parser:

```sh
go test -run=^$ -fuzz=FuzzParseKeyFingerprint -fuzztime=5m ./pkgmgr/
```

A crash writes its input under `testdata/fuzz/`; commit that file and it
becomes a seed the tests replay forever. `pkgmgr/fuzz_test.go` is the worked
example, and [`templates/FUZZING.md`](templates/FUZZING.md) is the rule the
whole family follows, including why CI runs the seeds and not the fuzzer.

### Cutting a release

Tags are annotated, and the message is the release notes:

```sh
git tag -a v0.3.0 -m "What changed for somebody running the tool."
git push origin v0.3.0
```

GoReleaser renders that message as the release header (`{{ .TagBody }}` in
`templates/.goreleaser.yaml`) above the generated commit list, so a release
opens with a sentence a person wrote instead of a list of subjects. A
lightweight tag leaves the header blank, which is the reminder to go back and
write one.

## Contributing

Issues and pull requests are welcome. Start with
[CONTRIBUTING.md](CONTRIBUTING.md): it covers the pull-request flow — open an
issue first for anything larger than a fix — and the bar a change has to
clear, which is `make check` green, a table-driven test built from real
command output for any parsing change, and preview-then-confirm for anything
that mutates a system. [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) applies to
every interaction here.

Security problems do not go in the issue tracker:
[SECURITY.md](SECURITY.md) says how to report one privately and what response
to expect.

## Status

Early. The API may still move before v1; tools pin an exact version.

**Unofficial.** The family follows the Omarchy visual style. It is not part of
the Omarchy project and not endorsed by its maintainers.

## License

MIT — see [LICENSE](LICENSE).
