# taskflow: a demo project

A fictional project with a realistic cerne history, so you can explore cerne with data before adopting it. One command builds it on your machine:

```bash
cerne demo ~/cerne-demo
```

It creates `~/cerne-demo/taskflow` (the project) and `~/cerne-demo/taskflow-docs.git` (a local bare backend) from the documents under [`content/`](content), which are embedded in the binary: no clone, no shell, no network. Nothing leaves your machine. The directory must not exist yet and must not sit inside another cerne project (a clone of this repository is one), because cerne finds its project by walking up. Delete the directory to start over. If a step fails midway, the command removes what it created and names the step.

It needs `git`. On a `cerne` without the `demo` command (older releases, newer than v0.5.0), run the script from a clone instead: `sh examples/demo/seed.sh ~/cerne-demo`. With a newer binary the script just calls `cerne demo`.

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

`cerne demo` runs the same public commands a user types, in the order [`seed.sh`](seed.sh) lists them, so the script doubles as a tour of the commands: `feature new`, `approve`, `pull`, `push`, `start`, `complete`, `reject`, `decision new`, `decision substituir`, `note new`, `runbook new`, `reference new`. The documents it feeds them are under [`content/`](content).

Two things in the demo differ from a real project, on purpose:

- **The gates.** Approving a spec and accepting a delivery are human acts by default; an AI agent cannot sign off its own work. The demo opens those gates while it builds, so it can run unattended, and restores the default before it finishes. The generated project behaves like a real one.
- **The bar.** Every spec declares `Bar: not applicable (…)`. A real feature names a reference to be compared against; sample data has nothing real to compare with, and inventing a judge's verdict would be fabricating evidence.
