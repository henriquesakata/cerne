#!/bin/sh
# Builds "taskflow", a fictional project with a realistic cerne history, so you
# can explore cerne with data before adopting it.
#
#   sh examples/demo/seed.sh [dir]      # default dir: ./cerne-demo
#
# Creates <dir>/taskflow (the project) and <dir>/taskflow-docs.git (a local
# bare backend). Nothing leaves your machine. Uses only the public cerne CLI,
# so this file doubles as a tour of the commands.
set -eu

fail() {
  echo "seed: $*" >&2
  exit 1
}

dest="${1:-cerne-demo}"
content="$(cd "$(dirname "$0")" && pwd)/content"

command -v cerne >/dev/null 2>&1 ||
  fail "cerne is not on your PATH. Install it first: curl -fsSL https://raw.githubusercontent.com/henriquesakata/cerne/main/install.sh | sh"
command -v git >/dev/null 2>&1 || fail "git is required"
# The demo is written in English, which cerne scaffolds and lints since the
# release that added --docs-language.
cerne init --help 2>/dev/null | grep -q -- '--docs-language' ||
  fail "this cerne is too old for the demo: it needs a release newer than v0.5.0 (English documents). Update with install.sh, or build it from this clone: go install ./cmd/cerne"
[ -d "$content" ] || fail "content directory not found at $content"
[ ! -e "$dest" ] || fail "$dest already exists; remove it or pass another directory"

mkdir -p "$dest"
dest="$(cd "$dest" && pwd)"
log="$dest/seed.log"
# The demo keeps its cache inside <dir>, away from your real projects.
export CERNE_CACHE_DIR="$dest/.cache"

# run <cerne args...>: runs cerne quietly; on failure shows what it said.
run() {
  if ! cerne "$@" >"$dest/.out" 2>&1; then
    cat "$dest/.out" >&2
    fail "'cerne $*' failed (full log: $log)"
  fi
  cat "$dest/.out" >>"$log"
}
# id_of: the entity id the last command printed (F-0001, D-0002, …).
id_of() {
  grep -o '[A-Z][A-Z]*-[0-9][0-9]*' "$dest/.out" | head -1
}
# put <id> <slug> <doc>: copies a plan or result into the materialized slot.
put() {
  slot=$(ls -d ".docs/current/features/$1-"*)
  {
    printf -- '---\ncerne_id: %s\n---\n\n' "$1"
    cat "$content/features/$2/$3.md"
  } >"$slot/$3.md"
}
# feature <slug> <title> <target state> <cerne feature new flags...>
# Walks one feature through the lifecycle up to the target state.
feature() {
  slug=$1 title=$2 target=$3
  shift 3
  run feature new "$title" --file "$content/features/$slug/spec.md" "$@"
  id=$(id_of)
  if [ "$target" != candidate ] && [ "$target" != rejected ]; then
    run feature approve "$id"
  fi
  case "$target" in in_development | implemented | done)
    run pull
    put "$id" "$slug" plan
    run push
    run feature start "$id"
    ;;
  esac
  case "$target" in implemented | done)
    run pull
    put "$id" "$slug" result
    run push
    run feature complete "$id"
    ;;
  esac
  echo "  $id  $(printf '%-15s' "$target") $title"
}

echo "Creating the taskflow demo in $dest"
git init -q --bare "$dest/taskflow-docs.git"
mkdir "$dest/taskflow"
cd "$dest/taskflow"
git init -q
run init --backend "$dest/taskflow-docs.git" --name taskflow --docs-language en
echo ".docs/" >.gitignore

# In a real project, approving a spec and accepting a delivery are human acts:
# an AI agent cannot sign off its own work. While seeding, the script opens
# those gates so it can run unattended (even when an agent runs it for you),
# and restores the default configuration before it finishes.
cp .cernerc "$dest/.cernerc.default"
# gates <approve> <accept>: sets who may cross each gate ("auto" or "human").
gates() {
  cp "$dest/.cernerc.default" .cernerc
  printf '\n[gates]\napprove = "%s"\naccept = "%s"\n' "$1" "$2" >>.cernerc
}

echo "Features:"
gates auto auto # with accept on auto, completing a feature lands it on done
feature sign-in-with-google "Sign in with Google" done --facet auth --component oauth,login
google=$id
feature sign-in-with-facebook "Sign in with Facebook" rejected --facet auth --component oauth,login
run feature reject "$id" --reason "Measured first: 1.8% of new sign-ups asked for it, against 58% using Google."
feature export-tasks-as-csv "Export tasks as CSV" done --facet data --component export,api
gates auto human # from here on, a completed feature waits for a human accept
feature notifications "Notifications" in_development --facet code --component notify
epic=$id
feature weekly-email-digest "Weekly email digest" implemented --parent "$epic" --facet code --component notify,email
digest=$id
feature slack-notifications "Slack notifications" approved --parent "$epic" --facet code --component notify,slack
feature recurring-tasks "Recurring tasks" in_development --facet code --component tasks,recurrence
feature offline-mode "Offline mode" candidate --facet code --component web,sync

echo "Decisions, knowledge and sources:"
run decision new "Use server-side session cookies for auth" --file "$content/decisions/session-cookies.md" --facet auth --component session
cookies=$(id_of)
run decision new "Use JWT access tokens with rotating refresh tokens" --file "$content/decisions/jwt-tokens.md" --facet auth --component tokens
jwt=$(id_of)
run decision substituir "$cookies" --by "$jwt" --reason "The public API and the mobile client cannot use cookies."
echo "  $cookies  superseded by $jwt"
run decision new "Use Postgres as the primary store" --file "$content/decisions/postgres.md" --facet data --component postgres
echo "  $(id_of)  active decision"
run note new "Google OAuth callback must stay on the apex domain" --informs "$jwt" --file "$content/notes/oauth-apex-domain.md" --facet auth --component oauth
echo "  $(id_of)  note"
run runbook new "Rotate the JWT signing key" --file "$content/runbooks/rotate-jwt-signing-key.md"
runbook=$(id_of)
run runbook retag "$runbook" --facet auth --component tokens
echo "  $runbook  runbook"
run reference new "RFC 6749: The OAuth 2.0 Authorization Framework" --url https://datatracker.ietf.org/doc/html/rfc6749 \
  --informs "$google" --file "$content/references/rfc6749.md" --facet auth --component oauth
echo "  $(id_of)  external reference"

mv "$dest/.cernerc.default" .cernerc # back to the default: both gates human
run pull
rm -f "$dest/.out"

cat <<EOF

Done. Explore it:

  cd $dest/taskflow
  cerne status               # features by state
  cerne ui                   # dashboard and kanban at http://localhost:7717
  cat .docs/CONTEXT.md       # what an AI agent reads: only the current state
  cerne search facebook      # the rejected proposal is still one search away
  cerne savings              # how much history stays out of the agent's context
  cerne feature accept $digest   # the weekly digest waits for you: accepting is a human act

To start over, delete $dest.
EOF
