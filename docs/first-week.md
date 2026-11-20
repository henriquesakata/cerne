# Your first week with cerne

You installed cerne and ran `cerne init` (or imported existing docs). This page is what to do next, in the order you will need it. Commands you type yourself are shown as code; the rest is what your agent runs for you.

The shortest path: ask your agent for a small feature, approve its spec, accept the delivery. `cerne next` and the dashboard tick those three steps for you until the first accept.

## After init and after an import

Inside your project's repository (skip this if you already ran it):

<!-- quickstart -->
```bash
cerne init --name my-project
```

`init` ends with the same three steps it shows in `cerne next`:

```text
Next steps:
  1. Open the dashboard: cerne ui  (http://localhost:7717)
  2. Ask your agent: "Use cerne to plan <a small change you want>: register the feature and write its spec."
  3. Approve the spec on the dashboard.
Agents connected: claude. To connect cursor, codex, gemini, copilot: cerne hook install --provider <name>, then cerne skill install
Commit .cernerc, docs.lock, .gitattributes and .gitignore to your project repository.
First week: https://github.com/henriquesakata/cerne/blob/main/docs/first-week.md
```

Commit the four files it names. Then ask `cerne next` where you are:

```text
FIRST STEPS (guide: https://github.com/henriquesakata/cerne/blob/main/docs/first-week.md):
  [ ] Ask your agent: "Use cerne to plan <a small change you want>: register the feature and write its spec."
  [ ] Approve the spec: cerne ui → Kanban, or cerne feature approve <id>
  [ ] Accept the delivery: cerne feature accept <id>
```

Each step is ticked from the project's event log. Nothing is stored, and the block disappears after the first accept. The dashboard (`cerne ui`) shows the same block.

If the project already had docs, `cerne import --agent` maps them (see the README). When you apply the plan with `--yes`, it ends by saying what came in:

```text
What came in:
  features 1: 1 implemented
  decisions 1: 1 active
Next steps:
  1. Search what came in:  cerne search "Postgres"
  2. Ask your agent for the first feature:
     "Use cerne to plan <a small change you want>: register the feature and write its spec."
```

On a terminal, an import that brings delivered features asks once whether they enter as `implemented` (delivered, waiting for your accept) or `done` (already validated). In a script, pass `--delivered-state implemented|done`. Imported features that are `implemented` count as waiting for accept; `done` ones do not.

## Connect your agent

`init` connects Claude Code on its own. For the others, add the provider and then the skill:

<!-- quickstart -->
```bash
cerne hook install --provider cursor,codex,gemini,copilot
cerne skill install
```

Pick only the providers you use. What each one receives:

| Agent | What lands in your project |
|---|---|
| Claude Code | Both skills in `.claude/skills/`, the hooks and `CERNE_ACTOR=agent` in `.claude/settings.json` (always installed by `init`) |
| Cursor | `.cursor/rules/cerne.mdc`, `.cursor/hooks.json`, the guide in `.cursor/skills/` |
| Codex CLI | Hooks, `.codex/config.toml`, the guide in `.agents/skills/` and a pointer in `AGENTS.md` |
| Gemini CLI | `.gemini/settings.json` and `.gemini/.env`, the guide in `.gemini/skills/` and a pointer in `GEMINI.md` |
| GitHub Copilot | `.github/instructions/cerne.instructions.md`, `.github/hooks/cerne.json`, the guide in `.github/skills/` |
| Any MCP client | `cerne mcp` over stdio, read-only (see the README) |

Some hooks are degraded on some agents. `cerne hook install --provider cursor` says which ones it skipped and why; the [provider parity matrix](provider-parity.md) lists all of them. cerne cannot check that an agent loads what it installed, so ask yours once: "What does cerne's authoring guide tell you to do when I ask for a feature?"

## Ask for the first feature and approve it

Ask your agent in plain words, for a small change:

> Use cerne to plan a "Sign in with Google" button: register the feature and write its spec.

The agent registers a candidate and fills in the spec. By hand, the same thing is:

<!-- quickstart -->
```bash
cerne feature new "Sign in with Google" --context "Users asked for social login."
cerne brief
```

```text
F-0001 created (candidate) with its context seeded into the spec.
FEATURES (1)
  F-0001  candidate  Sign in with Google
```

Before you approve, read the spec in the dashboard (Kanban, then the card) or in `.docs/current/features/F-0001-…/spec.md` after `cerne pull`. A spec must be written and tagged before it can be approved. `--check` tells you what is missing, and changes nothing. Right after `feature new` it refuses (exit 1):

```text
$ cerne feature approve F-0001 --check
Check: F-0001 is not approvable yet — 1 impediment(s) (nothing approved, nothing written):
  - untagged: no tag (facet/component) — classify it with 'cerne feature retag F-0001 --facet <f> --component <c>' ...
Complete the card with the commands above, then check again.
Error: approve check found impediments: F-0001 has 1 (0 hard)
```

