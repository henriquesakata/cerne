# Offline mode

## Problem

Field teams lose signal in warehouses and on the road. The web app shows a blank board and discards edits made while disconnected.

## Goal and acceptance criteria

A person can read their board and edit tasks without a connection, and the changes are saved when the connection returns.

1. WHEN the app loses its connection, THE SYSTEM SHALL keep showing the last synced board.
2. WHEN a task is edited offline, THE SYSTEM SHALL queue the edit and apply it on reconnect.
3. WHEN a queued edit conflicts with a newer server change, THE SYSTEM SHALL keep both versions of the task and ask the user which one to keep.

## Design decisions

| Fork | Decision | Reason |
|---|---|---|
| Storage | IndexedDB behind a service worker | The only storage with enough room that survives a tab close. |

## Design

Not designed yet; this candidate records the demand. The sync protocol and the conflict screen are the open design work.

## Verification

1. To be written with the design.

Bar: not applicable (sample data for the cerne demo; there is no real artifact to compare against) — alternative standard: the tests listed above

## Recommended execution (agent)

Standard capability, one agent: the tasks share the same package and context.

## Out of scope

Native mobile apps.
