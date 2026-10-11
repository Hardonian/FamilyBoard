$ErrorActionPreference = "Stop"

Write-Host "=== Checking per-package code coverage thresholds ==="

function Check-PackageCoverage {
    param(
        [string]$Package,
        [double]$Minimum
    )
    $output = go test -cover $Package | Out-String
    if ($output -match 'coverage:\s+([0-9.]+)%') {
        $cov = [double]$matches[1]
    } else {
        $cov = 0.0
    }

    Write-Host "Package $Package : $cov% (minimum: $Minimum%)"
    if ($cov -lt $Minimum) {
        Write-Error "Coverage for $Package ($cov%) is below required threshold ($Minimum%)"
        exit 1
    }
}

Check-PackageCoverage "./internal/clock" 80.0
Check-PackageCoverage "./internal/errdefs" 80.0
Check-PackageCoverage "./internal/timezone" 70.0
Check-PackageCoverage "./internal/config" 70.0
Check-PackageCoverage "./internal/queue" 55.0
Check-PackageCoverage "./internal/backup" 55.0
Check-PackageCoverage "./internal/restore" 55.0

Write-Host "All critical package coverage thresholds satisfied!"
