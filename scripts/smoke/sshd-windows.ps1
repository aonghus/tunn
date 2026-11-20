# Installs and starts the Windows OpenSSH server on port 22 and authorises
# ~/.ssh/tunn_smoke.pub. Needs an elevated shell (GitHub runners are).
# Run setup-client.sh first.
$ErrorActionPreference = 'Stop'

if (-not (Get-Service sshd -ErrorAction SilentlyContinue)) {
    Write-Host 'Installing OpenSSH.Server capability...'
    Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0 | Out-Null
}

# The first start creates C:\ProgramData\ssh, its sshd_config and host keys.
Start-Service sshd

$pub = Get-Content "$env:USERPROFILE\.ssh\tunn_smoke.pub"

# The default sshd_config reads keys for members of Administrators from this file,
# and ignores it unless only Administrators and SYSTEM can access it.
$adminKeys = "$env:ProgramData\ssh\administrators_authorized_keys"
Set-Content -Path $adminKeys -Value $pub -Encoding ascii
icacls.exe $adminKeys /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' | Out-Null

# Covers the case where the runner user is not an administrator.
Set-Content -Path "$env:USERPROFILE\.ssh\authorized_keys" -Value $pub -Encoding ascii

Restart-Service sshd
Get-Service sshd | Format-Table -AutoSize
