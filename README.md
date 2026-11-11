# cerne

> *Cerne* is Portuguese for heartwood: the hard core at the center of a tree, the part that stays while the tree grows.

**cerne** manages the living documentation of software projects built with AI agents. It separates what is **current** from what is **history**, materializes only the current state into the user's repository, and keeps the lifecycle of features (candidate → approved → in development → implemented → done) as structured data, never prose.

## The problem

Project documentation is append-only: specs, plans, ADRs and results pile up, and nothing mechanically tells the document that holds today apart from the one rejected or superseded six months ago. AI agents read everything with the same weight, and cite structures that no longer exist.

## The thesis

The union of three things no existing tool combines:

1. **Structured lifecycle**: the state of features and decisions as first-class data, with validated transitions and an event log (Jira has this; RAG doesn't).
2. **Materialization of the current state**: a gitignored `.docs/` folder in the user's repo holding *only* the current state, plus a committed `docs.lock` pinning code to docs (docs-as-code has files; it has no state).
3. **A compiled layer for agents**: a synthesis wiki plus a generated `CONTEXT.md`, in the spirit of Karpathy's [LLM Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f), but with a **deterministic** staleness lint, because the lifecycle is data, not text.

## Install

A single binary for macOS, Linux and Windows: see [INSTALL.md](INSTALL.md).

## Status

In daily use: this repository documents itself with cerne. Founding document: [docs/process/cases/0001_founding-spec/spec.md](docs/process/cases/0001_founding-spec/spec.md).

## License

[Apache License 2.0](LICENSE).
