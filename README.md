<h1 align="center">cerne</h1>

<p align="center">
  <strong>Living documentation for projects built with AI agents.</strong><br>
  You steer from a dashboard. Your agent does the paperwork, and reads only what is true today.
</p>

<p align="center">
  <a href="https://github.com/henriquesakata/cerne/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/henriquesakata/cerne"></a>
  <a href="https://github.com/henriquesakata/cerne/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/henriquesakata/cerne/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="License: Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-blue"></a>
  <img alt="Platforms: macOS, Linux, Windows" src="https://img.shields.io/badge/platforms-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey">
</p>

<p align="center">
  <a href="#try-it-in-two-minutes">Quickstart</a> ·
  <a href="#you-use-the-dashboard-your-agent-uses-the-cli">Who does what</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#what-you-get">Features</a> ·
  <a href="INSTALL.md">Install guide</a>
</p>

![The cerne web UI: a dashboard of what is waiting on you, the kanban board, and a feature page with its spec and the Accept button](docs/assets/ui-tour.gif)

## The problem

Project documentation is append-only. Specs, plans, ADRs and results pile up, and nothing mechanical tells the document that holds today apart from the one rejected or superseded six months ago.

A human skims and guesses. An AI agent reads everything with the same weight, then confidently cites a design you abandoned in March.

## What cerne does

cerne is a single binary that keeps the lifecycle of your features and decisions as **structured data** and hands the agent **only the current state**. You follow and steer the work in a local web UI; the agent drives it through a CLI.

- **State is data, never prose.** A feature is `candidate`, `approved`, `in development`, `implemented` or `done` because a gated command moved it there, with an event log behind it. No "Status: WIP" line rotting in a markdown file.
- **The agent reads a projection, not the archive.** `cerne pull` materializes the current state into a gitignored `.docs/` folder with a generated `CONTEXT.md`. Finished, rejected and superseded work stays out.
- **History is kept, and searchable.** Nothing is deleted. `cerne search` and `cerne ask` reach the whole history when you need the why behind a decision.
- **Staleness is linted deterministically.** Because state is data, `cerne lint` can flag a doc that links to a superseded decision, a broken reference, or a feature closed without evidence. No LLM guessing.

## You use the dashboard, your agent uses the CLI

cerne has two faces, and you only need to learn one of them.

| | You, in the browser (`cerne ui`) | Your agent, in the terminal |
|---|---|---|
| **Starting work** | Describe a feature, or read the spec the agent drafted | Registers the feature and writes its spec |
| **The gate in** | **Approve** the spec. The agent cannot | Waits for your approval, then plans and builds |
| **While it runs** | Watch the board move; see what each agent is on | Moves the feature through the lifecycle as it works |
| **The gate out** | Read the result and its evidence, then **Accept** | Writes the result, with evidence for each acceptance criterion |
| **Later** | Search why something was decided or rejected | Reads only what is current, so it does not cite the past |

The CLI has many commands because the agent needs them. `cerne init` installs the skills and hooks that teach it the whole workflow, so in practice you ask for a feature in plain words and review it in the UI. Every command is still there for you: nothing the agent does is hidden or out of reach.

![A feature page: the spec the agent wrote, its state, and the Accept button reserved for a human](docs/assets/ui-feature.png)

### The number

This repository documents itself with cerne. After 359 features:

| | Documents | ~Tokens |
|---|---:|---:|
| Full documentation corpus | 475 | 1,618,000 |
| What the agent actually reads (`.docs/`) | 115 | 33,000 |

**98% of the corpus stays out of the agent's context**, and none of it is lost. Run `cerne savings` on your own project to get your number.

## Try it in two minutes

