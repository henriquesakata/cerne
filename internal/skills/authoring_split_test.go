package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// maxSkillBytes is the budget of the SKILL.md every agent loads each session
// (F-0399 criterion 1).
const maxSkillBytes = 30 * 1024

// referenceDir is where the detail that left SKILL.md lives, read from disk so
// these tests do not depend on the embed.
// minSentence is the shortest unit the sentence check trusts.
const minSentence = 25

const referenceDir = "cerne-authoring/reference"

// references returns the reference files' contents by file name.
func references(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(referenceDir)
	if err != nil {
		t.Fatalf("reference dir: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(referenceDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(b)
	}
	return out
}

// fullAuthoring is SKILL.md followed by every reference file: the whole
// authoring guide, wherever a passage now lives.
func fullAuthoring(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("cerne-authoring/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.Write(b)
	refs := references(t)
	for _, name := range sortedKeys(refs) {
		sb.WriteString("\n" + refs[name])
	}
	return sb.String()
}

func sortedKeys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	for i := range ks {
		for j := i + 1; j < len(ks); j++ {
			if ks[j] < ks[i] {
				ks[i], ks[j] = ks[j], ks[i]
			}
		}
	}
	return ks
}

// TestSkillMdFitsTheBudget pins criterion 1: without a number the skill grows
// back section by section.
func TestSkillMdFitsTheBudget(t *testing.T) {
	if n := len(All()[0].Content); n > maxSkillBytes {
		t.Fatalf("SKILL.md is %d bytes, the budget is %d", n, maxSkillBytes)
	}
}

// TestSkillMdKeepsTheRules pins criterion 2: each rule an agent must not
// forget stays, as a whole sentence, in the file every session loads. Loose
// tokens would not do: the catalog names every command, so a deleted rule
// would still match.
func TestSkillMdKeepsTheRules(t *testing.T) {
	c := All()[0].Content
	rules := map[string][]string{
		"human gates": {
			"Do not run `cerne feature approve` yourself — an agent approving its own spec defeats the gate.",
			"Do not run `cerne feature accept` yourself either — report the feature is implemented and ask a human to accept it.",
			"**Only run `cerne feature approve`/`accept` yourself when `[gates]` explicitly sets that gate to `\"auto\"`**",
		},
		"complete card":           {"**Complete card at birth:** the spec, the tags", "`capability-missing`"},
		"never import a new case": {"not for registering a single new case"},
		"external source":         {"register it — as you use it, **without waiting to be asked**", "Register **only the sources that actually informed the outcome**"},
		"note over local memory":  {"**Durable, team-relevant project knowledge → `cerne note new`.**", "**Personal or machine-specific knowledge → stays in local Claude Code memory, never pushed.**"},
		"the cycle":               {"**Register at the start, not the end:**", "then `cerne feature complete F-NNNN` (gate: a real result)", "`cerne feature accept` is the only way to the actual `done`."},
		"lint before the push":    {"`cerne lint --staged` — runs every lint rule over your **local** `.docs/` edits"},
		"the Gauntlet": {
			"A silent absence of a bar is never acceptable.",
			"are three separate agents; one agent playing two collapses it into self-scoring.",
			"Hand the critic one file path under the rule \"read exactly this file, do not run git, do not read the repository\".",
			"Never fix the bar inside the delivery it is judging: route a hole in the bar as a separate improvement (a new feature, or a note).",
			"A `Round N` line carries the judge's quoted verdict; a critic-only pass (no judge) goes on the `Critic findings` line, never on a `Round N` line.",
			"`cerne feature complete` refuses a result whose record `cerne lint` would flag (a numbered criterion never cited, or no valid `## Gauntlet`), and `--force` does not cross it.",
		},
		"errata for the concluded":     {"is **history: its body is immutable**", "do **not** rewrite it and do **not** reopen it. Append an **errata** instead"},
		"do not turn the Gauntlet off": {"Do not switch it yourself to get past a gate: turning the Gauntlet off is the project owner's decision"},
		"triage the learnings":         {"**Closing includes triaging the learnings (F-0401).**"},
		"mark ambiguity":               {"**Mark ambiguity, don't guess:** leave `[CLARIFY: specific question]`"},
		"project language":             {"Write every document in the project's language: check the headings of the stub you are filling."},
		"register non-feature work":    {"The moment you pick up a non-feature task, register it with `cerne work start", "run `cerne work list` before delegating an investigation"},
		"ideas on demand":              {"Ideas are on demand only: never pull ideas in unasked."},
		"pendings":                     {"`cerne feature complete` refuses a result that does not cite every pending the feature pays", "a promoted pending is never closed by hand"},
		"the pointers":                 {"`cerne guide`", "reference/"},
	}
	for rule, passages := range rules {
		for _, p := range passages {
			if !strings.Contains(c, p) {
				t.Errorf("rule %q: SKILL.md lost %q", rule, p)
			}
		}
	}
}

// rewritten are the sentences changed on purpose since the split: F-0399's
// guide pointer at the top of SKILL.md now describes the catalog and the
// reference files; F-0402's manual stamp step is now done by
// `cerne feature complete` itself; F-0418's bar forms follow the project's
// documentation language instead of being pt-BR only.
var rewritten = []string{
	// F-0482: a retag is accepted in any state, so the "active only" wording went.
	"Re-tag in place — any state except terminal history",
	"Re-tag an active decision in place with",
	"Re-tag an active note in place with",
	"Re-tag a current reference in place with",
	"`cerne guide \"<question>\"` answers about any cerne feature",
	"Between the two, run `cerne cost F-NNNN --stamp`",
	// F-0418: the forms follow the project's documentation language.
	"The spec's **Verificação** section then carries exactly one of three literal forms",
	// F-0419: the idea lifecycle (title + body, resolve, link, unlink, state
	// filters) rewrote the idea capture/read/drop/promote bullets.
	"- **Capture:** `cerne idea new",
	"- **Read:** `cerne idea list` (active only",
	"- **Drop:** `cerne idea drop I-NNNN",
	"- **Promote:** `cerne idea promote I-NNNN",
	"Prints both ids. A dropped or promoted idea is refused",
	// F-0442: the default work actor is a machine pseudonym, not <user>@<hostname>.
	"Actor defaults to `<user>@<hostname>`",
	// B-0041: pull merges different lines of one document; push refuses a
	// document while its remote copy is stashed in .docs/.conflicts/.
	"Disjoint documents reconcile automatically; a real **same-document** conflict",
	"- **`cerne pull`** 3-way merges: it brings in the remote advance while keeping your unsent `.docs/` edits (it does",
	// B-0054: the prompt and every shell command became channel contact
	// points, and a human message is addressed to every live work.
	"Pending messages reach you on their own at session start, in subagents, in `cerne next`",
	// E-0005: a session starts from cerne next; CONTEXT.md is the map of what
	// is live, not the first read.
	"Read current state from .docs/CONTEXT.md and `cerne status`.",
	"Read current state from `.docs/CONTEXT.md` and `cerne status` (or `cerne brief`",
	// E-0005: CONTEXT.md counts the notes and lists their components.
	"Active notes also appear in the `## Conhecimento` section of `.docs/CONTEXT.md`",
	"Tags render inline in `note list`/`note show`, fold into the FTS body",
	// E-0005 (T-0017): the MCP server serves the context tool too.
	"An MCP client without a shell reads the same surface through **`cerne mcp`** (F-0363): a stdio MCP server whose read-only tools",
	// F-0499: revert also walks back from Ready and Done.
	"- `cerne feature revert F-NNNN --reason \"…\"` — undoes an approval, back to `candidate`",
	// B-0073: the accept queue prints English whatever the project's language.
	"A field the result does not carry reads `não informado`",
	// F-0510: idea promote classifies the card itself; only the spec follows.
	"From there the candidate is a regular demand: complete its spec and tags in the same act",
	// B-0084: room examples keep the capability off the command line; join
	// and open store it for the agent session.
	"export CERNE_ROOM_SESSION=<printed by join>",
	"cerne room say <id> \"<msg>\" --session <token>",
	"cerne room watch <id> --session <token> [--once]",
	"cerne room close <id> --facilitator <token> --minutes <...>",
	"`--session`/`CERNE_ROOM_SESSION` is required (same convention as `say`)",
	"**Facilitation and structured close (F-0228).** `cerne room open` also mints a **facilitator capability**",
	"cerne room propose <id> --session <token>",
	"cerne room request-decision <id> --session <token>",
	"cerne room pending <id> --facilitator <token>",
	"cerne room resolve <id> <item-id> --facilitator <token>",
}

// b0015Renames maps the pt-BR names and placeholders the 2026-11-13 snapshot
// teaches to the English ones B-0015 put in the skill, so the snapshot stays
// a historical golden while the comparison follows the rename.
var b0015Renames = strings.NewReplacer(
	"substituir", "supersede", "--altera", "--step", "--sem-passo", "--no-step",
	"--consolidado", "--consolidated", "--janela", "--window", "--passos", "--steps",
	"--palavra", "--word", "--pare ", "--stop ", "--por ", "--by ", "--evidencia", "--evidence",
	"--esperada", "--expected", "<1-3 frases do porquê>", "<1-3 sentences on why>",
	"<porquê>", "<why>", "já sincronizada", "already synced", "<apelido>", "<nick>",
)

var sentenceEnd = regexp.MustCompile(`(?:[.!?])\s+`)

// TestNoSentenceOfTheOldSkillIsLost pins that the split moved text, never
// dropped it: every sentence of the 35 sections of 2026-11-13 is still in
// SKILL.md or a reference file.
func TestNoSentenceOfTheOldSkillIsLost(t *testing.T) {
	old, err := os.ReadFile("testdata/skill-before-f0399.md")
	if err != nil {
		t.Fatal(err)
	}
	now := fullAuthoring(t)
	lost := 0
	for _, line := range strings.Split(b0015Renames.Replace(string(old)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		for _, s := range sentences(line) {
			if s = strings.TrimSpace(s); len(s) > 3 && !strings.Contains(now, s) && !isRewritten(s) {
				lost++
				if lost <= 5 {
					t.Errorf("sentence lost: %.120q", s)
				}
			}
		}
	}
	if lost > 0 {
		t.Fatalf("%d sentences lost", lost)
	}
}

// f0475Rewritten are the old sentences F-0475 re-presented English-first (the
// pt-BR form moved beside the English one), each by its first 100 characters.
// Naming them keeps the guard strict: any other lost sentence still fails.
var f0475Rewritten = []string{
	"**Write every document in the project's language**: check the headings of the stub",
	"The sections and literal forms below are shown in Portuguese",
	"| Document | Portuguese sections | English sections |",
	"| Literal form | Portuguese | English |",
	"| EARS criterion |",
	"The minimal path is `--context` (1-3 sentences of the *why*, seeded into the spec's Problema section",
	"2. **Spec** — `cerne pull`, author `.docs/current/features/F-NNNN-<slug>/spec.md` (keep the `cerne_i",
	"| Spec | Problema · Objetivo e critérios de aceitação · Decisões do design · Desenho · Verificação ·",
	"| Plan | Estratégia · Sequência de tarefas · Dependências e ordem · Riscos e rollback · Restrições p",
	"| Result | Resultado · Aceitação · O que mudou · Débitos e follow-ups · Aprendizados e desvios · Gau",
	"| Decision | Contexto · Decisão · Consequências · Alternativas consideradas · Implementação e relaçõ",
	"| Runbook | Quando usar · Pré-requisitos · Procedimento · Verificação · Rollback e troubleshooting |",
	"| Ambiguity marker | `[ESCLARECER: …]` / `[ESCLARECIDO: …]` | `[CLARIFY: …]` / `[CLARIFIED: …]` |",
	"| Bar | `Barra: <referência + acesso>` | `Bar: <named reference + access>` |",
	"| Method and exit lines | `Método:` / `Saída:` | `Method:` / `Exit:` |",
	"| No applicable bar | `Barra: não aplicável (<justificativa>) — padrão alternativo: <…>` | `Bar: not",
	"| Waived by a human | `Barra: dispensada por decisão humana (<citação>)` | `Bar: waived by human dec",
	"| Evidence citation | `critério N`, `critérios N-M` | `criterion N`, `criteria N-M` |",
	"| Gauntlet round | `Rodada N — <vitória\\|derrota\\|empate>: \"<citação>\"` | `Round N — <win\\|loss\\|tie",
	"| Critic findings | `Achados do crítico: <n> — …` | `Critic findings: <n> — …` |",
	"| Judge's fixed last line | `VEREDITO: A` / `B` / `EMPATE` | `VERDICT: A` / `B` / `TIE` |",
	"1. **Problema** — what is missing or broken **today**, grounded in real code (`file.go:line`); why n",
	"2. **Objetivo e critérios de aceitação** — one objective sentence, then **numbered, testable** crite",
	"For behaviour, use EARS — *\"QUANDO `<condição>` O SISTEMA DEVE `<comportamento>`\"*; structural invar",
	"3. **Decisões do design** — a `| Fork | Decisão | Razão |` table; record the rejected alternatives, ",
	"4. **Desenho** — by module/package, grounded in real files, with explicit contracts (signatures, sch",
	"5. **Verificação** — the test plan (TDD), 1:1 with the acceptance criteria.",
	"6. **Fora de escopo** — what you will **not** do now, and why (or what becomes a follow-up).",
	"**Ordering is deliberate, not cosmetic.** Problema and the acceptance criteria sit at the top and Ve",
	"**Mark ambiguity, don't guess.** Wherever something is unresolved, leave `[ESCLARECER: pergunta espe",
	"1. **Estratégia** — the approach in 2-3 sentences and how the work is sliced (single MR or N phases)",
	"2. **Sequência de tarefas** — small, **ordered** tasks, each leaving the repo green.",
	"Mark `[paralela]` when a task is independent.",
	"3. **Dependências e ordem** — what unblocks what (the real order, not just a list), which tasks are ",
	"4. **Riscos e rollback** — per risk: how to detect + how to mitigate; and how to revert (each commit",
	"5. **Restrições para o agente** — execution guardrails: what **not** to touch, decisions already set",
	"6. **Definição de pronto** — a **binary** checklist (each item MET/UNMET with `file:line` evidence, ",
	"**Ordering is deliberate, not cosmetic.** Estratégia and the Sequência de tarefas sit at the top and",
	"**Mark ambiguity, don't guess** — leave `[ESCLARECER: pergunta específica]` and resolve every marker",
	"1. **Resultado** — what shipped and the state (delivered / partial), in 1-2 sentences. The bottom li",
	"2. **Aceitação** — each numbered spec criterion → **MET/UNMET (ATENDIDO/PENDENTE)** with **evidence*",
	"3. **O que mudou** — the concrete changes from the reader's perspective: files/modules created and e",
	"4. **Débitos e follow-ups** — what's incomplete or deferred, as concrete work (a new feature/case), ",
	"5. **Aprendizados e desvios** — what you learned and any intentional deviations from the spec/plan (",
	"**Ordering is deliberate** (primacy/recency): Resultado and Aceitação lead — the outcome and its pro",
	"**Mark ambiguity, don't guess** — `[ESCLARECER: pergunta específica]`.",
	"In light mode `cerne feature approve` takes a spec with no `Barra:` line, `cerne feature complete` t",
	"What stays: the per-criterion evidence (`result-missing-evidence` — the result still cites `critério",
	"The bar you write is a proposal: put it in the spec's `Barra:` line yourself, with no acceptance rou",
	"Filled in, the whole `<…>` gives way to the value — `Barra: modo spec do Kiro — kiro.dev/docs/specs;",
	"A named bar also needs its judge question: a `Pergunta ao juiz: <a pergunta que o juiz cego responde",
	"— or the single line `Barra: não aplicável (<justificativa de 1 linha>) — padrão alternativo: <teste",
	"— or the single line `Barra: dispensada por decisão humana (<citação curta da fala que dispensou>)`,",
	"It pays on a change of behaviour, of contract, of persisted format, or of UI with a design choice; t",
	"`cerne feature approve F-NNNN --check` reads the spec's `Nível:` line and the stamped `## Custo` anc",
	"Ask one question — the job, named (\"which text instructs an implementing agent better\", \"which rule ",
	"`result.md` maps each numbered spec criterion to its evidence and must cite the number in words — `c",
	"Recording the loop: `cerne gauntlet record F-NNNN --verdict <judge-reply-file> --ours A|B` writes th",
	"A critic-only pass (no judge) goes on the `Achados do crítico` line, never on a `Rodada N` line — a ",
	"1. **Contexto** — the forces and constraints in tension and the trigger (why decide now).",
	"2. **Decisão** — the choice stated imperatively (\"Vamos X\" / \"Usaremos X\") with the two or three rea",
	"3. **Consequências** — what follows: positives, **negatives/trade-offs (always list them)**, and con",
	"4. **Alternativas consideradas** — the options weighed (2+ when there is a real trade-off), each wit",
	"5. **Implementação e relações** — where the decision shows up in code (real files/modules) and the l",
	"**Ordering is deliberate** (primacy/recency): Contexto and Decisão lead; Implementação e relações is",
	"**Status is data, not prose** — the lifecycle is owned by cerne (`cerne decision supersede`), so the",
	"1. **Quando usar** — the trigger, symptoms or goal: the situation that fires this runbook, so the re",
	"2. **Pré-requisitos** — access/tokens, tools and versions, environment state, window/approvals to co",
	"3. **Procedimento** — numbered, action-first steps with copy-pasteable commands, each with its expec",
	"4. **Verificação** — how to confirm success: observable checks/signals with expected output.",
	"5. **Rollback e troubleshooting** — how to undo/recover (flag destructive steps), common failures as",
	"The plan template prompts this lookup in Estratégia and asks you to **cite the applicable N-NNNN ids",
	"- **Register pendings when you close a feature:** for every debt you would write in the result's *Dé",
	"**Accept queue (F-0875).** `cerne accept queue [--json]` lists the features in implemented, oldest f",
	"The result rules count it too: a `critério N` cited in an errata entry clears `result-missing-eviden",
	// T-0020: room watch --once now waits out --settle before it exits.
	"`--once` long-polls until the first **relevant** (non-echo) event, prints it, and exits 0",
}

func isRewritten(s string) bool {
	for _, r := range f0475Rewritten {
		if strings.HasPrefix(s, r) {
			return true
		}
	}
	for _, r := range rewritten {
		if strings.HasPrefix(s, r) {
			return true
		}
	}
	return false
}

// sentences splits a line at sentence boundaries; a fragment shorter than
// minSentence ("e.g", "Ok") is kept together with what follows it, so the
// original text is always a substring of the unit.
func sentences(line string) []string {
	var out []string
	start := 0
	for _, m := range sentenceEnd.FindAllStringIndex(line, -1) {
		if m[0]+1-start >= minSentence {
			out = append(out, line[start:m[0]+1])
			start = m[1]
		}
	}
	if rest := line[start:]; rest != "" {
		if len(out) > 0 && len(rest) < minSentence {
			out[len(out)-1] += " " + rest
		} else {
			out = append(out, rest)
		}
	}
	return out
}

// TestSentencesDoNotSplitOnAbbreviationsOrFragments: a fragment such as
// "e.g" must never be a unit of the check, or it would match anywhere.
func TestSentencesDoNotSplitOnAbbreviationsOrFragments(t *testing.T) {
	got := sentences("Use a flag, e.g. the first one, to start. Then do the next thing in the list. Ok. Stop here and think about it.")
	for _, s := range got {
		if len(strings.TrimSpace(s)) < minSentence {
			t.Errorf("fragment %q is too short to be a sentence", s)
		}
	}
	if len(got) != 3 {
		t.Errorf("sentences = %q, want 3", got)
	}
}
