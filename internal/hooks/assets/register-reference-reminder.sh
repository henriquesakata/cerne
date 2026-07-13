#!/usr/bin/env sh
# F-0103 — provenance capture, reworked by F-0172: all parsing and decisions
# live in `cerne hook provenance` (Go, testable), which reminds the agent to
# register the external sources that SHAPED a doc at the hot moments — a
# successful `cerne push` whose authored docs cite an unbacked external URL
# (same detector as the unregistered-source lint rule), and the decision gates
# (`feature complete`, `decision new/substituir`). This wrapper only degrades
# silently (exit 0) when the cerne binary is absent, so a broken install never
# breaks a tool call. Tool-managed — do not edit; fix it upstream in
# internal/hooks/assets/ and re-run `cerne hook install`.

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook provenance 2>/dev/null || true
exit 0
