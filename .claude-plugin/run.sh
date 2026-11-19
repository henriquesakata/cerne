#!/usr/bin/env bash
# Entry script of cerne's Claude Code plugin (F-0454): `run.sh hook <script>`
# runs one of the agent hooks `cerne hook install` wires, from the copy in
# internal/hooks/assets/ — the same file the binary embeds.
# - Outside a cerne project (no .cernerc) every hook is silent.
# - The project's own install wins: a project that already has the script under
#   .claude/hooks/ (cerne init, cerne hook install) runs it from its
#   settings.json, so the plugin's copy steps aside instead of firing twice.
# - The plugin does not ship the binary: without cerne, SessionStart prints how
#   to install it, in any project, and the other hooks stay silent.
# CERNE_PLUGIN=1 tells the binary it runs from the plugin; CERNE_ACTOR=agent is
# what `cerne hook install` puts in settings.json's env for every hook, so the
# hooks attribute their work to the agent. To turn the hooks off for a process
# (eval, CI), set CERNE_NO_AGENT_HOOKS=1 (F-0360).
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0
[ "$1" = hook ] || exit 0
# Look where cerne installs to as well, like the hooks it runs (F-0428).
command -v cerne >/dev/null 2>&1 || PATH="${PATH:+$PATH:}$HOME/go/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin"
if ! command -v cerne >/dev/null 2>&1; then
  [ "$2" = cerne-register-reminder.sh ] &&
    echo 'cerne plugin: the cerne CLI is not installed. Install it with `brew install henriquesakata/tap/cerne` (macOS/Linux) or, on Windows, `irm https://raw.githubusercontent.com/henriquesakata/cerne/main/install.ps1 | iex`; other ways: https://github.com/henriquesakata/cerne/blob/main/INSTALL.md'
  exit 0
fi
proj=${CLAUDE_PROJECT_DIR:-$PWD}
asset="${CLAUDE_PLUGIN_ROOT:-$(dirname "$0")/..}/internal/hooks/assets/${2##*/}"
[ -f "$proj/.cernerc" ] && [ -f "$asset" ] || exit 0
[ -f "$proj/.claude/hooks/${2##*/}" ] && exit 0
export CERNE_PLUGIN=1 CERNE_ACTOR=agent
exec bash "$asset"
