#!/usr/bin/env sh
# F-0046 — freeze docs/process/cases/: block creating a NEW case folder.
#
# docs/process/cases/ is frozen history. New documentation is authored
# cerne-native (cerne feature new -> cerne pull -> fill .docs/current/features/
# F-NNNN/ -> cerne push -> approve/start/complete), like the hub. Editing an
# EXISTING case is still allowed (its folder already exists on disk).
#
# PreToolUse hook (matcher Write). Inspects ONLY the Write target path
# (tool_input.file_path) — never free file content — so a doc that merely
# mentions a case path is not blocked. Blocks (exit 2) when the target lands in a
# docs/process/cases/NNNN_ folder that does not yet exist (i.e. a new case).
input=$(cat)
fp=$(printf '%s' "$input" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
case "$fp" in
  *docs/process/cases/[0-9][0-9][0-9][0-9]_*)
    dir=$(printf '%s' "$fp" | sed -E 's#^.*(docs/process/cases/[0-9]{4}_[^/"]+).*#\1#')
    root="${CLAUDE_PROJECT_DIR:-.}"
    if [ ! -e "$root/$dir" ] && [ ! -e "$dir" ]; then
      echo "BLOQUEADO: $dir é uma NOVA case-dir. docs/process/cases/ está congelada (history)." >&2
      echo "Autore cerne-nativo: cerne feature new \"<título>\" -> cerne pull -> .docs/current/features/F-NNNN/ -> cerne push -> approve/start/complete." >&2
      exit 2
    fi ;;
esac
exit 0
