## Q: How do I change a project setting from the CLI?
Use `cerne settings set <key> <value> --scope project`. `--scope` is required: `project` writes `.cernerc`, `host` writes the user's `settings.toml`. See every key, its value and where it comes from with `cerne settings list`, read one with `cerne settings get <key>`, remove an override with `cerne settings unset <key> --scope project`. Owner-only keys such as `gates.gauntlet` refuse an agent (`CERNE_ACTOR=agent`): ask the owner.

## Q: Where is the cerne UI API? Which URL does the dashboard call?
`cerne ui` serves every project from one process, so each route is under `/api/projects/<pid>/…`, where `<pid>` is the project id of `.cernerc`. `GET /api/projects` lists the registered ids. A bare `/api/<route>` is not the UI API. The default port is 7717 (`cerne ui --port`).

## Q: How is the before and after of a process marker dated?
A process marker is a decision with facet `process` and component `metric`. `cerne stats --process` judges the 28 days before and after it, and the date it uses is when the decision was created (`cerne decision new`), not when the change shipped. Create the marker decision the day the change goes live, or the window is wrong.

## Q: How does the judge's cost reach the feature? Why is the judge missing from by_role?
The Gauntlet's critic and judge run no cerne command, so their tokens are not bound to the feature. After launching one, run `cerne cost attribute <agent-id> <feature-id> --role judge` (or `--role critic`). A `general-purpose` agent has no role of its own, so without `--role` it is not counted as the judge. Check the result with `cerne cost <feature-id>`, whose By role block lists builder, critic, judge, other and main.

## Q: How do I get the id of an idea I just created?
`cerne idea new <text>` prints the id on its first line (`I-0007 created (active) — …`). Lost it? `cerne idea list` shows ids, newest first. Turn it into a candidate with `cerne idea promote <id>`, or drop it with `cerne idea drop <id> --reason "<why>"`.

## Q: How do I turn the Gauntlet off? Light mode without the bar and the judge.
The Gauntlet is the setting `gates.gauntlet` (`on` or `off`). The owner flips it in the Settings screen of `cerne ui` or with `cerne settings set gates.gauntlet off --scope project`; the command refuses an agent. With it off, `cerne feature approve` takes a spec with no `Barra:` line and `cerne feature complete` takes a result with no `## Gauntlet` section, but each criterion still needs its evidence. `cerne status` prints `Gauntlet: off (light mode)`. An agent never switches it to get past a gate.

## Q: How do I hand my work to another agent? Pass the baton.
Record the handoff with `cerne work handoff <W-id> --next "<next step>" --file brief.md --evidence <ref>,<ref>`; it prints an `HO-` code. Pass only that code. The receiver runs `cerne handoff take <HO-id>`, which opens its own work, closes yours and prints the brief. Review pending ones with `cerne handoff list`, withdraw with `cerne handoff cancel <HO-id>`. Add `--if "<condition> → <action>"` once per contingency.

## Q: How do I register an external source, an article, a link or a spec?
`cerne reference new "<title>" --url <url> --informs F-0001,D-0002 --file summary.md` (or pipe the summary on stdin). The reference is marked external, its text is indexed so `cerne ask` cites it, and `--informs` links it to the entities it motivated. A bare URL in a decision or feature makes `cerne lint` report `unregistered-source`. A live reference with the same URL is refused unless `--force`.

## Q: Why does cerne ui serve an old screen or a 404 after I reinstalled cerne?
A `cerne ui` process started before `go install` keeps serving the embedded UI and API of the old binary. Stop that process (it listens on port 7717) and start `cerne ui` again.

## Q: How do I start working on a feature and finish it?
`cerne feature new "<title>"` registers the candidate, `cerne pull` materializes `.docs/`, fill the spec and plan, `cerne push`, then the human runs `cerne feature approve <F-id>`. The builder runs `cerne feature start <F-id>`, writes the result, `cerne push`, then `cerne feature complete <F-id>`. `cerne feature accept <F-id>` is the human validation step that makes it done.

## Q: How do I read the project state without loading everything?
`cerne brief` is the cheapest map (id, state, title). `cerne status --active` shows only active work, `cerne next` the next action per feature plus documentation debts. Read one entity with `cerne show <id>`, search by subject with `cerne search "<query>"`.

## Q: Where do the learnings from a result go? How do I reuse what a past agent learned?
A result's learnings are history and do not reach the next agent by themselves. `cerne feature complete` lists them. A durable one becomes a note with `cerne note new "<title>" --informs <F-id> --component <topic>`; first find a note that already covers it (`cerne note list --path <file>` or `cerne search "<terms>"`): complement it with `cerne note errata <N-old> ...`, or if it contradicts, replace it with `cerne note substituir <N-old> --by <N-new> --reason "..."`; none durable: `cerne feature learnings --skip <F-id>`. `cerne feature learnings` lists, read-only, the past features nobody triaged yet.
