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

version=$("$bin" --version 2>&1 </dev/null | head -n 1 | sed 's/^cerne //')
[ -n "$version" ] || version=unknown
tag=${QUICKSTART_TAG:+ (release $QUICKSTART_TAG)}
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

# Marked blocks, one file per block (block.N), one command per line; blank and
# comment lines dropped. CRLF is normalized first.
tr -d '\r' <"$md" | awk -v dir="$work" '
  /^<!-- quickstart -->[ \t]*$/ { marked = 1; next }
  marked && /^```bash[ \t]*$/   { inblock = 1; marked = 0; n++; next }
  inblock && /^```/             { inblock = 0; next }
  inblock && $0 !~ /^[ \t]*(#|$)/ { print >(dir "/block." n) }
  { marked = 0 }
'

set -- "$work"/block.*
[ -e "$1" ] || { echo "quickstart: no <!-- quickstart --> block in $md" >&2; exit 2; }

fail() {
  [ -n "$ui_pid" ] && kill "$ui_pid" 2>/dev/null
  echo "quickstart: '$1' failed with cerne $version$tag" >&2
  exit 1
}

# True when nothing accepts connections on the port (curl exit 7).
port_free() {
  curl -s --max-time 1 -o /dev/null "http://127.0.0.1:$1/" 2>/dev/null
  [ $? -eq 7 ]
}

start_ui() {
  cmd=$(printf '%s' "$1" | sed 's/[ \t]*#.*$//')
  port=
  if [ -n "${QUICKSTART_UI_PORT:-}" ]; then
    port_free "$QUICKSTART_UI_PORT" && port=$QUICKSTART_UI_PORT
  else
    for _ in 1 2 3 4 5; do
      p=$(awk 'BEGIN { srand(); print 20000 + int(rand() * 20000) }')
      port_free "$p" && { port=$p; break; }
      sleep 1
    done
  fi
  [ -n "$port" ] || { echo "quickstart: no free port for cerne ui" >&2; fail "$1"; }
  # exec: $! is the binary itself, so kill stops the server and not a wrapper.
  eval "exec $cmd --no-browser --port $port" </dev/null >"$work/ui.log" 2>&1 &
  ui_pid=$!
  i=0
  until curl -fs --max-time 2 "http://127.0.0.1:$port/api/projects" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt "$(( ${QUICKSTART_UI_TIMEOUT:-30} * 2 ))" ] || ! kill -0 "$ui_pid" 2>/dev/null; then
      cat "$work/ui.log" >&2
      fail "$1"
    fi
    sleep 0.5
  done
  # A foreign listener answering is not our server: ours must still be alive.
  kill -0 "$ui_pid" 2>/dev/null || fail "$1"
  kill "$ui_pid" 2>/dev/null
  wait "$ui_pid" 2>/dev/null
  ui_pid=
}

git -C "$work/repo" init -q || exit 2

# Each block in its own subshell: a cd or export does not leak to the next.
for block in "$@"; do
  (
    cd "$work/repo" || exit 2
    ui_pid=
    while IFS= read -r line; do
      if printf '%s' "$line" | grep -Eq '(^|[^[:alnum:]_])sudo([[:space:]]|$)|\|[[:space:]]*(sh|bash)([[:space:]]|$)'; then
        echo "quickstart: refusing '$line': a marked line must not use sudo or pipe into a shell" >&2
        exit 1
      fi
      case $line in
        "cerne ui"*) start_ui "$line" ;;
        *) eval "$line" </dev/null || fail "$line" ;;
      esac
      echo "quickstart: ok: $line"
    done <"$block"
  ) || exit $?
done
