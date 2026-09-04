param(
    [Parameter(Mandatory = $false)]
    [string]$Path = (Join-Path $PSScriptRoot "..\build\bin\pwdtt-windows-amd64.exe"),

    [switch]$SkipSignatureUpdate
)

$ErrorActionPreference = "Stop"

$resolvedPath = (Resolve-Path -LiteralPath $Path).Path
$fileHash = (Get-FileHash -LiteralPath $resolvedPath -Algorithm SHA256).Hash.ToLowerInvariant()

$status = Get-MpComputerStatus
if (-not $status.AntivirusEnabled) {
    throw "Microsoft Defender Antivirus is not enabled."
}

if (-not $SkipSignatureUpdate) {
    Update-MpSignature | Out-Null
    $status = Get-MpComputerStatus
}

Write-Host "File: $resolvedPath"
Write-Host "SHA-256: $fileHash"
Write-Host "Defender signatures: $($status.AntivirusSignatureVersion)"
Write-Host "Signatures updated: $($status.AntivirusSignatureLastUpdated)"

$scanStarted = Get-Date
Start-MpScan -ScanType CustomScan -ScanPath $resolvedPath
Start-Sleep -Seconds 2

$recentDetections = @(
    Get-MpThreatDetection -ErrorAction SilentlyContinue |
        Where-Object {
            $_.InitialDetectionTime -ge $scanStarted.AddMinutes(-1)
        }
)

$pathPattern = [regex]::Escape($resolvedPath)
$fileDetections = @(
    $recentDetections |
        Where-Object {
            (($_.Resources | Out-String) -match $pathPattern)
        }
)

if ($fileDetections.Count -gt 0) {
    Write-Host ""
    Write-Host "Microsoft Defender detected the candidate binary:"
    $fileDetections | Format-List ThreatID, InitialDetectionTime, ActionSuccess, Resources
    exit 1
}

if (-not (Test-Path -LiteralPath $resolvedPath)) {
    Write-Host ""
    Write-Host "The binary disappeared during the scan. Recent Defender detections:"
    $recentDetections | Format-List ThreatID, InitialDetectionTime, ActionSuccess, Resources
    exit 1
}

Write-Host "Defender result: no detection for this file."
