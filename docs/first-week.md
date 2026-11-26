# Your first week with cerne

You installed cerne and ran `cerne init` (or imported existing docs). This page is what to do next, in the order you will need it. Commands you type yourself are shown as code; the rest is what your agent runs for you.

The shortest path: ask your agent for a small feature, approve its spec, accept the delivery. `cerne next` and the dashboard tick those three steps for you until the first accept.

What your agent reads: it starts a session with `cerne next`, and a task with that task's package, `cerne context <id>`. `.docs/CONTEXT.md` stays as the map of what is live, for when it needs the whole picture.

## After init and after an import

Inside your project's repository (skip this if you already ran it):

<!-- quickstart -->
```bash
cerne init --name my-project
```

`init` ends with the steps to take now: the dashboard, the request to your agent and the approval. `cerne next` then adds the accept step:

```text
Gauntlet is off (light mode). Turn it on in the Settings screen of cerne ui, or run: cerne settings set gates.gauntlet on --scope project
Next steps:
  1. Open the dashboard: cerne ui  (http://localhost:7717)
  2. Ask your agent: "Use cerne to plan <a small change you want>: register the feature and write its spec."
  3. Approve the spec on the dashboard.
Agents connected: claude. To connect cursor, codex, gemini, copilot: cerne hook install --provider <name>, then cerne skill install
Commit .cernerc, docs.lock, .gitattributes and .gitignore to your project repository.
First week: https://github.com/henriquesakata/cerne/blob/main/docs/first-week.md
```

When another agent's CLI is on your `PATH` (Cursor, Codex, Gemini, Copilot), a step 2 asks you to connect it before the first request; see [Connect your agent](#connect-your-agent).

The Gauntlet is an optional quality loop: each spec names a bar, a real reference the delivery is compared against, and the result quotes a judge's verdict against that bar. A new project starts with it off (light mode), so nothing in this guide asks you for a bar. Turn it on when you want deliveries measured against something outside their own spec.

