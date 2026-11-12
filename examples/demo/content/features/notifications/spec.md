# Notifications

## Problem

taskflow tells nobody anything. A task assigned on Monday is discovered on Thursday, when someone opens the board. There is no outbound channel in the codebase at all: no mailer, no webhook client, no user preference to store.

## Goal and acceptance criteria

People learn about the work that concerns them without opening taskflow.

1. WHEN this epic is complete, a user SHALL be able to choose, per channel, which events reach them.
2. Each channel is delivered as its own feature under this epic and can ship independently.

## Design decisions

| Fork | Decision | Reason |
|---|---|---|
| One feature or an epic | An epic with one feature per channel | Email and Slack share only the event model; their delivery, auth and failure modes differ. |
| Event source | An `events` outbox table written in the same transaction as the change | A notification must never be sent for a change that rolled back. |

## Design

**`internal/notify`:** an outbox reader that hands each event to the registered channels. Channels implement `Send(ctx, user, event) error` and are retried with backoff.

Child features: *Weekly email digest*, *Slack notifications*.

## Verification

1. Each child feature carries its own tests.
2. `TestOutbox_SkipsRolledBackChanges` covers the shared event source.

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above

## Recommended execution (agent)

Standard capability, one agent: the tasks share the same package and context.

## Out of scope

Mobile push and in-app notification center.
