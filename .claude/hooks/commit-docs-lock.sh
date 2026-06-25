#!/usr/bin/env sh
# Auto-commit a left-behind docs.lock snapshot bump.
#
# cerne backend mutations (feature transitions, push, pull) rewrite docs.lock in
# the project working tree as a side-effect (CommitAndSnapshot, app/artifact.go)
# but NEVER commit it there — cerne owns the backend repo, not yours. Pinning the
# new snapshot is a manual trailing commit that is easy to forget, so docs.lock
# is repeatedly left dirty after a task.
#
# Stop hook: when docs.lock is the dirty leftover, commit ONLY it. The
# path-limited commit (git commit -- docs.lock) records the working-tree contents
# of docs.lock alone — any other staged/unstaged changes are left exactly as they
# are. No-op when docs.lock is clean, untracked, or absent.

input=$(cat)
cwd=$(printf '%s' "$input" | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
dir="${cwd:-${CLAUDE_PROJECT_DIR:-.}}"

root=$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null) || exit 0

status=$(git -C "$root" status --porcelain -- docs.lock 2>/dev/null)
[ -n "$status" ] || exit 0

case "$status" in
  '??'*) exit 0 ;;
esac

git -C "$root" commit -q \
  -m "chore(docs): pin docs.lock" \
  -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" \
  -- docs.lock 2>/dev/null

exit 0
