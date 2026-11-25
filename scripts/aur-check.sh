#!/bin/sh
# Build and install the AUR package cerne-bin for a published release (F-0507).
#
#   scripts/aur-check.sh <tag> [<out-dir>]
#
# Runs as root inside an archlinux:base-devel container (the release's `aur`
# job and the PR's packaging.yml). It renders the PKGBUILD from the release's
# SHA256SUMS, writes the .SRCINFO, lints the PKGBUILD with namcap (an `E:` line
# fails), installs the package with makepkg -si and fails unless
# `cerne --version` prints `cerne <tag without the v>`. With <out-dir> it leaves
# the approved PKGBUILD and .SRCINFO there, for the push to the AUR.
set -eu

fail() {
  echo "aur-check: $*" >&2
  exit 1
}

[ $# -ge 1 ] && [ $# -le 2 ] || fail "usage: aur-check.sh <tag> [<out-dir>]"
tag=$1
out=${2:-}
[ "$(id -u)" -eq 0 ] || fail "run it as root in an archlinux:base-devel container"
here=$(cd "$(dirname "$0")" && pwd)

# makepkg refuses root: a plain user builds, and may run only pacman through sudo.
pacman -Syu --noconfirm --needed namcap sudo >/dev/null
id builder >/dev/null 2>&1 || useradd -m builder
echo 'builder ALL=(root) NOPASSWD: /usr/bin/pacman' >/etc/sudoers.d/builder
chmod 440 /etc/sudoers.d/builder

work=$(mktemp -d)
chmod 755 "$work"
curl -fsSL -o "$work/SHA256SUMS" "https://github.com/henriquesakata/cerne/releases/download/$tag/SHA256SUMS" ||
  fail "cannot download SHA256SUMS of $tag"
mkdir "$work/pkg"
sh "$here/aur-pkgbuild.sh" "$tag" "$work/SHA256SUMS" >"$work/pkg/PKGBUILD"
chown -R builder "$work/pkg"

as_builder() { su builder -s /bin/sh -c "cd '$work/pkg' && $1"; }

as_builder 'makepkg --printsrcinfo >.SRCINFO'
echo "--- .SRCINFO"
cat "$work/pkg/.SRCINFO"

echo "--- namcap PKGBUILD"
lint=$(as_builder 'namcap PKGBUILD' 2>&1) || fail "namcap failed: $lint"
echo "$lint"
if echo "$lint" | grep -q ' E: '; then
  fail "namcap reports an error in the PKGBUILD"
fi

echo "--- makepkg -si"
as_builder 'makepkg -si --noconfirm'

want="cerne ${tag#v}"
got=$(cerne --version)
echo "--- cerne --version: $got"
[ "$got" = "$want" ] || fail "installed '$got', want '$want'"

if [ -n "$out" ]; then
  mkdir -p "$out"
  cp "$work/pkg/PKGBUILD" "$work/pkg/.SRCINFO" "$out/"
fi
