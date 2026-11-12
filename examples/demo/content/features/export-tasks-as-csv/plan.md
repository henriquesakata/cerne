# Export tasks as CSV

**Spec:** [spec.md](spec.md)
**Exit (Gauntlet):** not applicable, as declared in the spec; the alternative standard is the spec's test list.

## Strategy

One merge request: the writer first, with table-driven tests, then the handler.

## Task sequence

1. **Writer.** Files: `internal/export/csv.go`, `csv_test.go`. Verification: `go test ./internal/export/` (criteria 1 and 2).
2. **Handler and route.** Files: `internal/api/export.go`, `export_test.go`. Verification: `go test ./internal/api/ -run ExportCSV` (criterion 3).

## Dependencies and order

Sequential; each task leaves the build green.

## Risks and rollback

Read-only endpoint; reverting the commit removes it.

## Constraints for the agent

Do not change public API routes outside the ones named above. Stop for review before any database migration.

## Definition of done

- Every acceptance criterion has a test that proves it.
- Full suite green.
- result.md written.
