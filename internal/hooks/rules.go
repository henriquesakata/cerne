package hooks

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// RegisterRules is the canonical registration ruleset injected at session
// start (F-0139/F-0174) — the floor every provider gets. The Claude Code
// asset (cerne-register-reminder.sh) carries the same text in shell form;
// the Go copy exists so provider-format subcommands (`cerne hook
// session-context`) can emit it inside each provider's own envelope.
const RegisterRules = registerHead + "\n" + GauntletRule + " " + gauntletSkip + "\n" + registerTail

// SessionRules is the ruleset the dynamic session-start path injects
// (F-0364): in light mode ([gates] gauntlet = "off") the GAUNTLET item is
// left out, so a new project is not told to write a bar it is not charged.
//
// l is the project's documentation language (F-0475): the planning example
// speaks it and a DOCS LANGUAGE line lists its forms. An empty l — the
// language could not be resolved — keeps the neutral text and adds no line.
func SessionRules(gauntletOn bool, l doclang.Lang) string {
	head := registerHead
	if l != "" {
		head = langHead(l)
	}
	if !gauntletOn {
		return head + "\n" + registerTail
	}
	return head + "\n" + GauntletRule + "\n" + registerTail
}

// planningNeutral is what the static texts (RegisterRules, the installed
// script) put where the planning word of the project's language goes.
const planningNeutral = "<planning, in the docs language>"

// langHead is registerHead with the planning word of l and the DOCS LANGUAGE
// line after it; every name comes from internal/doclang.
func langHead(l doclang.Lang) string {
	return strings.Replace(registerHead, planningNeutral, doclang.Examples(l).Planning, 1) + "\n" + langLine(l)
}

func langLine(l doclang.Lang) string {
	d := func(t doclang.Term) string { return doclang.Display(t, l) }
	sections := func(k doclang.Kind) string { return strings.Join(doclang.Sections(k, l), " · ") }
	return "- DOCS LANGUAGE: " + string(l) + " — write every document in it, with these forms only. Spec sections: " + sections(doclang.Spec) +
		". Plan sections: " + sections(doclang.Plan) + ". Result sections: " + sections(doclang.Result) +
		". Bar line `" + d(doclang.BarLabel) + ":` (with `" + d(doclang.MethodLabel) + ":` and `" + d(doclang.ExitLabel) + ":`); evidence `" + d(doclang.Criterion) +
		" N`; open question `" + doclang.AmbiguityMarker(l) + " …]`; " + doclang.ClarifyRule(l)
}

// LangRules is what `cerne hook lang-rules` prints for the Claude Code
// script (F-0475): the planning item and, when the language is known, the
// DOCS LANGUAGE line.
func LangRules(l doclang.Lang) string {
	if l == "" {
		return planningLine(registerHead)
	}
	return planningLine(strings.Replace(registerHead, planningNeutral, doclang.Examples(l).Planning, 1)) + "\n" + langLine(l)
}

func planningLine(head string) string {
	for _, ln := range strings.Split(head, "\n") {
		if strings.Contains(ln, "ALREADY-APPROVED") {
			return ln
		}
	}
	return ""
}

// gauntletSkip is the light-mode escape the static Cursor rule carries: it is
// written once, so it cannot omit the item per project.
const gauntletSkip = `(skip it when .cernerc sets [gates] gauntlet = "off")`

