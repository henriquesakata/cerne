---
name: cerne-self-learning
description: "Harvest a hard-won golden path from the current session into the cerne backend, so the next session (yours, a teammate's, another agent's) starts already knowing the route. Use it (1) right after non-trivial debugging, after a task only worked following several attempts or a user correction, after working out a multi-step operational workflow (deploy, migration, environment access, live verification), or after discovering project facts you didn't know up front; and (2) whenever the user says 'remember this', 'lembra disso', 'save this', or 'don't make me re-explain this next time'. Act on the cue proactively, without being asked: triage the lesson into a cerne runbook (procedure), a cerne note (fact/gotcha), local agent memory (personal/machine detail), or skip — then report what was captured and its id."
---

# Cerne Self-Learning: harvest golden paths into the backend

Knowledge earned the hard way in a session — the command that finally worked,
the sequence that matters, the dead-ends you ruled out — evaporates when the
session ends. In a cerne-guarded project the durable home for that knowledge is
the **cerne backend**: versioned, shared with the whole team and every agent,
indexed (`cerne search` / `cerne ask` cite it), linted for staleness, with a
real lifecycle (errata / retire / supersede). This skill is the capture loop:
recognize the moment, triage, dedupe, write, report.

It is a *meta-skill*: it doesn't do the work, it captures **how** the work got
done — including the failures, since skipping a known dead-end next session is
often worth more than the win itself.

## Recognize the moment

Any one of these signals is a cue to harvest. **Act immediately, don't ask for
permission first** — capture, then tell the user what you captured and where.
They can always edit, errata, or retire it.

- A task only worked **after several attempts**, wrong turns, or a correction
  from the user. The successful path is worth more than the failures around it.
- You discovered **project facts you didn't know up front**: which service
  talks to which backend, a non-obvious command or flag, a required order, a
  gotcha that defies the obvious assumption.
- You worked out an **operational workflow likely to recur**: reach an
  environment, deploy, run migrations, seed data, verify a change live, tail
  the right logs, rotate a key.
- The user **signals it explicitly**: "remember this", "lembra disso",
  "save this", "don't make me re-explain this next time".

## Promotion rule (procedure or not?)

A lesson is only a **runbook** (reusable procedure) if you can point at all
three:

1. a **verification that passed** — you ran the route and saw it work, not
   "this should work";
2. a **named failure pattern** — the symptom that tells the next reader they
   need this runbook;
3. at least one **ruled-out dead-end** — an approach that looked right and
   wasn't, with the why.

Missing any of the three? It's a note, or a skip. Never harvest a confident
guess.

## Triage: where each lesson goes

| Lesson | Destination |
|---|---|
| Multi-step, reusable **procedure/workflow** (passes the promotion rule) | `cerne runbook new "<title>" --file <body.md>` (or piped stdin — one-shot, committed and pushed) |
| A durable **project fact, gotcha, or invariant** (one paragraph, not a procedure) | `cerne note new "<title>" --facet <infra\|code\|data\|process\|auth> --component <subsystem> [--informs F-…,D-…] --file <body.md>` |
| **Personal or machine-local** detail (your paths, your tool setup, your preferences) | local agent memory (e.g. Claude Code auto-memory) — **never** the backend; the team doesn't share your machine |
| A genuine **one-off** (won't recur, trivially rediscoverable) | skip — don't bloat the backend |

Tag notes on both axes: `--facet` is the closed set (`infra`, `code`, `data`,
`process`, `auth`); `--component` is open vocabulary (the subsystem or tool the
lesson is about). Link with `--informs` when the lesson explains or motivates an
existing entity (`F-…`, `D-…`, `RB-…`, `N-…`).

## Harvest procedure

1. **Apply the promotion rule.** Passing check + named failure pattern + one
   ruled-out dead-end — or it isn't a runbook: route it as a note or skip.
2. **Dedupe before minting.** Run `cerne search "<key terms>"` and, if in
   doubt, `cerne ask "<question the lesson answers>"`. If an entity already
   covers it: update it (for active notes/runbooks, edit under `.docs/current/`
   and `cerne push`, or append `cerne note errata` / `cerne runbook errata` to a
   concluded one) instead of creating a near-duplicate.
3. **Distill from THIS conversation while it's fresh**: the exact working
   commands, real paths and flags, the required order — and the dead-ends with
   the reason each one failed. The conversation is the only place the golden
   path lives; whoever writes it must have that context (write it yourself, or
   delegate only to a subagent that inherits this conversation — never a fresh
   one).
4. **Write the body, failures included.** For a runbook: symptom → verified
   route (numbered, exact commands) → dead-ends ruled out (each with its why)
   → how to verify success. For a note: the fact, why it is true, and what
   breaks if you ignore it.
5. **Mint it** with the one-shot form (`--file` or piped stdin) so it lands
   committed and pushed in a single command.
6. **Report**: relay the new id (`RB-NNNN` / `N-NNNN`) to the user and, in one
   line, what it captured. If a `docs.lock` pin applies on the current branch,
   cerne handles it — don't hand-edit the lock.

## Learnings already written in past results

A result's "Aprendizados e desvios" is history: it never reaches the next agent by itself. `cerne feature learnings` lists, read-only, the concluded features whose learnings were never triaged, each item with the active notes it may complement or replace. Apply the triage above to what it lists: a durable item becomes a note (`cerne note new ... --informs F-NNNN`), one that contradicts a note replaces it (`cerne note substituir`), the rest gets `cerne feature learnings --skip F-NNNN`.

## What NOT to capture

- Secrets, credentials, tokens — never, anywhere.
- Anything the repo already records (CLAUDE.md, an existing runbook/note, code
  comments, git history): point to it, or `--informs` it, instead of restating.
- Personal/machine-local setup — local memory, not the backend (see triage).
- Speculation. Only harvested, verified experience.
