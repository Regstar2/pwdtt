param(
    [Parameter(Mandatory = $true)]
    [string]$Path
)

$ErrorActionPreference = 'Stop'
$resolvedPath = (Resolve-Path -LiteralPath $Path).Path
$signature = Get-AuthenticodeSignature -FilePath $resolvedPath

if ($signature.Status -ne 'Valid') {
    $subject = if ($signature.SignerCertificate) {
        $signature.SignerCertificate.Subject
    } else {
        '<no signer>'
    }
    throw "Authenticode verification failed for $resolvedPath: status=$($signature.Status), signer=$subject"
}

if (-not $signature.SignerCertificate) {
    throw "Authenticode verification returned no signer certificate for $resolvedPath"
}

Write-Host "Authenticode signature valid: $resolvedPath"
Write-Host "Signer: $($signature.SignerCertificate.Subject)"
Write-Host "Thumbprint: $($signature.SignerCertificate.Thumbprint)"
