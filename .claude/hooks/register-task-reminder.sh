#!/usr/bin/env bash
# SessionStart hook — cerne dogfooding rule (case 0035): register work at the
# start. This script's stdout is injected as session context, so the agent sees
# the rule before it touches code. Deliberately static: it must not depend on
# the cerne backend (SSH/network) being reachable at session start.
cat <<'EOF'
cerne project rule — register the task BEFORE you start implementing, so in-flight work is visible on the kanban from minute one:
- First action when you pick up a task: `cerne feature new "<title>"` -> lands as a candidate, shows in the board's CANDIDATAS column.
- Advance as the work proceeds: spec -> `cerne feature approve`, plan -> `cerne feature start`, result -> `cerne feature complete`.
- Abandoned it? `cerne feature reject <id> --reason "..."`. Forgotten specless candidates are surfaced by `cerne lint` (stale-candidate rule).
EOF
