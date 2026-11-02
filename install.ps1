# credproxy: build and install this checkout's own executables.
# Config, hooks, wrappers, and services belong to the consuming environment.

$repoDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$goCommand = @(Get-Command go -CommandType Application -ErrorAction SilentlyContinue)[0]
if ($null -eq $goCommand) {
    throw "credproxy: ERROR go unavailable; credproxy binaries were not built"
}

foreach ($source in @("cmd/credproxyd", "cmd/credproxy")) {
    $sourcePath = Join-Path $repoDir $source
    if (-not (Test-Path -LiteralPath $sourcePath -PathType Container)) {
        throw "credproxy: ERROR source incomplete (missing $source); credproxy binaries were not built"
    }
}

if ([string]::IsNullOrEmpty($env:BINDIR)) {
    if ([string]::IsNullOrEmpty($env:USERPROFILE)) {
        throw "credproxy: ERROR USERPROFILE unavailable; credproxy binaries were not built"
    }
    $binDir = Join-Path (Join-Path $env:USERPROFILE ".local") "bin"
} else {
    $binDir = $env:BINDIR
}
$binDir = [System.IO.Path]::GetFullPath($binDir)
[System.IO.Directory]::CreateDirectory($binDir) | Out-Null

$targets = @(
    @{ Name = "credproxyd.exe"; Package = "./cmd/credproxyd" },
    @{ Name = "credproxy.exe"; Package = "./cmd/credproxy" }
)

Push-Location -LiteralPath $repoDir
try {
    Write-Host "credproxy: building credproxyd + credproxy -> $binDir"
    foreach ($target in $targets) {
        $outputPath = Join-Path $binDir $target.Name
        & $goCommand.Path build -o $outputPath $target.Package
        $buildExitCode = $LASTEXITCODE
        if ($buildExitCode -ne 0) {
            throw "credproxy: ERROR go build failed for $($target.Name) (exit code $buildExitCode); an earlier output may already be installed and no rollback was attempted"
        }
    }
} finally {
    Pop-Location
}

$daemonPath = Join-Path $binDir "credproxyd.exe"
$clientPath = Join-Path $binDir "credproxy.exe"
Write-Host "credproxy: install done ($daemonPath, $clientPath)"
