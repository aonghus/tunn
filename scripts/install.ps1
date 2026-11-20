# Installs the latest tunn release for Windows.
#
#   irm https://raw.githubusercontent.com/aonghus/tunn/main/scripts/install.ps1 | iex
#
# Installs to $env:TUNN_INSTALL_DIR, or %LOCALAPPDATA%\Programs\tunn by default,
# and adds that folder to the user PATH. No administrator rights needed.
$ErrorActionPreference = 'Stop'

$repo = 'aonghus/tunn'

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}
$asset = "tunn-windows-$arch.exe"

$installDir = if ($env:TUNN_INSTALL_DIR) { $env:TUNN_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\tunn' }
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$target = Join-Path $installDir 'tunn.exe'

Write-Host "Downloading $asset ..."
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
try {
    Invoke-WebRequest -UseBasicParsing -Uri "https://github.com/$repo/releases/latest/download/$asset" -OutFile $tmp
    Move-Item -Force -Path $tmp -Destination $target
} finally {
    if (Test-Path $tmp) { Remove-Item -Force $tmp }
}
Write-Host "tunn installed to $installDir"

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries = if ($userPath) { $userPath -split ';' } else { @() }
if ($entries -notcontains $installDir) {
    [Environment]::SetEnvironmentVariable('Path', (($entries + $installDir) -join ';').Trim(';'), 'User')
    $env:Path = "$env:Path;$installDir"
    Write-Host "Added $installDir to your user PATH (open a new terminal for other windows to see it)"
}

if (-not (Get-Command ssh -ErrorAction SilentlyContinue)) {
    Write-Warning 'ssh was not found. Install the OpenSSH client: Settings > System > Optional features > OpenSSH Client'
}

try { Write-Host "version: $(& $target version)" } catch { Write-Host 'version: unknown' }
