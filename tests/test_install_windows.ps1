$ErrorActionPreference = "Stop"

function Assert-Condition {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) {
        throw "ASSERT: $Message"
    }
}

$repoDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$installer = Join-Path $repoDir "install.ps1"
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("credproxy-install-test-" + [guid]::NewGuid().ToString("N"))
$fixtureDir = Join-Path $tempRoot "fake go"
$runningOnWindows = [System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT
$fixtureName = if ($runningOnWindows) { "go.cmd" } else { "go" }
$fixturePath = Join-Path $fixtureDir $fixtureName
$logPath = Join-Path $tempRoot "go-calls.txt"
$oldPath = $env:PATH
$oldBinDir = $env:BINDIR
$oldProfile = $env:USERPROFILE
$oldLog = $env:GO_FIXTURE_LOG
$oldFailPackage = $env:GO_FIXTURE_FAIL_PACKAGE

New-Item -ItemType Directory -Path $fixtureDir -Force | Out-Null
if ($runningOnWindows) {
    $fixture = "@echo off`r`nsetlocal`r`n>>`"%GO_FIXTURE_LOG%`" echo %CD%^|%~3^|%~4`r`nif `"%GO_FIXTURE_FAIL_PACKAGE%`"==`"%~4`" (>&2 echo fixture failure & exit /b 17)`r`n>&2 echo fixture progress`r`n> `"%~3`" echo fixture`r`nexit /b 0`r`n"
} else {
    $fixture = "#!/bin/sh`nprintf '%s|%s|%s\n' `"`$PWD`" `"`$3`" `"`$4`" >> `"`$GO_FIXTURE_LOG`"`nif [ `"`$GO_FIXTURE_FAIL_PACKAGE`" = `"`$4`" ]; then echo 'fixture failure' >&2; exit 17; fi`necho 'fixture progress' >&2`nprintf fixture > `"`$3`"`n"
}
[System.IO.File]::WriteAllText($fixturePath, $fixture, [System.Text.Encoding]::ASCII)
if (-not $runningOnWindows) {
    & chmod +x $fixturePath
}

try {
    $env:PATH = $fixtureDir
    $env:GO_FIXTURE_LOG = $logPath
    $env:GO_FIXTURE_FAIL_PACKAGE = ""
    $beforeLocation = (Get-Location).Path

    $env:BINDIR = Join-Path $tempRoot "override output"
    & $installer
    Assert-Condition (Test-Path -LiteralPath (Join-Path $env:BINDIR "credproxyd.exe") -PathType Leaf) "daemon exe was not installed to BINDIR"
    Assert-Condition (Test-Path -LiteralPath (Join-Path $env:BINDIR "credproxy.exe") -PathType Leaf) "client exe was not installed to BINDIR"
    Assert-Condition ((Get-Location).Path -eq $beforeLocation) "installer did not restore the caller's location"

    $calls = @(Get-Content -LiteralPath $logPath)
    Assert-Condition ($calls.Count -eq 2) "expected exactly two Go builds"
    $expectedPackages = @("./cmd/credproxyd", "./cmd/credproxy")
    $expectedOutputs = @("credproxyd.exe", "credproxy.exe")
    for ($index = 0; $index -lt $calls.Count; $index++) {
        $parts = $calls[$index] -split "\|", 3
        Assert-Condition ($parts.Count -eq 3) "Go fixture did not record cwd, output, and package"
        Assert-Condition ([System.IO.Path]::GetFullPath($parts[0]) -eq [System.IO.Path]::GetFullPath($repoDir)) "Go build did not run from the checkout root"
        Assert-Condition ($parts[1] -eq (Join-Path $env:BINDIR $expectedOutputs[$index])) "Go build targeted an unexpected output path"
        Assert-Condition ($parts[2] -eq $expectedPackages[$index]) "Go build targeted an unexpected package"
    }

    $env:BINDIR = $null
    $env:USERPROFILE = Join-Path $tempRoot "fake profile"
    $defaultBin = Join-Path (Join-Path $env:USERPROFILE ".local") "bin"
    $env:GO_FIXTURE_LOG = Join-Path $tempRoot "default-go-calls.txt"
    & $installer
    Assert-Condition (Test-Path -LiteralPath (Join-Path $defaultBin "credproxyd.exe") -PathType Leaf) "default native user bin path was not used"
    Assert-Condition (Test-Path -LiteralPath (Join-Path $defaultBin "credproxy.exe") -PathType Leaf) "default native user bin path was not used"

    $env:BINDIR = Join-Path $tempRoot "partial output"
    $env:GO_FIXTURE_LOG = Join-Path $tempRoot "failure-go-calls.txt"
    $env:GO_FIXTURE_FAIL_PACKAGE = "./cmd/credproxy"
    $failureMessage = ""
    try {
        & $installer
    } catch {
        $failureMessage = $_.Exception.Message
    }
    Assert-Condition ($failureMessage -match "credproxy\.exe.*exit code 17") "build failure was not reported clearly"
    Assert-Condition ($failureMessage -match "no rollback was attempted") "failure message implied rollback"
    Assert-Condition (Test-Path -LiteralPath (Join-Path $env:BINDIR "credproxyd.exe") -PathType Leaf) "successful first output was unexpectedly removed"
    Assert-Condition (-not (Test-Path -LiteralPath (Join-Path $env:BINDIR "credproxy.exe"))) "failed second output should not exist"
    Assert-Condition ((Get-Location).Path -eq $beforeLocation) "installer did not restore location after failure"
} finally {
    $env:PATH = $oldPath
    $env:BINDIR = $oldBinDir
    $env:USERPROFILE = $oldProfile
    $env:GO_FIXTURE_LOG = $oldLog
    $env:GO_FIXTURE_FAIL_PACKAGE = $oldFailPackage
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host "test_install_windows.ps1: passed"
