param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [Parameter(Mandatory = $true)]
    [string]$DistDir,

    [string]$SigningStatusFile
)

$ErrorActionPreference = 'Stop'

if ($Version -notmatch '^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$') {
    throw "Unsupported release version: $Version"
}

$dist = (Resolve-Path -LiteralPath $DistDir).Path
$assets = @(
    'pwdtt-linux-amd64',
    'pwdtt-windows-amd64.exe',
    'pwdtt-windows-amd64-setup.exe',
    'PWDTT-macos.zip'
)

$checksumLines = foreach ($asset in $assets) {
    $path = Join-Path $dist $asset
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Missing final release artifact: $asset"
    }
    $hash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $asset"
}

$checksumPath = Join-Path $dist 'SHA256SUMS'
$checksumLines | Set-Content -LiteralPath $checksumPath -Encoding utf8

$signingStatus = 'Authenticode: status unavailable'
if (-not [string]::IsNullOrWhiteSpace($SigningStatusFile)) {
    $statusPath = (Resolve-Path -LiteralPath $SigningStatusFile).Path
    $signingStatus = (Get-Content -LiteralPath $statusPath -Raw).Trim()
    if ([string]::IsNullOrWhiteSpace($signingStatus)) {
        throw 'Windows signing status file is empty.'
    }
}

$sourceLine = if ([string]::IsNullOrWhiteSpace($env:GITHUB_SHA)) {
    'Source commit: local release preparation'
} else {
    "Source commit: $env:GITHUB_SHA"
}

$notes = @"
# PWDTT $Version

$sourceLine

## Установка и обновление

- Windows installer: pwdtt-windows-amd64-setup.exe — рекомендуемая per-user установка с uninstall и upgrade поверх предыдущей версии.
- Windows portable: pwdtt-windows-amd64.exe остаётся официальным вариантом без установки.
- Linux: pwdtt-linux-amd64.
- macOS: PWDTT-macos.zip.

## Windows signing

$signingStatus

## Артефакты и SHA-256

~~~text
$($checksumLines -join [Environment]::NewLine)
~~~

SHA256SUMS содержит те же SHA-256 для всех распространяемых payload-файлов этого релиза.
"@

$notesPath = Join-Path $dist 'RELEASE_NOTES.md'
$notes | Set-Content -LiteralPath $notesPath -Encoding utf8

Write-Host "Generated $checksumPath"
Write-Host "Generated $notesPath"
