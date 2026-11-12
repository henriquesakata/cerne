#!/bin/sh
# cerne installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/henriquesakata/cerne/main/install.sh | sh
#
# Downloads the release binary for this machine, verifies it against the
# release's SHA256SUMS and installs it as `cerne`. Never calls sudo and never
# edits your shell profile.
#
# Environment:
#   CERNE_VERSION      release tag to install (default: the latest release)
#   CERNE_INSTALL_DIR  where to put the binary (default: /usr/local/bin if
#                      writable, otherwise ~/.local/bin)
#   CERNE_BASE_URL     repository URL to download from (mirrors, tests)
set -eu

fail() {
  echo "cerne install: $*" >&2
  exit 1
}

# Everything runs inside main, called on the last line: a download cut short
# in `curl | sh` then runs nothing instead of half a script.
main() {
  base="${CERNE_BASE_URL:-https://github.com/henriquesakata/cerne}"
  version="${CERNE_VERSION:-}"

  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) fail "unsupported operating system: $(uname -s) (releases cover macOS and Linux; for Windows see INSTALL.md)" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) fail "unsupported architecture: $(uname -m) (releases cover amd64 and arm64)" ;;
  esac

  command -v curl >/dev/null 2>&1 || fail "curl is required"

  if [ -z "$version" ]; then
    # The /releases/latest redirect ends at /releases/tag/<tag>: no API call,
    # so no anonymous rate limit.
    latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$base/releases/latest") ||
      fail "could not reach $base/releases/latest"
    version="${latest##*/}"
  fi
  case "$version" in
    v[0-9]*) ;;
    *) fail "could not resolve a release version (got '$version'); set CERNE_VERSION, e.g. CERNE_VERSION=v0.5.0" ;;
  esac

  if [ -n "${CERNE_INSTALL_DIR:-}" ]; then
    dir="$CERNE_INSTALL_DIR"
  elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    dir=/usr/local/bin
  else
    dir="$HOME/.local/bin"
  fi

  asset="cerne_${version}_${os}_${arch}"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT

  echo "Downloading cerne $version ($os/$arch)..."
  curl -fsSL -o "$tmp/$asset" "$base/releases/download/$version/$asset" ||
    fail "could not download $asset from release $version"
  curl -fsSL -o "$tmp/SHA256SUMS" "$base/releases/download/$version/SHA256SUMS" ||
    fail "could not download SHA256SUMS from release $version"

  want=$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/SHA256SUMS")
  [ -n "$want" ] || fail "SHA256SUMS of release $version has no entry for $asset"
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$tmp/$asset" | awk '{ print $1 }')
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')
  else
    fail "sha256sum or shasum is required to verify the download"
  fi
  [ "$got" = "$want" ] ||
    fail "checksum mismatch for $asset (expected $want, got $got); nothing was installed"

  mkdir -p "$dir" || fail "could not create $dir; set CERNE_INSTALL_DIR to a directory you can write to"
  chmod +x "$tmp/$asset"
  mv "$tmp/$asset" "$dir/cerne" ||
    fail "could not write to $dir; set CERNE_INSTALL_DIR to a directory you can write to"

  echo "Installed cerne $version to $dir/cerne"
  case ":$PATH:" in
    *":$dir:"*) ;;
    *)
      echo
      echo "$dir is not on your PATH. Add this line to your shell profile:"
      echo "  export PATH=\"$dir:\$PATH\""
      ;;
  esac
  echo
  echo "Next: run 'cerne init' inside your project. Guide: https://github.com/henriquesakata/cerne#try-it-in-two-minutes"
}

main "$@"
