package hooks

// RegisterRules is the canonical registration ruleset injected at session
// start (F-0139/F-0174) — the floor every provider gets. The Claude Code
// asset (cerne-register-reminder.sh) carries the same text in shell form;
// the Go copy exists so provider-format subcommands (`cerne hook
// session-context`) can emit it inside each provider's own envelope.
const RegisterRules = `cerne project rules — make in-flight work visible from minute one:
- Picking up a FEATURE? First action: ` + "`cerne feature new \"<title>\" --context \"<1-3 sentences of why>\"`" + ` (lands as a candidate on the kanban; --file spec.md for a one-shot spec). Advance it as you go: spec -> approve (HUMAN gate), plan -> ` + "`cerne feature start`" + `, result -> ` + "`cerne feature complete`" + `. Abandoned? ` + "`cerne feature reject <id> --reason \"...\"`" + `.
- Picking up a NON-feature task (error investigation, review, ops)? First action: ` + "`cerne work start \"<description>\" [--ref F-...]`" + `; when finished, ` + "`cerne work done W-... [--note \"...\"]`" + `. BEFORE delegating an error investigation to another agent, run ` + "`cerne work list`" + ` — someone may already be on it.
- HUMAN GATES (F-0131): ` + "`cerne feature approve`" + ` and ` + "`cerne feature accept`" + ` are human acts by default — write the spec, ask for approval, STOP.
- DUPLICATE GUARD (F-0147): ` + "`feature new`/`work start`" + ` refuse a title similar to a live feature or active work — join that work instead; --force only for a genuinely distinct problem.`
