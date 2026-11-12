# taskflow: a demo project

A fictional project with a realistic cerne history, so you can explore cerne with data before adopting it. One command builds it on your machine:

```bash
sh examples/demo/seed.sh ~/cerne-demo
```

It creates `~/cerne-demo/taskflow` (the project) and `~/cerne-demo/taskflow-docs.git` (a local bare backend). Nothing leaves your machine. The directory must not exist yet and must be outside this clone: the clone is itself a cerne project, and cerne finds its project by walking up. Delete the directory to start over. If the script fails midway it removes what it created.

It needs `git` and a `cerne` newer than v0.5.0 (the first with English documents).

## What is in it

taskflow is a small task-tracking API. Its history was written to show what cerne keeps apart:

| Entity | State |
|---|---|
| Sign in with Google | done |
| Sign in with Facebook | rejected: measured first, 1.8% asked for it |
| Export tasks as CSV | done |
| Notifications (an epic) | in development |
| ↳ Weekly email digest | implemented, waiting for a human to accept it |
| ↳ Slack notifications | approved |
| Recurring tasks | in development |
| Offline mode | candidate |
| Decision: server-side session cookies | superseded |
| Decision: JWT access tokens with rotating refresh tokens | active, replaced the one above |
| Decision: Postgres as the primary store | active |
| A note, a runbook, an external reference (RFC 6749) | active |

## Things to try

```bash
cd ~/cerne-demo/taskflow
cerne status                  # features by state, and the epic's progress
cerne ui                      # dashboard and kanban at http://localhost:7717
cat .docs/CONTEXT.md          # what an AI agent reads: only the current state
cerne search facebook         # the rejected proposal is history, one search away
cerne show D-0001             # the superseded decision, and what replaced it
cerne savings                 # how much of the corpus stays out of the agent's context
cerne feature accept F-0005   # accepting a delivery is a human act: try it
```

## How it was built

[`seed.sh`](seed.sh) uses only the public CLI, so it doubles as a tour of the commands: `feature new`, `approve`, `pull`, `push`, `start`, `complete`, `reject`, `decision new`, `decision substituir`, `note new`, `runbook new`, `reference new`. The documents it feeds them are under [`content/`](content).

Two things in the demo differ from a real project, on purpose:

- **The gates.** Approving a spec and accepting a delivery are human acts by default; an AI agent cannot sign off its own work. The script opens those gates while it seeds, so it can run unattended, and restores the default before it finishes. The generated project behaves like a real one.
- **The bar.** Every spec declares `Bar: not applicable (…)`. A real feature names a reference to be compared against; sample data has nothing real to compare with, and inventing a judge's verdict would be fabricating evidence.
