#!/bin/sh
# Publish a rendered server.json to the MCP Registry, once per version (F-0455).
#   sh scripts/mcp-registry-publish.sh <server.json>
# A published version can never change, so the registry refuses it a second
# time and a re-run of the release on the same tag (B-0010) would fail here.
# The version is looked up first: 200 means it is already published and nothing
# runs; 404 logs in with the workflow's GitHub OIDC token and publishes; any
# other answer, or no answer, fails without logging in.
# MCP_PUBLISHER names the publisher binary (default ./mcp-publisher).
set -eu
[ $# -eq 1 ] || { echo "usage: mcp-registry-publish.sh <server.json>" >&2; exit 2; }
file=$1
pub=${MCP_PUBLISHER:-./mcp-publisher}
name=$(jq -r .name "$file")
version=$(jq -r .version "$file")
url="https://registry.modelcontextprotocol.io/v0.1/servers/$(printf %s "$name" | sed 's|/|%2F|g')/versions/$version"
code=$(curl -s -o /dev/null -w '%{http_code}' "$url") || true
case $code in
  200) echo "$name $version is already in the MCP Registry: nothing to publish" ;;
  404) "$pub" login github-oidc && "$pub" publish "$file" ;;
  *) echo "MCP Registry lookup of $name $version answered '$code' ($url): not publishing" >&2; exit 1 ;;
esac
