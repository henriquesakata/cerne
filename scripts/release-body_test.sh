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

tap=$(sed -n 's/^brew install \(.*\)$/\1/p' "$root/INSTALL.md" | head -1)
gi_line=$(grep -m1 '^go install .*@latest$' "$root/INSTALL.md")

for tag in v0.9.0 v1.0.0-rc.1; do
  out=$(sh "$here/release-body.sh" "$tag" "$tmp/section.md" 2>&1) || { bad "$tag: script failed: $out"; continue; }
  has() { printf '%s\n' "$out" | grep -qF -- "$1"; }
  has 'Section text.' && ok "$tag: section" || bad "$tag: section"
  [ -n "$gi_line" ] && has "$(printf '%s' "$gi_line" | sed "s/@latest\$/@$tag/")" && ok "$tag: go install" || bad "$tag: go install"
  [ -n "$tap" ] && has "brew upgrade $tap" && ok "$tag: brew" || bad "$tag: brew"
  has 'SHA256SUMS' && ok "$tag: install block" || bad "$tag: install block"
  has "blob/$tag/INSTALL.md" && ok "$tag: INSTALL link" || bad "$tag: INSTALL link"
  has 'gh attestation verify <file> --repo' && ok "$tag: verify block" || bad "$tag: verify block"
  case $tag in
    *-*) has 'pre-release' && has 'files attached to this release' && ok "$tag: rc notice" || bad "$tag: rc notice" ;;
    *) printf '%s\n' "$out" | grep -qF 'pre-release' && bad "$tag: stable has rc notice" || ok "$tag: no rc notice" ;;
  esac
  [ -n "$sh_line" ] && has "$sh_line" && ok "$tag: shell one-liner" || bad "$tag: shell one-liner"
  [ -n "$ps_line" ] && has "$ps_line" && ok "$tag: powershell one-liner" || bad "$tag: powershell one-liner"
  has 'cerne --version' && ok "$tag: version check" || bad "$tag: version check"
  order=$(printf '%s\n' "$out" | grep -n -e '^Section text' -e '^---$' -e '^## [UIV]' | cut -d: -f2- | tr '\n' '|')
  [ "$order" = 'Section text.|---|## Update|## Install|## Verify|' ] && ok "$tag: order" || bad "$tag: order ($order)"
done

[ $fail = 0 ]
