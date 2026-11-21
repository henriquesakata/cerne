#!/usr/bin/env bash
# PreToolUse/Bash hook installed by `cerne hook install` (F-0457). Pipes the
# hook payload to `cerne hook tool-notes`, which delivers the session's pending
# project-channel messages (B-0054: a shell command is a contact point) and
# matches the command against the bash: triggers declared on the active notes
# (local index, no LLM, no network), adding up to 3 `id — title` lines to the
# agent's context, each note once per session. It blocks a command only in a
# project under [gates] accept = "merge", where it denies the agent's PR merge
# (F-0488); it never alters one, and degrades silently (exit 0) when the cerne
# binary is absent or fails. Tool-managed — do not edit; fix it upstream in internal/hooks/assets/
# and re-run `cerne hook install`.
# To turn it off for a process (eval, CI), set CERNE_NO_AGENT_HOOKS=1; to turn
# the notes off for a project, set hooks.notes_on_bash = false (cerne
# settings).
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0
# A GUI host (Cursor, an IDE opened from the Dock) can run hooks with the
# bare system PATH; look where cerne installs to as well (F-0428).
command -v cerne >/dev/null 2>&1 || PATH="${PATH:+$PATH:}$HOME/go/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin"

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook tool-notes 2>/dev/null || true
exit 0
