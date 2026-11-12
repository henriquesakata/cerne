#!/usr/bin/env bash
# Stop hook installed by `cerne hook install` (F-0171). Pipes the hook payload
# to `cerne hook stop-check`, which reminds the session to close or update its
# live registration (W-/F- binding) before the turn ends — advisory
# additionalContext, never a blocking decision, and 100% local (no network on
# the Stop path). Degrades silently (exit 0) when the cerne binary is absent,
# so a broken install never breaks the end of a turn. Tool-managed — do not
# edit; fix it upstream in internal/hooks/assets/ and re-run
# `cerne hook install`.
# To turn it off for a process (eval, CI), set CERNE_NO_AGENT_HOOKS=1
# instead of editing it (F-0360).
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook stop-check 2>/dev/null || true
exit 0
