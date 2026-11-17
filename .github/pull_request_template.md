<!--
Write the title as a Conventional Commit a reader can follow:
"fix(cli): pull no longer drops local edits on a conflict", not "fix stuff"
or "address review". Release notes are not generated from pull request titles:
they come from the version's section in CHANGELOG.md, curated before each tag.
-->

## What and why

<!-- What changes for a user of cerne, and why. Link the issue it closes: "Closes #123".
If someone upgrading needs to know about it or do something, say so here: it feeds the CHANGELOG. -->

## How it was tested

<!-- The test that fails without this change, and how you ran it. -->

## Checklist

- [ ] A test that fails without this change
- [ ] One concern per commit, Conventional Commits messages
- [ ] Touches `ui/src`? Say so here: a maintainer rebuilds `ui/dist` (do not commit it)
- [ ] Does not touch `docs.lock`
