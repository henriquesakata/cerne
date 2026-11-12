# Weekly email digest

**Spec:** [spec.md](spec.md)
**Exit (Gauntlet):** not applicable, as declared in the spec; the alternative standard is the spec's test list.

## Strategy

One merge request on top of the outbox: aggregation, then scheduling, then the template.

## Task sequence

1. **Aggregation.** Files: `internal/notify/digest.go`, `digest_test.go`. Verification: `go test ./internal/notify/ -run Digest` (criterion 2).
2. **Scheduling and opt-out.** Files: `internal/notify/schedule.go`. Verification: criteria 1 and 3.
3. **Template.** Files: `internal/notify/templates/digest.html`, `digest.txt`. Verification: golden-file test.

## Dependencies and order

Sequential; each task leaves the build green.

## Risks and rollback

A scheduling bug could send duplicates; each send is keyed by (user, ISO week) with a unique index.

## Constraints for the agent

Do not change public API routes outside the ones named above. Stop for review before any database migration.

## Definition of done

- Every acceptance criterion has a test that proves it.
- Full suite green.
- result.md written.
