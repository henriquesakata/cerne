## Permanent channel (F-0325): quick coordination, no ceremony

Every project has one always-on channel for short exchanges between agents with open work and the human ("touching `internal/server/rooms.go`, hold on"; "contract done, consume it"). No room id, no open/close/minutes — you speak as your session's live work (register it with `cerne work start` first). Pending messages reach you on their own at every contact point — the `cerne feature wait` you left running in the background (it exits 4 with the messages addressed to you that you have not seen yet, B-0065/B-0066, so an idle session wakes up), session start, subagents, your user's next prompt, before every shell command and every Edit/Write (after them on Cursor, Gemini and Copilot), `cerne next` and `cerne status --active` — capped to a few lines; a message that names your W-, one of your work's refs or a path in its `--paths` is **addressed to you**, and so is every message the human sends while you have live work: it repeats until you answer it (any `cerne msg send` of yours after you saw it) or ack it — at each turn start (session start, your user's next prompt), while inside a turn only its first arrival shows it (F-0490). A human's message reads as a report: who wrote it, when, the text in quotes, and the reply it expects.

    cerne msg send "<text>"     # post as your work; says which works with a live session will get it
    cerne msg read [--json]     # the whole live channel, each message's delivery state (not_delivered / delivered / answered)
    cerne msg ack <seq>...      # silence an addressed message without replying (any later message of yours also counts)

Messages expire after 24h, or when the sender's work closes. No read receipts: the delivery state is derived, never typed.

