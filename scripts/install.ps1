# Installs the latest tunn release for Windows.
#
#   irm https://raw.githubusercontent.com/aonghus/tunn/main/scripts/install.ps1 | iex
#
# Installs to $env:TUNN_INSTALL_DIR, or %LOCALAPPDATA%\Programs\tunn by default,
# and adds that folder to the user PATH. No administrator rights needed.
# Works in Windows PowerShell 5.1 and PowerShell 7.

# Everything runs inside a script block so that, when piped to iex, no
# variables or preference settings leak into the caller's session.
& {
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell 5.1 redraws a progress bar per chunk, which can make a
    # few-megabyte download take minutes.
    $ProgressPreference = 'SilentlyContinue'
    # Windows PowerShell 5.1 may not offer TLS 1.2, which GitHub requires.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'aonghus/tunn'
    $base = "https://github.com/$repo/releases/latest/download"

    # Use the OS architecture, not the shell's: an x64 PowerShell running under
    # emulation on Windows on Arm reports AMD64 in PROCESSOR_ARCHITECTURE.
    $osArch = $null
    try {
        $osArch = [string][System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    } catch {
        $osArch = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment').PROCESSOR_ARCHITECTURE
    }
    $arch = switch ($osArch) {
        { $_ -in 'X64', 'AMD64' } { 'amd64'; break }
        { $_ -in 'Arm64', 'ARM64' } { 'arm64'; break }
        default { throw "tunn has no build for this processor architecture ($osArch)." }
    }
    $asset = "tunn-windows-$arch.exe"

    $installDir = if ($env:TUNN_INSTALL_DIR) { $env:TUNN_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\tunn' }
    $target = Join-Path $installDir 'tunn.exe'

    # A running tunn.exe cannot be overwritten.
    $running = Get-Process -Name tunn -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $target }
    if ($running) {
        throw "tunn is running (pid $($running.Id -join ', ')). Run 'tunn stop', then run the installer again."
    }

    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
    $sums = "$tmp.sums"
    try {
        Write-Host "Downloading $asset ..."
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $tmp
        Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile $sums

        $line = Get-Content $sums | Where-Object { ($_ -split '\s+')[1] -eq $asset }
        if (-not $line) { throw "SHA256SUMS has no entry for $asset." }
        $want = ($line -split '\s+')[0].ToLower()
        $got = (Get-FileHash -Algorithm SHA256 -Path $tmp).Hash.ToLower()
        if ($got -ne $want) { throw "Checksum mismatch for $asset (expected $want, got $got). Nothing was installed." }
        Write-Host 'Checksum verified.'

        Move-Item -Force -Path $tmp -Destination $target
    } finally {
        Remove-Item -Force -ErrorAction SilentlyContinue -Path $tmp, $sums
    }
    Write-Host "tunn installed to $target"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @(if ($userPath) { $userPath -split ';' | Where-Object { $_ } })
    if ($entries -notcontains $installDir) {
        [Environment]::SetEnvironmentVariable('Path', (@($entries) + $installDir) -join ';', 'User')
        Write-Host "Added $installDir to your user PATH."
    }
    if (($env:Path -split ';') -notcontains $installDir) {
        $env:Path = "$env:Path;$installDir"
    }

    try { Write-Host "version: $(& $target version)" } catch { Write-Host 'version: unknown' }

    if (-not (Get-Command ssh -ErrorAction SilentlyContinue)) {
        Write-Warning 'ssh was not found. Install it under Settings > System > Optional features > OpenSSH Client.'
    }

    Write-Host ''
    Write-Host 'Next steps:'
    Write-Host '  1. Open a new terminal so other windows see the updated PATH.'
    Write-Host '  2. Check that ssh to your server works, e.g.  ssh myserver'
    Write-Host '  3. Describe your tunnels in  notepad $HOME\.tunnrc'
    Write-Host '  4. Run  tunn --detach  then  tunn status'
    Write-Host "  Guide: https://github.com/$repo#windows"
}