const registerHead = `cerne project rules — make in-flight work visible from minute one:
- START a session with ` + "`cerne next`" + ` (what to do now, and the lint debts), then a task with its package, ` + "`cerne context <id>`" + ` (the stage document, the epic and the notes that inform it, within a token budget), not by reading ` + "`.docs/CONTEXT.md`" + `: that file is the map of what is live and of the note components — read it when you need the map. Tagging? Reuse a component ` + "`cerne tags`" + ` lists; a new name near an existing one is refused without --force.
- Picking up a FEATURE? First action: ` + "`cerne feature new \"<title>\" --context \"<1-3 sentences of why>\"`" + ` (lands as a candidate on the kanban; --file spec.md for a one-shot spec). In an agent shell every card creation (feature/bug/epic/task new) needs ` + "`--on-behalf \"<the human's literal words>\"`" + ` when a human asked for it, or ` + "`--origin agent`" + ` when the idea is yours; neither is refused (B-0086). Before writing that spec, read the code the request touches: the Problem cites real file:line and no open question asks what the code already answers. Register it complete in that same act: spec (` + "`--file`" + `), tags (` + "`--facet`/`--component`" + `) and, once the project has a capability taxonomy, capability (` + "`--capability C-…`" + ` on ` + "`feature new`/`task new`" + `) — ` + "`cerne feature approve <id> --check`" + ` flags a card without them and the approve refuses it. Advance it as you go: spec -> approve (HUMAN gate), plan -> ` + "`cerne feature start`" + `, result -> ` + "`cerne feature complete`" + ` as soon as the PR is open (never wait for CI or the merge). Abandoned? ` + "`cerne feature reject <id> --reason \"...\"`" + `.
- Picking up an ALREADY-APPROVED feature to plan? First action, before writing the plan: ` + "`cerne work start \"F-NNNN: <what> — " + planningNeutral + "\" --ref F-NNNN`" + ` (F-0208) — registers the pickup immediately, closing the blind window before ` + "`feature start`" + ` (which needs a real plan and so lags). A ` + "`cerne push`" + ` of the plan auto-opens the same work if you forget. It closes by itself at ` + "`cerne feature start`" + ` (F-0341).
- Picking up a NON-feature task (error investigation, review, ops)? First action: ` + "`cerne work start \"<description>\" [--ref F-...]`" + `; when finished, ` + "`cerne work done W-... [--note \"...\"]`" + `. BEFORE delegating an error investigation to another agent, run ` + "`cerne work list`" + ` — someone may already be on it.
- HANDOFF (F-0368): Passing the baton? ` + "`cerne work handoff W-… --next …`" + ` and pass only the HO- code. Received an HO-? First action: ` + "`cerne handoff take HO-…`" + `. Continuing a card another session started, with no HO- (a fork, an app handoff)? First action: ` + "`cerne work start \"F-NNNN: <what>\" --ref F-NNNN`" + ` — it binds your session to the card's cost.
- HUMAN GATES (F-0131): ` + "`cerne feature approve`" + ` and ` + "`cerne feature accept`" + ` are human acts by default — write the spec, ask for approval, STOP. Under [gates] accept = "merge" the human's merge of the PR is the acceptance: record it with ` + "`cerne feature accept --merged`" + `, and never merge a PR yourself. Under accept = "risk" complete accepts alone only a low-risk delivery; the rest waits for that merge, and you never force a condition to pass. To be woken when the human acts (F-0460): if your harness wakes you when a background command ends (interactive Claude Code does), leave ` + "`cerne feature wait F-… --for approved`" + ` (` + "`--for done`" + ` after the complete) running in the background, then stop; otherwise just stop.`

// GauntletRule is the GAUNTLET item — F-0270 is the protocol's origin. The
// Claude Code script prints it through 'cerne hook gauntlet-rule', which says
// nothing in light mode (F-0364).
const GauntletRule = `- GAUNTLET (F-0270): every feature is born with a bar and does not close without the loop — the spec's Verification (pt-BR: Verificação) section carries a literal bar line, ` + "`Bar:`" + ` (pt-BR: ` + "`Barra:`" + `), in the project's documentation language (approve refuses it otherwise) — the line you write is a proposal and the human approve is its acceptance, so run ` + "`cerne feature approve <id> --check`" + ` before asking for it — and ` + "`result.md`" + ` carries ` + "`## Gauntlet`" + ` with the judge's quoted verdict (` + "`cerne lint`" + ` charges it). Protocol details: cerne-authoring skill.`

const registerTail = `- DUPLICATE GUARD (F-0147): ` + "`feature new`/`work start`" + ` refuse a title similar to a live feature or active work — join that work instead; --force only for a genuinely distinct problem.`
