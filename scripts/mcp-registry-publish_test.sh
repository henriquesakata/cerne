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
# curl prints FAKE_CODE (what -w '%{http_code}' prints) and exits FAKE_EXIT.
cat >"$tmp/bin/curl" <<'SH'
#!/bin/sh
echo "curl $*" >>"$CALL_LOG"
printf '%s' "$FAKE_CODE"
exit "${FAKE_EXIT:-0}"
SH
cat >"$tmp/bin/mcp-publisher" <<'SH'
#!/bin/sh
echo "mcp-publisher $*" >>"$CALL_LOG"
SH
chmod +x "$tmp/bin/curl" "$tmp/bin/mcp-publisher"
printf '{"name": "io.github.henriquesakata/cerne", "version": "9.9.9", "packages": []}\n' >"$tmp/server.json"

# run <http code> [curl exit]; leaves the calls in $tmp/calls, stderr in
# $tmp/err, the exit code in $rc
run() {
  : >"$tmp/calls"
  CALL_LOG="$tmp/calls" FAKE_CODE=$1 FAKE_EXIT=${2:-0} MCP_PUBLISHER=mcp-publisher PATH="$tmp/bin:$PATH" \
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

run 200
[ $rc -eq 0 ] && ok "200: exits 0" || bad "200: exit $rc"
[ -z "$(publisher_calls)" ] && ok "200: no login, no publish" || bad "200: calls: $(cat "$tmp/calls")"

run 500
[ $rc -ne 0 ] && ok "500: fails" || bad "500: exited 0"
grep -q 500 "$tmp/err" && ok "500: names the answer" || bad "500: stderr: $(cat "$tmp/err")"
[ -z "$(publisher_calls)" ] && ok "500: no login, no publish" || bad "500: calls: $(cat "$tmp/calls")"

run 000 7 # curl could not connect
[ $rc -ne 0 ] && ok "no connection: fails" || bad "no connection: exited 0"
[ -z "$(publisher_calls)" ] && ok "no connection: no login, no publish" || bad "no connection: calls: $(cat "$tmp/calls")"

exit $fail
