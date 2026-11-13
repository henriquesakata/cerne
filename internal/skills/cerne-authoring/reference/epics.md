## Epics (large work, decomposed)

An **epic is just a feature that has children** — the role is inferred from the parent link, there is no separate entity, so the epic gets the full feature lifecycle/spec/plan/result for free. Use one when a body of work is too large for a single spec and naturally splits into sub-features.

- Create a child under an epic: `cerne feature new "<title>" --context "<porquê>" --parent F-NNNN`.
- Link, move, or detach an existing feature later: `cerne feature reparent F-NNNN F-MMMM` (or `--clear`).
- **Epic ≠ module.** A module is the subsystem; an epic is decomposed work that may span modules. A feature can have both.
- **Read the breakdown, don't hand-maintain it:** `cerne status` shows an `ÉPICOS` section with `n/m` progress, and `.docs/CONTEXT.md` carries an *Épico* column plus a `## Épicos` section.
- The epic's lifecycle is independent (children completing does not auto-complete it). `cerne lint` flags inconsistencies — a live feature under a terminal epic, an epic completed with open children, or an epic still candidate/approved while a child is already in development (`epic-behind-children`: work started but the epic was never planned/started — write its plan and `cerne feature start` it).
- **Declaring intent upfront:** when a PRD is known from the start to decompose, `cerne epic new "<title>" --context "…"` mints a **declared** epic (`E-` prefix) instead of `feature new` — same lifecycle/gates, the prefix just signals the intent at birth (epic *behavior* is always structural: any feature with children). Decompose it with `cerne task new "<step>" --parent E-NNNN --context "…"` — a **task** (`T-` prefix) is a PRD execution unit already past its human gate: it is born `approved` (`cerne feature start T-NNNN` needs no plan of its own — the why lives in the parent PRD), but `cerne feature complete T-NNNN` still requires a real `result.md`. `--parent` on a task must name a committed feature/epic (approved or beyond).

**Housekeeping on a feature/epic in place (no lifecycle change):**

- `cerne feature rename F-NNNN "<new title>"` — allowed until development starts; frozen once implemented/terminal.
- `cerne feature spec F-NNNN --file <spec.md>` (or piped stdin) — replaces the whole spec of a **candidate** or an **approved** feature, same gated write the Backlog chat performs on confirmation; no state change, no retitle. On an approved feature a different spec stales the approval (F-0318, see **Plan** above). Refused past `approved`.
- `cerne feature set F-NNNN --owner <name> --priority p1` — sets owner and/or priority in place (only the flag passed changes; empty string clears); doesn't touch state, module, tags, or docs.
- `cerne feature revert F-NNNN --reason "…"` — undoes an approval, back to `candidate` (spec/plan stay intact).
- `cerne feature deliver F-NNNN --commit <sha-or-url> --mr <url>` — records the delivery commit/MR post-hoc, after the MR merges.
- `cerne feature capability F-NNNN C-0001,C-0002` — links the feature to nodes in the product-capability taxonomy (`--clear` unlinks). Manage the taxonomy itself with `cerne capability new "<name>" [--parent C-NNNN]`, browse it with `cerne capability list --tree`, retire a node with `cerne capability retire C-NNNN --reason "…"` (refuses with active children or linked features), or propose the whole tree from existing specs with `cerne capability bootstrap [--apply]`.
