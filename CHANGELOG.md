# Changelog

All notable changes to cerne are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While cerne is below 1.0.0, a minor version may change behavior: read the
**Upgrading** section of each release before you update.

## [Unreleased]

## [0.16.0] - 2026-11-26

### Highlights

- **Acceptance by risk.** With `[gates] accept = "risk"`, a delivery that
  touches no sensitive path or facet, won its Gauntlet and has a test for
  every criterion is accepted on its own at `cerne feature complete`; every
  other card waits for the human merge and shows why. The gate learns from
  the owner: one click says whether a delivery needed you, and cerne proposes
  looser or tighter lists from those answers. The approval and acceptance
  gates are now on the Settings screen.
  ([#222](https://github.com/henriquesakata/cerne/pull/222),
  [#228](https://github.com/henriquesakata/cerne/pull/228),
  [#229](https://github.com/henriquesakata/cerne/pull/229),
  [#236](https://github.com/henriquesakata/cerne/pull/236))
- **A War Room you can leave, watch and close together.** Participants leave
  explicitly or just observe, the roster shows who is listening right now, and
  anyone can write a shared minutes draft that the facilitator closes the room
  with. Room credentials no longer travel on the command line.
  ([#213](https://github.com/henriquesakata/cerne/pull/213),
  [#214](https://github.com/henriquesakata/cerne/pull/214),
  [#215](https://github.com/henriquesakata/cerne/pull/215),
  [#216](https://github.com/henriquesakata/cerne/pull/216),
  [#217](https://github.com/henriquesakata/cerne/pull/217),
  [#218](https://github.com/henriquesakata/cerne/pull/218))
- **Images in the Backlog chat.** Paste, drop or pick a screenshot and the
  agent reads it in that turn and the next ones, on Claude Code, Codex and
  Copilot CLIs and on an Anthropic or OpenAI API key.
  ([#235](https://github.com/henriquesakata/cerne/pull/235),
  [#237](https://github.com/henriquesakata/cerne/pull/237))
- **The agent hears when you move its card.** When a human approves,
  accepts, rejects, reverts or supersedes a card, every agent session that
  worked on it sees the new state at its next turn, without asking.
  ([#187](https://github.com/henriquesakata/cerne/pull/187))
- **More places to run cerne.** Arch Linux gets the `cerne-bin` package on
  the AUR and Windows a Scoop bucket, both updated by each stable release, and
  `cerne hook install --provider cursor` sets up Cursor's sandbox and MCP
  files on the first try.
  ([#201](https://github.com/henriquesakata/cerne/pull/201),
  [#202](https://github.com/henriquesakata/cerne/pull/202),
  [#220](https://github.com/henriquesakata/cerne/pull/220),
  [#221](https://github.com/henriquesakata/cerne/pull/221))

### Added

- Card notices. When a human approves, accepts, rejects, reverts or
  supersedes a card, every session on this machine that worked on it gets a
  notice, the same line `cerne feature wait` prints. The session sees it once,
  at its next session start, prompt or tool call; the actor's own session is
  skipped, and a notice older than 24 hours is dropped.
  ([#187](https://github.com/henriquesakata/cerne/pull/187))
- `cerne reference check [R-…]` requests each current external reference and
  records the date, the HTTP status and a hash of the visible text: ok,
  changed, gone or transient. `cerne reference keep R-…` confirms a changed
  source still holds. `cerne lint` flags a gone source as `reference-gone`,
  reading only the recorded check: lint, pull and push never touch the
  network.
  ([#184](https://github.com/henriquesakata/cerne/pull/184))
- Archived copies of public sources. With the human-only setting
  `references.archive` on (off by default), `cerne reference new` and
  `cerne reference archive [R-…] [--dry-run]` keep the link of a dated Wayback
  Machine copy, the capture nearest the day the reference was registered. Only
  the URL is sent, and never for an internal or private host, a URL with
  credentials or a secret, a webhook or a page an anonymous request cannot
  read. `--no-archive` and `reference archive --opt-out` keep a URL out.
  `cerne show`, `reference list`, `reference check` and `reference-gone` carry
  the copy's link.
  ([#186](https://github.com/henriquesakata/cerne/pull/186))
- `cerne links suggest | pending | accept | reject`: the configured LLM
  suggests the relations a document mentions in prose without a link, each
  with a literal excerpt, and a person accepts or rejects each one. An
  accepted relation becomes a link in the graph, in `cerne context` and in
  `cerne lint`. It spends tokens, so the project owner turns it on
  (`links.suggest`, human-only, off by default), and it shows an estimate
  before it sends anything.
  ([#191](https://github.com/henriquesakata/cerne/pull/191),
  [#192](https://github.com/henriquesakata/cerne/pull/192))
- `cerne import --agent --ingest` keeps an imported entity only with a
  literal excerpt of its source as evidence; the rest is left out and listed
  with the reason, and `cerne show` prints the excerpt.
  ([#183](https://github.com/henriquesakata/cerne/pull/183))

- Acceptance by risk. With `[gates] accept = "risk"`, `cerne feature complete`
  accepts a delivery on its own only when no changed file matches
  `gates.risk_human_paths`, the card carries no facet of
  `gates.risk_human_facets`, its last Gauntlet round is a win against a bar
  that is not exempt, and every numbered criterion has a marked test. A card
  with no facet, or a diff that cannot be read, does not pass. The accepted
  card goes to done in the same commit, marked "Accepted as low risk" with the
  measurements; a held card prints one line per failed condition, keeps those
  lines as a badge on the kanban and its page, and waits for the merge as
  under `accept = "merge"`. Both lists are human-only settings.
  ([#228](https://github.com/henriquesakata/cerne/pull/228),
  [#236](https://github.com/henriquesakata/cerne/pull/236))
- The risk gate learns from the owner.
  `cerne feature feedback <id> --should-have-asked | --not-needed` (and the
  same buttons on the feature page) records whether a delivery needed you;
  *should have asked* adds the folders and facets you name to the lists at
  once. For the first `gates.risk_calibration` deliveries (20 by default, 0
  turns it off) every delivery is held and the merged card asks "Did this
  delivery need you?", listed by `cerne next` under RISK CALIBRATION. After
  `gates.risk_relax_after` (3) *not needed* answers on one pattern, or a fix or
  revert of a low-risk card, `cerne next` shows a proposal under GATE
  PROPOSALS, applied or dismissed in Settings.
  `cerne feature set <id> --accept-mode human|auto|gate` pins one card. Every
  one of these writes is the owner's.
  ([#229](https://github.com/henriquesakata/cerne/pull/229))
- `gates.approve` (`human` | `auto`) and `gates.accept` (`human` | `merge` |
  `risk` | `auto`) are on the Settings screen and in `cerne settings set`,
  human-only. A card that an agent approved under `approve = "auto"`, or that
  reached done under `accept = "auto"`, is marked: `cerne show` says
  `approved: automatically (no human act)`, and the kanban and the feature page
  show "Approved/Accepted without human review".
  ([#222](https://github.com/henriquesakata/cerne/pull/222))
- War Room: `cerne room leave <room>` revokes your session and keeps the
  history, and a room you have not joined opens in the UI as "Observing";
  joining is only needed to post. The roster shows who is listening and when
  each participant was last active, and `cerne room list` prints
  `<N> participants (<L> listening)`.
  ([#214](https://github.com/henriquesakata/cerne/pull/214),
  [#216](https://github.com/henriquesakata/cerne/pull/216))
- War Room shared minutes: `cerne room minutes draft <room> --minutes …`
  saves a draft any participant or the facilitator can update,
  `cerne room minutes show <room> --draft` prints it, and
  `cerne room close <room> --use-draft` closes with it. The UI shows the open
  proposals and decision requests to everyone, the latest draft with its
  author, and lets the facilitator close with it in one click.
  ([#218](https://github.com/henriquesakata/cerne/pull/218))
- War Room CLI: `cerne room say` reads the message from stdin (`-`) or
  `--file`; `room read` and `room watch` show local `HH:MM` times and the
  facilitation events; `cerne room watch --once --settle <duration>` (default
  `1.5s`) gathers a whole burst of messages before it exits.
  ([#213](https://github.com/henriquesakata/cerne/pull/213),
  [#217](https://github.com/henriquesakata/cerne/pull/217))
- `cerne room facilitator reset <room>` gives the facilitator role back when
  its browser lost it, so the room can still be closed. The UI asks before it
  hands the facilitation to someone else.
  ([#212](https://github.com/henriquesakata/cerne/pull/212))
- Images in the Backlog chat: paste, drop on the panel or pick a file (PNG,
  JPEG, WebP or GIF, 10 MB each, 5 per message). The agent sees them in that
  turn and the following ones of the conversation. They stay in the project's
  local cache, never in the backend or `.docs/`, and go away with the
  conversation. Works on `claude-cli`, `codex-cli`, `copilot-cli` and byo-key
  Anthropic and OpenAI; other modes disable the button and say why.
  ([#235](https://github.com/henriquesakata/cerne/pull/235),
  [#237](https://github.com/henriquesakata/cerne/pull/237))
- Cursor: `cerne hook install --provider cursor` adds cerne's cache folders to
  `.cursor/sandbox.json` and the `cerne mcp` server to `.cursor/mcp.json`,
  keeping your own keys; with `--network` it allows only the remote backend's
  host. A file it cannot merge is left alone, with the block to add by hand.
  A clone or sync blocked by Cursor's sandbox now says so and names the fix,
  and `cerne harness` lists Cursor's MCP servers.
  ([#220](https://github.com/henriquesakata/cerne/pull/220))
- Cursor: a document written from a Cursor agent records `cursor` and its
  model as the author (never a guessed model), and Cursor sessions show in
  the Agents panel.
  ([#221](https://github.com/henriquesakata/cerne/pull/221))
- `cerne idea promote <id>` takes `--facet`, `--component` and
  `--capability`, so the card is classified in the same command.
  ([#206](https://github.com/henriquesakata/cerne/pull/206))
- `cerne idea unpromote <I-id>` (and "Undo" in the UI) undoes a promotion:
  it rejects the candidate the promote created and returns the idea to active.
  A mirrored issue stays open.
  ([#207](https://github.com/henriquesakata/cerne/pull/207))
- `--capability` on `cerne feature|epic|task new` links the capability in the
  same commit, and `cerne feature capability` takes a comma-separated batch.
  ([#219](https://github.com/henriquesakata/cerne/pull/219))
- Clarifications can carry the author's proposed answers: the recommended one
  first with a badge, "Other answer" last, in the UI and in
  `cerne clarify list`/`answer --pick`. `cerne lint` warns when a question
  marks more than one answer as recommended.
  ([#223](https://github.com/henriquesakata/cerne/pull/223))
- `cerne feature set <id> --origin human|agent [--on-behalf "<quote>"]`
  corrects who a card came from, as a human act, recorded as its own event.
  ([#226](https://github.com/henriquesakata/cerne/pull/226))
- Arch Linux: the `cerne-bin` package on the AUR, with the release binary
  checked against `SHA256SUMS`, updated by each stable release. `cerne update`
  leaves a binary in `/usr/bin` to the system package manager and prints
  `yay -Syu cerne-bin`.
  ([#201](https://github.com/henriquesakata/cerne/pull/201))
- Windows: a Scoop manifest in the `henriquesakata/scoop-bucket` bucket
  (`scoop bucket add henriquesakata https://github.com/henriquesakata/scoop-bucket`,
  `scoop install henriquesakata/cerne`), updated by each stable release.
  `cerne update` leaves a Scoop install to `scoop update cerne`.
  ([#202](https://github.com/henriquesakata/cerne/pull/202))
- INSTALL.md shows how to turn on shell completion in bash, zsh, fish and
  PowerShell, writing only inside your home.
  ([#198](https://github.com/henriquesakata/cerne/pull/198))

### Changed

- In an agent's shell (`CERNE_ACTOR=agent`), `cerne feature|bug|epic|task new`
  asks where the card came from: `--on-behalf "<the human's words>"` when a
  person asked for it, or `--origin agent` when the idea is the agent's. The
  demo, the import, the UI and the Backlog chat create cards as before.
  ([#227](https://github.com/henriquesakata/cerne/pull/227))
- `cerne idea promote` from the CLI refuses to create a card with no tag, or
  with no capability in a project that has a capability taxonomy, and names
  the flags that fix it; `--allow-incomplete` creates it anyway. The UI keeps
  its behavior.
  ([#206](https://github.com/henriquesakata/cerne/pull/206))
- `CERNE_LLM_API_KEY` is read only for Anthropic, and only for an `sk-ant-`
  key (any value with a custom `base_url`). The `openai` provider reads only
  `OPENAI_API_KEY`.
  ([#239](https://github.com/henriquesakata/cerne/pull/239))
- `cerne ui` refuses a write that does not come from its own page (no
  `Origin`, or another host's): a local script can no longer approve, accept,
  report a bug as a human or change a human-only setting through it. The UI
  and the `cerne` commands that talk to a running panel are unaffected.
  ([#234](https://github.com/henriquesakata/cerne/pull/234))
- War Room credentials stay off the command line. In an agent session,
  `cerne room join` stores the session (0600, in the cache) and `say`,
  `watch`, `propose` and `request-decision` find it, by `--nick` when you
  joined more than one; elsewhere use `CERNE_ROOM_SESSION` and
  `CERNE_ROOM_FACILITATOR`. The printed examples and the guide carry no token.
  ([#215](https://github.com/henriquesakata/cerne/pull/215))
- `cerne next` shows at most 5 findings per lint rule; `--all` shows every
  one, and `--json` is never capped.
  ([#219](https://github.com/henriquesakata/cerne/pull/219))
- Every `claude -p` call cerne makes skips Claude Code's session-title
  request, which resent the whole prompt: a `claude-cli` call costs about half
  the tokens it did.
  ([#188](https://github.com/henriquesakata/cerne/pull/188))
- Refusals and next steps point to the file you edit,
  `.docs/current/features/<id>-<slug>/<doc>`, between `cerne pull` and
  `cerne push`, instead of a path inside the backend.
  ([#197](https://github.com/henriquesakata/cerne/pull/197))
- A repeated `cerne feature accept` on a done card exits 0 with
  "already done; nothing changed".
  ([#195](https://github.com/henriquesakata/cerne/pull/195))

### Fixed

- With `mirror.close_on = "merge"`, accepting a card no longer closes its
  GitHub, GitLab or Gitea issue before the PR merges: the issue gets the
  `cerne:done` label and closes on the first mirror round after the merge. The
  accept warning says whether the PR is still open or was closed without merge.
  ([#210](https://github.com/henriquesakata/cerne/pull/210))
- Undoing an idea's promotion through `cerne idea unlink` or a reject no longer
  leaves the card driving the mirrored issue, or closes it.
  ([#207](https://github.com/henriquesakata/cerne/pull/207))
- The commit hooks read the project and branch of the tree that commits: in a
  linked worktree they no longer follow the main checkout's `.cernerc`, so
  `mirror.close_on = "merge"` writes `Closes:` as it should.
  ([#209](https://github.com/henriquesakata/cerne/pull/209))
- `cerne search` snippets no longer quote the frontmatter and the template's
  HTML comments.
  ([#199](https://github.com/henriquesakata/cerne/pull/199))
- A `<!--` quoted inside code in a spec or result no longer hides the rest of
  the document: the `Barra:` line, the `## Gauntlet` section and the cost stamp
  read right.
  ([#203](https://github.com/henriquesakata/cerne/pull/203),
  [#205](https://github.com/henriquesakata/cerne/pull/205))
- `cerne import --agent` leaves out the files cerne itself installs (the guide
  copies, the hooks, the learned skills and cerne's block in `AGENTS.md` and
  `GEMINI.md`).
  ([#190](https://github.com/henriquesakata/cerne/pull/190))
- `cerne accept queue` prints English in every project, in text and in
  `--json`.
  ([#196](https://github.com/henriquesakata/cerne/pull/196))
- An unknown facet is refused in English with the valid list, and an unknown
  capability points to `cerne capability list --tree`.
  ([#208](https://github.com/henriquesakata/cerne/pull/208),
  [#211](https://github.com/henriquesakata/cerne/pull/211))
- Under `accept = "auto"`, `cerne feature complete` stamps the cost on the
  card it carries to done, instead of warning with a command that cannot work.
  ([#231](https://github.com/henriquesakata/cerne/pull/231))
- A provider is never handed another vendor's API key: an `sk-ant-` key in
  `CERNE_LLM_API_KEY` no longer reaches `api.openai.com`, and Settings no
  longer reports it as the OpenAI key.
  ([#238](https://github.com/henriquesakata/cerne/pull/238))
- The clarification popover takes its name from the question and closes with
  Esc.
  ([#232](https://github.com/henriquesakata/cerne/pull/232))
- `docs/first-week.md` and the README Quickstart match what the release prints
  and asks, including which `.claude/` files to commit.
  ([#194](https://github.com/henriquesakata/cerne/pull/194))

### Upgrading

Everything not listed here needs no action.

- **Update the binary.** Run `cerne update` in a project, or
  `brew upgrade henriquesakata/tap/cerne`, or run the install script again, or
  `go install github.com/henriquesakata/cerne/cmd/cerne@v0.16.0` (details in
  INSTALL.md, *Updating*). `cerne --version` then prints `cerne 0.16.0`. A
  `cerne ui` that is already open offers to restart into the new binary.
- **Hooks and the authoring guide changed.** They renew on their own at the
  next agent session start when nobody edited them; if you did, run
  `cerne hook install` and `cerne skill install`. On Cursor, run
  `cerne hook install --provider cursor` (add `--network` for a remote
  backend) to get the sandbox and MCP files.
- **Agents creating cards pass an origin.** Scripts or agents that run
  `cerne feature|bug|epic|task new` with `CERNE_ACTOR=agent` add
  `--on-behalf "<quote>"` or `--origin agent`.
- **`cerne idea promote` needs the tags.** Pass `--facet`/`--component` (and
  `--capability` when the project has a taxonomy), or `--allow-incomplete`.
- **OpenAI key.** If your OpenAI key is in `CERNE_LLM_API_KEY`, move it to
  `OPENAI_API_KEY`; the error and Settings say so until you do.
- **Scripts that write through `cerne ui`.** A `curl` to its API without the
  page's `Origin` now gets 403; use the `cerne` commands instead.
- **`cerne accept queue --json`.** The Gauntlet outcome is `win`, `loss`,
  `tie` or `exempt`, and an absent field reads `not reported`.
- **Git submodules.** A submodule with no `.cernerc` of its own no longer
  borrows the superproject's project in the commit hooks; they do nothing
  there.
- **Acceptance by risk is opt-in.** Set `gates.accept` to `risk` in Settings ›
  Process or `cerne settings set`. For the first 20 deliveries
  (`gates.risk_calibration`) it holds every card and asks you to answer.

### Known limitations

- `cerne ui` trusts the `Origin` header as the page's proof, so a local
  process that forges it still passes; the facilitator reset route still
  accepts a write with no `Origin`, because `cerne room facilitator reset`
  calls it without one.
- The stored War Room session guards against accidental exposure (process
  list, shell history, transcripts), not against another process of the same
  user reading the cache.
- Images in the Backlog chat do not work on `cursor-cli`, `gemini-cli` or the
  remote `cerne serve`.
- Going back to an older binary while a War Room is open turns the sessions
  revoked by `cerne room leave` valid again.

**Full changelog:** [v0.15.2...v0.16.0](https://github.com/henriquesakata/cerne/compare/v0.15.2...v0.16.0)

## [0.15.2] - 2026-11-22

### Added

- Accept on merge. With `[gates] accept = "merge"` in `.cernerc` and a
  `[delivery] forge`, the human's merge of a card's delivery PR into the
  default branch accepts the card, for features, tasks and bugs.
  `cerne feature accept --merged [<id>]` asks the forge about each implemented
  card's PR and moves the merged ones to done; a card whose PR is still open,
  was closed without merge, was merged into another branch or belongs to
  another repository stays in Ready, with one line saying why. An agent may run
  `--merged`; a plain `accept` stays human. `cerne feature wait <id> --for done`
  records the merge as soon as it lands. Under this gate `cerne next` shows
  `WAITING FOR THE MERGE`, the shell hook refuses an agent's `gh pr merge`,
  `glab mr merge`, `tea pulls merge` and the merge API calls, and `cerne show`
  and the feature page say "Accepted on merge · PR #N". Works with GitHub,
  GitLab and Gitea.
  ([#172](https://github.com/henriquesakata/cerne/pull/172))
- `cerne feature revert` also steps back after the delivery: from Ready to In
  development (the delivery was rejected, or a bug turned up) and from Done to
  Ready (an acceptance is undone). The reason is required and the human gate
  applies; spec, plan, result and delivery links stay, and the next `complete`
  runs every gate again. On the kanban you can drag a card from Done to Ready
  or from Ready to In development, and the drop asks for the reason. A card
  accepted on merge and then reverted is not accepted again from the same
  merge.
  ([#176](https://github.com/henriquesakata/cerne/pull/176))
- A channel message for an idle Claude Code session wakes it, through Claude
  Code's cross-session messaging, when the session's last permission mode is
  `default`, `acceptEdits`, `auto` or `dontAsk`. A human's message counts for
  the sessions it names by work or by card. cerne never reads or sends the
  messaging token and never wakes a session in `bypassPermissions` or `plan`
  mode; when it does not wake a session, the channel screen says why. Codex now
  gets the channel and the prompt notes on every prompt.
  ([#177](https://github.com/henriquesakata/cerne/pull/177))
- The project channel screen keeps the composer at the bottom while only the
  messages scroll, follows new messages while you are at the end, and shows
  when each message was sent and when each target answered.
  ([#166](https://github.com/henriquesakata/cerne/pull/166))
- The UI shows dates and times in your computer's locale and time zone, no
  longer in the PT/EN switch's locale. Settings › Interface › *Date and time*
  picks another format or zone for this browser.
  ([#171](https://github.com/henriquesakata/cerne/pull/171))

### Changed

- An addressed channel message shows once per turn, not at every tool call,
  and a human's message reads as a report from its sender, with the date, the
  time and how to reply.
  ([#167](https://github.com/henriquesakata/cerne/pull/167))
- The **Update** button of `cerne ui` asks before it updates, and both update
  notices offer **Later**. Nothing is stored: the button stays in the bar.
  ([#168](https://github.com/henriquesakata/cerne/pull/168))
- The feature page: the action that moves the card forward is the one filled
  button, the side rail stays in view, and done, rejected and superseded cards
  are read-only. Secondary text meets the WCAG AA contrast and links use the
  theme's colour. The document starts higher, prose lines stop near 72
  characters, and the Agents panel, Backlinks and the delivery pendings start
  collapsed to one line. Complete and Accept ask for a second click, the page
  opens on the stage's document, and an unsaved draft survives a tab switch or
  a push underneath.
  ([#170](https://github.com/henriquesakata/cerne/pull/170),
  [#173](https://github.com/henriquesakata/cerne/pull/173),
  [#174](https://github.com/henriquesakata/cerne/pull/174),
  [#178](https://github.com/henriquesakata/cerne/pull/178),
  [#179](https://github.com/henriquesakata/cerne/pull/179))
- `cerne feature complete` on a card with no delivery link lists
  `cerne feature deliver <id> --mr <url>` before the accept, and the warning
  for an unbound cost says how a session binds.
  ([#175](https://github.com/henriquesakata/cerne/pull/175))

### Fixed

- `cerne feature wait` wakes only on a channel message the session has not
  seen yet. Since 0.15.1 an unanswered message woke it again every time the
  wait was run.
  ([#164](https://github.com/henriquesakata/cerne/pull/164))
- **Update** and **Restart the panel** in `cerne ui` no longer leave the panel
  hung on its own backend lock, with port 7717 taken until `kill -9`. A second
  Ctrl+C or SIGTERM now ends a panel that is still starting.
  ([#165](https://github.com/henriquesakata/cerne/pull/165))
- With six or more `cerne ui` tabs open, the board no longer freezes after a
  change: the tabs share one live connection.
  ([#169](https://github.com/henriquesakata/cerne/pull/169))
- A session's cost binds when `cerne feature start` runs behind `rtk proxy`,
  `rtk`, `time`, `nice`, `command` or `env`.
  ([#175](https://github.com/henriquesakata/cerne/pull/175))
- At phone width the `cerne ui` top bar wraps instead of overlapping itself
  and pushing its controls off the screen.
  ([#180](https://github.com/henriquesakata/cerne/pull/180))

### Upgrading

Everything not listed here needs no action.

- **Update the binary.** Run `cerne update` in a project, or
  `brew upgrade henriquesakata/tap/cerne`, or run the install script again, or
  `go install github.com/henriquesakata/cerne/cmd/cerne@v0.15.2` (details in
  INSTALL.md, *Updating*). `cerne --version` then prints `cerne 0.15.2`. A
  `cerne ui` that is already open offers to restart into the new binary.
- **Hooks and the authoring guide changed.** They renew on their own at the
  next agent session start when nobody edited them; if you did, run
  `cerne hook install` and `cerne skill install`. On Codex,
  `cerne hook install --provider codex` brings the channel on every prompt.
- **Accept on merge is opt-in.** Add `accept = "merge"` under `[gates]` in
  `.cernerc`; it is not on the Settings screen yet.
- **Dates and times may look different.** They follow your computer's locale
  and time zone now; Settings › Interface › *Date and time* changes that.

### Known limitations

- The merge guard sees only merges an agent runs through the shell: a merge by
  `git push`, a raw HTTP call or a tool outside the shell passes. The agent and
  the human share one forge login, so the guard stops the configured agent but
  does not prove who merged.
- Waking an idle session works only on Claude Code, and not on Windows.

**Full changelog:** [v0.15.1...v0.15.2](https://github.com/henriquesakata/cerne/compare/v0.15.1...v0.15.2)

## [0.15.1] - 2026-11-21

### Fixed

- The Vault screen has a real structure: group headings with their counts,
  one line per document with the full title on hover, a filter that stays at
  the top of the tree, a heading on the empty state, and a tree and side rail
  that no longer slide under the top bar.
  ([#154](https://github.com/henriquesakata/cerne/pull/154))
- With the mirror on and no tracker set, the Mirror tab says the mirror needs
  a tracker and links to Settings › Integrations. Before, it only said no card
  was published yet.
  ([#156](https://github.com/henriquesakata/cerne/pull/156))
- Ideas imported from a tracker, and cards linked to an issue, show a badge
  with the tracker and the issue number ("GitHub issue #4"). On Work › Ideas
  the badge links to the issue; on the Backlog and kanban cards it is a label,
  since the whole card is already a link.
  ([#157](https://github.com/henriquesakata/cerne/pull/157))
- The `component-note-only` finding of `cerne lint` names each active note
  that shares no component with any feature, with the `cerne note retag` that
  clears it, instead of charging each component. The reserved markers
  `golden-path`, `convention` and `ata` no longer count as a note's subject,
  and `component-single-use` no longer suggests renaming them.
  ([#158](https://github.com/henriquesakata/cerne/pull/158))
- The feature page gives every button and select one style: *Set epic*,
  *Run roast*, *Run plan roast* and the title pencil no longer fall back to the
  browser's default look. The Agents panel is a section like Backlinks, and on
  a phone a code block no longer makes the page wider than the screen.
  ([#160](https://github.com/henriquesakata/cerne/pull/160))
- `cerne feature wait` also ends when the project channel has a message for
  the session that nobody answered yet, such as one sent from the card's page.
  It exits 4 with the message, how to answer it and the wait to run again, so
  an idle agent waiting in the background wakes up for it. A state that was
  reached still wins, and the check needs `CERNE_SESSION`.
  ([#161](https://github.com/henriquesakata/cerne/pull/161))

### Upgrading

Everything not listed here needs no action.

- **Update the binary.** Run `cerne update` in a project, or
  `brew upgrade henriquesakata/tap/cerne`, or run the install script again, or
  `go install github.com/henriquesakata/cerne/cmd/cerne@v0.15.1` (details in
  INSTALL.md, *Updating*). `cerne --version` then prints `cerne 0.15.1`. A
  `cerne ui` that is already open offers to restart into the new binary.
- **The authoring guide teaches exit 4 of `cerne feature wait`.** It renews on
  its own at the next agent session start when nobody edited it; if you did,
  run `cerne skill install`.

**Full changelog:** [v0.15.0...v0.15.1](https://github.com/henriquesakata/cerne/compare/v0.15.0...v0.15.1)

## [0.15.0] - 2026-11-20

### Highlights

- **Mirror cards to the tracker your team already uses.** A human publishes a
  card to GitHub, GitLab, Gitea, Jira or Trello with a public title and
  summary. The issue then follows the card's state, issues opened outside come
  back as ideas, and the delivery commit cites the issue. The Mirror tab shows
  what is mirrored and what drifted. Off by default.
  ([#78](https://github.com/henriquesakata/cerne/pull/78),
  [#80](https://github.com/henriquesakata/cerne/pull/80),
  [#81](https://github.com/henriquesakata/cerne/pull/81),
  [#82](https://github.com/henriquesakata/cerne/pull/82),
  [#84](https://github.com/henriquesakata/cerne/pull/84),
  [#99](https://github.com/henriquesakata/cerne/pull/99))
- **One command to update, and the project follows.** `cerne update` updates
  the binary the way you installed it and renews the project's hooks and
  authoring guide. From then on, an agent's session start renews the managed
  files nobody edited, and `cerne ui` shows an Update button and restarts into
  the new binary without losing its projects.
  ([#127](https://github.com/henriquesakata/cerne/pull/127),
  [#139](https://github.com/henriquesakata/cerne/pull/139),
  [#132](https://github.com/henriquesakata/cerne/pull/132),
  [#134](https://github.com/henriquesakata/cerne/pull/134))
- **An agent starts a task with what it needs.** `cerne context <id>` hands
  over the task's document, its epic and the notes and decisions linked to it,
  within a size budget, from the CLI or the MCP server. `.docs/CONTEXT.md` now
  holds only what is live and stays under 16 KB.
  ([#138](https://github.com/henriquesakata/cerne/pull/138),
  [#150](https://github.com/henriquesakata/cerne/pull/150),
  [#151](https://github.com/henriquesakata/cerne/pull/151))
- **Notes show up when they matter.** A note can name the files and shell
  commands it is about, and the agent gets it, with a one-line gist, when it
  edits one of those files or runs one of those commands, on every agent cerne
  installs.
  ([#88](https://github.com/henriquesakata/cerne/pull/88),
  [#90](https://github.com/henriquesakata/cerne/pull/90))
- **More ways to get cerne.** Claude Code installs it from cerne's own plugin
  marketplace, the `cerne mcp` server is listed in the official MCP Registry
  with an MCP Bundle in every release, and outside a cerne project the server
  answers with the guide instead of failing.
  ([#77](https://github.com/henriquesakata/cerne/pull/77),
  [#76](https://github.com/henriquesakata/cerne/pull/76),
  [#79](https://github.com/henriquesakata/cerne/pull/79))

### Added

- Issue mirror on GitHub. A human publishes a card with
  `cerne feature mirror <id> --title <public title> --summary <public summary>`;
  only that public text, the card id and its state leave the backend. The issue
  then follows the card's state on every transition: open with a
  `cerne:<state>` label, closed as completed on accept, closed as not planned on
  reject and supersede. A failed or slow tracker never blocks the transition: it
  prints one warning line, and `cerne mirror sync` reapplies the state. The sync
  also imports outside issues as ideas, and promoting one hands its issue to the
  card. With the mirror on, `prepare-commit-msg` adds `Refs:` (or `Closes:` with
  `mirror.close_on = "merge"`) for the cards a commit cites in its `Cerne:`
  trailers. Off by default; `mirror.enabled` and `mirror.trackers` are
  human-only, and a tracker `label` in cerne's own `cerne:` namespace is
  refused.
  ([#78](https://github.com/henriquesakata/cerne/pull/78),
  [#111](https://github.com/henriquesakata/cerne/pull/111))
- GitLab issue mirror. `kind = "gitlab"` in `mirror.trackers` mirrors cards
  into the issues of a GitLab project, on gitlab.com or self-hosted, with
  subgroups in the `url` (`https://<host>/<group>/<subgroup>/<project>`). It
  talks through `glab` and its `glab auth login`; no token goes into
  `.cernerc`. GitLab has no "not planned" close reason, so a rejected or
  superseded card closes its issue with the `cerne:rejected` or
  `cerne:superseded` label. Needs GitLab 12.3 or newer (`add_labels` and
  `remove_labels`).
  ([#80](https://github.com/henriquesakata/cerne/pull/80))
- Gitea issue mirror. A `mirror.trackers` record with `kind = "gitea"` and an
  `https://<host>/<owner>/<repo>` URL mirrors cards as issues on that Gitea
  repository, with the login `git credential fill` holds for the host: it
  travels only in the `Authorization` header, and a redirect is never
  followed. Gitea has no close reason, so a rejected or superseded card's
  issue closes with its `cerne:rejected` or `cerne:superseded` label, created
  when the repository lacks it. An IP address or `localhost` host is refused.
  ([#81](https://github.com/henriquesakata/cerne/pull/81))
- Trello board mirror. A `mirror.trackers` record with `kind = "trello"` and
  an `https://trello.com/b/<boardId>/<slug>` URL mirrors cards as cards on that
  board. A card sits in the list of its state (`Candidate`, `Approved`,
  `In development`, `Ready`, `Done`); a list the board lacks, or one a third
  party renamed or archived, is created at the end of the board, never twice.
  Rejected and superseded archive the card where it is, with one comment giving
  the reason; done stays visible in `Done`. `cerne mirror sync` imports the
  open cards outside `Done` as ideas. Trello reads no keyword in a commit, so
  the trailer is always `Refs: https://trello.com/c/<shortLink>`. The login is
  what `git credential fill` holds for `api.trello.com`, the API key as the
  username and the token as the password (store it with
  `git credential approve`); it travels only in the `Authorization` header, and
  a redirect is never followed.
  ([#84](https://github.com/henriquesakata/cerne/pull/84))
- Jira issue mirror. A `mirror.trackers` record with `kind = "jira"` and an
  `https://<site>/browse/<KEY>` URL mirrors cards as issues of that Jira
  project, on Jira Cloud or Data Center (a context path such as
  `/jira/browse/<KEY>` too). The login is the one `git credential fill` holds
  for the site: on Cloud the account e-mail and an API token, on Data Center a
  personal access token. It travels only in the `Authorization` header, and a
  redirect is never followed. The issue moves through the project's own
  workflow by status category (To Do, In Progress, Done), with no status names
  to configure; when no transition reaches the category, the issue gets a
  `cerne:<state>` label and the transition prints a warning. A rejected or
  superseded card's issue resolves as `Won't Do` when the project has it,
  otherwise as `Done` with its `cerne:<state>` label. The commit trailer is
  `Refs: <KEY>-<n>`, and `mirror.close_on = "merge"` changes nothing on Jira.
  An IP address or `localhost` host is refused.
  ([#82](https://github.com/henriquesakata/cerne/pull/82),
  [#112](https://github.com/henriquesakata/cerne/pull/112))
- Mirror tab in the UI, read-only. It lists, per tracker, the cards a human
  mirrored (state, issue link) and the ideas `cerne mirror sync` imported.
  **Check now** asks each tracker only for its open issues and marks each card
  up to date, drifted (closed on one side only) or not checked, with the
  reason; opening the tab calls no tracker. Each card expands to the commits
  whose `Cerne:` trailer cites it, from the local git. `cerne mirror sync` now
  keeps the time and per-tracker counts of its last run in the local cache,
  shown as "last sync on this computer". Both say when a tracker's list of open
  issues stopped at 100.
  ([#99](https://github.com/henriquesakata/cerne/pull/99),
  [#116](https://github.com/henriquesakata/cerne/pull/116))
- Settings › Integrations shows `delivery.forge`, `delivery.commit_url`,
  `delivery.pr_url`, `mirror.enabled`, `mirror.trackers` and
  `mirror.close_on`. The existing `[delivery]` block is read and written as it
  is. `mirror.trackers` is a list of trackers, edited as a table in the UI and
  as JSON in `cerne settings set`. `cerne guide` answers, for each tracker, the
  shape of its address and which credential it reads.
  ([#78](https://github.com/henriquesakata/cerne/pull/78),
  [#114](https://github.com/henriquesakata/cerne/pull/114))
- `cerne update`: one command, run inside a project, updates the binary and
  the project's cerne files. The binary goes to the latest release the way it
  was installed: through `brew upgrade henriquesakata/tap/cerne` under
  Homebrew; otherwise it downloads the release file, checks it against
  `SHA256SUMS` and swaps the executable in place, keeping the old one when the
  check or the swap fails. A local build, a `go install`, a folder you cannot
  write or an agent session leave the binary alone and print the command to
  run. Then the new binary renews the hooks and the authoring guide of the
  agents already connected, like `cerne hook install` plus
  `cerne skill install`. It ends with one line per part (binary, hooks, guide)
  and exits non-zero when a part was not done. `cerne update --check` lists
  what is out of date without writing or using the network.
  ([#127](https://github.com/henriquesakata/cerne/pull/127))
- The project's hooks and authoring guide renew by themselves. At an agent's
  session start and in `cerne next`, cerne renews every connected agent whose
  managed files all match what some earlier release wrote, and says so in one
  line. An agent with a file you edited, or one a newer cerne wrote, is left
  untouched, and the notice names the files. Each file is replaced through a
  temporary file, so a hook script that is running keeps its old content.
  ([#139](https://github.com/henriquesakata/cerne/pull/139),
  [#141](https://github.com/henriquesakata/cerne/pull/141))
- `cerne ui` shows **Update to X.Y.Z** when a newer release exists, and one
  click updates cerne the way `cerne update` does. A binary the panel cannot
  replace (a local build, `go install`, a folder it cannot write) shows
  **X.Y.Z available** with the command to run instead. The panel asks GitHub
  for the latest release when it starts and every 24 hours while it is open;
  the host setting `ui.update_check` turns that off.
  ([#134](https://github.com/henriquesakata/cerne/pull/134))
- `cerne ui` notices when the cerne on disk is a different build from the one
  running (every 30 seconds, without the network), shows both versions and
  **Restart the panel**, and comes back on the same port with every registered
  project; open tabs reload where they were. A `cerne ui` started from the new
  binary restarts the panel instead of asking you to stop it.
  ([#132](https://github.com/henriquesakata/cerne/pull/132))
- `cerne context <id> [--budget N] [--json]` gives an agent what it needs to
  start a task: the task's current document whole, its epic, and around it, in
  order, the notes and decisions linked to the task, the notes whose triggers
  or cited files match a file the spec or plan names, the notes that share a
  component, and a few more from search. Linked notes and decisions come with
  their body, the rest as id, title and gist. Past the budget the outer rings
  leave first and the last line counts what left; the document is never cut.
  It reads only the local clone and index. The MCP server offers the same as
  the `context` tool, and `cerne feature start`, `cerne work start --ref` and
  `cerne handoff take` end with the index of those notes and the command.
  ([#150](https://github.com/henriquesakata/cerne/pull/150),
  [#151](https://github.com/henriquesakata/cerne/pull/151))
- `cerne note link` and `cerne decision link <id> --informs <ids>
  [--unlink <ids>]` (also `cerne decision new --informs`) link a note or a
  decision to the work it informs; `cerne show` on a decision lists it.
  `cerne lint` reports, as advice, a note no task can reach
  (`note-unreachable`).
  ([#150](https://github.com/henriquesakata/cerne/pull/150),
  [#151](https://github.com/henriquesakata/cerne/pull/151))
- Components as a shared vocabulary. `cerne tags` lists the components in use
  with a count per kind of entity; `cerne tags rename <from> <to>` renames one
  on every entity in a single commit (`--dry-run` shows the change first, and
  renaming onto a name in use merges the two); `--component a,b` on
  `note list`, `decision list` and `reference list` ranks the entries that
  carry any of them. `cerne lint` flags, as advice, a component used once,
  names that share a root and components only notes use.
  ([#138](https://github.com/henriquesakata/cerne/pull/138),
  [#144](https://github.com/henriquesakata/cerne/pull/144))
- `retag` works on entities in any state, and `cerne idea retag` and
  `cerne pending retag` are new.
  ([#135](https://github.com/henriquesakata/cerne/pull/135))
- A note can say when it shows up: `cerne note new --when 'edit:<glob>'` or
  `--when 'bash:<command>'`, and `cerne note when <id> <trigger>...` to replace
  them (`--clear` removes them). When an agent edits a matching file or runs a
  matching command, it gets up to three matching notes per event, each once per
  session: before the tool on Claude Code and Codex, after it on Cursor, Gemini
  and Copilot, where the block says the command already ran.
  ([#88](https://github.com/henriquesakata/cerne/pull/88))
- `cerne note new --gist "<one line>"` and `cerne note gist <id> "<one line>"`
  (or `--clear`) give a note a summary of at most 160 characters. It is
  printed under the note's title in the edit and command blocks, and by
  `cerne show`.
  ([#90](https://github.com/henriquesakata/cerne/pull/90))
- `cerne feature wait <id> --for <state> [--timeout 24h] [--interval 30s]`:
  an agent leaves it running in the background at a human gate and is woken
  when the human approves or accepts. It refreshes the backend clone without
  touching `.docs/` or `docs.lock`, and exits 0 when the state is reached (an
  approval gone stale does not count), 2 when the state can no longer be
  reached and 3 on timeout. The gate messages teach it to agents whose harness
  wakes them when a background command ends.
  ([#93](https://github.com/henriquesakata/cerne/pull/93))
- The feature page has an **Agents** panel with the sessions that opened the
  card or wrote its spec, plan or result, each with its folder, branch, PR and
  last activity (read from the transcript's metadata, never from the
  conversation), and a chat that sends a message only to the sessions you pick,
  through the project channel.
  ([#149](https://github.com/henriquesakata/cerne/pull/149))
- A **Vault** screen (`#/vault`): every project document in a tree, the open
  one with clickable `[[ID]]` links, its outline and its backlinks. A live
  document can be edited in place beside a preview, with `[[` completing
  entity ids; saving commits and pushes as you and never overwrites a change
  another agent made in the meantime. Concluded documents stay read-only.
  ([#95](https://github.com/henriquesakata/cerne/pull/95),
  [#100](https://github.com/henriquesakata/cerne/pull/100))
- Criteria tied to tests. A test with the comment `F-NNNN crit. N` (`T-NNNN`
  for a task; for a bug, its id alone on the regression test) proves that
  criterion. `cerne lint` reports `criterion-without-test` and
  `orphan-criterion-test`, and `cerne feature complete` refuses while either
  stands. cerne reads the marks and never runs the tests. It applies only to
  work started from 2026-11-21; `test_roots` and `test_patterns` in `.cernerc`
  say where the tests are, and the setting `lint.criterion_tests` turns it off.
  ([#124](https://github.com/henriquesakata/cerne/pull/124))
- Each release ships `cerne_<tag>.mcpb`, an MCP Bundle of the read-only
  `cerne mcp` server with the binaries for every platform, and the
  `server.json` that lists it in the official MCP Registry as
  `io.github.henriquesakata/cerne`. A client that installs MCP Bundles asks
  for the project folder and starts the server on it.
  ([#76](https://github.com/henriquesakata/cerne/pull/76))
- Claude Code can install cerne from the repository's own plugin marketplace:
  `/plugin marketplace add henriquesakata/cerne`, then
  `/plugin install cerne@cerne`. The plugin brings the authoring skills, the
  hooks and the MCP server that `cerne init` sets up for Claude Code; a hook
  the project already has takes precedence, and every hook stays quiet outside
  a cerne project. The plugin does not install the binary: its session start
  says how.
  ([#77](https://github.com/henriquesakata/cerne/pull/77))
- First use. `cerne init` ends with three first steps, the agents it connected
  and one line to connect the others; the per-file detail moved behind
  `--verbose`. `cerne next` and the dashboard show a first-steps checklist
  until the first accept. `cerne import` ends with what came in and what to do
  next, and `--delivered-state implemented|done` says where delivered features
  land (default `implemented`). A first-week guide, `docs/first-week.md`, is
  linked from the README, and `cerne guide` now answers what cerne is and what
  to do after installing.
  ([#83](https://github.com/henriquesakata/cerne/pull/83),
  [#104](https://github.com/henriquesakata/cerne/pull/104),
  [#105](https://github.com/henriquesakata/cerne/pull/105),
  [#106](https://github.com/henriquesakata/cerne/pull/106),
  [#107](https://github.com/henriquesakata/cerne/pull/107),
  [#108](https://github.com/henriquesakata/cerne/pull/108),
  [#109](https://github.com/henriquesakata/cerne/pull/109))
- `cerne hook install --provider codex` writes a `cerne` sandbox profile into
  `.codex/config.toml` with cerne's cache and local backend folders, and says
  how to trust the project; `cerne init` shows a Connect step for an agent CLI
  it detects. `--network` adds network access to that profile, never by
  default, and the two errors a cut network causes inside Codex say so.
  ([#128](https://github.com/henriquesakata/cerne/pull/128),
  [#140](https://github.com/henriquesakata/cerne/pull/140))
- cerne has a mark, a cross-section of a tree trunk: the `cerne ui` tab icon,
  the README header and the dashboard's top bar.
  ([#91](https://github.com/henriquesakata/cerne/pull/91),
  [#92](https://github.com/henriquesakata/cerne/pull/92),
  [#94](https://github.com/henriquesakata/cerne/pull/94))

### Changed

- `.docs/CONTEXT.md` lists only live work: live features, epics with a live
  child, active decisions and runbooks, and work in progress. Concluded
  features, notes and references become counts with the command that lists
  them, and the file stays within 16 KB, saying how many features it left out.
  ([#138](https://github.com/henriquesakata/cerne/pull/138))
- A new component name close to an existing one (the same root, or a few
  letters apart) is refused with the existing name; `--force` writes it.
  ([#138](https://github.com/henriquesakata/cerne/pull/138))
- `cerne feature amend --doc spec|plan` refuses a card another session is
  working on, listing those works and what the amend would cause; `--force`
  writes anyway and records that it did.
  ([#148](https://github.com/henriquesakata/cerne/pull/148))
- `cerne pull` merges your edit and the remote one line by line when both
  changed the same document. Only overlapping lines keep yours and put theirs
  in `.docs/.conflicts/`, and `cerne push` refuses that document until you
  delete the copy (or pass `--force`).
  ([#110](https://github.com/henriquesakata/cerne/pull/110))
- `cerne work done` on a work that is already done exits 0 and changes
  nothing.
  ([#137](https://github.com/henriquesakata/cerne/pull/137))
- Outside a cerne project, `cerne mcp` serves only the `guide` tool and says
  the folder is not a cerne project, instead of exiting with an error.
  ([#79](https://github.com/henriquesakata/cerne/pull/79))
- `cerne savings` and the UI call the number what it measures, the reduction
  of the corpus the agent searches, not context savings, and show the size of
  `CONTEXT.md`.
  ([#138](https://github.com/henriquesakata/cerne/pull/138),
  [#147](https://github.com/henriquesakata/cerne/pull/147))
- The authoring guide and the session-start rules name sections and forms in
  the project's documentation language. The guide asks the agent to read the
  code a request touches before writing the spec, asks for EARS-style
  criteria, and explains when knowledge is a note and when it is a reference.
  ([#129](https://github.com/henriquesakata/cerne/pull/129),
  [#133](https://github.com/henriquesakata/cerne/pull/133),
  [#136](https://github.com/henriquesakata/cerne/pull/136),
  [#146](https://github.com/henriquesakata/cerne/pull/146))
- The Backlog chat asks one question per turn, and a message sent while the
  agent works waits in a queue instead of being blocked. The candidates column
  and the conversations rail collapse, and the browser remembers it.
  ([#121](https://github.com/henriquesakata/cerne/pull/121))
- On the feature page, the **Actions** card holds only the transition
  buttons; whether the card can be approved moved to an **Approval** card
  below it.
  ([#130](https://github.com/henriquesakata/cerne/pull/130))
- A human-only switch in Settings asks before it moves away from its default:
  turning the mirror on now asks, as turning the Gauntlet off already did.
  ([#78](https://github.com/henriquesakata/cerne/pull/78))
- The README lists what cerne does not do.
  ([#75](https://github.com/henriquesakata/cerne/pull/75))

### Fixed

- `cerne ui` answers only requests whose Host is a loopback address, for
  reads as well as writes, and a write that sends an Origin must name that same
  host. A web page that rebinds its domain to 127.0.0.1 can no longer read or
  change your documents through the panel.
  ([#115](https://github.com/henriquesakata/cerne/pull/115),
  [#119](https://github.com/henriquesakata/cerne/pull/119))
- An entity id with an encoded `../` (`%2F`) could write or delete files
  outside the backend's content folder through the local server, and
  `cerne serve` accepted a project id that cloned into and read from a folder
  outside the cache. Both are refused.
  ([#115](https://github.com/henriquesakata/cerne/pull/115),
  [#120](https://github.com/henriquesakata/cerne/pull/120))
- A Claude Code shell (`CLAUDECODE=1`) and a Codex shell (`CODEX_THREAD_ID`)
  count as agents at the human gates, so an agent can no longer approve or
  accept on a machine without cerne's hooks. The refusal says how a human in
  that shell gets through.
  ([#87](https://github.com/henriquesakata/cerne/pull/87),
  [#125](https://github.com/henriquesakata/cerne/pull/125))
- The Backlog chat's agent can no longer reach a lifecycle command through a
  hidden alias or a flag placed before the verb, and the texts agents read name
  `supersede`, not its old alias.
  ([#96](https://github.com/henriquesakata/cerne/pull/96),
  [#97](https://github.com/henriquesakata/cerne/pull/97),
  [#98](https://github.com/henriquesakata/cerne/pull/98))
- Calls to `gh` and `glab` always name the host, so `GH_HOST`, `GITLAB_HOST`
  or the repository of the working directory no longer send them to another
  server, and the runbook preflight no longer hands your other token variables
  to `glab`.
  ([#86](https://github.com/henriquesakata/cerne/pull/86),
  [#89](https://github.com/henriquesakata/cerne/pull/89))
- A push after a pull conflict no longer erases the other author's edit, and a
  markdown file in a hidden folder under `.docs/` (`.obsidian/`, `.trash/`) no
  longer traps `cerne push` in a push and pull loop.
  ([#110](https://github.com/henriquesakata/cerne/pull/110))
- A human message on the project channel reaches every live work and repeats
  until each answers or acknowledges it. Sessions also hear the channel when
  you send a prompt and before a shell command, not only at session start and
  on file edits.
  ([#126](https://github.com/henriquesakata/cerne/pull/126))
- A `cerne ui` stopped with SIGTERM leaves the panel's project list and shuts
  down cleanly.
  ([#103](https://github.com/henriquesakata/cerne/pull/103))
- `cerne room watch` releases its lock on SIGTERM, and the operating system
  now holds the lock, so a watch killed by SIGKILL or a crash no longer blocks
  every later watch on that room.
  ([#113](https://github.com/henriquesakata/cerne/pull/113),
  [#118](https://github.com/henriquesakata/cerne/pull/118))
- `cerne idea link` accepts every card kind on the board (epic, task, bug), not
  only features.
  ([#74](https://github.com/henriquesakata/cerne/pull/74))
- The notes recalled from a prompt split identifiers such as
  `JsonDateSerializer`, so a prompt that says "date serializer" finds them.
  ([#72](https://github.com/henriquesakata/cerne/pull/72))
- `cerne gauntlet artifact` accepts bug cards, and the reminder to register an
  external source fires only when a command actually runs cerne.
  ([#117](https://github.com/henriquesakata/cerne/pull/117))
- When the cache or the local backend folder does not accept writes (an agent
  sandbox, for example), cerne says so, names both folders and no longer
  suggests deleting them.
  ([#122](https://github.com/henriquesakata/cerne/pull/122))
- Outside Claude Code, `cerne next` no longer lists a delivery step with no
  command, and `cerne feature complete` no longer advises a `--stamp` that
  cannot run.
  ([#123](https://github.com/henriquesakata/cerne/pull/123),
  [#131](https://github.com/henriquesakata/cerne/pull/131))
- The War Room page puts the conversation beside its side panel again, its
  composer sends with Enter and breaks the line with Shift+Enter, and the room
  list counts rooms, not participants.
  ([#121](https://github.com/henriquesakata/cerne/pull/121))

### Upgrading

Everything not listed here needs no action.

- **Update the binary.** `brew upgrade henriquesakata/tap/cerne`, or run the
  install script again, or
  `go install github.com/henriquesakata/cerne/cmd/cerne@v0.15.0` (details in
  INSTALL.md, *Updating*). `cerne --version` then prints 0.15.0. From this
  version on, `cerne update` does this for you.
- **Hooks and the authoring guide renew on their own.** After the update, the
  next agent session start or `cerne next` renews the managed files nobody
  edited and prints one line. Files you edited are left alone and named: run
  `cerne hook install` and `cerne skill install` to review them.
- **Human gates inside an agent's shell.** `cerne feature approve` and
  `accept` typed in a Claude Code or Codex shell are now refused as an agent's.
  Run them in your own terminal, or prefix them with `CERNE_ACTOR=human`.
- **Open `cerne ui` at `localhost` or `127.0.0.1`.** Any other host name, even
  one that resolves to your machine, gets a 403.
- **`GH_HOST` and `GITLAB_HOST` no longer pick the server.** cerne passes the
  host of the project's repository or tracker URL to `gh` and `glab`.
- **`CONTEXT.md` no longer lists concluded features, notes or references.**
  An agent that read them there finds them with
  `cerne feature list --state done`, `cerne note list` or `cerne search`.
- **Work started from 2026-11-21 needs its criteria tied to tests.** Mark each
  proving test with `F-NNNN crit. N`, or end an untested criterion with
  `(manual verification)`. If your tests are outside the default places, set
  `test_roots` and `test_patterns` in `.cernerc`;
  `cerne settings set lint.criterion_tests false` turns the rule off.
- **A remote copy in `.docs/.conflicts/` now blocks its push.** Reconcile the
  document, delete the copy and push again, or use `cerne push --force`.
- **New component names are checked.** A name close to one already in use is
  refused: reuse the existing one (`cerne tags` lists them), or pass `--force`.
- **`cerne ui` checks for new releases.** It asks GitHub when it starts and
  every 24 hours. Turn it off with
  `cerne settings set --scope host ui.update_check false`.

### Known limitations

- The note triggers on Gemini follow Gemini's documented hook format but were
  not checked against a live Gemini session.
- Cursor and the Copilot CLI drop the context of a prompt hook, so there the
  project channel arrives only at the other points (session start, after a
  command or an edit).
- Restarting the panel into a new binary was checked by hand on macOS only; on
  Windows the new binary starts as a child process in the same console.
- `cerne mirror sync` and **Check now** read at most 100 open issues per
  tracker, and say so when a list stops there.

**Full changelog:** [v0.14.3...v0.15.0](https://github.com/henriquesakata/cerne/compare/v0.14.3...v0.15.0)

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

[Unreleased]: https://github.com/henriquesakata/cerne/compare/v0.16.0...HEAD
[0.16.0]: https://github.com/henriquesakata/cerne/compare/v0.15.2...v0.16.0
[0.15.2]: https://github.com/henriquesakata/cerne/compare/v0.15.1...v0.15.2
[0.15.1]: https://github.com/henriquesakata/cerne/compare/v0.15.0...v0.15.1
[0.15.0]: https://github.com/henriquesakata/cerne/compare/v0.14.3...v0.15.0
[0.14.3]: https://github.com/henriquesakata/cerne/compare/v0.14.2...v0.14.3
[0.14.2]: https://github.com/henriquesakata/cerne/compare/v0.14.1...v0.14.2
[0.14.1]: https://github.com/henriquesakata/cerne/compare/v0.14.0...v0.14.1
[0.14.0]: https://github.com/henriquesakata/cerne/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/henriquesakata/cerne/releases/tag/v0.13.0
