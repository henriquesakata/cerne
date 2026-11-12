# Weekly email digest

## Result

Delivered and waiting for product validation in staging.

## Acceptance

- Criterion 1: MET. `TestDigest_SendsAtLocalMonday` (`internal/notify/schedule_test.go:22`), 6 time zones.
- Criterion 2: MET. `TestDigest_SkipsEmptyWeek` (`digest_test.go:40`).
- Criterion 3: MET. `TestDigest_RespectsOptOut` (`digest_test.go:71`).

## What changed

- `internal/notify/digest.go`, `schedule.go` and two templates (new).
- Settings page gained the "Weekly digest" toggle.

## Debts and follow-ups

The digest does not yet group changes by project when a user has more than ten.

## Learnings and deviations

The unique (user, ISO week) key caught a real duplicate during the daylight-saving change in the test matrix.

## Gauntlet

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above
