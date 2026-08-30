#!/usr/bin/env bash
# Assert the family's exec boundary: only a backend package may start a process.
#
# Every tui-tools binary makes one promise — the command line you were shown is
# the command line that ran. That promise is only checkable if there is exactly
# one place a process can be started from: tui-kit's `runner`, which previews
# and executes the same value. A tool's UI, its config layer and its main
# package must never reach for os/exec themselves.
#
# The rule, therefore: `os/exec` may be imported by
#
#   - internal/<backend>/...   in a tool (the package that owns a backend)
#   - runner/...               in tui-kit (the boundary itself)
#
# and nowhere else. Test files are exempt: a test that spawns a helper process
# is not a path a user's confirmation flows through.
#
#   tui-kit/tools/check-exec.sh [root]     # default root: the current directory
#
# It exits non-zero listing every file that crossed the line.
set -uo pipefail

root="${1:-.}"
cd "$root" || exit 2

# allowed reports whether a path is one of the two places allowed to exec.
allowed() {
  local path="${1#./}"
  case "$path" in
    internal/*/*) return 0 ;; # a tool's backend package
    runner/*) return 0 ;;     # the kit's runner, the boundary itself
    *) return 1 ;;
  esac
}

violations=0
while IFS= read -r file; do
  case "$file" in
    *_test.go) continue ;;
  esac
  if allowed "$file"; then
    continue
  fi
  # Both forms are caught: the import itself, and any use of the package. The
  # import is what matters, the second is a cheap guard against an alias.
  if grep -qE '"os/exec"|\bexec\.Command(Context)?\(' "$file"; then
    printf 'exec outside a backend package: %s\n' "${file#./}"
    grep -nE '"os/exec"|\bexec\.Command(Context)?\(' "$file" | sed 's/^/      /'
    violations=$((violations + 1))
  fi
done < <(find . -name '*.go' -not -path './.git/*' -not -path './vendor/*' | sort)

if [[ $violations -gt 0 ]]; then
  cat >&2 <<'EOF'

Only tui-kit/runner and a tool's internal/<backend>/ package may start a
process. Everything else goes through runner.Runner, so the preview the user
confirmed and the command that runs are the same value.
EOF
  exit 1
fi

echo "exec boundary: clean"
