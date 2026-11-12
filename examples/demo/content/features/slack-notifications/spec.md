# Slack notifications

## Problem

The second notification channel. Teams that coordinate in Slack want assignments and due-date changes posted where they already talk.

## Goal and acceptance criteria

A project can post its task events to a Slack channel.

1. WHEN a project admin connects a Slack workspace and picks a channel, THE SYSTEM SHALL post a message there for every task assigned, completed or re-dated in that project.
2. WHEN Slack answers 429, THE SYSTEM SHALL retry after the `Retry-After` interval without dropping the event.
3. WHEN the Slack token is revoked, THE SYSTEM SHALL mark the connection as broken and email the project admins once.

## Design decisions

| Fork | Decision | Reason |
|---|---|---|
| Incoming webhook or bot token | Bot token through OAuth | Webhooks cannot be revoked from our side and tie the connection to one channel forever. |
| Message format | Block Kit with a plain-text fallback | Fallback text is what shows in notifications. |

## Design

**`internal/notify/slack.go`:** implements the channel interface of the Notifications epic over `chat.postMessage`. Tokens are stored encrypted with the existing `secrets` package.

## Verification

1. `TestSlack_PostsOnAssignment`
2. `TestSlack_HonorsRetryAfter`
3. `TestSlack_MarksRevokedTokenBroken`

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above

## Recommended execution (agent)

Standard capability, one agent: the tasks share the same package and context.

## Out of scope

Slash commands and creating tasks from Slack.
