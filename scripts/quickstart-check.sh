#!/bin/sh
# Runs the quickstart blocks of a markdown file against a cerne binary (F-0374).
#
#   scripts/quickstart-check.sh <markdown-file> <cerne-binary>
#
# Only a ```bash block whose previous line is `<!-- quickstart -->` runs, in
# file order, one line at a time, in an empty git repository under a throwaway
# HOME and cerne cache/data/config. `cerne ui` is started in the background on
# a free port, must answer GET /api/projects within QUICKSTART_UI_TIMEOUT
# seconds (default 30), and is stopped by PID. A failing line prints
# `quickstart: '<line>' failed with cerne <version>` and exits non-zero.
set -u

[ $# -eq 2 ] || { echo "usage: $0 <markdown-file> <cerne-binary>" >&2; exit 2; }
md=$1
bin=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
[ -r "$md" ] || { echo "quickstart: cannot read $md" >&2; exit 2; }
[ -x "$bin" ] || { echo "quickstart: $bin is not executable" >&2; exit 2; }

version=$("$bin" --version 2>&1 | head -n 1)
work=$(mktemp -d)
ui_pid=
cleanup() {
  [ -n "$ui_pid" ] && kill "$ui_pid" 2>/dev/null
  rm -rf "$work"
}
trap cleanup EXIT INT TERM

mkdir "$work/bin" "$work/home" "$work/repo"
ln -s "$bin" "$work/bin/cerne"
PATH="$work/bin:$PATH"
HOME="$work/home"
CERNE_CACHE_DIR="$work/cache"
CERNE_DATA_DIR="$work/data"
CERNE_CONFIG_DIR="$work/config"
GIT_AUTHOR_NAME=quickstart GIT_AUTHOR_EMAIL=quickstart@example.invalid
GIT_COMMITTER_NAME=quickstart GIT_COMMITTER_EMAIL=quickstart@example.invalid
export PATH HOME CERNE_CACHE_DIR CERNE_DATA_DIR CERNE_CONFIG_DIR \
  GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL

# Marked blocks, one command per line; blank and comment lines dropped.
awk '
  /^<!-- quickstart -->[ \t]*$/ { marked = 1; next }
  marked && /^```bash[ \t]*$/   { inblock = 1; marked = 0; next }
  inblock && /^```/             { inblock = 0; next }
  inblock && $0 !~ /^[ \t]*(#|$)/ { print }
  { marked = 0 }
' "$md" >"$work/lines"

[ -s "$work/lines" ] || { echo "quickstart: no <!-- quickstart --> block in $md" >&2; exit 2; }

fail() {
  echo "quickstart: '$1' failed with cerne $version" >&2
  exit 1
}

cd "$work/repo" && git init -q || exit 2

while IFS= read -r line; do
  case $line in
    "cerne ui"*)
      # ponytail: random port, a clash shows up as a failed readiness check; retry if it ever flakes
      port=$(awk 'BEGIN { srand(); print 20000 + int(rand() * 20000) }')
      cmd=$(printf '%s' "$line" | sed 's/[ \t]*#.*$//')
      eval "$cmd --no-browser --port $port" >"$work/ui.log" 2>&1 &
      ui_pid=$!
      i=0
      until curl -fs "http://127.0.0.1:$port/api/projects" >/dev/null 2>&1; do
        i=$((i + 1))
        if [ "$i" -gt "$(( ${QUICKSTART_UI_TIMEOUT:-30} * 2 ))" ] || ! kill -0 "$ui_pid" 2>/dev/null; then
          cat "$work/ui.log" >&2
          fail "$line"
        fi
        sleep 0.5
      done
      kill "$ui_pid" 2>/dev/null
      wait "$ui_pid" 2>/dev/null
      ui_pid=
      ;;
    *)
      eval "$line" || fail "$line"
      ;;
  esac
  echo "quickstart: ok: $line"
done <"$work/lines"
