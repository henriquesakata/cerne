# Export tasks as CSV

## Result

Delivered. The export link is on the project menu.

## Acceptance

- Criterion 1: MET. `TestExportCSV_Columns` (`internal/export/csv_test.go:18`).
- Criterion 2: MET. `TestExportCSV_QuotesSpecialCharacters` (`csv_test.go:44`), 9 cases.
- Criterion 3: MET. `TestExportCSV_NonMemberGets404` (`internal/api/export_test.go:31`).

## What changed

- `internal/export/csv.go` and `internal/api/export.go` (new).
- Exporting 180,000 tasks takes 3.1 s and peaks at 14 MB of memory.

## Debts and follow-ups

Dates are exported in UTC; a per-user time zone is a follow-up.

## Learnings and deviations

N/A.

## Gauntlet

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above