Commit the four files it names. `init` also wrote `.claude/` for Claude Code: commit `.claude/settings.json` and `.claude/hooks/` too, so a teammate's Claude Code gets the same hooks. `.claude/skills/` can stay out of git (add it to `.gitignore`), because `cerne skill install` recreates it on each clone. Then ask `cerne next` where you are:

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
First-week guide: https://github.com/henriquesakata/cerne/blob/main/docs/first-week.md
```

When the import brings delivered features and you gave no `--delivered-state`, the import asks on a terminal dry-run, never with `--yes`: it asks once whether they enter as `implemented` (delivered, waiting for your accept) or `done` (already validated). In a script or an agent run (no terminal) the dry-run does not ask; it prints `N delivered feature(s) will enter as implemented; add --delivered-state done to enter them as done.`, so pass `--delivered-state implemented|done` when you apply. Imported features that are `implemented` count as waiting for accept; `done` ones do not.

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
| Cursor | `.cursor/rules/cerne.mdc`, `.cursor/hooks.json`, `.cursor/sandbox.json` (cerne's folders), `.cursor/mcp.json` (the read-only `cerne mcp` server), the guide in `.cursor/skills/`; see [Cursor](#cursor) below |
| Codex CLI | Hooks, `.codex/config.toml` (with the sandbox profile), the guide in `.agents/skills/` and a pointer in `AGENTS.md`; see [Codex](#codex) below |
| Gemini CLI | `.gemini/settings.json` and `.gemini/.env`, the guide in `.gemini/skills/` and a pointer in `GEMINI.md` |
| GitHub Copilot | `.github/instructions/cerne.instructions.md`, `.github/hooks/cerne.json`, the guide in `.github/skills/` |
| Any MCP client | `cerne mcp` over stdio, read-only (see the README) |

Some hooks are degraded on some agents. `cerne hook install --provider cursor` says which ones it skipped and why; the [provider parity matrix](provider-parity.md) lists all of them. cerne cannot check that an agent loads what it installed, so ask yours once: "What does cerne's authoring guide tell you to do when I ask for a feature?"

### Codex

Codex runs every command in a sandbox that writes only your project, `/tmp` and `$TMPDIR`. cerne keeps its cache and a local backend in your home, so out of the box every cerne command fails inside Codex. `cerne hook install --provider codex` fixes that in two steps:

- It adds a `cerne` profile to the project's `.codex/config.toml` (`default_permissions = "cerne"`, extending `:workspace`) that lets Codex write cerne's cache folder and your local backend folder. Paths use `~/`, so the file works on every machine of your team, and your own lines in the file are kept. The project's `default_permissions = "cerne"` replaces your own default profile inside this project; it extends `:workspace`, and `-s`/`-P` on the command line still win. If the file already sets `default_permissions`, if it is not valid TOML, or if any folder is specific to this machine (`CERNE_CACHE_DIR`, `XDG_CACHE_HOME`, a local backend outside your home), the install writes nothing to the project file and prints one snippet with all the folders to paste into `~/.codex/config.toml`, plus the alternative `codex --add-dir <folder>`.
- Codex reads `.codex/` only in a trusted project, and the install tells you so. Trust the project by adding this to `~/.codex/config.toml` (the install prints it with your path):

```toml
[projects."/path/to/your/project"]
trust_level = "trusted"
```

Check it from inside a Codex session: `printenv CERNE_ACTOR` prints `agent`, and `cerne next` runs with no permission error.

The sandbox also cuts the network, because `:workspace` has none. Inside Codex, `cerne pull` and `cerne push` with a remote backend (SSH or HTTPS) and the AI layer (`plan draft`, `result draft`, `ask`, `compile`, which call your subscription CLI and fail with `Not logged in`) fail, and cerne's error says the Codex sandbox may be cutting the network. To open it, run `cerne hook install --provider codex --network`: it adds `[permissions.cerne.network]` with `enabled = true` to the profile, also in a project that already has it. This opens the whole network to the agent in this project, because Codex has no domain list. That is why it is never the default, and `cerne init` never suggests it. To turn it off, set `enabled = false` (or delete the table) in `.codex/config.toml`.

### Cursor

Cursor runs an agent command that is not on your allowlist in a sandbox that writes only the workspace and `/tmp` and denies the network. A chained command (`cerne next && git status`) or a pipe falls into it, and cerne fails there, even to read, with `open ~/Library/Caches/cerne/<project>/repo.lock: operation not permitted`. `cerne hook install --provider cursor` fixes the folders and connects the MCP server:

- It adds cerne's cache folders (`~/Library/Caches/cerne/<project id>` and `~/.cache/cerne/<project id>`) and a local backend inside your home to `additionalReadwritePaths` in the project's `.cursor/sandbox.json`. Paths use `~/`, so the file works on every machine of your team; your own keys and paths are kept, and Cursor joins the file with your `~/.cursor/sandbox.json`. If the file is not valid JSON, or a folder is specific to this machine (`CERNE_CACHE_DIR`, `XDG_CACHE_HOME`, a local backend outside your home), the install writes nothing to the project file and prints the block to merge into `~/.cursor/sandbox.json`.
- It declares the read-only `cerne mcp` server in `.cursor/mcp.json`, the only project MCP file Cursor reads (it ignores the root `.mcp.json`), so the agent reads the board through MCP calls instead of the shell. Your other servers are kept; a different `cerne` entry is left as it is.

The sandbox also denies the network, so inside Cursor `cerne pull` and `cerne push` with a remote backend fail, and cerne's error says the Cursor sandbox may be cutting the network. Run `cerne hook install --provider cursor --network`: it adds only the backend's host (`github.com` for `git@github.com:you/docs.git`) to `networkPolicy.allow`, sets no `default`, and never overrides the same host in your `networkPolicy.deny` (it writes nothing and says why). With a local backend there is no host to allow, so nothing opens. The AI layer (`plan draft`, `ask`, `compile`) calls your provider's hosts, which you add to `networkPolicy.allow` yourself.

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

Before you approve, read the spec in the dashboard (Kanban, then the card) or in `.docs/current/features/F-0001-…/spec.md` after `cerne pull`. `--check` tells you what still blocks the approval, and changes nothing. Right after `feature new` it refuses (exit 1):

```text
$ cerne feature approve F-0001 --check
Check: F-0001 is not approvable yet — 1 impediment(s) (nothing approved, nothing written):
  - untagged: no tag (facet/component) — classify it with 'cerne feature retag F-0001 --facet <f> --component <c>' ...
