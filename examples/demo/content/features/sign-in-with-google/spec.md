# Sign in with Google

## Problem

taskflow only supports email and password. In the last onboarding survey, 41% of people who abandoned sign-up said they did not want another password. `internal/auth/handler.go` has no notion of an external identity: `users.password_hash` is `NOT NULL`.

## Goal and acceptance criteria

A person can create an account and sign in with a Google account, without a taskflow password.

1. WHEN a signed-out person completes the Google consent screen for the first time, THE SYSTEM SHALL create a user with a verified email and no password, and start a session.
2. WHEN the Google account's email already belongs to a taskflow user, THE SYSTEM SHALL link the identity to that user instead of creating a second one.
3. WHEN the `state` parameter of the callback does not match the one issued, THE SYSTEM SHALL reject the callback with 400 and start no session.

## Design decisions

| Fork | Decision | Reason |
|---|---|---|
| Library or hand-written flow | `golang.org/x/oauth2` | Maintained by the Go team; the flow is short but easy to get subtly wrong. |
| Where identities live | New `identities` table (user_id, provider, subject) | A user may link more than one provider later; a column per provider does not scale. |
| Password for Google-only users | `password_hash` becomes nullable | A random unusable password would look like a real credential in audits. |

## Design

**`internal/auth/google.go`:** `GET /auth/google/start` issues `state` in a signed, short-lived cookie and redirects; `GET /auth/google/callback` validates `state`, exchanges the code, reads the `sub` and `email` claims and calls `LinkOrCreate(provider, subject, email)`.

**Migration `0007_identities.sql`:** creates `identities` with a unique index on `(provider, subject)` and drops `NOT NULL` from `users.password_hash`.

## Verification

1. `TestGoogleCallback_CreatesUser`
2. `TestGoogleCallback_LinksExistingEmail`
3. `TestGoogleCallback_RejectsStateMismatch`

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above

## Recommended execution (agent)

Standard capability, one agent: the tasks share the same package and context.

## Out of scope

Other providers (see the rejected Facebook proposal) and unlinking an identity.
