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
# Records one line per call; `release view` answers FAKE_EXISTS.
cat >"$tmp/bin/gh" <<'GH'
#!/bin/sh
echo "$*" >>"$GH_LOG"
if [ "$1 $2" = "release view" ]; then [ "$FAKE_EXISTS" = 1 ]; exit $?; fi
exit 0
GH
chmod +x "$tmp/bin/gh"
echo notes >"$tmp/notes.md"

# run <exists 0|1> <tag>; leaves the non-view calls in $tmp/log
run() {
  : >"$tmp/log"
  PATH="$tmp/bin:$PATH" GH_LOG="$tmp/log" FAKE_EXISTS=$1 sh "$here/release-publish.sh" "$2" "$tmp/notes.md" a.bin b.bin >/dev/null 2>&1
  grep -v '^release view' "$tmp/log" >"$tmp/calls"
}
check() { # name expected-calls-file-content
  if [ "$(cat "$tmp/calls")" = "$2" ]; then ok "$1"; else bad "$1: got [$(cat "$tmp/calls")]"; fi
}

run 0 v1.0.0
check "new stable: one create, no prerelease" "release create v1.0.0 a.bin b.bin --title cerne v1.0.0 --notes-file $tmp/notes.md"
run 0 v1.0.0-rc.1
check "new rc: create with --prerelease" "release create v1.0.0-rc.1 a.bin b.bin --title cerne v1.0.0-rc.1 --notes-file $tmp/notes.md --prerelease"
run 1 v1.0.0
check "existing stable: edit + upload --clobber, no create" "release edit v1.0.0 --notes-file $tmp/notes.md
release upload v1.0.0 a.bin b.bin --clobber"
run 1 v1.0.0-rc.1
check "existing rc: edit --prerelease + upload --clobber" "release edit v1.0.0-rc.1 --notes-file $tmp/notes.md --prerelease
release upload v1.0.0-rc.1 a.bin b.bin --clobber"

exit $fail