Complete the card with the commands above, then check again.
Error: approve check found impediments: F-0001 has 1 (0 hard)
```

The check looks at the card's shape, not at the quality of the spec. It refuses a spec that is still the empty template, open `[CLARIFY: …]` questions, a card with no tag, a missing `Bar:` line when the Gauntlet is on, and a card with no capability once the project has a capability taxonomy. The context you gave at creation already counts as content, so the check can pass while the goal and the criteria are still empty: reading the spec before you approve is your part. Your agent writes the spec; the tag goes in with `--facet`/`--component` at creation or afterwards:

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

After the approval the agent writes the plan (`plan.md`; `cerne feature start` refuses without one), starts the feature and builds it. When it is done it writes the result (`result.md`: what changed and the evidence for each criterion; `cerne feature complete` refuses without one) and completes the feature, which puts the card in Ready. The plan and the result sit on the card next to the spec. You then check the work and run `cerne feature accept F-0001` (or use the dashboard). That is the third step of `cerne next`.

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
- A task or a bug: an execution unit. A task decomposes a feature that is approved and still open (`--parent` is required); a bug you ask for is born approved.

<!-- quickstart -->
```bash
cerne idea new "Dark mode"
```

```text
I-0001 created (active) — Dark mode
```

```bash
cerne idea promote I-0001 --facet code --component <c>      # becomes a candidate feature, tagged
cerne task new "<step>" --parent <feature-id> --context "<what this step does>" --facet code --component <c>
cerne bug new "<what breaks>" --context "<what happens, where>" --on-behalf "<your words>" --facet code --component <c>
```

Tag every card, at creation or later with `cerne feature retag <id> --facet <f> --component <c>`: `cerne lint` lists the live cards with no tag. A task turns its parent into an epic, and `cerne lint` also flags an epic that is already done while a child is still open, so give a task a parent you have not accepted yet.

To work one: ask your agent to start it. The agent runs `cerne feature start <id>` and, for work that is not a card (a review, an investigation), `cerne work start "<what>" --ref <id>`. Both end with the index of the notes that inform that task and the command of its package, `cerne context <id>`: the task's document of the stage, its epic and the notes, decisions and sources that inform it, within a token budget: the notes and decisions linked to the task come with their body, the rest as id, title and a one-line gist; the agent opens another body with `cerne show <id>` when it needs it. Starting a task also registers what is in progress, so two agents do not do the same thing:

<!-- quickstart -->
```bash
cerne work start "Add Google sign-in button" --ref F-0001
cerne work list
cerne work done W-0001 --note "button shipped"
```

## The daily routine

1. Start with `cerne next`: what needs a spec, an approval, work, or your accept. Below it are the documentation debts. Your agent starts each task from its package, `cerne context <id>`; `.docs/CONTEXT.md` is the map of what is live, not its first read.
2. Approve the candidates whose spec you read, in the dashboard or with `cerne feature approve <id>`.
3. Let the agent work. When it completes a feature, review it and accept it (`cerne accept queue` lists them).
4. When something changes your plan, reject or supersede with a reason.
5. `cerne sync` brings in what others changed and sends your own edits. `cerne lint` lists stale documentation. While it finds no test file in the project root or in `test_roots`, it also warns `CRITERION-TESTS-CONFIG`: point `test_roots` (and `test_patterns`) in `.cernerc` at your tests, so it can link each criterion to the test that proves it.
6. When a new cerne release comes out, run `cerne update` inside the project. It updates the binary, then renews the hooks and the guide your agent reads. While the project's files are older than the binary, `cerne next` opens with a line asking for it. The hooks and the guide also renew by themselves at the next session start or `cerne next` when no one edited them (a line says so); a file you edited, or one a newer cerne wrote, is never overwritten: cerne only warns, naming it, until you run `cerne update`. To turn that off: `cerne settings set hooks.auto_refresh false`. On a version without the command, update the binary first with `brew upgrade henriquesakata/tap/cerne` or the install script.

When you forget a command, ask: `cerne guide "how do I close a feature"`.
