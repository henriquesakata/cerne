#!/bin/sh
# Publish a GitHub Release: sh scripts/release-publish.sh <tag> <notes-file> <asset>...
# Idempotent (B-0010): if the release already exists (a re-run after a later
# step failed), update its notes and re-upload the assets; otherwise create it.
# Only an HTTP 404 from the releases/tags lookup means "does not exist"; any
# other failure (auth, network, rate limit) aborts with gh's own message.
# A tag with a hyphen (v1.0.0-rc.1) is a pre-release. The flag is passed
# explicitly on both paths, so a re-run corrects a release left with the wrong one.
# Limit: `upload --clobber` overwrites same-name assets; an extra asset from an
# earlier attempt under a different name stays on the release.
# The rest of the pipeline re-runs harmlessly: the attestation step just signs
# again, and the tap step was already idempotent ("A re-run on the same tag has
# nothing to commit").
set -u
tag=$1 notes=$2
shift 2
case "$tag" in
  *-*) flags=--prerelease=true ;;
  *) flags="--prerelease=false --latest" ;;
esac
err=$(gh api "repos/${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}/releases/tags/$tag" 2>&1 >/dev/null)
rc=$?
if [ $rc -eq 0 ]; then
  # shellcheck disable=SC2086
  gh release edit "$tag" --notes-file "$notes" $flags &&
    gh release upload "$tag" "$@" --clobber
elif printf '%s' "$err" | grep -q 'HTTP 404'; then
  # shellcheck disable=SC2086
  gh release create "$tag" "$@" --title "cerne $tag" --notes-file "$notes" $flags
else
  echo "release lookup for $tag failed: $err" >&2
  exit 1
fi
