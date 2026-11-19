#!/bin/sh
# Publish a rendered server.json to the MCP Registry, once per version (F-0455).
#   sh scripts/mcp-registry-publish.sh <server.json>
# A published version can never change, so the registry refuses it a second
# time and a re-run of the release on the same tag (B-0010) would fail here.
# The version is looked up first:
#   200  already published: nothing runs, as long as the registry pins the same
#        bundle hash as this server.json; another hash means the release asset
#        changed under the published entry, and that fails loudly
#   404  logs in with the workflow's GitHub OIDC token and publishes
#   else (or no answer within 30 s) fails without logging in
# MCP_PUBLISHER names the publisher binary (default ./mcp-publisher).
set -eu
[ $# -eq 1 ] || { echo "usage: mcp-registry-publish.sh <server.json>" >&2; exit 2; }
file=$1
pub=${MCP_PUBLISHER:-./mcp-publisher}
name=$(jq -r .name "$file")
version=$(jq -r .version "$file")
url="https://registry.modelcontextprotocol.io/v0.1/servers/$(printf %s "$name" | sed 's|/|%2F|g')/versions/$version"
body=$(mktemp)
trap 'rm -f "$body"' EXIT
code=$(curl -s --max-time 30 -o "$body" -w '%{http_code}' "$url") || true
case $code in
  200)
    have=$(jq -r '.server.packages[0].fileSha256' "$body")
    want=$(jq -r '.packages[0].fileSha256' "$file")
    if [ "$have" != "$want" ]; then
      echo "MCP Registry has $name $version with bundle sha256 $have, but this release's server.json says $want:" >&2
      echo "the release's .mcpb no longer matches the published entry; restore that asset or release a new version" >&2
      exit 1
    fi
    echo "$name $version is already in the MCP Registry: nothing to publish"
    ;;
  404) "$pub" login github-oidc && "$pub" publish "$file" ;;
  *) echo "MCP Registry lookup of $name $version answered '$code' ($url): not publishing" >&2; exit 1 ;;
esac
