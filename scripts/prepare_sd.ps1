<#
.SYNOPSIS
    Prepares a flashed Raspberry Pi MicroSD card for offline FamilyBoard deployment.
    Since Hardonian/FamilyBoard is a PRIVATE repository, copying the release package
    directly from your PC to the MicroSD card allows 100% offline, zero-token,
    zero-credential installation on the Raspberry Pi.

.PARAMETER Drive
    The drive letter of the mounted MicroSD bootfs partition (e.g. E: or E).

.PARAMETER NoAutoRun
    If specified, skips modifying cmdline.txt for automatic execution on first boot.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$Drive,

    [switch]$NoAutoRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Normalize Drive letter
$Drive = $Drive.TrimEnd('\', '/').TrimEnd(':') + ":"
$DriveRoot = "$Drive\"

Write-Host "==============================================================" -ForegroundColor Cyan
Write-Host "      FamilyBoard MicroSD Turnkey Preparation Engine          " -ForegroundColor Cyan
Write-Host "      (Private Repository: Zero-Token Offline Deployment)     " -ForegroundColor Cyan
Write-Host "==============================================================" -ForegroundColor Cyan

if (-not (Test-Path $DriveRoot)) {
    Write-Error "Target drive $DriveRoot does not exist. Please insert your MicroSD card and check the drive letter."
    exit 1
}

# Locate project root (where this script lives in scripts/)
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir

$SetupScript = Join-Path $ProjectRoot "familyboard-setup.sh"
if (-not (Test-Path $SetupScript)) {
    $SetupScript = Join-Path $ProjectRoot "deploy\pi\familyboard-setup.sh"
}

if (-not (Test-Path $SetupScript)) {
    Write-Error "Could not locate familyboard-setup.sh in $ProjectRoot."
    exit 1
}

# Locate release tarball
$DistDir = Join-Path $ProjectRoot "dist"
$Tarballs = Get-ChildItem -Path $DistDir -Filter "familyboard-*-linux-arm64.tar.gz" -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending

if (-not $Tarballs -or $Tarballs.Count -eq 0) {
    Write-Warning "No ARM64 release tarball found in dist/. Attempting to package release..."
    Push-Location $ProjectRoot
    try {
        & mingw32-make package-arm64
        $Tarballs = Get-ChildItem -Path $DistDir -Filter "familyboard-*-linux-arm64.tar.gz" -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending
    } finally {
        Pop-Location
    }
}

if (-not $Tarballs -or $Tarballs.Count -eq 0) {
    Write-Error "Could not find or build familyboard release tarball in $DistDir. Run 'make package-arm64' first."
    exit 1
}

$ReleaseTarball = $Tarballs[0].FullName
$TarballName = $Tarballs[0].Name

Write-Host "`nTarget MicroSD Card: $DriveRoot" -ForegroundColor Yellow
Write-Host "Installer Script:    $SetupScript" -ForegroundColor Gray
Write-Host "Release Package:     $ReleaseTarball ($([math]::Round($Tarballs[0].Length / 1MB, 2)) MB)" -ForegroundColor Gray

# Copy familyboard-setup.sh
$DestScript = Join-Path $DriveRoot "familyboard-setup.sh"
Write-Host "`n[1/3] Copying setup script to $DestScript..." -ForegroundColor Green
Copy-Item -Path $SetupScript -Destination $DestScript -Force

# Copy release tarball
$DestTarball = Join-Path $DriveRoot $TarballName
Write-Host "[2/3] Copying $TarballName to $DestTarball..." -ForegroundColor Green
Copy-Item -Path $ReleaseTarball -Destination $DestTarball -Force

# Check and update cmdline.txt for automatic execution on first boot
$CmdlinePath = Join-Path $DriveRoot "cmdline.txt"
if (-not $NoAutoRun -and (Test-Path $CmdlinePath)) {
    Write-Host "[3/3] Configuring cmdline.txt for automatic first-boot takeover..." -ForegroundColor Green
    $CmdlineContent = Get-Content -Path $CmdlinePath -Raw
    $AutoRunHook = "systemd.run=/boot/firmware/familyboard-setup.sh systemd.run_success_action=reboot"

    if ($CmdlineContent -notmatch "systemd\.run=") {
        # Ensure single-line format
        $Trimmed = $CmdlineContent.TrimEnd("`r", "`n", " ")
        $NewCmdline = "$Trimmed $AutoRunHook`n"
        Set-Content -Path $CmdlinePath -Value $NewCmdline -NoNewline
        Write-Host "      Added first-boot auto-run hook to cmdline.txt." -ForegroundColor Cyan
    } else {
        Write-Host "      cmdline.txt already contains systemd.run hook." -ForegroundColor Gray
    }
} else {
    Write-Host "[3/3] Skipping cmdline.txt modification." -ForegroundColor Gray
}

Write-Host "`n==============================================================" -ForegroundColor Green
Write-Host "  SUCCESS! MicroSD card is ready for FamilyBoard Appliance.  " -ForegroundColor Green
Write-Host "==============================================================" -ForegroundColor Green
Write-Host "What happens next:"
Write-Host "  1. Safely eject the MicroSD card from your PC."
Write-Host "  2. Insert the card into your Raspberry Pi and connect HDMI & power."
Write-Host "  3. The Pi boots, detects the local package (100% offline, zero tokens),"
Write-Host "     installs FamilyBoard, configures auto-login & Chromium kiosk,"
Write-Host "     and automatically reboots straight into FamilyBoard!"
Write-Host ""
