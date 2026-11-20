#!/usr/bin/env bash
# PostToolUse/Bash|Edit|Write hook installed by `cerne hook install` (F-0457),
# for Cursor only: Cursor runs the .claude hooks through its third-party
# import but takes no context before a tool, so the notes whose declared
# trigger matches a shell command or an edit reach its agent right after the
# tool, with the session's pending project-channel messages (B-0054). Claude
# Code already got both before the tool: outside Cursor (no CURSOR_VERSION,
# which Cursor sets on every hook) this script leaves at once.
# Never blocks: degrades silently (exit 0) when the cerne binary is absent or
# fails. Tool-managed — do not edit; fix it upstream in internal/hooks/assets/
# and re-run `cerne hook install`.
# To turn it off for a process (eval, CI), set CERNE_NO_AGENT_HOOKS=1; to turn
# the notes off for a project, set
# hooks.notes_on_bash / hooks.notes_on_edit = false.
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0
[ -z "${CURSOR_VERSION:-}" ] && exit 0
# A GUI host (Cursor, an IDE opened from the Dock) can run hooks with the
# bare system PATH; look where cerne installs to as well (F-0428).
command -v cerne >/dev/null 2>&1 || PATH="${PATH:+$PATH:}$HOME/go/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin"

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook tool-notes --after 2>/dev/null || true
exit 0
