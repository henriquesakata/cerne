#!/bin/sh
# Print the GitHub Release body for a tag (B-0011): the CHANGELOG section,
# then the fixed Update, Install and Verify blocks.
#
#   scripts/release-body.sh <tag> <section-file>
set -eu
tag=${1:?usage: release-body.sh <tag> <section-file>}
section=${2:?usage: release-body.sh <tag> <section-file>}
repo=${GITHUB_REPOSITORY:-henriquesakata/cerne}

cat "$section"
printf '\n---\n\n'
printf '## Update\n\n'
case $tag in
  *-*) printf 'This is a pre-release: Homebrew and the install script track stable releases only. To get exactly this version, use `go install` below or the files attached to this release.\n\nAlready installed? Update the way you installed it:\n' ;;
  *) printf 'Already installed? Update the way you installed it:\n' ;;
esac
cat <<EOT

\`\`\`sh
# Homebrew
brew upgrade henriquesakata/tap/cerne

# macOS and Linux: run the install script again
curl -fsSL https://raw.githubusercontent.com/henriquesakata/cerne/main/install.sh | sh

# Go
go install github.com/henriquesakata/cerne/cmd/cerne@$tag
\`\`\`

On Windows, run the install script again in PowerShell:

\`\`\`powershell
irm https://raw.githubusercontent.com/henriquesakata/cerne/main/install.ps1 | iex
\`\`\`

Then run \`cerne --version\` (it prints the version, without the leading v) and follow this version's Upgrading section above. Details: [INSTALL.md](https://github.com/$repo/blob/$tag/INSTALL.md#updating).

## Install

Cross-compiled binaries (linux and darwin amd64/arm64, windows amd64) and \`SHA256SUMS\`. See [INSTALL.md](https://github.com/$repo/blob/$tag/INSTALL.md).

## Verify

Every file carries a signed provenance attestation:

\`\`\`sh
gh attestation verify <file> --repo $repo
\`\`\`
EOT
