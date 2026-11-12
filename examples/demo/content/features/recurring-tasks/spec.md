# Recurring tasks

## Problem

Routine work is recreated by hand every week. The most-copied task in production has been duplicated 212 times. `tasks` has no notion of a schedule.

## Goal and acceptance criteria

A task can repeat on a schedule without anyone copying it.

1. WHEN a task with a recurrence rule is completed, THE SYSTEM SHALL create its next occurrence with the due date the rule yields.
2. WHEN a rule lands on a day that does not exist in a month, THE SYSTEM SHALL use the last day of that month.
3. WHEN a user deletes a recurring task and chooses "this and following", THE SYSTEM SHALL end the series without touching past occurrences.

## Design decisions

| Fork | Decision | Reason |
|---|---|---|
| Rule format | A subset of iCalendar RRULE (daily, weekly, monthly; interval; by-weekday) | A known format people can reason about, without yearly and by-set-position edge cases. |
| Generate ahead or on completion | On completion | No backlog of future tasks to clean up when the rule changes. |

## Design

**`internal/recurrence`:** `Next(rule Rule, after time.Time) time.Time`, pure and table-tested. `tasks` gains `series_id` and `rrule` columns.

## Verification

1. `TestNext_Weekly`, `TestNext_MonthlyInterval`
2. `TestNext_ClampsToMonthEnd`
3. `TestSeries_DeleteThisAndFollowing`

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above

## Recommended execution (agent)

Standard capability, one agent: the tasks share the same package and context.

## Out of scope

Yearly rules, time-of-day and calendar export.
