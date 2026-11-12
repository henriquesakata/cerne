# Export tasks as CSV

## Problem

Teams report on their work in spreadsheets. Support receives about ten requests a month for a data dump, each answered by an engineer running a query by hand. `GET /api/tasks` is paginated JSON and has no bulk form.

## Goal and acceptance criteria

A project member can download every task of a project as a CSV file.

1. WHEN a member requests `GET /api/projects/{id}/tasks.csv`, THE SYSTEM SHALL stream one row per task with the columns id, title, status, assignee, due_date and created_at.
2. WHEN a field contains a comma, a quote or a newline, THE SYSTEM SHALL quote it as RFC 4180 requires.
3. WHEN the requester is not a member of the project, THE SYSTEM SHALL answer 404 and send no rows.

## Design decisions

| Fork | Decision | Reason |
|---|---|---|
| Build in memory or stream | Stream from a database cursor | The largest project has 180,000 tasks; buffering them would spike memory per request. |
| CSV or XLSX | CSV | Every spreadsheet opens it and the standard library writes it. |

## Design

**`internal/export/csv.go`:** `WriteTasks(w io.Writer, rows TaskCursor) error` wraps `encoding/csv`. The handler sets `Content-Disposition: attachment` and flushes every 500 rows.

## Verification

1. `TestExportCSV_Columns`
2. `TestExportCSV_QuotesSpecialCharacters`
3. `TestExportCSV_NonMemberGets404`

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above

## Recommended execution (agent)

Standard capability, one agent: the tasks share the same package and context.

## Out of scope

Filters, column selection and scheduled exports.