What makes it approvable: the spec is really written (your agent does this), it carries the `Bar:` line when the Gauntlet is on (a new project starts in light mode, which asks for no bar), and the card is tagged, with `--facet`/`--component` at creation or afterwards:

```bash
cerne feature retag F-0001 --facet code --component auth
```

Then the check passes:

```text
$ cerne feature approve F-0001 --check
Check: F-0001 is approvable (nothing approved, nothing written).
```

Approving is your call, not the agent's: click Approve on the card, or run `cerne feature approve F-0001` in your own terminal. An agent that tries gets this refusal and stops:

```text
Error: human gate: this transition is a human act in this project — ask a human for the approval/acceptance ...
```

After the approval the agent starts the feature, builds it and completes it, which puts the card in Ready. You then check the work and run `cerne feature accept F-0001` (or use the dashboard). That is the third step of `cerne next`.

## Change your mind and find what went out of force

Plans change. Nothing is deleted; a card that leaves the current state keeps its history.

- A candidate you no longer want: `cerne feature reject <id> --reason "<why>"`. Write the reason; it is what the next person reads.
- A feature replaced by another: `cerne feature supersede <id> --by <new-id> --reason "<why>"`.
- A decision replaced by another: `cerne decision supersede` (same idea).

<!-- quickstart -->
```bash
cerne feature new "Sign in with GitHub" --context "Same as Google, for developers."
cerne feature reject F-0002 --reason "Google covers our users; revisit if developers ask."
```

```text
F-0002: candidate → rejected
```

Rejected and superseded entities leave `.docs/` and `cerne status`, so your agent stops reading them. They stay one search away:

<!-- quickstart -->
```bash
cerne search github
```

```text
F-0002 [rejected] Sign in with GitHub — spec
```

`cerne search` covers the whole history, including what is no longer in force; the state is in brackets.

## Search, and when to compile the wiki

Use `cerne search "<words>"` when you know a word from the thing you want. Use `cerne ask "<question>"` for a question in plain language; it ranks the sources and cites them. `cerne brief` is the cheap map of every entity: `id  state  title`. `cerne guide "<question>"` answers how to use cerne itself.

The wiki is a per-module summary written from your current docs. You do not need it on day one. Compile when the project has a few accepted features or an import you want summarized:

```bash
cerne compile
```

```text
1 page(s) to compile (prompts in .docs/wiki/.pending/):
  project → .docs/wiki/.pending/project.md
Contract in .docs/wiki/.pending/manifest.json: write each response_path (echoing run_id: <id> in the frontmatter) and run 'cerne compile --ingest'.
```

Whether `compile` calls a model itself depends on `llm.mode`. With none set, it uses the first subscription CLI it finds on your `PATH` (`claude`, `codex`, `gemini`, then `cursor-agent`), or the API key set in Settings, and writes the pages itself; if that CLI is not logged in, it fails and says so. With `llm.mode` set to `delegation` (`cerne settings set llm.mode delegation --scope host`), or no CLI and no key, it only writes the prompts, as above. Your agent answers them, then you run `cerne compile --ingest`. Running it again only redoes the pages whose inputs changed.

## Create and work the backlog

The backlog is whatever you have not started. There are three kinds of card, from loosest to most committed:

- An idea: raw material nobody evaluated. The title is all it takes.
- A candidate feature: a demand worth a spec, as above.
- A task or a bug: an execution unit. A task decomposes an approved feature (`--parent` is required); a bug you ask for is born approved.

<!-- quickstart -->
```bash
cerne idea new "Dark mode"
```

```text
I-0001 created (active) — Dark mode
```

```bash
cerne idea promote I-0001      # becomes a candidate feature
cerne task new "<step>" --parent F-0001 --context "<what this step does>"
cerne bug new "<what breaks>" --context "<what happens, where>" --on-behalf "<your words>"
```

To work one: ask your agent to start it. The agent runs `cerne feature start <id>` and, for work that is not a card (a review, an investigation), `cerne work start "<what>" --ref <id>`. That registers what is in progress, so two agents do not do the same thing:

<!-- quickstart -->
```bash
cerne work start "Add Google sign-in button" --ref F-0001
cerne work list
cerne work done W-0001 --note "button shipped"
```

## The daily routine

1. Start with `cerne next`: what needs a spec, an approval, work, or your accept. Below it are the documentation debts.
2. Approve the candidates whose spec you read, in the dashboard or with `cerne feature approve <id>`.
3. Let the agent work. When it completes a feature, review it and accept it (`cerne accept queue` lists them).
4. When something changes your plan, reject or supersede with a reason.
5. `cerne sync` brings in what others changed and sends your own edits. `cerne lint` lists stale documentation.

When you forget a command, ask: `cerne guide "how do I close a feature"`.