Install on macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/henriquesakata/cerne/main/install.sh | sh
```

The script downloads the latest release for your machine, verifies its SHA-256 and installs it without `sudo`. On Windows, or to install by hand or with Go, see [INSTALL.md](INSTALL.md).

Then, inside your project's git repository (with no `--backend`, `init` creates a local backend for you):

```bash
cerne init --name my-project
echo ".docs/" >> .gitignore
cerne ui        # your dashboard, at http://localhost:7717
```

That is all you need to type. Now open your agent in the project (Claude Code picks up what `init` installed) and ask for something: *"add sign in with Google"*. It registers the feature and drafts the spec; the card shows up on your board, waiting for you to approve it.

For a team, pass `--backend` an empty private repository on GitHub or GitLab instead. `init` prints how to move a local backend to a remote later.

<details>
<summary>Prefer the terminal, or working without an agent? The same flow by hand.</summary>

```bash
cerne feature new "Sign in with Google" --context "Users asked for social login."
cerne pull      # materializes the current state into .docs/
cerne status    # features grouped by state
cerne search facebook   # history stays searchable
```

![cerne in the terminal: a rejected feature leaves the current state and stays searchable](docs/assets/demo.gif)

</details>

### See it with data

An empty project does not show what cerne is for. This builds a fictional one with a realistic history (features in every state, a rejected proposal, a superseded decision, an epic) on your machine, in a few seconds:

```bash
git clone https://github.com/henriquesakata/cerne && sh cerne/examples/demo/seed.sh
```

Then `cd cerne-demo/taskflow && cerne ui` and click around. What is in it and what to try: [examples/demo](examples/demo/README.md).

## How it works

```mermaid
flowchart LR
    B[("Backend git repo<br>entities + markdown + event log<br><b>source of truth</b>")]
    D[".docs/ in your project<br>current state only<br>gitignored"]
    L["docs.lock<br>committed pin<br>code ↔ docs"]
    A(["AI agent"])
    H(["You"])

    B -- "cerne pull" --> D
    D -- "cerne push" --> B
    B -.-> L
    D --> A
    H -- "approve / accept" --> B
```

- The **backend** is a plain git repository you own: entities as JSON, content as markdown, events as NDJSON. No server, no SaaS, no database to operate.
- **`.docs/`** is a projection of the current state. It is what the agent reads, and it is regenerated, never hand-curated.
- **`docs.lock`** is committed in your project and pins the code to a documentation snapshot, so a checkout from last year reads last year's docs.

## What you get

| | |
|---|---|
| **A dashboard for humans** | `cerne ui` serves a local web UI: what is waiting on you, the kanban board, epics, agent activity, flow metrics, lint debts. English and Portuguese. |
| **Gated lifecycle** | A spec needs an acceptance bar before it is approved, a plan before work starts, a result with evidence before it is done. Approving and accepting are human acts by default, one click in the UI: the agent cannot sign off its own work. |
| **Decisions, runbooks, notes, references** | ADRs with supersession, operational procedures, durable team knowledge, and external sources registered as provenance instead of loose links. |
| **Search and Q&A over history** | Full-text search (SQLite FTS5) over everything, including what was rejected. `cerne ask` answers with citations. |
| **Deterministic lint** | Stale references, broken links, orphans, missing gate documents, unregistered sources. |
| **Built for parallel agents** | Per-document merge on `pull`, auto-rebase on `push`, a conflict-free `docs.lock` across branches and worktrees. |
| **Your agent already knows how** | `cerne init` installs the skills and hooks that teach Claude Code the workflow, so you just ask for the feature. |
| **Single binary** | macOS, Linux and Windows. The UI is embedded. No runtime dependencies. |

![The dashboard: what is waiting on you, features by state, lint debts and context savings](docs/assets/ui-dashboard.png)

![The kanban board: every feature in its state, epics and tags](docs/assets/ui-kanban.png)

The screenshots are from the [demo project](examples/demo/README.md), which you can build on your machine in a few seconds.

## Why not just…

| | Knows the state of work | Lives next to the code | Agent reads only what is current |
|---|:---:|:---:|:---:|
| Issue tracker (Jira, Linear) | yes | no | no |
| Docs-as-code (markdown, ADR folders) | no | yes | no |
| RAG over your docs | no | no | no, it retrieves stale text just as happily |
| **cerne** | **yes** | **yes** | **yes** |

The idea is close to Karpathy's [LLM Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f), with one difference: the lifecycle is data, so staleness is checked by a deterministic lint instead of a model's judgment.

## Status

cerne is young and in daily use. This repository has been built with it since day one, and the 359 features above went through the same gates you would use. Expect rough edges, and please [open an issue](https://github.com/henriquesakata/cerne/issues) when you hit one: feedback from early users shapes what comes next.

Questions and ideas go to [Discussions](https://github.com/henriquesakata/cerne/discussions). To contribute code, start with [CONTRIBUTING.md](CONTRIBUTING.md).

The founding design document is [docs/process/cases/0001_founding-spec/spec.md](docs/process/cases/0001_founding-spec/spec.md).

## The name

*Cerne* is Portuguese for heartwood: the hard core at the center of a tree, the part that stays while the tree grows.

## License

[Apache License 2.0](LICENSE).
