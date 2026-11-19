#!/bin/sh
# Build a release's MCPB bundle and the server.json the MCP Registry gets (F-0455).
#   sh scripts/mcpb-bundle.sh <tag> [dist]
# Reads the five binaries and the SHA256SUMS that scripts/build-release.sh left
# in dist (default dist/) and writes:
#   dist/cerne_<tag>.mcpb  zip of packaging/mcpb/manifest.json (version filled
#                          in), the launcher and the binaries as
#                          server/cerne_<os>_<arch>[.exe]
#   dist/SHA256SUMS        gains the bundle's line (replacing an earlier one)
#   dist/server.json       the root server.json with the version, the bundle's
#                          release URL and its SHA-256
# A missing input fails before anything is written.
# The bundle is reproducible: the same binaries give the same bytes on any
# run, because the registry entry pins the hash of the first upload and a
# re-run of the release re-uploads the asset (release-publish.sh --clobber).
set -eu
[ $# -ge 1 ] || { echo "usage: mcpb-bundle.sh <tag> [dist]" >&2; exit 2; }
tag=$1
dist=${2:-dist}
version=${tag#v}
root=$(cd "$(dirname "$0")/.." && pwd)
pairs="darwin_amd64 darwin_arm64 linux_amd64 linux_arm64 windows_amd64.exe"

[ -f "$dist/SHA256SUMS" ] || { echo "mcpb-bundle: $dist has no SHA256SUMS" >&2; exit 1; }
for p in $pairs; do
  [ -f "$dist/cerne_${tag}_$p" ] || { echo "mcpb-bundle: $dist has no cerne_${tag}_$p" >&2; exit 1; }
done

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/server"
sed "s|\${VERSION}|$version|g" "$root/packaging/mcpb/manifest.json" >"$stage/manifest.json"
cp "$root/packaging/mcpb/cerne.sh" "$stage/server/cerne.sh"
chmod 644 "$stage/manifest.json" "$stage/server/cerne.sh"
files="manifest.json server/cerne.sh"
for p in $pairs; do
  cp "$dist/cerne_${tag}_$p" "$stage/server/cerne_$p"
  chmod 755 "$stage/server/cerne_$p"
  files="$files server/cerne_$p"
done

bundle="cerne_${tag}.mcpb"
out=$(cd "$dist" && pwd)/$bundle
rm -f "$out" # zip adds to an existing archive
# Fixed times, a fixed file order, no directory entries and no extra fields
# (-X drops the uid/gid and Unix time fields): nothing of the run's own.
# shellcheck disable=SC2086
(cd "$stage" && TZ=UTC0 touch -t 200001010000 $files && TZ=UTC0 zip -q -X -D "$out" $files)

if command -v sha256sum >/dev/null 2>&1; then
  sum=$(sha256sum "$out" | cut -d' ' -f1)
else
  sum=$(shasum -a 256 "$out" | cut -d' ' -f1)
fi
grep -v "  $bundle\$" "$dist/SHA256SUMS" >"$stage/SHA256SUMS" || true
printf '%s  %s\n' "$sum" "$bundle" >>"$stage/SHA256SUMS"
cp "$stage/SHA256SUMS" "$dist/SHA256SUMS"
sed -e "s|\${VERSION}|$version|g" -e "s|\${TAG}|$tag|g" -e "s|\${SHA256}|$sum|g" \
  "$root/server.json" >"$dist/server.json"
echo "$bundle $(wc -c <"$out" | tr -d ' ') bytes, sha256 $sum"
