#!/usr/bin/env bash
# PostToolUse/Bash hook installed by `cerne hook install` (F-0170). Pipes the
# hook payload to `cerne hook lifecycle-reminder`, which injects the guidance
# for the exact lifecycle moment that just happened: after a successful
# `cerne feature new`, write the real spec in this same turn; after a
# confirmed `cerne feature start`, consult the project's notes/conventions
# and keep progress real-time. All parsing and decisions live in the Go
# subcommand; this wrapper only degrades silently (exit 0) when the cerne
# binary is absent, so a broken install never breaks a tool call.
# Tool-managed — do not edit; fix it upstream in internal/hooks/assets/ and
# re-run `cerne hook install`.

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook lifecycle-reminder 2>/dev/null || true
exit 0
