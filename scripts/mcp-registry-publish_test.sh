#!/bin/sh
# Tests for scripts/mcp-registry-publish.sh with a fake curl and a fake
# mcp-publisher on PATH (F-0455).
#   sh scripts/mcp-registry-publish_test.sh
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0
ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; fail=1; }

mkdir "$tmp/bin"
# curl writes FAKE_BODY to its -o file, prints FAKE_CODE (what
# -w '%{http_code}' prints) and exits FAKE_EXIT.
cat >"$tmp/bin/curl" <<'SH'
#!/bin/sh
echo "curl $*" >>"$CALL_LOG"
while [ $# -gt 0 ]; do
  [ "$1" = -o ] && printf '%s' "$FAKE_BODY" >"$2"
  shift
done
printf '%s' "$FAKE_CODE"
exit "${FAKE_EXIT:-0}"
SH
# The publisher's login exits FAKE_LOGIN_EXIT.
cat >"$tmp/bin/mcp-publisher" <<'SH'
#!/bin/sh
echo "mcp-publisher $*" >>"$CALL_LOG"
[ "$1" = login ] && exit "${FAKE_LOGIN_EXIT:-0}"
exit 0
SH
chmod +x "$tmp/bin/curl" "$tmp/bin/mcp-publisher"
sha=aaaa000000000000000000000000000000000000000000000000000000000000
printf '{"name": "io.github.henriquesakata/cerne", "version": "9.9.9", "packages": [{"fileSha256": "%s"}]}\n' "$sha" >"$tmp/server.json"
# The registry's record of a version: GET /v0.1/servers/<name>/versions/<v>.
record() { printf '{"server": {"name": "io.github.henriquesakata/cerne", "version": "9.9.9", "packages": [{"fileSha256": "%s"}]}, "_meta": {}}' "$1"; }

# run <http code> [curl exit] [body] [login exit]; leaves the calls in
# $tmp/calls, stderr in $tmp/err, the exit code in $rc
run() {
  : >"$tmp/calls"
  CALL_LOG="$tmp/calls" FAKE_CODE=$1 FAKE_EXIT=${2:-0} FAKE_BODY=${3:-} FAKE_LOGIN_EXIT=${4:-0} \
    MCP_PUBLISHER=mcp-publisher PATH="$tmp/bin:$PATH" \
    sh "$here/mcp-registry-publish.sh" "$tmp/server.json" >/dev/null 2>"$tmp/err"
  rc=$?
}
publisher_calls() { grep '^mcp-publisher' "$tmp/calls"; }

run 404
[ $rc -eq 0 ] && ok "404: exits 0" || bad "404: exit $rc ($(cat "$tmp/err"))"
grep -q '^curl .*/v0.1/servers/io.github.henriquesakata%2Fcerne/versions/9.9.9' "$tmp/calls" &&
  ok "404: looks the version up" || bad "404: lookup: $(cat "$tmp/calls")"
[ "$(publisher_calls)" = "mcp-publisher login github-oidc
mcp-publisher publish $tmp/server.json" ] && ok "404: logs in, then publishes" || bad "404: calls: $(cat "$tmp/calls")"

grep -q -- '--max-time' "$tmp/calls" && ok "lookup has a timeout" || bad "lookup without --max-time: $(cat "$tmp/calls")"

run 404 0 "" 1 # login refused
[ $rc -ne 0 ] && ok "login fails: fails" || bad "login fails: exited 0"
[ "$(publisher_calls)" = "mcp-publisher login github-oidc" ] && ok "login fails: no publish" || bad "login fails: calls: $(cat "$tmp/calls")"

run 200 0 "$(record "$sha")"
[ $rc -eq 0 ] && ok "200, same hash: exits 0" || bad "200, same hash: exit $rc ($(cat "$tmp/err"))"
[ -z "$(publisher_calls)" ] && ok "200, same hash: no login, no publish" || bad "200, same hash: calls: $(cat "$tmp/calls")"

# The registry pins another bundle hash than this release's server.json: the
# release asset changed under a published entry, which no skip may hide.
run 200 0 "$(record bbbb000000000000000000000000000000000000000000000000000000000000)"
[ $rc -ne 0 ] && ok "200, other hash: fails" || bad "200, other hash: exited 0"
grep -q bbbb "$tmp/err" && grep -q aaaa "$tmp/err" && ok "200, other hash: names both" || bad "200, other hash: stderr: $(cat "$tmp/err")"
[ -z "$(publisher_calls)" ] && ok "200, other hash: no login, no publish" || bad "200, other hash: calls: $(cat "$tmp/calls")"

run 500
[ $rc -ne 0 ] && ok "500: fails" || bad "500: exited 0"
grep -q 500 "$tmp/err" && ok "500: names the answer" || bad "500: stderr: $(cat "$tmp/err")"
[ -z "$(publisher_calls)" ] && ok "500: no login, no publish" || bad "500: calls: $(cat "$tmp/calls")"

run 000 7 # curl could not connect
[ $rc -ne 0 ] && ok "no connection: fails" || bad "no connection: exited 0"
[ -z "$(publisher_calls)" ] && ok "no connection: no login, no publish" || bad "no connection: calls: $(cat "$tmp/calls")"

exit $fail
