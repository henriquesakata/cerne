#!/usr/bin/env bash
# Cross-compile the cerne release matrix with the version stamped via ldflags.
#
# Usage: scripts/build-release.sh [version]
#   version defaults to `git describe --tags --always`.
#
# The whole stack is pure Go (modernc.org/sqlite, go-git, go:embed UI), so
# CGO_ENABLED=0 cross-compiles every target without a C toolchain. Output and
# SHA256SUMS land in dist/ (gitignored).
set -euo pipefail

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X gitlab.com/rockt/cerne/internal/cli.version=${VERSION}"
OUT="dist"

# Matrix: os/arch pairs.
TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
)

rm -rf "${OUT}"
mkdir -p "${OUT}"

echo "Building cerne ${VERSION}"
for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  name="cerne_${VERSION}_${os}_${arch}"
  [ "${os}" = "windows" ] && name="${name}.exe"
  echo "  ${target} -> ${OUT}/${name}"
  CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" \
    go build -trimpath -ldflags "${LDFLAGS}" -o "${OUT}/${name}" ./cmd/cerne
done

# Checksums (portable: sha256sum or shasum).
( cd "${OUT}"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum cerne_* > SHA256SUMS
  else
    shasum -a 256 cerne_* > SHA256SUMS
  fi
)

echo "Done. Artifacts in ${OUT}/:"
ls -1 "${OUT}"
