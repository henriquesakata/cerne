---
name: cerne-authoring
description: "Use this whenever you create, plan, finish, or manage a feature, decision (ADR), or external reference in a project guarded by cerne. cerne is the documentation source of truth — author through it (cerne feature new -> pull -> push -> approve/start/complete), never as loose docs/case folders. Read current state from .docs/CONTEXT.md and `cerne status`."
---

# Cerne-Native Authoring

## Overview

In a cerne-managed project the living documentation is owned by **cerne**: it holds the lifecycle of **features**, **decisions** (ADRs) and **references**, and is the source of truth for the current state. The backend (a git repo) is pinned by the committed `docs.lock`; `.cernerc` wires it. You author documents under `.docs/current/…` and push them back through cerne — you do **not** hand-create folders of loose markdown for tracked work.

This skill is the **mechanics and lifecycle**. The *content* of each document (the actual spec/plan/result prose) comes from your normal process. Read current state from `.docs/CONTEXT.md` and `cerne status`; reach superseded/rejected history with `cerne search "<q>"` / `cerne ask "<q>"`.

## When to use

- Starting a new feature, decision (ADR), or registering an external source that informed your work.
- Moving a feature through its lifecycle (approve / start / complete).
- Any moment you would otherwise hand-create a tracked "case" or "feature" doc folder — don't; use this loop.

## Feature loop

cerne mints the canonical `F-NNNN` id and gates each transition on a real document.

1. **Create** — `cerne feature new "<title>" [--module <module>] [--parent F-NNNN]` → candidate.
   - `--module` assigns the subsystem when the project declares modules; omit for cross-cutting work.
   - `--parent F-NNNN` makes this feature decompose an epic. See **Epics**.
2. **Spec** — `cerne pull`, author `.docs/current/features/F-NNNN-<slug>/spec.md` (keep the `cerne_id` frontmatter), `cerne push`, then `cerne feature approve F-NNNN` (gate: a real spec, not the stub).
3. **Plan** — author `plan.md` next to the spec, `cerne push`, then `cerne feature start F-NNNN` (gate: a real plan).
4. **Implement** — build it per your project's stack and conventions; add tests; review.
5. **Result** — author `result.md` next to the spec, `cerne push`, then `cerne feature complete F-NNNN` (gate: a real result). No manual "Done" status, no index edits — `cerne feature complete` is the source of truth.

Terminal transitions when work is dropped or replaced: `cerne feature reject F-NNNN --reason "…"`; `cerne feature substituir F-NNNN --by F-MMMM --reason "…"`.

## Epics (large work, decomposed)

An **epic is just a feature that has children** — the role is inferred from the parent link, there is no separate entity, so the epic gets the full feature lifecycle/spec/plan/result for free. Use one when a body of work is too large for a single spec and naturally splits into sub-features.

- Create a child under an epic: `cerne feature new "<title>" --parent F-NNNN`.
- Link, move, or detach an existing feature later: `cerne feature reparent F-NNNN F-MMMM` (or `--clear`).
- **Epic ≠ module.** A module is the subsystem; an epic is decomposed work that may span modules. A feature can have both.
- **Read the breakdown, don't hand-maintain it:** `cerne status` shows an `ÉPICOS` section with `n/m` progress, and `.docs/CONTEXT.md` carries an *Épico* column plus a `## Épicos` section.
- The epic's lifecycle is independent (children completing does not auto-complete it). `cerne lint` flags inconsistencies — a live feature under a terminal epic, or an epic completed with open children.

## Decisions (ADRs)

Same model: `cerne decision new "<title>"` (mints `D-NNNN`, active) → `cerne pull` → author `.docs/current/decisions/D-NNNN-<slug>.md` (keep the `cerne_id` frontmatter) → `cerne push`. Supersede with `cerne decision substituir D-NNNN --by D-MMMM --reason "…"`.

## External sources (provenance)

When an external article / doc / spec informs a feature, decision, or fix, register it instead of leaving a bare URL in prose:

```
cerne reference new "<title>" --url <url> --informs F-NNNN,D-NNNN [--file <summary> | stdin]
```

It is marked external, its content is indexed (so `cerne ask` cites it), and `--informs` backlinks it to the entity it motivated.

## Hard rules

- **Author through cerne**, not as loose markdown folders for tracked features/decisions — loose markdown is authoring input, not authority.
- **Never hand-edit generated outputs:** `.docs/CONTEXT.md` and the compiled wiki regenerate (`cerne pull` / `cerne compile`).
- After a `cerne push` that changes the backend, commit the updated `docs.lock` (the code↔docs pin) alongside your code change.
- `.docs/` is gitignored; only `.cernerc` and `docs.lock` are committed in your project repo.
