# Changelog

All notable changes to cerne are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While cerne is below 1.0.0, a minor version may change behavior: read the
**Upgrading** section of each release before you update.

## [Unreleased]

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

[Unreleased]: https://github.com/henriquesakata/cerne/compare/v0.14.0...HEAD
[0.14.0]: https://github.com/henriquesakata/cerne/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/henriquesakata/cerne/releases/tag/v0.13.0
