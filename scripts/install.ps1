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

    # Find the machine's real architecture, not the shell's: a 32-bit or
    # emulated x64 PowerShell on Windows on Arm reports x86 or AMD64 for itself.
    # The system environment in the registry always holds the native value;
    # the other sources are fallbacks, tried in order.
    $sources = [ordered]@{
        'registry'               = { (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment' -ErrorAction Stop).PROCESSOR_ARCHITECTURE }
        'PROCESSOR_ARCHITEW6432' = { $env:PROCESSOR_ARCHITEW6432 }
        'PROCESSOR_ARCHITECTURE' = { $env:PROCESSOR_ARCHITECTURE }
        'OSArchitecture'         = { [string][System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture }
    }
    $seen = @()
    $arch = $null
    foreach ($name in $sources.Keys) {
        $value = try { & $sources[$name] } catch { $null }
        if (-not $value) { continue }
        $seen += "$name=$value"
        $arch = switch -Regex ("$value") {
            '^(AMD64|X64)$' { 'amd64'; break }
            '^(ARM64|Arm64)$' { 'arm64'; break }
            default { $null }
        }
        if ($arch) { break }
    }
    if (-not $arch) {
        throw "tunn has no build for this processor architecture ($($seen -join ', ')). Builds exist for 64-bit Intel/AMD (amd64) and Arm (arm64)."
    }
    $asset = "tunn-windows-$arch.exe"

    $installDir = if ($env:TUNN_INSTALL_DIR) { $env:TUNN_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\tunn' }
    $target = Join-Path $installDir 'tunn.exe'

    # A running tunn.exe cannot be overwritten. Test by opening it for writing:
    # a 32-bit PowerShell cannot see the paths of 64-bit processes.
    if (Test-Path $target) {
        try {
            [System.IO.File]::Open($target, 'Open', 'ReadWrite', 'None').Close()
        } catch {
            throw "tunn is running, so $target cannot be replaced. Run 'tunn stop', then run the installer again."
        }
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

    # Starter config, only if the user has none; an existing one is never touched.
    $config = Join-Path $HOME '.tunnrc'
    $createdConfig = $false
    if (-not (Test-Path $config)) {
        $template = @(
            '# tunn configuration: https://github.com/aonghus/tunn#configuration'
            '#'
            '# Each tunnel connects to an ssh host (a Host from ~/.ssh/config, or'
            '# user@server) and forwards local ports to ports on the remote side.'
            'tunnels:'
            '  example:'
            '    host: myserver              # change to your server'
            '    ports:'
            '      - 8080:80                 # localhost:8080 -> port 80 on the server'
            '      # - 5432:db.internal:5432  # localhost:5432 -> db.internal:5432, reached via the server'
        )
        # ASCII: Windows PowerShell 5.1's UTF8 encoding would add a byte-order mark.
        Set-Content -Path $config -Value $template -Encoding ascii
        $createdConfig = $true
    }

    Write-Host ''
    Write-Host 'Set up an ssh key (skip if ssh to your server already works without a password):'
    Write-Host '  ssh-keygen -t rsa -b 4096'
    Write-Host '  Get-Content $HOME\.ssh\id_rsa.pub | ssh user@server.example.com "mkdir -p ~/.ssh && chmod 700 ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"'
    Write-Host ''
    if ($createdConfig) {
        Write-Host "Created a starter config at $config. Edit it to describe your tunnels."
    } else {
        Write-Host "Your existing $config was left unchanged."
    }
    Write-Host ''
    Write-Host 'Then, in a new terminal:'
    Write-Host '  tunn -d        start your tunnels in the background (same as tunn --detach)'
    Write-Host '  tunn status    show their state'
    Write-Host '  tunn stop      stop them'
    Write-Host ''
    Write-Host "Guide: https://github.com/$repo#windows"

    # Open the new config for editing. Skipped in CI or when TUNN_NO_EDITOR is set.
    if ($createdConfig -and -not $env:CI -and -not $env:TUNN_NO_EDITOR) {
        Start-Process notepad.exe -ArgumentList "`"$config`""
    }
}
