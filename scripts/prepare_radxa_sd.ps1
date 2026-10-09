# ==============================================================================
# FamilyBoard Radxa Dragon SD Card Preparation Script (Windows PowerShell)
# Prepares a standalone folder on your SD Card (e.g. E:\familyboard) ready to run
# ==============================================================================

param (
    [Parameter(Mandatory=$false)]
    [string]$DriveLetter = "",

    [Parameter(Mandatory=$false)]
    [string]$OutputDir = ""
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir

Write-Host "==============================================================" -ForegroundColor Cyan
Write-Host "   FamilyBoard Radxa Dragon SD Card Preparation Engine       " -ForegroundColor Cyan
Write-Host "==============================================================" -ForegroundColor Cyan

# 1. Ensure ARM64 binaries exist or compile them
$Arm64Bin = Join-Path $ProjectRoot "familyboard-linux-arm64"
$CtlArm64Bin = Join-Path $ProjectRoot "familyboardctl-linux-arm64"

if (-not (Test-Path $Arm64Bin)) {
    Write-Host "[1/4] Compiling native ARM64 Linux binaries..." -ForegroundColor Yellow
    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = "arm64"
    go build -ldflags "-s -w" -o (Join-Path $ProjectRoot "familyboard-linux-arm64") (Join-Path $ProjectRoot "cmd\familyboard")
    go build -ldflags "-s -w" -o (Join-Path $ProjectRoot "familyboardctl-linux-arm64") (Join-Path $ProjectRoot "cmd\familyboardctl")
} else {
    Write-Host "[1/4] Found precompiled ARM64 Linux binaries." -ForegroundColor Green
}

# 2. Determine target directory
$TargetDir = ""
if ($DriveLetter -ne "") {
    $CleanDrive = $DriveLetter.TrimEnd(":\") + ":\"
    if (-not (Test-Path $CleanDrive)) {
        Write-Error "Drive letter $DriveLetter does not exist or is not mounted."
    }
    $TargetDir = Join-Path $CleanDrive "familyboard"
} elseif ($OutputDir -ne "") {
    $TargetDir = $OutputDir
} else {
    $TargetDir = Join-Path $ProjectRoot "dist\familyboard-sd-standalone"
}

Write-Host "[2/4] Preparing standalone folder at: $TargetDir" -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $TargetDir | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $TargetDir "photos") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $TargetDir "data") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $TargetDir "credentials") | Out-Null

# 3. Copy files
Write-Host "[3/4] Copying binaries, configurations, and deployment tools..." -ForegroundColor Yellow
Copy-Item -Force $Arm64Bin (Join-Path $TargetDir "familyboard")
if (Test-Path $CtlArm64Bin) {
    Copy-Item -Force $CtlArm64Bin (Join-Path $TargetDir "familyboardctl")
}

Copy-Item -Force (Join-Path $ProjectRoot "config.example.toml") (Join-Path $TargetDir "config.toml")
Copy-Item -Force (Join-Path $ProjectRoot "deploy\radxa\run-standalone.sh") (Join-Path $TargetDir "run-standalone.sh")
Copy-Item -Force (Join-Path $ProjectRoot "deploy\radxa\setup-radxa.sh") (Join-Path $TargetDir "install-appliance.sh")
Copy-Item -Force (Join-Path $ProjectRoot "deploy\radxa\migrate-to-nvme.sh") (Join-Path $TargetDir "migrate-to-nvme.sh")
Copy-Item -Force (Join-Path $ProjectRoot "deploy\radxa\familyboard-kiosk.sh") (Join-Path $TargetDir "familyboard-kiosk.sh")
Copy-Item -Force (Join-Path $ProjectRoot "deploy\radxa\familyboard.service") (Join-Path $TargetDir "familyboard.service")
Copy-Item -Force (Join-Path $ProjectRoot "deploy\radxa\familyboard-kiosk.service") (Join-Path $TargetDir "familyboard-kiosk.service")
Copy-Item -Force (Join-Path $ProjectRoot "deploy\radxa\10-familyboard-polkit.rules") (Join-Path $TargetDir "10-familyboard-polkit.rules")

# Create a clear README on the SD card
$ReadmeContent = @"
# FamilyBoard Standalone on Radxa Dragon

This folder contains the complete, self-contained FamilyBoard appliance.

## Option A: Run Directly from SD Card (Portable, Zero-Install)
In terminal on the Radxa Dragon:
    cd /path/to/familyboard
    chmod +x *.sh familyboard familyboardctl
    ./run-standalone.sh

## Option B: Install as Full Appliance (Fullscreen Kiosk on Boot)
To make FamilyBoard start automatically on boot and replace desktop:
    sudo ./install-appliance.sh

## Option C: Port from SD Card to High-Speed M.2 NVMe SSD
Once running on the Radxa Dragon with an M.2 NVMe SSD installed:
    sudo ./migrate-to-nvme.sh
This automatically formats the NVMe SSD, clones the system, and sets up direct boot!
"@
Set-Content -Path (Join-Path $TargetDir "README.txt") -Value $ReadmeContent

Write-Host "[4/4] Complete!" -ForegroundColor Green
Write-Host "==============================================================" -ForegroundColor Green
Write-Host "  FamilyBoard standalone folder is ready at: $TargetDir" -ForegroundColor Green
Write-Host "  Just cut and paste this folder onto your SD card or USB drive." -ForegroundColor Green
Write-Host "==============================================================" -ForegroundColor Green
