## Drift: code moved, docs stood still

An `implemented` feature's docs are frozen history — but the code under them keeps changing. **Drift detection** (F-0113/F-0116) maps the project repo's git diff since the last docs sync to the implemented features whose scope it touches, deterministically (no LLM). It is **opt-in**: the project's `.cernerc` must map scopes to path globs — `[module_paths]` (module → globs, for moduled projects) and/or `[component_paths]` (component tag → globs, the axis that works in a moduleless project). No config, no baseline → the rule is silent, never noisy.

- **See it:** `cerne drift` (also folded into `cerne lint`). A scope with a *live* feature is suppressed — active work already tracks that change.
- **Fix it — three paths, pick by project policy:**
  - **Manual:** review the finding, append `cerne feature errata <id> "<text>"`.
  - **Gated draft:** `cerne drift --draft` emits one delegation prompt per finding under `.docs/.drift-proposals/`; **you** (the host agent) write the errata draft into the sibling `<id>.md`, a human applies it via `cerne feature errata <id> --file …` (F-0114).
  - **Automatic:** `cerne drift --auto` drafts via the configured `[llm]` provider (claude-cli/byo-key) and applies the errata itself under the `drift-auto` actor, then records a pending review (F-0115, D-0005 apply-then-notify).
- **Review duty (D-0005):** every automatic update waits for acknowledgment. `cerne drift pending` lists them (`cerne next` shows the count) — treat a pending entry as a work item: read the applied errata (`cerne show <id>`), then `cerne drift ack <id>` to accept, or append a correcting errata / supersede if the agent got it wrong. Never leave pendings to rot.
- **Invariant:** drift never rewrites history. All three paths end in an **additive errata**; the reading surface (`cerne show`, wiki, lint) folds erratas in, so the reader sees current truth over an intact record.

Adjacent freshness knob: with `compile_on_push = true` in `.cernerc` (a top-level key: write it above the first `[table]` — `cerne init` ends the file with `[gates]`, and a key below it is read as a gates key; `cerne` warns when that happens) (and a callable `[llm]` mode), every successful `cerne push` incrementally resynthesizes only the **dirty** wiki pages (F-0117) — a failed compile never fails the push. No config → recompile stays manual (`cerne compile`).
