#!/bin/sh
# Print the release notes for a tag: its CHANGELOG.md section (F-0446).
#
#   scripts/release-notes.sh <tag>        # v0.14.0 or v0.14.0-rc.1
#
# The section `## [X.Y.Z]` runs to the next `## [` heading or to the link
# references at the end of the file; lines inside a fenced code block never end
# it. A pre-release tag (-rc.N) reads the same section, under a notice.
# CHANGELOG overrides the file path (tests). Exits 1 when the section is missing.
set -eu
tag=${1:?usage: release-notes.sh <tag>}
file=${CHANGELOG:-CHANGELOG.md}
ver=${tag#v}
ver=${ver%%-*}

notes=$(awk -v head="## [$ver]" '
  /^```/ { fence = !fence }
  !fence && (index($0, "## [") == 1 || $0 ~ /^\[[^]]+\]: /) {
    if (on) exit
    if (index($0, head) == 1) { on = 1; found = 1 }
  }
  on { print }
  END { exit !found }
' "$file") || {
  echo "release-notes: no section '## [$ver]' in $file" >&2
  exit 1
}

case $tag in
  *-*) printf '> **This is a pre-release** of %s, published to try the release before it ships. It is not on Homebrew; do not use it in production.\n\n' "$ver" ;;
esac
printf '%s\n' "$notes"
