# Sign in with Facebook

## Problem

Proposed together with Google sign-in, on the assumption that a second provider would recover more abandoned sign-ups.

## Goal and acceptance criteria

A person can create an account and sign in with a Facebook account.

1. WHEN a signed-out person completes the Facebook login dialog, THE SYSTEM SHALL create or link a user and start a session.

## Design decisions

| Fork | Decision | Reason |
|---|---|---|
| Build now or measure first | Measure first | Google sign-in shipped with a provider-interest question on the sign-up page. |

## Design

Would reuse the `identities` table and `LinkOrCreate` introduced for Google.

**Why this was rejected:** after four weeks, 1.8% of new sign-ups picked Facebook in the interest question, against 58% using Google. The app-review process and a second OAuth surface to maintain are not worth that share.

## Verification

1. `TestFacebookCallback_CreatesOrLinksUser`

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above

## Recommended execution (agent)

Standard capability, one agent: the tasks share the same package and context.

## Out of scope

Everything: the proposal was rejected before any work started.
