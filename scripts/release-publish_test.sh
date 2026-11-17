#!/bin/sh
# Tests for scripts/release-publish.sh with a fake gh on PATH (B-0010).
#   sh scripts/release-publish_test.sh
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0
ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; fail=1; }

mkdir "$tmp/bin"
# Records one line per call; the releases/tags lookup answers FAKE_STATE:
# exists (success), missing (HTTP 404), error (any other failure).
cat >"$tmp/bin/gh" <<'GH'
#!/bin/sh
echo "$*" >>"$GH_LOG"
if [ "$1" = api ]; then
  case "$FAKE_STATE" in
    exists) exit 0 ;;
    missing) echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
    *) echo "gh: Bad credentials (HTTP 401)" >&2; exit 1 ;;
  esac
fi
exit 0
GH
chmod +x "$tmp/bin/gh"
echo notes >"$tmp/notes.md"

# run <state> <tag>; leaves the non-lookup calls in $tmp/calls, exit in $rc
run() {
  : >"$tmp/log"
  PATH="$tmp/bin:$PATH" GH_LOG="$tmp/log" FAKE_STATE=$1 GITHUB_REPOSITORY=o/r \
    sh "$here/release-publish.sh" "$2" "$tmp/notes.md" a.bin b.bin >"$tmp/out" 2>&1
  rc=$?
  grep -v '^api ' "$tmp/log" >"$tmp/calls"
}
check() { # name expected-calls
  if [ "$(cat "$tmp/calls")" = "$2" ]; then ok "$1"; else bad "$1: got [$(cat "$tmp/calls")]"; fi
}
N="--notes-file $tmp/notes.md"

run missing v1.0.0
check "new stable: one create, explicit prerelease=false + latest" "release create v1.0.0 a.bin b.bin --title cerne v1.0.0 $N --prerelease=false --latest"
run missing v1.0.0-rc.1
check "new rc: create with prerelease=true" "release create v1.0.0-rc.1 a.bin b.bin --title cerne v1.0.0-rc.1 $N --prerelease=true"
run exists v1.0.0
check "existing stable: edit corrects flag + upload --clobber, no create" "release edit v1.0.0 $N --prerelease=false --latest
release upload v1.0.0 a.bin b.bin --clobber"
run exists v1.0.0-rc.1
check "existing rc: edit prerelease=true + upload --clobber" "release edit v1.0.0-rc.1 $N --prerelease=true
release upload v1.0.0-rc.1 a.bin b.bin --clobber"
run error v1.0.0
check "lookup failure other than 404: no create, no edit" ""
if [ "$rc" -ne 0 ] && grep -q "Bad credentials" "$tmp/out"; then ok "lookup failure: non-zero exit with the error text"; else bad "lookup failure: rc=$rc out=[$(cat "$tmp/out")]"; fi

exit $fail
