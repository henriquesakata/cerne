# Sign in with Google

**Spec:** [spec.md](spec.md)
**Exit (Gauntlet):** not applicable, as declared in the spec; the alternative standard is the spec's test list.

## Strategy

One merge request, test-first: migration, then the linking logic, then the HTTP handlers.

## Task sequence

1. **Migration.** Files: `migrations/0007_identities.sql`. Verification: `make migrate-test` passes.
2. **LinkOrCreate.** Files: `internal/auth/identity.go`, `identity_test.go`. Verification: `go test ./internal/auth/ -run LinkOrCreate` (criteria 1 and 2).
3. **Handlers.** Files: `internal/auth/google.go`, `google_test.go`. Verification: `go test ./internal/auth/ -run GoogleCallback` (criterion 3).

## Dependencies and order

Sequential; each task leaves the build green.

## Risks and rollback

Dropping `NOT NULL` is not reversible without data loss once Google-only users exist; the down migration refuses to run if any `password_hash` is null.

## Constraints for the agent

Do not change public API routes outside the ones named above. Stop for review before any database migration.

## Definition of done

- Every acceptance criterion has a test that proves it.
- Full suite green.
- result.md written.
