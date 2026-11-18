# Changelog

All notable changes to cerne are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While cerne is below 1.0.0, a minor version may change behavior: read the
**Upgrading** section of each release before you update.

## [Unreleased]

## [0.14.3] - 2026-11-18

### Added

- `cerne cost backfill-effort` adds the measured model effort to cost stamps
  written before this release, reading it from the Claude Code transcripts. It
  writes only when the recomputed tokens equal the stamp's, adds an erratum to
  done features and is a dry run unless you pass `--apply`.
  ([#66](https://github.com/henriquesakata/cerne/pull/66))

### Changed

- The session rules, the `cerne-authoring` skill and `cerne guide` tell agents
  to run `cerne feature complete` as soon as the PR is open, not after CI or
  the merge, and `complete` ends by saying to mark the PR ready for review now:
  human review starts when the card reaches Ready.
  ([#69](https://github.com/henriquesakata/cerne/pull/69))

### Fixed

- The costs screen no longer shows the model effort as unknown when the result
  has no `**Agente:**` line. The effort now comes from the Claude Code
  transcripts, which record it on every turn: the detail pairs each model with
  its efforts (`claude-opus-5-5 xhigh, claude-sonnet-5-5 medium (measured)`)
  and adds a *By effort* breakdown, the `## Custo` block gains a
  `Por esforço:` line, and the effort filter matches every effort a feature
  spent tokens at. ([#65](https://github.com/henriquesakata/cerne/pull/65))
- Boolean settings on the Settings screen (Graph, Mindmap, Wiki, notes on edit
  and on prompt, Gauntlet) save on a real mouse click. Before, the checkbox
  snapped back and nothing was written.
  ([#67](https://github.com/henriquesakata/cerne/pull/67))
- The prompt reminder fires when your message shares two or more rare words
  with the title of a note or runbook, instead of needing half of the title.
  A long paste still needs most of the title.
  ([#68](https://github.com/henriquesakata/cerne/pull/68))
- In the Backlog chat, a round of two or more question cards is answered as
  one turn: a click picks an option and *Send answers* sends them all, so no
  card is left unanswered.
  ([#64](https://github.com/henriquesakata/cerne/pull/64))

### Upgrading

Everything not listed here needs no action.

- **Update the binary on every machine that pushes to the project.**
  `brew upgrade henriquesakata/tap/cerne`, or run the install script again, or
  `go install github.com/henriquesakata/cerne/cmd/cerne@v0.14.3` (details in
  INSTALL.md, *Updating*). `cerne --version` then prints `cerne 0.14.3`.
- **Run `cerne hook install` and `cerne skill install` in each project.** This
  brings the new session rule and the skill text on when to complete.
- **To fill the effort into older stamps, run `cerne cost backfill-effort`,
  then again with `--apply`,** on a machine that still has the Claude Code
  transcripts of those sessions. Without it, features stamped before 0.14.3
  keep the effort declared in their result, or show it as unknown.
- **Scripts that read the UI's costs API** (`/api/costs`, `/api/costs/{id}`)
  get `efforts`, a list with the heaviest effort first, instead of `effort`,
  and `effort_source` can now be `measured`.

**Full changelog:** [v0.14.2...v0.14.3](https://github.com/henriquesakata/cerne/compare/v0.14.2...v0.14.3)

## [0.14.2] - 2026-11-17

### Added

- A prompt reminder for Claude Code: when your message shares words with the
  title of an active note or runbook, the agent sees up to three of their ids
  and titles, once per session, so a recorded fix is found even when the code
  isn't in the repository. Turn it off with the `hooks.prompt_notes` setting.
  ([#57](https://github.com/henriquesakata/cerne/pull/57))
- `note`, `decision`, `runbook`, `reference`, `feature` and `bug new` take the
  title from the body's first `# ` heading when you don't pass one, from
  `--file` or stdin. ([#59](https://github.com/henriquesakata/cerne/pull/59))
- `work start --ref` accepts other works (W-), notes (N-) and runbooks (RB-),
  and `cerne show` lists them. ([#54](https://github.com/henriquesakata/cerne/pull/54))

### Changed

- The CLI speaks English throughout: `supersede` replaces `substituir`, and
  `--step`, `--no-step`, `--consolidated`, `--window`, `--steps`, `--word`,
  `--stop`, `--by`, `--evidence` and `--expected` replace their Portuguese
  names, which keep working but are no longer shown in help. Lint findings
  and hook messages are English too.
  ([#55](https://github.com/henriquesakata/cerne/pull/55))
- cerne's own AI prompts (drafts, the Backlog chat, roast, `ask`, `compile`)
  are written in English and ask for the document in your project's
  documentation language, with the section titles and protocol forms that
  language uses. ([#61](https://github.com/henriquesakata/cerne/pull/61))
- A session started in a git worktree that has no `.docs/` gets it built from
  the local backend clone, with no network and no reset, and is told to run
  `cerne pull` when that isn't possible.
  ([#56](https://github.com/henriquesakata/cerne/pull/56))

### Fixed

- Bugs, epics and tasks no longer drop out of `cerne search` and `cerne ask`
  after they change, and the commit-message guard, cost attribution and the
  Gauntlet now recognize B-, E- and T- ids.
  ([#58](https://github.com/henriquesakata/cerne/pull/58))
- A git worktree of the same repository, created from a commit older than
  `cerne init`, resolves to the project of its main checkout instead of being
  refused. Clones, fixtures and submodules are still refused.
  ([#53](https://github.com/henriquesakata/cerne/pull/53))
- Ids of every kind link to the right page in the UI, and `[[W-…]]`, `[[C-…]]`
  and `[[HO-…]]` links no longer show as broken in `cerne lint`; notes and
  runbooks get their links in the graph.
  ([#60](https://github.com/henriquesakata/cerne/pull/60))

### Upgrading

Everything not listed here needs no action.

- **Update the binary on every machine that pushes to the project.**
  `brew upgrade henriquesakata/tap/cerne`, or run the install script again, or
  `go install github.com/henriquesakata/cerne/cmd/cerne@v0.14.2` (details in
  INSTALL.md, *Updating*). `cerne --version` then prints `cerne 0.14.2`.
- **Run `cerne hook install` and `cerne skill install` in each project.** This
  adds the prompt reminder hook and teaches the skills the English command
  names.
- **Scripts that read cerne's output may need the new names.** The Portuguese
  commands and flags still work as input, but help text and printed hints now
  show only the English names (`supersede`, `--consolidated`, `--stop`…).
- **`cerne ask` in delegation mode** prints a prompt that starts with
  `Question:` instead of `Pergunta:`.

**Full changelog:** [v0.14.1...v0.14.2](https://github.com/henriquesakata/cerne/compare/v0.14.1...v0.14.2)

## [0.14.1] - 2026-11-17

### Fixed

- An entity's errata now reach everyone who reads it. `cerne pull` appends
  them to the entity's file in `.docs/` as a generated block, and
  `cerne note show` prints them as `cerne show` does. Before, only
  `cerne show` printed them, so an agent reading `.docs/` saw a corrected note
  without its correction. `cerne push` never sends the block back as body
  text. ([#44](https://github.com/henriquesakata/cerne/pull/44))
- `cerne hook install` run inside a git worktree or a submodule installs the
  `prepare-commit-msg` and `commit-msg` hooks where git runs them, instead of
  skipping them as "not a git repository". A `core.hooksPath` set for the
  repository is still honored from a worktree.
  ([#43](https://github.com/henriquesakata/cerne/pull/43))
- `cerne --version`, the UI, `cerne harness` and the MCP server show the full
  release version (`0.14.1`), so two releases of the same minor no longer look
  the same. Local builds still show `dev`.
  ([#45](https://github.com/henriquesakata/cerne/pull/45))
- The `cerne pull` summary counts notes and runbooks.
  ([#44](https://github.com/henriquesakata/cerne/pull/44))

### Changed

- Release pages open with an Update section: the command to update for each
  install method. A release whose publishing failed halfway can be run again.
  ([#40](https://github.com/henriquesakata/cerne/pull/40),
  [#41](https://github.com/henriquesakata/cerne/pull/41))

### Upgrading

Everything not listed here needs no action.

- **Update the binary on every machine that pushes to the project.**
  `brew upgrade henriquesakata/tap/cerne`, or run the install script again, or
  `go install github.com/henriquesakata/cerne/cmd/cerne@v0.14.1` (details in
  INSTALL.md, *Updating*). `cerne --version` then prints `cerne 0.14.1`. A
  0.14.0 binary pushing from a `.docs/` written by 0.14.1 would send the errata
  blocks as body text.
- **Run `cerne pull` in each project** so the files in `.docs/` get their
  errata.
- **Leave the errata block alone.** It is generated from the errata and
  rewritten on every pull. To correct an erratum, add another with
  `cerne <kind> errata`. If you edit inside the block, `cerne push` refuses
  that file: delete the whole block, markers included, or restore it.
- **Installed cerne only from inside a git worktree?** Run
  `cerne hook install` once more to get the two commit hooks.

**Full changelog:** [v0.14.0...v0.14.1](https://github.com/henriquesakata/cerne/compare/v0.14.0...v0.14.1)

## [0.14.0] - 2026-11-16

### Highlights

- **GitHub Copilot support, both ways.** Copilot can now work in a cerne project
  guided by cerne (session board, edit gate, authoring skill, read-only MCP —
  the provider parity matrix lists what Copilot cannot host), and cerne can use
  your Copilot subscription for its own AI features through the new
  `copilot-cli` mode.
  ([#28](https://github.com/henriquesakata/cerne/pull/28),
  [#29](https://github.com/henriquesakata/cerne/pull/29))
- **A Backlog chat that interviews before it proposes.** The analyst works
  through the spec's decisions in short rounds, asks each product question as
  an option card with its recommendation, lists what it assumed, and checks the
  proposal before showing it.
  ([#25](https://github.com/henriquesakata/cerne/pull/25),
  [#30](https://github.com/henriquesakata/cerne/pull/30),
  [#32](https://github.com/henriquesakata/cerne/pull/32))
- **Pick the subscription that answers, per conversation.** The chat offers
  every subscription CLI installed on your machine (Claude Code, Codex, Cursor,
  Copilot) and shows under each reply which provider and model answered.
  ([#24](https://github.com/henriquesakata/cerne/pull/24),
  [#26](https://github.com/henriquesakata/cerne/pull/26))
- **Codex can look at your code from the chat.** In `codex-cli` mode the local
  chat searches and reads the committed project files and cerne's own records,
  read-only, and answers with the files it consulted.
  ([#20](https://github.com/henriquesakata/cerne/pull/20),
  [#34](https://github.com/henriquesakata/cerne/pull/34))
- **No login or machine name in the record.** Work, event and handoff authors
  are now shown as a stable pseudonym of the machine.
  ([#27](https://github.com/henriquesakata/cerne/pull/27))

### Added

- GitHub Copilot as a project agent: `cerne hook install --provider copilot`
  writes `.github/hooks/cerne.json`, adds a read-only `cerne` server to the
  root `.mcp.json` and prints the block for the cloud agent's repository
  settings; `cerne skill install` writes the authoring skill to
  `.github/skills/cerne-authoring/` and the project rules to
  `.github/instructions/cerne.instructions.md`. Copilot is detected by its own
  files under `.github/`, never by `.github/` alone, and gets a column in the
  provider parity matrix and the `cerne harness` inventory.
  ([#28](https://github.com/henriquesakata/cerne/pull/28))
- `copilot-cli` subscription mode for `ask`, `compile`, the document drafts and
  the Backlog chat, isolated: no tools, no MCP, no custom instructions and no
  repository hooks, in an empty temporary directory with a throwaway
  `COPILOT_HOME`.
  ([#29](https://github.com/henriquesakata/cerne/pull/29))
- `cursor-cli` subscription mode through the Cursor CLI (`cursor-agent`).
  ([#24](https://github.com/henriquesakata/cerne/pull/24))
- Backlog chat: a provider menu with every installed subscription CLI, and a
  line under each reply with the provider, model and effort that answered.
  ([#26](https://github.com/henriquesakata/cerne/pull/26))
- Backlog chat: product questions as option cards with a recommended option and
  a free answer; an answered card shows the choice, and a card left behind by
  the conversation is locked.
  ([#30](https://github.com/henriquesakata/cerne/pull/30))
- Backlog chat: interview rounds over the spec's decision tree — at most three
  questions per round, low-impact decisions listed under "Assumed", a progress
  line, and no proposal while a product decision is open unless you ask for it.
  ([#32](https://github.com/henriquesakata/cerne/pull/32))
- `cerne spec check`, and the chat analyst runs it on its proposal before
  showing it. ([#25](https://github.com/henriquesakata/cerne/pull/25))
- Backlog chat: the cerne context injected in each turn is visible under the
  reply. ([#23](https://github.com/henriquesakata/cerne/pull/23))
- Backlog chat: a new-conversation button and the conversation history in the
  toolbar. ([#15](https://github.com/henriquesakata/cerne/pull/15))
- Codex chat (local `cerne ui`): finds and reads the committed project files.
  Thanks to [@GuilhermeSk](https://github.com/GuilhermeSk).
  ([#20](https://github.com/henriquesakata/cerne/pull/20))
- Codex chat (local `cerne ui`): content search and path globs over the
  committed files, plus cerne's read-only tools (status, brief, search, show,
  ask); replies cite paths relative to the project and the lines they read.
  ([#34](https://github.com/henriquesakata/cerne/pull/34))
- Codex chat offers the models of your account and passes the chosen effort.
  ([#21](https://github.com/henriquesakata/cerne/pull/21))
- The session-start board shows the work in progress, read from the backend
  clone. ([#16](https://github.com/henriquesakata/cerne/pull/16))
- `cerne pull` (with `sync_skills` on) and session start refresh the canonical
  skills and flag managed hooks that are out of date with the installed binary.
  ([#8](https://github.com/henriquesakata/cerne/pull/8))
- `cerne search` shows works in their own group and how fresh the index is.
  ([#9](https://github.com/henriquesakata/cerne/pull/9))

### Changed

- Authors of works, events and handoffs are shown as a machine pseudonym
  (`m-<8hex>`, or `agent:m-<8hex>`) everywhere: `cerne show`, `cerne work list`,
  search, the session board, `CONTEXT.md`, the UI and webhooks.
  ([#27](https://github.com/henriquesakata/cerne/pull/27))
- Backlog chat in `claude-cli` mode runs with the project's and local Claude
  Code settings only: your user-level hooks, plugins, `CLAUDE.md` and MCP
  servers no longer reach cerne's analyst. ([#36](https://github.com/henriquesakata/cerne/pull/36))
- Backlog chat in `claude-cli` mode honors the model and effort picked in the
  composer. ([#35](https://github.com/henriquesakata/cerne/pull/35))
- A Backlog proposal is written in the project's documentation language.
  ([#22](https://github.com/henriquesakata/cerne/pull/22))
- `cerne lint --staged` is much faster when you have a local edit.
  ([#10](https://github.com/henriquesakata/cerne/pull/10))
- `cerne feature learnings` lists, by default, only the features concluded since
  2026-11-13 and counts the older ones on one line; `--since` shows more.
  ([#12](https://github.com/henriquesakata/cerne/pull/12))
- The README was rewritten for first-time readers and checked against the code.
  ([#6](https://github.com/henriquesakata/cerne/pull/6),
  [#14](https://github.com/henriquesakata/cerne/pull/14),
  [#31](https://github.com/henriquesakata/cerne/pull/31),
  [#33](https://github.com/henriquesakata/cerne/pull/33),
  [#37](https://github.com/henriquesakata/cerne/pull/37))

### Fixed

- `cerne ask` through a subscription CLI no longer stops at "I will read the
  file" when the question also asks to read one: every one-shot provider is
  told it has no tools, answers from the cerne sources and declines the file.
  ([#38](https://github.com/henriquesakata/cerne/pull/38))
- A cache clone that points to another project's backend is refused instead of
  being written to. ([#17](https://github.com/henriquesakata/cerne/pull/17))
- A Backlog proposal with code blocks inside is no longer cut when registered,
  and `cerne feature approve` refuses a spec missing required sections.
  ([#18](https://github.com/henriquesakata/cerne/pull/18))
- `cerne import --agent` keeps its prompt valid UTF-8 for CLIs that reject
  invalid bytes. ([#19](https://github.com/henriquesakata/cerne/pull/19))
- `cerne lint --staged` sees the event log, so event-based rules no longer
  misfire. ([#7](https://github.com/henriquesakata/cerne/pull/7))
- Work log entries record their author without an e-mail shape and show
  Brazilian time zones by name. ([#13](https://github.com/henriquesakata/cerne/pull/13))
- `cerne cost backfill-roles` no longer attributes subagents that worked on
  another feature. ([#11](https://github.com/henriquesakata/cerne/pull/11))
- `cerne mcp --help` lists every tool the server registers, and `cerne savings`
  says what it measures. ([#33](https://github.com/henriquesakata/cerne/pull/33))

### Upgrading

Everything not listed here needs no action.

- **Update the binary.** `brew upgrade henriquesakata/tap/cerne`, or run the
  install script again, or `go install github.com/henriquesakata/cerne/cmd/cerne@v0.14.0`
  (details in INSTALL.md, *Updating*). `cerne --version` then prints 0.14.
- **Refresh the installed hooks and skill.** After updating the binary, run
  `cerne hook install` and `cerne skill install` in each project (add
  `--provider copilot` for Copilot). `cerne pull` and the session start warn
  when a managed hook is older than the binary.
- **Authors look different, nothing is rewritten.** Lists and the UI show
  `m-<8hex>` where they showed `login@host`. Stored records are left as they
  are; old and new records from the same machine still count as yours for the
  parallel-work guard.
- **Claude Code must know `--setting-sources` and `--strict-mcp-config`.** The
  `claude-cli` chat turn now passes both. An older Claude Code fails with a
  message to update it; cerne never falls back to a less isolated turn.
- **The `claude-cli` chat no longer sees your personal Claude Code setup.** If
  you relied on a user-level hook, plugin or MCP server inside the Backlog
  chat, move it to the project's settings.
- **Copilot projects get a `cerne` entry in `.mcp.json`.** It is a read-only
  stdio server; Claude Code reads the same file. Commit or ignore it as you do
  the rest of your agent configuration.
- **The Codex chat needs a commit.** A project without commits gets no
  project-file tools and no live query in the Codex chat.
- **Specs are checked the same way everywhere.** `cerne feature approve` now
  refuses a spec that is missing required sections, and the same check runs in
  `cerne spec check`, `cerne lint` and the chat, so an old spec can show new
  findings. Run `cerne feature approve <id> --check` on approved work you plan
  to change.
- **A cache clone of another project's backend is refused.** If two projects
  share a `project_id` on one machine, cerne now stops with an error instead of
  writing to the other backend: give each project its own `project_id`, or its
  own `CERNE_CACHE_DIR`.

### Known limitations

- Copilot as a project agent was verified on the Copilot CLI. VS Code agent
  mode and the Copilot cloud agent are not verified yet; the cloud agent reads
  MCP servers only from the repository settings and needs `cerne` installed in
  its environment.
- When both the Claude Code and the Copilot integrations are installed, the
  Copilot CLI also runs the hooks in `.claude/settings.json`, so their side
  effects run twice.
- `cursor-cli` mode cannot fully isolate the Cursor CLI: it runs in ask mode
  with a deny-all permissions file, but your own Cursor rules, hooks and
  plugins still load, because the CLI has no switch to turn them off.
- `copilot-cli` mode keeps your Copilot login through a throwaway `COPILOT_HOME`
  on macOS. On Linux and Windows this is not verified: set
  `COPILOT_GITHUB_TOKEN`. The Copilot CLI has no model listing, so the chat
  offers no Copilot models and the model is only the one you configure.

**Full changelog:** [v0.13.0...v0.14.0](https://github.com/henriquesakata/cerne/compare/v0.13.0...v0.14.0)

## [0.13.0] - 2026-11-15

First release published on GitHub. Versions 0.1.0 to 0.12.0 were released
before the move and have no public release notes.

[Unreleased]: https://github.com/henriquesakata/cerne/compare/v0.14.3...HEAD
[0.14.3]: https://github.com/henriquesakata/cerne/compare/v0.14.2...v0.14.3
[0.14.2]: https://github.com/henriquesakata/cerne/compare/v0.14.1...v0.14.2
[0.14.1]: https://github.com/henriquesakata/cerne/compare/v0.14.0...v0.14.1
[0.14.0]: https://github.com/henriquesakata/cerne/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/henriquesakata/cerne/releases/tag/v0.13.0
