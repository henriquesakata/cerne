# cerne installer for Windows.
#
#   irm https://raw.githubusercontent.com/henriquesakata/cerne/main/install.ps1 | iex
#
# Downloads the release binary, verifies it against the release's SHA256SUMS
# and installs it as cerne.exe. Adds the install directory to your user PATH,
# and to the current session, when it is not there yet. Never asks for admin
# rights. Run it again to update.
#
# Environment (set before running, e.g. $env:CERNE_VERSION = 'v0.10.0'):
#   CERNE_VERSION      release tag to install (default: the latest release)
#   CERNE_INSTALL_DIR  where to put cerne.exe (default: %LOCALAPPDATA%\Programs\cerne)
#   CERNE_BASE_URL     repository URL to download from (mirrors, tests)
#
# Errors are thrown, never `exit`: under `irm | iex` the script runs inside your
# session, and an exit would close the terminal. Everything runs inside
# Install-Cerne, called on the last line, so a download cut short runs nothing.

function Install-Cerne {
  $ErrorActionPreference = 'Stop'
  # The progress bar makes Invoke-WebRequest many times slower on PowerShell 5.1.
  $ProgressPreference = 'SilentlyContinue'
  # PowerShell 5.1 on an older .NET may not offer TLS 1.2, which GitHub requires.
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

  $base = 'https://github.com/henriquesakata/cerne'
  if ($env:CERNE_BASE_URL) { $base = $env:CERNE_BASE_URL.TrimEnd('/') }
  # The default origin is https and must stay https across redirects; a
  # CERNE_BASE_URL set to http:// is the user's explicit choice.
  $secure = $base.StartsWith('https://')

  if ($PSVersionTable.PSVersion.Major -ge 6 -and -not $IsWindows) {
    throw 'cerne install: install.ps1 is for Windows; on macOS and Linux use install.sh (see INSTALL.md)'
  }
  # A 32-bit PowerShell on 64-bit Windows reports x86 here and the real
  # architecture in PROCESSOR_ARCHITEW6432.
  $arch = $env:PROCESSOR_ARCHITECTURE
  if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
  $note = $null
  switch ($arch) {
    'AMD64' { }
    'ARM64' { $note = 'Windows on ARM64: installed the amd64 build, which Windows 11 runs under emulation (Windows 10 on ARM cannot run it).' }
    default { throw "cerne install: unsupported architecture: $arch (releases cover 64-bit Windows: amd64, and ARM64 through emulation)" }
  }

  # Fetch: a HEAD request, or a download into OutFile; returns the final URI
  # after redirects (BaseResponse differs between PowerShell 5.1 and 7).
  function Invoke-Fetch([string]$Uri, [string]$OutFile) {
    $params = @{ Uri = $Uri; UseBasicParsing = $true }
    if ($OutFile) { $params.OutFile = $OutFile; $params.PassThru = $true } else { $params.Method = 'Head' }
    $r = Invoke-WebRequest @params
    $final = $r.BaseResponse.ResponseUri
    if (-not $final) { $final = $r.BaseResponse.RequestMessage.RequestUri }
    # Checked after the fact: a downgraded redirect has already been fetched,
    # but its body is thrown away before anything reads it.
    if ($secure -and $final.Scheme -ne 'https') {
      throw "$Uri redirected to a non-https address ($final)"
    }
    $final
  }

  $version = $env:CERNE_VERSION
  if (-not $version) {
    # The /releases/latest redirect ends at /releases/tag/<tag>: no API call,
    # so no anonymous rate limit.
    try { $final = Invoke-Fetch "$base/releases/latest" }
    catch { throw "cerne install: could not resolve the latest release from $base/releases/latest ($($_.Exception.Message)); set `$env:CERNE_VERSION, e.g. 'v0.10.0'" }
    $version = $final.AbsolutePath.TrimEnd('/').Split('/')[-1]
  }
  if ($version -notmatch '^v[0-9][A-Za-z0-9._-]*$') {
    throw "cerne install: invalid release version '$version'; set `$env:CERNE_VERSION, e.g. 'v0.10.0'"
  }

  $dir = Join-Path $env:LOCALAPPDATA 'Programs\cerne'
  if ($env:CERNE_INSTALL_DIR) {
    # A relative dir would land on the user PATH as is, and resolve against
    # whatever folder each later terminal starts in.
    $dir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($env:CERNE_INSTALL_DIR)
  }
  $target = Join-Path $dir 'cerne.exe'
  if (Test-Path -LiteralPath $target -PathType Container) {
    throw "cerne install: $target is a directory; set `$env:CERNE_INSTALL_DIR to another directory"
  }

  $asset = "cerne_${version}_windows_amd64.exe"
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ('cerne-install-' + [Guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    Write-Host "Downloading cerne $version (windows/amd64)..."
    $download = Join-Path $tmp $asset
    $sums = Join-Path $tmp 'SHA256SUMS'
    try { Invoke-Fetch "$base/releases/download/$version/$asset" $download | Out-Null }
    catch { throw "cerne install: could not download $asset from release $version ($($_.Exception.Message))" }
    try { Invoke-Fetch "$base/releases/download/$version/SHA256SUMS" $sums | Out-Null }
    catch { throw "cerne install: could not download SHA256SUMS from release $version ($($_.Exception.Message))" }

    $want = $null
    foreach ($line in Get-Content -LiteralPath $sums) {
      $fields = -split $line
      if ($fields.Count -ge 2 -and $fields[1].TrimStart('*') -eq $asset) { $want = $fields[0].ToLowerInvariant() }
    }
    if (-not $want) { throw "cerne install: SHA256SUMS of release $version has no entry for $asset" }
    $got = (Get-FileHash -LiteralPath $download -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($got -ne $want) {
      throw "cerne install: checksum mismatch for $asset (expected $want, got $got); nothing was installed"
    }

    try { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    catch { throw "cerne install: could not create $dir; set `$env:CERNE_INSTALL_DIR to a directory you can write to" }
    # Stage next to the target, then swap. A running cerne.exe (a `cerne ui`)
    # cannot be overwritten, but it can be renamed out of the way, under a
    # name of its own: an older one may still be running too. Copies left by
    # earlier installs go now, unless still running.
    Get-ChildItem -LiteralPath $dir -Filter 'cerne.exe.*.old' -ErrorAction SilentlyContinue |
      Remove-Item -Force -ErrorAction SilentlyContinue
    $staged = Join-Path $dir ".cerne.install.$PID.exe"
    $old = "$target.$PID.old"
    try {
      Copy-Item -LiteralPath $download -Destination $staged -Force
      if (Test-Path -LiteralPath $target) { Move-Item -LiteralPath $target -Destination $old -Force }
      Move-Item -LiteralPath $staged -Destination $target -Force
    } catch {
      if (-not (Test-Path -LiteralPath $target) -and (Test-Path -LiteralPath $old)) {
        Move-Item -LiteralPath $old -Destination $target -Force -ErrorAction SilentlyContinue
      }
      throw "cerne install: could not write $target ($($_.Exception.Message)); set `$env:CERNE_INSTALL_DIR to a directory you can write to"
    } finally {
      Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
    }
    # Still running: it goes on the next install.
    Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
    Unblock-File -LiteralPath $target
  } finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }
  Write-Host "Installed cerne $version to $target"
  if ($note) { Write-Host $note }

  # The user PATH lives in the registry. Read it unexpanded and write it back
  # as REG_EXPAND_SZ, so entries like %USERPROFILE%\bin survive.
  $same = { param($a, $b) [Environment]::ExpandEnvironmentVariables($a).TrimEnd('\') -ieq [Environment]::ExpandEnvironmentVariables($b).TrimEnd('\') }
  $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
  try {
    $userPath = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    if (-not @($userPath -split ';' | Where-Object { $_ -and (& $same $_ $dir) }).Count) {
      $newPath = $dir
      if ($userPath) { $newPath = $userPath.TrimEnd(';') + ';' + $dir }
      $key.SetValue('Path', $newPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)
      # Setting a variable through .NET broadcasts WM_SETTINGCHANGE, so
      # terminals opened from now on see the new PATH.
      [Environment]::SetEnvironmentVariable('CERNE_INSTALL_REFRESH', '1', 'User')
      [Environment]::SetEnvironmentVariable('CERNE_INSTALL_REFRESH', $null, 'User')
      Write-Host "Added $dir to your user PATH."
    }
  } finally {
    $key.Close()
  }
  if (-not @($env:Path -split ';' | Where-Object { $_ -and (& $same $_ $dir) }).Count) {
    $env:Path = $env:Path.TrimEnd(';') + ';' + $dir
  }
  $found = Get-Command cerne -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($found -and $found.Source -ne $target) {
    Write-Host ''
    Write-Host "Note: 'cerne' still resolves to $($found.Source), which comes before $dir on your PATH."
    Write-Host "Remove that copy, or put $dir earlier on your PATH, to run the one just installed."
  }
  Write-Host ''
  Write-Host "Next: run 'cerne init --name <project>' inside your project. Guide: https://github.com/henriquesakata/cerne#try-it-in-two-minutes"
}

Install-Cerne
