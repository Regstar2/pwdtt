param(
    [Parameter(Mandatory = $true)]
    [string]$Path,

    [Parameter(Mandatory = $true)]
    [string]$PfxBase64,

    [Parameter(Mandatory = $true)]
    [string]$PfxPassword,

    [string]$TimestampUrl = 'http://timestamp.digicert.com'
)

$ErrorActionPreference = 'Stop'

$resolvedPath = (Resolve-Path -LiteralPath $Path).Path
if ([string]::IsNullOrWhiteSpace($PfxBase64)) {
    throw 'PFX payload is empty.'
}
if ([string]::IsNullOrWhiteSpace($PfxPassword)) {
    throw 'PFX password is empty.'
}

$programFilesX86 = [Environment]::GetFolderPath('ProgramFilesX86')
$kitsRoot = Join-Path $programFilesX86 'Windows Kits\10\bin'
$signtool = Get-ChildItem -Path $kitsRoot -Filter 'signtool.exe' -File -Recurse -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -match '\\x64\\signtool\.exe$' } |
    Sort-Object FullName -Descending |
    Select-Object -First 1

if (-not $signtool) {
    throw "signtool.exe was not found below $kitsRoot"
}

$tempPfx = Join-Path ([IO.Path]::GetTempPath()) "pwdtt-sign-$([guid]::NewGuid().ToString('N')).pfx"
$imported = @()

try {
    try {
        $pfxBytes = [Convert]::FromBase64String($PfxBase64)
    }
    catch {
        throw 'WINDOWS_SIGNING_PFX_BASE64 is not valid base64.'
    }

    [IO.File]::WriteAllBytes($tempPfx, $pfxBytes)
    $securePassword = ConvertTo-SecureString -String $PfxPassword -AsPlainText -Force
    $imported = @(Import-PfxCertificate -FilePath $tempPfx -CertStoreLocation 'Cert:\CurrentUser\My' -Password $securePassword -Exportable:$false)

    $signingCert = $imported |
        Where-Object {
            $_.HasPrivateKey -and
            ($_.EnhancedKeyUsageList | Where-Object { $_.ObjectId.Value -eq '1.3.6.1.5.5.7.3.3' })
        } |
        Select-Object -First 1

    if (-not $signingCert) {
        throw 'The PFX does not contain a private-key certificate with the Code Signing EKU.'
    }

    $arguments = @(
        'sign',
        '/fd', 'SHA256',
        '/sha1', $signingCert.Thumbprint,
        '/s', 'My'
    )
    if (-not [string]::IsNullOrWhiteSpace($TimestampUrl)) {
        $arguments += @('/tr', $TimestampUrl, '/td', 'SHA256')
    }
    $arguments += $resolvedPath

    & $signtool.FullName @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "signtool failed with exit code $LASTEXITCODE"
    }
}
finally {
    foreach ($cert in $imported) {
        $certPath = "Cert:\CurrentUser\My\$($cert.Thumbprint)"
        if (Test-Path -LiteralPath $certPath) {
            Remove-Item -LiteralPath $certPath -Force -ErrorAction SilentlyContinue
        }
    }
    if (Test-Path -LiteralPath $tempPfx) {
        Remove-Item -LiteralPath $tempPfx -Force -ErrorAction SilentlyContinue
    }
}
