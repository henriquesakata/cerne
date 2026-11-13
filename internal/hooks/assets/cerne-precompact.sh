#!/usr/bin/env bash
# PreCompact hook installed by `cerne hook install` (F-0368). Pipes the hook
# payload to `cerne hook precompact`, which asks the compaction summary to keep
# the session's W- and its latest HO- code verbatim and records the compaction
# locally — no network, no backend write. Never blocks a compaction: degrades
# silently (exit 0) when the cerne binary is absent or fails. Tool-managed — do
# not edit; fix it upstream in internal/hooks/assets/ and re-run
# `cerne hook install`.
# To turn it off for a process (eval, CI), set CERNE_NO_AGENT_HOOKS=1
# instead of editing it (F-0360).
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook precompact 2>/dev/null || true
exit 0
