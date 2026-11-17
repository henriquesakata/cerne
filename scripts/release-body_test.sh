#!/bin/sh
# Tests for scripts/release-body.sh (B-0011).
#   sh scripts/release-body_test.sh
here=$(cd "$(dirname "$0")" && pwd)
root=$(dirname "$here")
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0
ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; fail=1; }

printf '## [0.9.0] - 2026-01-01\n\nSection text.\n' >"$tmp/section.md"
# The one-liners as INSTALL.md shows them, so the body cannot drift from it.
sh_line=$(grep -m1 '^curl -fsSL .*install\.sh | sh$' "$root/INSTALL.md")
ps_line=$(grep -m1 '^irm .*install\.ps1 | iex$' "$root/INSTALL.md")

for tag in v0.9.0 v1.0.0-rc.1; do
  out=$(sh "$here/release-body.sh" "$tag" "$tmp/section.md" 2>&1) || { bad "$tag: script failed: $out"; continue; }
  has() { printf '%s\n' "$out" | grep -qF -- "$1"; }
  has 'Section text.' && ok "$tag: section" || bad "$tag: section"
  has "go install github.com/henriquesakata/cerne/cmd/cerne@$tag" && ok "$tag: go install" || bad "$tag: go install"
  has 'brew upgrade henriquesakata/tap/cerne' && ok "$tag: brew" || bad "$tag: brew"
  [ -n "$sh_line" ] && has "$sh_line" && ok "$tag: shell one-liner" || bad "$tag: shell one-liner"
  [ -n "$ps_line" ] && has "$ps_line" && ok "$tag: powershell one-liner" || bad "$tag: powershell one-liner"
  has 'cerne --version' && ok "$tag: version check" || bad "$tag: version check"
  order=$(printf '%s\n' "$out" | grep -n -e '^Section text' -e '^---$' -e '^## [UIV]' | cut -d: -f2- | tr '\n' '|')
  [ "$order" = 'Section text.|---|## Update|## Install|## Verify|' ] && ok "$tag: order" || bad "$tag: order ($order)"
done

[ $fail = 0 ]