**From a card's page (F-0486).** The human can message the sessions that worked on a card (opened it, wrote its spec, plan or result, implemented or reviewed it) from its page in `cerne ui`. That message says it was written to your session from the card's page and reaches only those sessions, even when your work on the card already closed; it repeats until you answer with `cerne msg send --ref F-NNNN "<text>"` (the answer goes to the card's thread for the human, not to the other agents; with no live work `--ref` may be left out, and the send is still accepted) or `cerne msg ack <seq>`. Without `--ref`, a session with live work posts a plain channel message, as always.

## War room (F-0211/F-0226/F-0227/F-0228): live multi-agent conversation

A **war room** is a live, ephemeral chat channel served by a running `cerne ui` — for real-time back-and-forth between agents/humans on a shared problem, distinct from the async `cerne work` registry above. **Opening a room is a human moderator's act (F-0211/F-0217).** A human opens one with `cerne room open "<topic>"` — you do **not** run this yourself; you join a room when invited, and only ever run `cerne room open` on an explicit human request.

**The cycle (F-0226: a joined session capability, not `--nick`, is the authority for everything you say):**

```
cerne room list                                                    # discover open rooms
cerne room join <id> --nick <nick> [--bio "..."]                 # mints a session capability, once per room — keep it
cerne room say <id> "<msg>"                                          # speak, authenticated by the session's nick
cerne room watch <id> [--once]                                       # F-0227: turn-based wakeup with a durable cursor, filters your own echo
cerne room read <id> --since <seq> --once                            # F-0220 primitive: stateless wakeup, no cursor/session/echo-filter
cerne room leave <id>                                                # F-0511: revokes your session capability; join again for a new one
cerne room close <id> --minutes <...>                                # F-0228: facilitator-only, structured minutes — see below
```

**Keep the capability off the command line (B-0084).** The session that `join` prints, and the facilitator capability that `open` prints, are bearer tokens: whoever reads one speaks as your nick, or resolves items and closes the room. A `--session`/`--facilitator` value or an inline `CERNE_ROOM_SESSION=<…>` prefix shows in the process list for as long as the command runs (an agent harness keeps each shell call's whole command line in the argv of its shell wrapper, and a long `watch --once` stays there for hours), in the shell history and in agents' transcripts — so the commands above carry none:
- **In an agent session** (`CERNE_SESSION` is set, exported by the SessionStart hook), `join` stores the session in a 0600 file of the cerne cache, one per room and per agent session, and every participant command finds it with no flag and no variable. An `export` would not help an agent anyway: it does not carry over from one shell call to the next.
- **In a human terminal** (no `CERNE_SESSION`), nothing is stored: `export CERNE_ROOM_SESSION=<printed by join>` once in that shell, then run the commands as shown.
- **The facilitator capability is never stored**, in any session: stored under the opener's `CERNE_SESSION`, every subagent of that session would hold the facilitator role. `export CERNE_ROOM_FACILITATOR=<printed by open>` in the shell that runs `pending`, `resolve`, `close` and `facilitator handoff`. Those commands are short-lived, so in an agent harness the capability can still sit briefly in the argv of the shell wrapper while one runs; that residual is accepted to keep the facilitator role apart from the participants.
- `--session`/`--facilitator` and `CERNE_ROOM_SESSION`/`CERNE_ROOM_FACILITATOR` still work; the session ones win over the stored session, in that order.
- A subagent shares its parent's `CERNE_SESSION`. If this agent session already keeps another nick in the room and that session still works, `join` does not replace it: it joins, warns, and prints the `export` form for the new nick.

This guards against accidental exposure, not against a process of the same user that goes looking: it can still read the cache or another process's environment.

**Observe without joining; leave when you are done (F-0511).** Reading needs no session: `cerne room read` is how you observe a room, and joining is only what lets you publish. When you are done, `cerne room leave <id>` revokes your capability on the server — it stops working for `say`, `watch`, `propose` and `request-decision`, a `leave` event goes into the transcript, and you drop out of the room's `active_participants` while staying in its historical `participants` (the minutes still list you). The nick is free again: a later `cerne room join` mints a new capability. Leaving never touches the facilitator capability. Closing a terminal or tab does not leave.

**`room watch --once`, not `room read`, for a turn-based agent (F-0227).** `watch` wraps the same long-poll primitive as `read` with what a turn-based agent actually needs: a durable cursor (a restart resumes where you left off — no manual `--since` bookkeeping), and it filters out your **own** messages so `say`-ing something doesn't consume your own wakeup. `--once` long-polls until the first **relevant** (non-echo) event, prints it, then keeps printing what arrives until `--settle` (default `1.5s`) passes with no new relevant event, at most 5s after the first, and exits 0: a burst of replies wakes you once, not once per message. Your own echo inside that window is skipped but still advances the cursor; `--settle 0` exits on the first batch; `cerne room watch <id> --once --settle 3s` waits longer for a slow burst. A room `close` event always ends the loop at once, printed, regardless of `--once` or `--settle`. It needs your session, found the same way as for `say`; the cursor is a local file, not a `--since` you track by hand. `watch` never blocks forever on its own the way `--follow` would — retry a transient connection failure with its own bounded backoff, or re-run `--once` for the next turn.

`room read --once`/`--follow` is still there underneath as the stateless primitive (no session required, no cursor persisted) — reach for it in a script or a one-off check where you don't want a cursor file on disk, not as the default turn-based wakeup.

**Facilitation and structured close (F-0228).** `cerne room open` also mints a **facilitator capability**, printed once and separate from any participant session — it is the authority to triage and close the room, never appears in the roster/transcript/list, and rotates atomically via `cerne room facilitator handoff <id>` (prints a new one, the old one stops working immediately — the way an opener agent hands the role to a human over an external channel; the new one is for the next bearer, so nothing is stored). A room opened in `cerne ui` keeps the capability in that browser. Lost it (a handoff nobody copied, a cleared browser)? A human runs `cerne room facilitator reset <id>`: it mints a new one without the current, the running `cerne ui` accepts it at once, and an agent is refused (B-0083) — ask the human, never work around it. Any joined participant can raise something the facilitator needs to see:

```
cerne room propose <id> --title "<t>" --body "<why>"           # a proposal
cerne room request-decision <id> --title "<t>" --body "<why>"  # a decision request
cerne room pending <id>                                         # facilitator's triage queue (no LLM synthesis)
cerne room resolve <id> <item-id> --result "<outcome>"          # facilitator-only
```

Closing requires the facilitator capability and a structured **minutes** JSON (`conclusion`/`rationale` required, `discarded_options`/`owners`/`next_steps` may be empty lists) — `cerne room minutes template` prints a blank scaffold to fill in. By default close **refuses** while facilitation items are still open and lists them; `--allow-open-items` forces it through and records the exception in the minutes. `cerne room minutes show <id> --format markdown` renders a closed room's minutes deterministically (no LLM) for copying elsewhere — it never creates a feature/decision/note or commits anything on its own.

**Registering the minutes as a governed note (F-0238).** A closed room's ata lives only in the room's transcript — invisible to `search`/`ask`, with no backlinks — until someone explicitly bridges it: `cerne room minutes register <id> [--informs F-…,D-…] [--facet f] [--component c]`. This is the **only** path that creates anything durable from a room; it is never automatic on close. It reads the closed room's minutes via the server, creates a **note** (N-) whose body is the same deterministic projection as `minutes show` plus provenance (objective, start/end, participants), always tagged with the reserved component `ata` (filter with `cerne note list --component ata`), and records the link on the room (shown by the UI as "Ata registrada: N-NNNN"). It refuses an open room (close it first) and refuses to re-register an already-linked room unless `--force` (e.g. an errata). After registering, cite the note's id-form wiki-link (`[[N-NNNN]]`) in the Origem line of any spec/decision the meeting produced — that backlinks the graph from room → minutes → derived entities.

**Troubleshooting a failed `cerne room join`, self-diagnosable without a human:**
- **Connection error ("no cerne ui answering ... start one with 'cerne ui'")** → no `cerne ui` is listening on the port (default 7717). Start one: `cerne ui` in the project directory.
- **"no cerne ui running this project" (404)** → a `cerne ui` is up but not serving *this* project directory. Not yours to fix — tell the human to restart `cerne ui` in the correct project directory.
- **Conflict, nick already taken (409)** → someone else in the room already holds that `--nick`. Pick a different one and retry.
