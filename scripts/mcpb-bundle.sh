#!/bin/sh
# Build a release's MCPB bundle and the server.json the MCP Registry gets (F-0455).
#   sh scripts/mcpb-bundle.sh <tag> [dist]
# Reads the five binaries scripts/build-release.sh left in dist (default dist/)
# and writes:
#   dist/cerne_<tag>.mcpb  zip of packaging/mcpb/manifest.json (version filled
#                          in), the launcher and the binaries as
#                          server/cerne_<os>_<arch>[.exe]
#   dist/SHA256SUMS        gains the bundle's line
#   dist/server.json       the root server.json with the version, the bundle's
#                          release URL and its SHA-256
# A missing binary fails before anything is written.
set -eu
[ $# -ge 1 ] || { echo "usage: mcpb-bundle.sh <tag> [dist]" >&2; exit 2; }
tag=$1
dist=${2:-dist}
version=${tag#v}
root=$(cd "$(dirname "$0")/.." && pwd)
pairs="darwin_amd64 darwin_arm64 linux_amd64 linux_arm64 windows_amd64.exe"

for p in $pairs; do
  [ -f "$dist/cerne_${tag}_$p" ] || { echo "mcpb-bundle: $dist has no cerne_${tag}_$p" >&2; exit 1; }
done

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/server"
sed "s|\${VERSION}|$version|g" "$root/packaging/mcpb/manifest.json" >"$stage/manifest.json"
cp "$root/packaging/mcpb/cerne.sh" "$stage/server/cerne.sh"
for p in $pairs; do
  cp "$dist/cerne_${tag}_$p" "$stage/server/cerne_$p"
  chmod 755 "$stage/server/cerne_$p"
done

bundle="cerne_${tag}.mcpb"
out=$(cd "$dist" && pwd)/$bundle
rm -f "$out" # zip adds to an existing archive
(cd "$stage" && zip -q -X -r "$out" manifest.json server)

if command -v sha256sum >/dev/null 2>&1; then
  sum=$(sha256sum "$out" | cut -d' ' -f1)
else
  sum=$(shasum -a 256 "$out" | cut -d' ' -f1)
fi
printf '%s  %s\n' "$sum" "$bundle" >>"$dist/SHA256SUMS"
sed -e "s|\${VERSION}|$version|g" -e "s|\${TAG}|$tag|g" -e "s|\${SHA256}|$sum|g" \
  "$root/server.json" >"$dist/server.json"
echo "$bundle $(wc -c <"$out" | tr -d ' ') bytes, sha256 $sum"
