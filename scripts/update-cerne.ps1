#requires -Version 5.1
<#
.SYNOPSIS
  Rebuild and reinstall cerne from the latest source.

.DESCRIPTION
  Fetches the repository (without touching your working tree), builds the
  target ref in a throwaway worktree, and installs it via `go install` to the
  Go bin dir on your PATH. Stamps the real version into the binary via
  -ldflags. Idempotent: does nothing if already up to date.

.PARAMETER Repo
  Path to the canonical cerne clone. Do NOT point this at a worktree.

.PARAMETER Ref
  'main'   -> latest commit (recommended for dogfooding).
  'latest' -> latest release tag (v0.2.0, v0.3.0, ...).

.EXAMPLE
  .\update-cerne.ps1

.EXAMPLE
  .\update-cerne.ps1 -Ref latest
#>
[CmdletBinding()]
param(
  [string]$Repo = 'C:\development\workspaces\rockt\cerne',
  [ValidateSet('main', 'latest')]
  [string]$Ref = 'main'
)

$ErrorActionPreference = 'Stop'

$go = 'go'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { $go = 'C:\development\tools\go\bin\go.exe' }

# 1) Update refs without touching the working tree (fetch only, never pull/checkout).
git -C $Repo fetch --tags --prune --quiet
if ($LASTEXITCODE -ne 0) { throw "git fetch failed (exit $LASTEXITCODE)." }

# 2) Resolve the target ref and the version string to stamp into the binary.
if ($Ref -eq 'latest') {
  $target = (git -C $Repo describe --tags --abbrev=0 origin/main).Trim()
}
else {
  $target = 'origin/main'
}
$ver = (git -C $Repo describe --tags --always $target).Trim()

# 3) Already installed at this version? Nothing to do.
$installed = ''
if (Get-Command cerne -ErrorAction SilentlyContinue) {
  $installed = ((cerne --version) -split '\s+')[-1]
}
if ($installed -eq $ver) {
  Write-Host "cerne already at $ver - nothing to do."
  return
}

# 4) Build exactly the target ref in a throwaway worktree, leaving your branch
#    and any uncommitted changes untouched.
$tmp = Join-Path $env:TEMP ('cerne-build-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
git -C $Repo worktree add --quiet --force --detach $tmp $target
if ($LASTEXITCODE -ne 0) { throw "git worktree add failed (exit $LASTEXITCODE)." }
try {
  Push-Location $tmp
  & $go install -ldflags "-X github.com/henriquesakata/cerne/internal/version.Version=$ver" ./cmd/cerne
  if ($LASTEXITCODE -ne 0) { throw "go install failed (exit $LASTEXITCODE)." }
}
finally {
  Pop-Location
  git -C $Repo worktree remove --force $tmp
  git -C $Repo worktree prune
}

Write-Host "cerne updated: $installed -> $ver  ($((Get-Command cerne).Source))"
