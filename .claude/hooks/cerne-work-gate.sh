#!/usr/bin/env bash
# PreToolUse/Edit|Write hook installed by `cerne hook install` (F-0151). Pipes
# the hook payload to `cerne hook work-gate`, which denies the edit when the
# project enables [gates] work_gate and the session holds no live cerne
# registration (run `cerne work start` / `cerne feature start` first). The
# toggle is evaluated inside the Go subcommand at run time — switching it in
# .cernerc needs no hook reinstall. Fail-open by design: with the binary
# absent (or anything unexpected) this wrapper exits 0 silently, because a
# broken install must never lock the user out of editing. Tool-managed — do
# not edit; fix it upstream in internal/hooks/assets/ and re-run
# `cerne hook install`.

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook work-gate 2>/dev/null || true
exit 0
