#!/bin/sh
# Tests for scripts/release-notes.sh against a fixture CHANGELOG (F-0446).
#   sh scripts/release-notes_test.sh
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0
ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; fail=1; }

cat >"$tmp/CHANGELOG.md" <<'MD'
# Changelog

## [Unreleased]

## [0.2.0] - 2026-02-02

### Added

- Thing two ([#2](https://example.com/2)).

```sh
## [9.9.9] not a heading, inside a fence
[x]: not a link either
```

After the fence.

## [0.1.0] - 2026-01-01

- Thing one.

[Unreleased]: https://example.com/compare/v0.2.0...HEAD
[0.2.0]: https://example.com/compare/v0.1.0...v0.2.0
[0.1.0]: https://example.com/releases/tag/v0.1.0
MD

run() { CHANGELOG="$tmp/CHANGELOG.md" sh "$here/release-notes.sh" "$@"; }

# section present, stops at the next heading
out=$(run v0.2.0) || bad "present: exit"
case $out in
  "## [0.2.0]"*"Thing two"*"After the fence."*) ok "present" ;;
  *) bad "present: $out" ;;
esac
case $out in *"Thing one"*) bad "present: leaked next section" ;; *) ok "present stops at next heading" ;; esac

# fence: a '## [' line inside a fence does not end the section
case $out in *"## [9.9.9] not a heading"*"[x]: not a link either"*) ok "fence" ;; *) bad "fence" ;; esac

# last section: ends before the link references
out=$(run v0.1.0) || bad "last: exit"
case $out in
  *"Thing one."*) ok "last section" ;;
  *) bad "last section: $out" ;;
esac
case $out in *"compare"*|*"releases/tag"*) bad "last: link refs leaked" ;; *) ok "last section drops link refs" ;; esac

# absent
if err=$(run v7.7.7 2>&1); then bad "absent: exit 0"; else
  case $err in *"7.7.7"*"CHANGELOG"*) ok "absent" ;; *) bad "absent message: $err" ;; esac
fi

# RC: same section, pre-release notice first
out=$(run v0.2.0-rc.1) || bad "rc: exit"
case $out in
  *"pre-release"*) ok "rc notice" ;;
  *) bad "rc notice: $out" ;;
esac
case $out in "## [0.2.0]"*) bad "rc: notice not first" ;; *"## [0.2.0]"*"Thing two"*) ok "rc section" ;; *) bad "rc section" ;; esac

# no argument
if run >/dev/null 2>&1; then bad "usage"; else ok "usage"; fi

# CRLF file: no CR in the output
printf '## [3.0.0] - 2026-03-03\r\n\r\n- crlf item\r\n\r\n## [2.0.0]\r\n- old\r\n' >"$tmp/crlf.md"
out=$(CHANGELOG="$tmp/crlf.md" sh "$here/release-notes.sh" v3.0.0) || bad "crlf: exit"
case $out in *"crlf item"*) ok "crlf section" ;; *) bad "crlf section" ;; esac
case $out in *"$(printf '\r')"*) bad "crlf: CR left" ;; *) ok "crlf stripped" ;; esac
case $out in *"old"*) bad "crlf: leaked next section" ;; *) ok "crlf stops at next heading" ;; esac

# empty section fails with a clear message
printf '## [4.0.0] - 2026-04-04\n\n## [3.0.0]\n- x\n' >"$tmp/empty.md"
if err=$(CHANGELOG="$tmp/empty.md" sh "$here/release-notes.sh" v4.0.0 2>&1); then bad "empty: exit 0"; else
  case $err in *"empty"*) ok "empty section" ;; *) bad "empty message: $err" ;; esac
fi

# ~~~ and 4-backtick fences hide a '## [' line too
printf '## [5.0.0]\n\n~~~\n## [8.8.8] inside tilde\n~~~\n\n````md\n```\n## [7.7.7] inside long\n```\n````\n\ntail5\n\n## [4.0.0]\n- no\n' >"$tmp/fences.md"
out=$(CHANGELOG="$tmp/fences.md" sh "$here/release-notes.sh" v5.0.0) || bad "fences: exit"
case $out in *"inside tilde"*"inside long"*"tail5"*) ok "tilde and long fences" ;; *) bad "tilde and long fences: $out" ;; esac
case $out in *"- no"*) bad "fences: leaked next section" ;; *) ok "fences stop at next heading" ;; esac

# the repository's own CHANGELOG: 0.14.0 runs to its last subsection, keeps the
# compare link, and stops before 0.13.0 without the link reference block
repo="$here/../CHANGELOG.md"
if [ -f "$repo" ]; then
  out=$(CHANGELOG="$repo" sh "$here/release-notes.sh" v0.14.0) || bad "repo: exit"
  case $out in "## [0.14.0]"*"### Highlights"*"### Known limitations"*) ok "repo section" ;; *) bad "repo section" ;; esac
  case $out in *"compare/v0.13.0...v0.14.0"*) ok "repo compare link" ;; *) bad "repo compare link" ;; esac
  case $out in *"## [0.13.0]"*) bad "repo: leaked 0.13.0" ;; *) ok "repo stops before 0.13.0" ;; esac
  if printf '%s\n' "$out" | grep -q '^\[[^]]*\]: '; then bad "repo: link references leaked"; else ok "repo drops link references"; fi
  if CHANGELOG="$repo" sh "$here/release-notes.sh" v99.0.0 >/dev/null 2>&1; then bad "repo: v99.0.0 passed"; else ok "repo absent version"; fi
fi

exit $fail
