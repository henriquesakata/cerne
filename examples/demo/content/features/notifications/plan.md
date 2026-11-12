# Notifications

**Spec:** [spec.md](spec.md)
**Exit (Gauntlet):** not applicable, as declared in the spec; the alternative standard is the spec's test list.

## Strategy

Ship the outbox with the first channel (email digest), then add channels one by one.

## Task sequence

1. **Outbox table and reader.** Files: `migrations/0009_events.sql`, `internal/notify/outbox.go`. Verification: `go test ./internal/notify/ -run Outbox`.
2. **Channel features.** Files: see each child feature. Verification: each child's own verification.

## Dependencies and order

Sequential; each task leaves the build green.

## Risks and rollback

A stuck outbox reader delays every channel; it exposes its lag as a metric and alerts above five minutes.

## Constraints for the agent

Do not change public API routes outside the ones named above. Stop for review before any database migration.

## Definition of done

- Every acceptance criterion has a test that proves it.
- Full suite green.
- result.md written.
