# Sign in with Google

## Result

Delivered. People can sign up and sign in with Google.

## Acceptance

- Criterion 1: MET. `TestGoogleCallback_CreatesUser` (`internal/auth/google_test.go:24`).
- Criterion 2: MET. `TestGoogleCallback_LinksExistingEmail` (`google_test.go:61`).
- Criterion 3: MET. `TestGoogleCallback_RejectsStateMismatch` (`google_test.go:97`); manual check with a tampered cookie returned 400.

## What changed

- `internal/auth/google.go`, `internal/auth/identity.go` (new).
- `migrations/0007_identities.sql` (new).
- Sign-in page gained a "Continue with Google" button.

## Debts and follow-ups

Unlinking a Google identity is not possible yet.

## Learnings and deviations

The callback must be served from the apex domain; a redirect from `www` drops the `state` cookie. Recorded as a note.

## Gauntlet

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above
