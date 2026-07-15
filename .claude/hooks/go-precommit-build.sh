#!/usr/bin/env sh
# Insights follow-up — pre-commit Go gate.
#
# Catches the "late-surfacing build failure" friction (wrong-package import,
# missing symbol) BEFORE it lands in a commit / CI / MR, instead of after.
# PreToolUse hook (matcher Bash): inspects the git commit command and runs
# `go build ./...` (fast; compiles every package). Blocks (exit 2) on failure so
# the commit is retried after a fix. Full `go test`/`go vet` stay manual/CI —
# too slow to gate every commit (see CLAUDE.md Build/test/run).
#
# Degrades gracefully:
#   - only gates real `git commit` invocations (skips everything else),
#   - honours the `--no-verify` escape hatch,
#   - if `go` cannot be found (PATH or the Windows dev-box location), it SKIPS
#     rather than blocking — never wedge a commit because the toolchain moved.
input=$(cat)
cmd=$(printf '%s' "$input" | sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\(.*\)"[^"]*}.*/\1/p' | head -1)

# Only gate real commits.
case "$cmd" in
  *"git commit"*) : ;;
  *) exit 0 ;;
esac
# Respect the explicit escape hatch.
case "$cmd" in
  *"--no-verify"*) exit 0 ;;
esac

# Resolve go: PATH first, then the Windows dev-box location (CLAUDE.md), else skip.
if command -v go >/dev/null 2>&1; then
  GO=go
elif [ -x /c/development/tools/go/bin/go ]; then
  GO=/c/development/tools/go/bin/go
else
  exit 0
fi

root="${CLAUDE_PROJECT_DIR:-.}"
out=$(cd "$root" 2>/dev/null && "$GO" build ./... 2>&1)
if [ $? -ne 0 ]; then
  echo "BLOQUEADO: go build ./... falhou — corrija antes de commitar (evita build quebrado no CI/MR)." >&2
  printf '%s\n' "$out" | tail -20 >&2
  echo "(escape: git commit --no-verify)" >&2
  exit 2
fi
exit 0
