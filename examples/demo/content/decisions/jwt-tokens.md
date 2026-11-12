# Use JWT access tokens with rotating refresh tokens

## Context

The public API and the mobile client cannot rely on cookies, and the session table became the hottest read in the database. Revocation still matters: a stolen laptop must be cut off within minutes.

## Decision

We will issue 10-minute JWT access tokens and opaque refresh tokens that rotate on every use. Reuse of a rotated refresh token revokes the whole token family.

## Consequences

- Positive: API requests are verified without a database read; one scheme for web, mobile and API.
- Negative: revocation takes up to 10 minutes to be effective; key rotation is now an operational duty (see the runbook).
- Constraint: access-token lifetime must not be raised without revisiting this decision.

## Alternatives considered

- Keep cookies and add API keys: two auth schemes to secure and explain.
- Long-lived JWTs with a deny list: the deny list is a session table by another name.

## Implementation and relations

`internal/auth/token.go`, `internal/auth/refresh.go`. Replaces the session-cookie decision. Sign in with Google issues tokens through the same path.
