#!/bin/sh
# Pin docs.lock on top of the current tip of main and push it (B-0089).
#
# A merge can land on main while the job runs, so the push may be rejected
# ("fetch first"). docs.lock is a pure function of the backend snapshot, not of
# main's tree, so a retry does not rebase: it drops the local pin commit, resets
# to the new origin/main and pins again. That never conflicts, and when the new
# tip already carries the same pin there is nothing left to push.
set -eu

PIN_CMD="${PIN_CMD:-go run ./cmd/cerne lock pin}"
PIN_ATTEMPTS="${PIN_ATTEMPTS:-3}"
PIN_PAUSE="${PIN_PAUSE:-5}"

git config user.name "cerne-ci"
git config user.email "cerne-ci@users.noreply.github.com"

attempt=1
while [ "$attempt" -le "$PIN_ATTEMPTS" ]; do
  sh -c "$PIN_CMD"
  changed="$(git diff --name-only)"
  if [ -n "$changed" ] && [ "$changed" != "docs.lock" ]; then
    echo "refusing to pin: unexpected changed paths:"
    echo "$changed"
    exit 1
  fi
  if git diff --quiet -- docs.lock; then
    echo "docs.lock already points at the current backend snapshot"
    exit 0
  fi
  git add docs.lock
  git commit -m "chore(docs): pin docs.lock [skip ci]"
  log="$(mktemp)"
  if git push origin HEAD:main 2>"$log"; then
    cat "$log"
    exit 0
  fi
  cat "$log"
  # Only a non-fast-forward rejection means main moved; anything else
  # (ruleset, pre-receive, auth) will not heal by retrying.
  if ! grep -Eq 'fetch first|non-fast-forward' "$log"; then
    echo "push was refused (not because main advanced); see git's error above"
    exit 1
  fi
  if [ "$attempt" -lt "$PIN_ATTEMPTS" ]; then
    echo "push rejected (attempt $attempt of $PIN_ATTEMPTS): main advanced; fetching and re-pinning on the new tip"
    sleep "$PIN_PAUSE"
    git fetch origin main
    git reset --hard origin/main
  fi
  attempt=$((attempt + 1))
done
echo "giving up: main kept advancing after $PIN_ATTEMPTS attempts; the docs.lock pin was not pushed"
exit 1
