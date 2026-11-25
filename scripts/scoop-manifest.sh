#!/bin/sh
# Print the Scoop manifest for a cerne release (F-0508).
#
#   scripts/scoop-manifest.sh <tag> <SHA256SUMS>
#
# release.yml writes the output to bucket/cerne.json in
# henriquesakata/scoop-bucket. The manifest installs the release's Windows
# executable (no build, no Go) pinned to its hash in SHA256SUMS; the `#/cerne.exe`
# suffix makes Scoop save it as cerne.exe, so `bin` is the same in every
# version. `checkver` and `autoupdate` let Scoop's own tooling follow later
# releases from the GitHub release and its SHA256SUMS.
set -eu

fail() {
  echo "scoop-manifest: $*" >&2
  exit 1
}

[ $# -eq 2 ] || fail "usage: scoop-manifest.sh <tag> <SHA256SUMS>"
tag=$1
sums=$2
case "$tag" in
  *[!A-Za-z0-9._-]*) fail "invalid tag '$tag'" ;;
  v[0-9]*) ;;
  *) fail "tag must look like v1.2.3, got '$tag'" ;;
esac
[ -r "$sums" ] || fail "cannot read $sums"

# The asset's SHA-256 from SHA256SUMS (`<hash>  <name>`), before any output:
# a missing asset prints no manifest.
asset="cerne_${tag}_windows_amd64.exe"
hash=$(awk -v n="$asset" '$2 == n || $2 == "*" n { print $1 }' "$sums")
[ -n "$hash" ] || fail "no line for $asset in $sums"

# A quoted template: the $version, $baseurl and the regex's backslashes are
# Scoop's, not the shell's.
sed -e "s|@VERSION@|${tag#v}|" -e "s|@TAG@|$tag|g" -e "s|@HASH@|$hash|" <<'EOF'
{
  "version": "@VERSION@",
  "description": "Living documentation for projects built with AI agents",
  "homepage": "https://github.com/henriquesakata/cerne",
  "license": "Apache-2.0",
  "architecture": {
    "64bit": {
      "url": "https://github.com/henriquesakata/cerne/releases/download/@TAG@/cerne_@TAG@_windows_amd64.exe#/cerne.exe",
      "hash": "@HASH@"
    }
  },
  "bin": "cerne.exe",
  "checkver": {
    "github": "https://github.com/henriquesakata/cerne"
  },
  "autoupdate": {
    "architecture": {
      "64bit": {
        "url": "https://github.com/henriquesakata/cerne/releases/download/v$version/cerne_v$version_windows_amd64.exe#/cerne.exe"
      }
    },
    "hash": {
      "url": "$baseurl/SHA256SUMS",
      "regex": "$sha256\\s+\\*?$basename"
    }
  }
}
EOF
