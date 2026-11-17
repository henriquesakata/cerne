#!/bin/sh
# Publish a GitHub Release: sh scripts/release-publish.sh <tag> <notes-file> <asset>...
# Idempotent (B-0010): if the release already exists (a re-run after a later
# step failed), update its notes and re-upload the assets; otherwise create it.
# A tag with a hyphen (v1.0.0-rc.1) is a pre-release.
# The rest of the pipeline re-runs harmlessly: the attestation step just signs
# again, and the tap step was already idempotent ("A re-run on the same tag has
# nothing to commit").
set -eu
tag=$1 notes=$2
shift 2
pre=
case "$tag" in *-*) pre=--prerelease ;; esac
if gh release view "$tag" >/dev/null 2>&1; then
  gh release edit "$tag" --notes-file "$notes" $pre
  gh release upload "$tag" "$@" --clobber
else
  gh release create "$tag" "$@" --title "cerne $tag" --notes-file "$notes" $pre
fi
