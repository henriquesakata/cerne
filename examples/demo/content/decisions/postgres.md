# Use Postgres as the primary store

## Context

taskflow needs transactions across tasks, projects and the notification outbox, and ad-hoc reporting queries from support.

## Decision

We will use a single Postgres database as the primary store, accessed through `database/sql` with hand-written queries.

## Consequences

- Positive: transactional outbox for notifications; mature tooling; one thing to back up.
- Negative: vertical scaling only until read replicas are needed; no schema-less escape hatch.

## Alternatives considered

- SQLite: no concurrent writers at our deployment shape.
- A document store: the data is relational, and the outbox needs multi-row transactions.

## Implementation and relations

`internal/db`, `migrations/`. The Notifications epic depends on the transactional outbox this choice allows.
