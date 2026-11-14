#!/bin/sh
# Print the Homebrew formula for a cerne release (F-0410).
#
#   scripts/homebrew-formula.sh <tag> <SHA256SUMS>
#
# release.yml writes the output to Formula/cerne.rb in henriquesakata/homebrew-tap.
# The formula installs the release binary for the machine, no build and no Go,
# with each platform pinned to its asset's hash in SHA256SUMS. A formula, not a
# cask: Homebrew quarantines cask downloads, and Gatekeeper then blocks a binary
# that is not notarized.
set -eu

fail() {
  echo "homebrew-formula: $*" >&2
  exit 1
}

[ $# -eq 2 ] || fail "usage: homebrew-formula.sh <tag> <SHA256SUMS>"
tag=$1
sums=$2
case "$tag" in
  *[!A-Za-z0-9._-]*) fail "invalid tag '$tag'" ;;
  v[0-9]*) ;;
  *) fail "tag must look like v1.2.3, got '$tag'" ;;
esac
[ -r "$sums" ] || fail "cannot read $sums"

# hash <os_arch>: the asset's SHA-256 from SHA256SUMS (`<hash>  <name>`).
hash() {
  h=$(awk -v n="cerne_${tag}_$1" '$2 == n || $2 == "*" n { print $1 }' "$sums")
  [ -n "$h" ] || fail "no line for cerne_${tag}_$1 in $sums"
  echo "$h"
}

# Every hash before any output: a missing asset prints no formula.
darwin_arm64=$(hash darwin_arm64) || exit 1
darwin_amd64=$(hash darwin_amd64) || exit 1
linux_arm64=$(hash linux_arm64) || exit 1
linux_amd64=$(hash linux_amd64) || exit 1

url="https://github.com/henriquesakata/cerne/releases/download/$tag/cerne_$tag"

cat <<EOF
class Cerne < Formula
  desc "Living documentation for projects built with AI agents"
  homepage "https://github.com/henriquesakata/cerne"
  version "${tag#v}"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "${url}_darwin_arm64"
      sha256 "$darwin_arm64"
    end
    on_intel do
      url "${url}_darwin_amd64"
      sha256 "$darwin_amd64"
    end
  end

  on_linux do
    on_arm do
      url "${url}_linux_arm64"
      sha256 "$linux_arm64"
    end
    on_intel do
      url "${url}_linux_amd64"
      sha256 "$linux_amd64"
    end
  end

  def install
    bin.install Dir["cerne_*"].first => "cerne"
  end

  test do
    # cerne --version prints MAJOR.MINOR ("cerne 0.10").
    assert_match "cerne #{version.major_minor}", shell_output("#{bin}/cerne --version")
  end
end
EOF
