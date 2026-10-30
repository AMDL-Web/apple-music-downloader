<#
Installs amdl on Windows from a GitHub release of AMDL-Web/apple-music-downloader,
verified against the release's SHA256SUMS. Running it again upgrades in place.
docs/install.md has the details.

  irm https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.ps1 | iex
  & ([scriptblock]::Create((irm …/install.ps1))) -Service -Wrapper http://nas:30020
#>
[CmdletBinding()]
param(
    # The release to install, such as v2.0.0; the latest by default.
    [string]$Version = $env:AMDL_VERSION,
    # Where amdl.exe goes; it is added to the user's PATH.
    [string]$InstallDir = $env:AMDL_INSTALL_DIR,
    # Run amdl as a background service afterwards (amdl service install).
    [switch]$Service,
    # The wrapper address the service uses on its first start.
    [string]$Wrapper,
    # Reinstall even when this version is already installed.
    [switch]$Force,
    # Print what would happen and change nothing.
    [switch]$DryRun,
    # A release directory or URL to install from instead of GitHub; tests
    # point it at a local directory.
    [string]$BaseUrl = $env:AMDL_INSTALL_BASE_URL
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest's progress bar slows downloads a lot in Windows PowerShell
$repo = 'AMDL-Web/apple-music-downloader'

function Say([string]$msg) { [Console]::Error.WriteLine("amdl-install: $msg") }

function Get-File([string]$from, [string]$to) {
    if ($from -match '^[a-z]+://') {
        Invoke-WebRequest -UseBasicParsing -Uri $from -OutFile $to
    } else {
        Copy-Item -LiteralPath $from -Destination $to
    }
}

function Get-FFmpegHint {
    if (Get-Command winget -ErrorAction SilentlyContinue) { return 'winget install --id Gyan.FFmpeg -e' }
    if (Get-Command scoop -ErrorAction SilentlyContinue) { return 'scoop install ffmpeg' }
    if (Get-Command choco -ErrorAction SilentlyContinue) { return 'choco install ffmpeg' }
    return 'download ffmpeg from https://ffmpeg.org/download.html and put it on PATH'
}

# Windows PowerShell 5.1 does not offer TLS 1.2 by default, which GitHub requires.
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\amdl' }
if (-not $Version) {
    if ($BaseUrl) { throw '-Version is needed with -BaseUrl' }
    $Version = (Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$repo/releases/latest").tag_name
    if (-not $Version) { throw 'the latest release has no tag; pass -Version' }
}
# Releases build amd64 only; Windows on ARM runs it under emulation.
$name = "amdl_${Version}_windows_amd64"
if ($BaseUrl) { $base = $BaseUrl.TrimEnd('/', '\') } else { $base = "https://github.com/$repo/releases/download/$Version" }
$sep = '/'
if ($base -notmatch '^[a-z]+://') { $sep = '\' }
$exe = Join-Path $InstallDir 'amdl.exe'

if ($DryRun) {
    Say "would install amdl $Version (windows/amd64) from $base$sep$name.zip"
    Say "would verify it against $base${sep}SHA256SUMS"
    Say "would write $exe and add $InstallDir to the user PATH"
    if ($Service) { Say "would run: amdl service install$(if ($Wrapper) { " --wrapper $Wrapper" })" }
    return
}

function Test-ServiceActive {
    if (-not (Test-Path -LiteralPath $exe)) { return $false }
    return ((& $exe service status 2>$null | Out-String) -match '"active":true')
}

# The refused AMDL_URL keeps amdl version from looking for a server.
$current = $null
if ((-not $Force) -and (Test-Path -LiteralPath $exe)) {
    $env:AMDL_URL, $saved = 'http://127.0.0.1:1', $env:AMDL_URL
    try { $current = (& $exe version --fields client 2>$null | ConvertFrom-Json).client.version } catch { $current = $null }
    $env:AMDL_URL = $saved
}
if ($current -eq $Version) {
    Say "amdl $Version is already installed in $InstallDir"
} else {
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("amdl-install-" + [Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Say "downloading amdl $Version for windows/amd64"
        Get-File "$base$sep$name.zip" (Join-Path $tmp "$name.zip")
        Get-File "$base${sep}SHA256SUMS" (Join-Path $tmp 'SHA256SUMS')
        $want = $null
        foreach ($line in Get-Content (Join-Path $tmp 'SHA256SUMS')) {
            $f = $line -split '\s+', 2
            if ($f.Count -eq 2 -and $f[1].TrimStart('*') -eq "$name.zip") { $want = $f[0] }
        }
        if (-not $want) { throw "SHA256SUMS lists no $name.zip" }
        $got = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $tmp "$name.zip")).Hash
        if ($got -ne $want) { throw "checksum mismatch for ${name}.zip: got $got, want $want" }
        Expand-Archive -LiteralPath (Join-Path $tmp "$name.zip") -DestinationPath $tmp -Force
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        # A service the user keeps running is stopped for the swap and started
        # on the new binary; one they stopped stays stopped.
        $active = Test-ServiceActive
        if ($active) {
            Say 'stopping the amdl service for the upgrade'
            & $exe service stop | Out-Null
        }
        # The old exe is renamed aside, which works even while a process
        # still runs it, and comes back if anything after fails.
        $old = $null
        if (Test-Path -LiteralPath $exe) {
            $old = "$exe.old-" + [DateTime]::UtcNow.Ticks
            Move-Item -LiteralPath $exe -Destination $old
        }
        try {
            Copy-Item -LiteralPath (Join-Path $tmp "$name\amdl.exe") -Destination $exe
            if ($active) {
                & $exe service start | Out-Null
                if ($LASTEXITCODE -ne 0) { throw "amdl $Version did not start the service" }
            }
        } catch {
            if ($old) {
                Remove-Item -LiteralPath $exe -Force -ErrorAction SilentlyContinue
                Move-Item -LiteralPath $old -Destination $exe -Force
                if ($active) { & $exe service start | Out-Null }
            }
            throw "$($_.Exception.Message); the previous amdl.exe is back. Its log: amdl service status"
        }
        Say "installed $exe ($Version)"
        # Leftovers of earlier upgrades whose process has since exited.
        Get-ChildItem -LiteralPath $InstallDir -Filter 'amdl.exe.old-*' -ErrorAction SilentlyContinue |
            Remove-Item -Force -ErrorAction SilentlyContinue
    } finally {
        Remove-Item -Recurse -Force -LiteralPath $tmp -ErrorAction SilentlyContinue
    }
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not $userPath) { $userPath = '' }
if (($userPath -split ';') -notcontains $InstallDir) {
    [Environment]::SetEnvironmentVariable('Path', (($userPath.TrimEnd(';'), $InstallDir) -join ';').TrimStart(';'), 'User')
    $env:Path = "$env:Path;$InstallDir"
    Say "added $InstallDir to the user PATH; new terminals pick it up"
}
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Say "ffmpeg is missing; amdl needs it to write tracks. Install it with: $(Get-FFmpegHint)"
}

if ($Service) {
    if ($Wrapper) { & $exe service install --wrapper $Wrapper } else { & $exe service install }
    # throw, not exit: under irm | iex, exit would close the user's PowerShell.
    if ($LASTEXITCODE -ne 0) { throw "amdl service install failed (exit $LASTEXITCODE)" }
}
