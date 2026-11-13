## Permanent channel (F-0325): quick coordination, no ceremony

Every project has one always-on channel for short exchanges between agents with open work and the human ("touching `internal/server/rooms.go`, hold on"; "contract done, consume it"). No room id, no open/close/minutes — you speak as your session's live work (register it with `cerne work start` first). Pending messages reach you on their own at session start, in subagents, in `cerne next`, `cerne status --active` and before every Edit/Write, capped to a few lines; a message that names your W-, one of your work's refs or a path in its `--paths` is **addressed to you** and repeats until you answer it.

    cerne msg send "<text>"     # post as your work; says which works with a live session will get it
    cerne msg read [--json]     # the whole live channel, each message's delivery state (not_delivered / delivered / answered)
    cerne msg ack <seq>...      # silence an addressed message without replying (any later message of yours also counts)

Messages expire after 24h, or when the sender's work closes. No read receipts: the delivery state is derived, never typed.

## War room (F-0211/F-0226/F-0227/F-0228): live multi-agent conversation

A **war room** is a live, ephemeral chat channel served by a running `cerne ui` — for real-time back-and-forth between agents/humans on a shared problem, distinct from the async `cerne work` registry above. **Opening a room is a human moderator's act (F-0211/F-0217).** A human opens one with `cerne room open "<topic>"` — you do **not** run this yourself; you join a room when invited, and only ever run `cerne room open` on an explicit human request.

**The cycle (F-0226: a joined session capability, not `--nick`, is the authority for everything you say):**

```
cerne room list                                                    # discover open rooms
cerne room join <id> --nick <apelido> [--bio "..."]                 # mints a session capability, once per room — keep it
export CERNE_ROOM_SESSION=<printed by join>                         # or pass --session on every call below
cerne room say <id> "<msg>" --session <token>                        # speak, authenticated by the session's nick
cerne room watch <id> --session <token> [--once]                     # F-0227: turn-based wakeup with a durable cursor, filters your own echo
cerne room read <id> --since <seq> --once                            # F-0220 primitive: stateless wakeup, no cursor/session/echo-filter
cerne room close <id> --facilitator <token> --minutes <...>          # F-0228: facilitator-only, structured minutes — see below
```

**`room watch --once`, not `room read`, for a turn-based agent (F-0227).** `watch` wraps the same long-poll primitive as `read` with what a turn-based agent actually needs: a durable cursor (a restart resumes where you left off — no manual `--since` bookkeeping), and it filters out your **own** messages so `say`-ing something doesn't consume your own wakeup. `--once` long-polls until the first **relevant** (non-echo) event, prints it, and exits 0; a room `close` event always ends the loop, printed, regardless of `--once`. `--session`/`CERNE_ROOM_SESSION` is required (same convention as `say`); the cursor is a local file, not a `--since` you track by hand. `watch` never blocks forever on its own the way `--follow` would — retry a transient connection failure with its own bounded backoff, or re-run `--once` for the next turn.

`room read --once`/`--follow` is still there underneath as the stateless primitive (no session required, no cursor persisted) — reach for it in a script or a one-off check where you don't want a cursor file on disk, not as the default turn-based wakeup.

**Facilitation and structured close (F-0228).** `cerne room open` also mints a **facilitator capability**, printed once and separate from any participant session — it is the authority to triage and close the room, never appears in the roster/transcript/list, and rotates atomically via `cerne room facilitator handoff <id> --facilitator <current>` (prints a new one, the old one stops working immediately — the way an opener agent hands the role to a human over an external channel). Any joined participant can raise something the facilitator needs to see:

```
cerne room propose <id> --session <token> --title "<t>" --body "<why>"           # a proposal
cerne room request-decision <id> --session <token> --title "<t>" --body "<why>"  # a decision request
cerne room pending <id> --facilitator <token>                                     # facilitator's triage queue (no LLM synthesis)
cerne room resolve <id> <item-id> --facilitator <token> --result "<outcome>"       # facilitator-only
```

Closing requires the facilitator capability and a structured **minutes** JSON (`conclusion`/`rationale` required, `discarded_options`/`owners`/`next_steps` may be empty lists) — `cerne room minutes template` prints a blank scaffold to fill in. By default close **refuses** while facilitation items are still open and lists them; `--allow-open-items` forces it through and records the exception in the minutes. `cerne room minutes show <id> --format markdown` renders a closed room's minutes deterministically (no LLM) for copying elsewhere — it never creates a feature/decision/note or commits anything on its own.

**Registering the minutes as a governed note (F-0238).** A closed room's ata lives only in the room's transcript — invisible to `search`/`ask`, with no backlinks — until someone explicitly bridges it: `cerne room minutes register <id> [--informs F-…,D-…] [--facet f] [--component c]`. This is the **only** path that creates anything durable from a room; it is never automatic on close. It reads the closed room's minutes via the server, creates a **note** (N-) whose body is the same deterministic projection as `minutes show` plus provenance (objective, start/end, participants), always tagged with the reserved component `ata` (filter with `cerne note list --component ata`), and records the link on the room (shown by the UI as "Ata registrada: N-NNNN"). It refuses an open room (close it first) and refuses to re-register an already-linked room unless `--force` (e.g. an errata). After registering, cite the note's id-form wiki-link (`[[N-NNNN]]`) in the Origem line of any spec/decision the meeting produced — that backlinks the graph from room → minutes → derived entities.

**Troubleshooting a failed `cerne room join`, self-diagnosable without a human:**
- **Connection error ("no cerne ui answering ... start one with 'cerne ui'")** → no `cerne ui` is listening on the port (default 7717). Start one: `cerne ui` in the project directory.
- **"no cerne ui running this project" (404)** → a `cerne ui` is up but not serving *this* project directory. Not yours to fix — tell the human to restart `cerne ui` in the correct project directory.
- **Conflict, nick already taken (409)** → someone else in the room already holds that `--nick`. Pick a different one and retry.
