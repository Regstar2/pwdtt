param(
    [Parameter(Mandatory = $true)]
    [string]$Path,

    [switch]$AllowUntrustedSigner,

    [string]$ExpectedThumbprint
)

$ErrorActionPreference = 'Stop'
$resolvedPath = (Resolve-Path -LiteralPath $Path).Path
$signature = Get-AuthenticodeSignature -FilePath $resolvedPath

if (-not $signature.SignerCertificate) {
    throw "Authenticode verification returned no signer certificate for $resolvedPath"
}

if (-not [string]::IsNullOrWhiteSpace($ExpectedThumbprint)) {
    $actualThumbprint = $signature.SignerCertificate.Thumbprint
    if (-not $actualThumbprint.Equals($ExpectedThumbprint, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Authenticode signer mismatch for ${resolvedPath}: expected=$ExpectedThumbprint, actual=$actualThumbprint"
    }
}

if ($signature.Status -eq 'Valid') {
    Write-Host "Authenticode signature valid: $resolvedPath"
} elseif ($AllowUntrustedSigner -and $signature.Status -eq 'UnknownError') {
    Write-Host "Authenticode signature present with expected untrusted CI signer: $resolvedPath"
} else {
    throw "Authenticode verification failed for ${resolvedPath}: status=$($signature.Status), signer=$($signature.SignerCertificate.Subject)"
}

Write-Host "Signer: $($signature.SignerCertificate.Subject)"
Write-Host "Thumbprint: $($signature.SignerCertificate.Thumbprint)"
