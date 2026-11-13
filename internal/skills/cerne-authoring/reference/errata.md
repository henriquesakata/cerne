# Errata: the full text

## Errata (correcting a concluded, immutable entity)

A concluded entity — a `done`/`rejected`/`superseded` feature, a `superseded`/`obsolete` decision, runbook or note — is **history: its body is immutable**. When such a record carries a stale reference or needs a clarification, do **not** rewrite it and do **not** reopen it. Append an **errata** instead.

**One window before that (F-0305): the result of an `implemented` feature, before the human `accept`.** `implemented` is "awaiting validation" and the FSM has no way back, so a result found wrong in validation is corrected in place, with a reason:

```
cerne show F-0008 --doc result > result.md    # edit it
cerne feature amend F-0008 --file result.md --reason "<why>"
```

Without `--doc`, the amend replaces only `result.md` (the spec and plan of a live feature have their own amend, `--doc spec|plan`, in the feature loop), records the reason on a `feature.doc_edited` event, and is refused when the new result fails the result checks (evidence per criterion, Gauntlet record). Any other state refuses it and names its path: `.docs/` + `cerne push` while live, errata from `accept` on. A changed document of a feature past the live states is refused by `cerne push` and by the feature page alike — once the feature leaves development, `.docs/` edits no longer reach it: carry the edit through the state's path (the push refusal points `amend --file` at the local copy itself), then delete the local copy, or every later push stays refused. For everything else, errata:

```
cerne <kind> errata <id> "<text>"             # kind: feature | decision | runbook | note
cerne feature errata F-0008 --file errata.md  # or piped stdin — one-shot, like note new
cerne decision errata D-0012 "<text>"
cerne note errata N-0003 "<text>"
cerne runbook errata RB-0002 "<text>"
```

**Runbook errata read together with the body (F-0872).** A runbook's errata stay annotations, but each one can declare the step it changes, be decided, and be folded into a new revision on request. `cerne runbook errata RB-0002 "<text>" --altera S6.6` writes an `Altera:` line and prints the item id (`E<n>`, by order). `cerne show RB-0002 --consolidado` prints the body with each open item under its step (date and state: `proposta` or `decidida` with the quoted decision), and lists apart any item with no step or whose step the body lacks. `cerne runbook decide RB-0002 E2 --by <nickname> --quote "<the decision, as said>"` changes the item's state in place (no new erratum). `cerne runbook merge RB-0002` folds the decided items into the body as a new revision; the body it replaces stays readable with `cerne show RB-0002 --rev N`. Merging is explicit, never a silent edit.

**Runbook execution as a record (F-0873).** An execution window of a runbook is recorded in the backend, not in a local file. `cerne runbook run RB-0070 --janela J3` opens an active work (it shows in `cerne work list` and `cerne show W-N`) with the runbook's steps: the `S<n>` tokens in the body outside code fences, `S3 a S7` expanded, or `--passos S1,S2,...` when that list has noise; `--palavra "<word>"` sets the word expected at each step. `cerne runbook step W-N S3 --ok "<the owner's literal word>" --por <nickname>` or `--pare "<the exit>"` records the step with UTC and BRT; `--evidencia <file>` keeps only the path and the sha256 of that local file, never its content (a value that is not an existing file is refused), and e-mails or ids are refused, a nickname only. `cerne runbook report W-N` lists the deviations (PARE, out of order, a word other than the expected one); `--errata` drafts each as a proposed erratum with `Altera: <step>` on the runbook (F-0872), once. Close the window with `cerne work done W-N`.

**Runbook preconditions and preflight (F-0874).** A runbook step declares what must hold before it with a line in the body, outside code fences: `Pré-condição S11: registro F-0869` (the entity exists), `Pré-condição S11: result F-0869` (the feature has its result), `Pré-condição S11: mr rockt/hub!2150 merged` (the merge request is in that state on the forge, asked of `glab`, read-only). The step is the `S<n>` token on the line; without one it holds for every step. Anything else is free text and is listed as manual. `cerne runbook preflight RB-0070 --ate S11` lists each precondition up to that step as met, pending or manual, changing nothing and never asking production; what the cerne cannot verify (or a forge it cannot reach) is manual, never met. `cerne next` shows the same warning (`RUNBOOK PREFLIGHT`) for a run in progress whose next step has an open precondition. It is informational: it does not block the step.

**Accept queue (F-0875).** `cerne accept queue [--json]` lists the features in implemented, oldest first (by last update, as the stale-ready lint ages them), each with the Gauntlet verdict (the last `Rodada N` line, or `dispensada`), the production evidence (first `Em produção:`/`Produção:`/`Prod:` line or `##` heading with "produção") and the pendings the result declares (`PENDENTE` lines and the items of `## Débitos e follow-ups` / `## Não verificado`). A field the result does not carry reads `não informado`; nothing is inferred. Read-only: it never runs accept (F-0131); it ends with the individual `cerne feature accept <id>` commands for the human to run one by one.

The errata is **purely additive** (`content/errata/<id>.md`, one dated entry per call, in chronological order): it never touches the original `spec.md`/`plan.md`/`result.md` or ADR body. It is read back wherever the entity is consumed — shown under `cerne show <id>`, folded into the wiki synthesis (`cerne compile`), and considered by `cerne lint` (scanned regardless of state: a superseded entity an entry names is flagged as in any doc, and since no entry can be edited, a later entry carrying the marker acknowledges it — see below). The result rules count it too: a `critério N` cited in an errata entry clears `result-missing-evidence` for that N, and a complete `## Gauntlet` section in any entry clears `result-missing-gauntlet` — the path for a result debt found after the accept. Only once the feature is closed: while it is live the result itself is the document to fix, and the `complete` gate ignores the errata. There is **no** edit/rewrite command — past the accept, errata is the only correction path, by design: annotate, never rewrite.

One tolerance is automatic: an errata may **name what its own entity superseded** (e.g. F-0001's errata saying "this feature replaced F-0002") without `cerne lint` raising `ref-to-superseded` — that supersession record is legitimate history. Any **other** superseded entity an errata names is still flagged, including one that was live when the entry was written and was superseded later. No command edits an entry, so the old line can never take the same-line marker; acknowledge it by **appending** an entry that carries the marker for that id:

```
cerne <kind> errata <id> '<!-- lint:ok-superseded X --> <why the mention is history>'
```

Use single quotes: inside double quotes an interactive zsh expands the marker's `!` before cerne runs. For several ids, list them in one marker — `<!-- lint:ok-superseded X, Y -->` — since only the first marker on a line counts. The finding itself names this command, with the owner's kind and id filled in.

A marker in a later entry covers every mention of X in the entries **before** it. It never covers a later entry, and inside its own entry the same-line rule holds: keep any mention of X in the new entry on the marker's line. If the old entry is wrong rather than dated, say what is current on that same line.
