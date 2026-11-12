# Recurring tasks

**Spec:** [spec.md](spec.md)
**Exit (Gauntlet):** not applicable, as declared in the spec; the alternative standard is the spec's test list.

## Strategy

Two merge requests: the pure recurrence package first, then the series behaviour in the task service.

## Task sequence

1. **Recurrence package.** Files: `internal/recurrence/rule.go`, `rule_test.go`. Verification: `go test ./internal/recurrence/` (criteria 1 and 2).
2. **Series columns and completion hook.** Files: `migrations/0011_series.sql`, `internal/tasks/service.go`. Verification: `go test ./internal/tasks/ -run Series` (criterion 3).

## Dependencies and order

Sequential; each task leaves the build green.

## Risks and rollback

Time-zone mistakes produce off-by-one due dates; every test case runs in UTC, America/Sao_Paulo and Pacific/Auckland.

## Constraints for the agent

Do not change public API routes outside the ones named above. Stop for review before any database migration.

## Definition of done

- Every acceptance criterion has a test that proves it.
- Full suite green.
- result.md written.
