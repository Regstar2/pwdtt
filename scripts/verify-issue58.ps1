#Requires -Version 7.0
# Offline/local PWDTT issue #58 verification, without GitHub Actions.
# Creates a ZIP with command outputs under TEMP; does not read user profiles.
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$dir = Join-Path $env:TEMP "pwdtt-issue58-$stamp"
New-Item -ItemType Directory -Path $dir -Force | Out-Null
$results = [System.Collections.Generic.List[object]]::new()

function Invoke-Check {
    param([string]$Name, [string]$Directory, [string]$Executable, [string[]]$Arguments)
    $log = Join-Path $dir "$Name.log"
    Push-Location $Directory
    try {
        "Working directory: $Directory" | Out-File -FilePath $log -Encoding utf8
        "Command: $Executable $($Arguments -join ' ')" | Out-File -FilePath $log -Append -Encoding utf8
        & $Executable @Arguments 2>&1 | Out-File -FilePath $log -Append -Encoding utf8
        $exitCode = $LASTEXITCODE
        if ($null -eq $exitCode) { $exitCode = 1 }
        $status = if ($exitCode -eq 0) { 'PASS' } else { 'FAIL' }
        $results.Add([pscustomobject]@{ Name=$Name; Status=$status; ExitCode=$exitCode })
        Write-Host "[$status] $Name (exit $exitCode)"
    } catch {
        "Error: $($_.Exception.Message)" | Out-File -FilePath $log -Append -Encoding utf8
        $results.Add([pscustomobject]@{ Name=$Name; Status='FAIL'; ExitCode=-1 })
        Write-Host "[FAIL] $Name"
    } finally {
        Pop-Location
    }
}

function Test-Available {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        $results.Add([pscustomobject]@{ Name=$Name; Status='NOT_RUN'; ExitCode=$null })
        Write-Host "[NOT_RUN] $Name is not installed"
        return $false
    }
    return $true
}

try {
    "PWDTT #58 validation | $(Get-Date -Format o)" | Out-File (Join-Path $dir 'environment.txt')
    if (Test-Available 'git') {
        (& git -C $root status --short) | Out-File (Join-Path $dir 'git-status.txt') -Encoding utf8
        (& git -C $root rev-parse HEAD) | Out-File (Join-Path $dir 'git-head.txt') -Encoding utf8
    }
    if (Test-Available 'go') {
        (& go version) | Out-File (Join-Path $dir 'go-version.txt') -Encoding utf8
        Invoke-Check -Name 'go-root' -Directory $root -Executable 'go' -Arguments @('test','-count=1','./...')
        Invoke-Check -Name 'go-core' -Directory (Join-Path $root 'core') -Executable 'go' -Arguments @('test','-count=1','./...')
        if (Get-Command 'gcc' -ErrorAction SilentlyContinue) {
            Invoke-Check -Name 'go-core-race' -Directory (Join-Path $root 'core') -Executable 'go' -Arguments @('test','-race','-count=1','./...')
        } else {
            $results.Add([pscustomobject]@{ Name='go-core-race'; Status='SKIPPED_OPTIONAL'; ExitCode=$null })
            Write-Host '[SKIPPED_OPTIONAL] gcc unavailable (race detector)'
        }
    }
    if (Test-Available 'npm') {
        $front = Join-Path $root 'frontend'
        (& npm --version) | Out-File (Join-Path $dir 'npm-version.txt') -Encoding utf8
        Invoke-Check -Name 'npm-ci' -Directory $front -Executable 'npm' -Arguments @('ci')
        if ($results[$results.Count-1].Status -eq 'PASS') {
            Invoke-Check -Name 'npm-test' -Directory $front -Executable 'npm' -Arguments @('test')
            Invoke-Check -Name 'npm-lint' -Directory $front -Executable 'npm' -Arguments @('run','lint')
            Invoke-Check -Name 'npm-build' -Directory $front -Executable 'npm' -Arguments @('run','build')
        } else {
            foreach ($name in @('npm-test','npm-lint','npm-build')) {
                $results.Add([pscustomobject]@{ Name=$name; Status='NOT_RUN'; ExitCode=$null })
            }
        }
    }
    if (Test-Available 'wails') {
        (& wails version 2>&1) | Out-File (Join-Path $dir 'wails-version.txt') -Encoding utf8
        Invoke-Check -Name 'wails-windows-build' -Directory $root -Executable 'wails' -Arguments @('build','-platform','windows/amd64','-o','pwdtt-windows-amd64.exe')
    }
} finally {
    $results | ConvertTo-Json -Depth 3 | Out-File (Join-Path $dir 'summary.json') -Encoding utf8
    $zip = "$dir.zip"
    Compress-Archive -Path (Join-Path $dir '*') -DestinationPath $zip -Force
    Write-Host "Evidence ZIP: $zip"
    if (@($results | Where-Object { $_.Status -notin @('PASS','SKIPPED_OPTIONAL') }).Count -gt 0) {
        Write-Host 'Validation incomplete. Examine summary.json and logs in the ZIP.'
        exit 1
    }
    Write-Host 'All executed checks passed.'
}
