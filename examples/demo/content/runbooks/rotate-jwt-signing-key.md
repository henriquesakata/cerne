# Rotate the JWT signing key

## When to use

Every 90 days, or immediately if the private key may have leaked.

## Prerequisites

- Access to the production secret store.
- `taskflowctl` 2.3 or newer.

## Procedure

1. Generate the new key pair: `taskflowctl keys generate --out next.pem`. You should see `key id: <kid>`.
2. Publish the new public key next to the current one: `taskflowctl keys publish next.pem`. You should see two entries in `/.well-known/jwks.json`. Safe to repeat.
3. Wait 15 minutes, longer than the access-token lifetime, so every instance has loaded both keys.
4. Switch signing to the new key: `taskflowctl keys activate <kid>`.
5. After another 15 minutes, retire the old key: `taskflowctl keys retire <old-kid>`.

## Verification

- `taskflowctl keys list` shows the new key as `active` and no key as `retiring`.
- The rate of 401 responses on the dashboard stays flat during the whole procedure.

## Rollback and troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| 401 spike right after step 4 | Step 3 was skipped | `taskflowctl keys activate <old-kid>`, wait, retry |
| A suspected leak | The old key is still trusted | Skip the waits; users are signed out and must sign in again |

Escalate to the platform on-call if 401s do not recover within 10 minutes.
