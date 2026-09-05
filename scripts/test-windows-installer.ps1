param(
    [Parameter(Mandatory = $true)]
    [string]$BaselineInstallerPath,

    [Parameter(Mandatory = $true)]
    [string]$InstallerPath
)

$ErrorActionPreference = 'Stop'

$baseline = (Resolve-Path -LiteralPath $BaselineInstallerPath).Path
$installer = (Resolve-Path -LiteralPath $InstallerPath).Path
$root = Join-Path ([IO.Path]::GetTempPath()) "pwdtt-installer-smoke-$([guid]::NewGuid().ToString('N'))"
$installDir = Join-Path $root 'PWDTT'
$appExe = Join-Path $installDir 'PWDTT.exe'
$uninstaller = Join-Path $installDir 'unins000.exe'
$sentinel = Join-Path $installDir 'upgrade-sentinel.txt'

function Invoke-Installer {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath
    )

    $arguments = @(
        '/VERYSILENT',
        '/SUPPRESSMSGBOXES',
        '/NORESTART',
        '/SP-',
        ('/DIR="' + $installDir + '"')
    )
    $process = Start-Process -FilePath $FilePath -ArgumentList $arguments -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        throw "Installer failed with exit code $($process.ExitCode): $FilePath"
    }
}

function Invoke-Uninstaller {
    if (-not (Test-Path -LiteralPath $uninstaller)) {
        return
    }

    $process = Start-Process -FilePath $uninstaller -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        throw "Uninstaller failed with exit code $($process.ExitCode)"
    }
}

try {
    New-Item -ItemType Directory -Path $root -Force | Out-Null

    Invoke-Installer -FilePath $baseline
    if (-not (Test-Path -LiteralPath $appExe)) {
        throw 'Baseline install did not create PWDTT.exe.'
    }
    if (-not (Test-Path -LiteralPath $uninstaller)) {
        throw 'Baseline install did not create the uninstaller.'
    }

    Set-Content -LiteralPath $sentinel -Value 'preserve across upgrade' -Encoding utf8

    Invoke-Installer -FilePath $installer
    if (-not (Test-Path -LiteralPath $appExe)) {
        throw 'Upgrade install did not preserve PWDTT.exe.'
    }
    if (-not (Test-Path -LiteralPath $sentinel)) {
        throw 'Upgrade install unexpectedly removed an existing file from the install directory.'
    }

    Invoke-Uninstaller
    Start-Sleep -Milliseconds 500

    if (Test-Path -LiteralPath $appExe) {
        throw 'Uninstall left PWDTT.exe behind.'
    }
    if (Test-Path -LiteralPath $uninstaller) {
        throw 'Uninstall left the uninstaller behind.'
    }

    Write-Host 'Windows installer install/upgrade/uninstall smoke passed.'
}
finally {
    try {
        Invoke-Uninstaller
    }
    catch {
        Write-Warning $_
    }
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}
