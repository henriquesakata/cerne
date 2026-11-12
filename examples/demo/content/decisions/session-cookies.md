# Use server-side session cookies for auth

## Context

taskflow started as a server-rendered app with one deployment. Authentication had to be simple and revocable.

## Decision

We will keep sessions in the database and identify them with an opaque, HTTP-only cookie.

## Consequences

- Positive: instant revocation; nothing sensitive in the browser.
- Negative: every request reads the session table; the mobile client and third-party API consumers cannot use cookies comfortably.

## Alternatives considered

- Signed stateless tokens: rejected at the time because nothing needed them and revocation is harder.

## Implementation and relations

`internal/auth/session.go`. Superseded once the public API and the mobile client arrived.
