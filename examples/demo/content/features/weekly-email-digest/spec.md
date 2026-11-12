# Weekly email digest

## Problem

The first notification channel. People who do not live in taskflow asked for a single weekly summary instead of an email per change.

## Goal and acceptance criteria

Every Monday a user receives one email with what changed in their projects during the previous week.

1. WHEN it is Monday 08:00 in the user's time zone, THE SYSTEM SHALL send one digest covering the previous seven days.
2. WHEN nothing changed in the user's projects that week, THE SYSTEM SHALL send nothing.
3. WHEN a user turns the digest off in settings, THE SYSTEM SHALL stop sending it from the next cycle.

## Design decisions

| Fork | Decision | Reason |
|---|---|---|
| Send time | User's local Monday 08:00 | A single UTC send lands on Sunday night for half the users. |
| Rendering | `html/template` with a plain-text alternative | No template engine to add; plain text keeps deliverability up. |

## Design

**`internal/notify/digest.go`:** an hourly job selects users whose local time just crossed Monday 08:00, aggregates their outbox events and hands one message to the mailer.

## Verification

1. `TestDigest_SendsAtLocalMonday`
2. `TestDigest_SkipsEmptyWeek`
3. `TestDigest_RespectsOptOut`

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above

## Recommended execution (agent)

Standard capability, one agent: the tasks share the same package and context.

## Out of scope

Daily digests and per-project digests.
