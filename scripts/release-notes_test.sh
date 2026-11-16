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

exit $fail
