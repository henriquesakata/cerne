#!/usr/bin/env bash
# UserPromptSubmit hook installed by `cerne hook install` (B-0020). Pipes the
# hook payload to `cerne hook prompt-notes`, which delivers the session's
# pending project-channel messages (B-0054: the prompt is a contact point) and
# matches the user's prompt against the titles of the active notes and
# runbooks (local index, no LLM, no network), injecting up to 3 `id — title`
# lines once per session. Never blocks a prompt: degrades silently (exit 0)
# when the cerne binary is absent or fails. Tool-managed — do not edit; fix it upstream in internal/hooks/assets/
# and re-run `cerne hook install`.
# To turn it off for a process (eval, CI), set CERNE_NO_AGENT_HOOKS=1; to turn
# the notes off for a project, set hooks.prompt_notes = false (cerne settings).
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0
# A GUI host (Cursor, an IDE opened from the Dock) can run hooks with the
# bare system PATH; look where cerne installs to as well (F-0428).
command -v cerne >/dev/null 2>&1 || PATH="${PATH:+$PATH:}$HOME/go/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin"

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook prompt-notes 2>/dev/null || true
exit 0
