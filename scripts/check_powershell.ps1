$ErrorActionPreference = 'Stop'

$scripts = Get-ChildItem -LiteralPath (Join-Path $PSScriptRoot '.') -Filter '*.ps1' -File
foreach ($script in $scripts) {
    if ($script.Name -eq 'check_powershell.ps1') {
        continue
    }

    $tokens = $null
    $parseErrors = $null
    [void][System.Management.Automation.Language.Parser]::ParseFile(
        $script.FullName,
        [ref]$tokens,
        [ref]$parseErrors
    )
    if ($parseErrors) {
        throw "PowerShell syntax errors in $($script.Name):`n$($parseErrors | Out-String)"
    }
    Write-Host "Syntax OK: $($script.Name)"
}
