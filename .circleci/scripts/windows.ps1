# Native Windows gates (mirrors the windows-latest job of .github/workflows/ci.yml)
# plus Windows race coverage and native install.ps1 tests.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-Native {
    param([Parameter(Mandatory)][string]$File, [string[]]$Arguments = @())
    Write-Host "+ $File $($Arguments -join ' ')"
    & $File @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$File exited with $LASTEXITCODE" }
}

if ($env:OS -ne 'Windows_NT') { throw 'Native Windows required' }
$revision = (git rev-parse HEAD).Trim()
if (-not $env:CIRCLE_SHA1 -or $revision -ne $env:CIRCLE_SHA1) { throw 'Checkout does not match the pipeline revision' }

# Select exactly the go.mod toolchain (the image's Go is only a bootstrap).
Remove-Item Env:GOROOT -ErrorAction SilentlyContinue
$pin = @(Get-Content go.mod | Where-Object { $_ -match '^toolchain\s+go\d+\.\d+\.\d+\s*$' })
if ($pin.Count -ne 1) { throw 'go.mod must pin exactly one toolchain' }
$toolchain = ($pin[0] -split '\s+')[1]
$env:GOTOOLCHAIN = $toolchain
$goroot = (go env GOROOT).Trim()
if ($LASTEXITCODE -ne 0) { throw 'go env GOROOT failed' }
$env:PATH = (Join-Path $goroot 'bin') + ';' + $env:PATH
$env:GOTOOLCHAIN = 'local'
$env:GOFLAGS = '-mod=readonly'
$env:CGO_ENABLED = '0'
$version = (go version).Trim()
if ($version -ne "go version $toolchain windows/amd64") { throw "Unexpected Go: $version" }
Write-Host $version

# MinGW-w64 13.2.0 (GNU make 4.4.1 as mingw32-make.exe, plus gcc for -race)
# comes from a pinned GitHub release verified by SHA-256. The Chocolatey
# community feed is deliberately not used: its outages (HTTP 503) failed CI.
$mingwUrl = 'https://github.com/niXman/mingw-builds-binaries/releases/download/13.2.0-rt_v11-rev1/x86_64-13.2.0-release-posix-seh-ucrt-rt_v11-rev1.7z'
$mingwSha256 = '475ee72c5ce1bd54a3e3c334bdd3be5e6575334184fd9718013aa362c9819d2f'
$toolsDir = Join-Path $env:TEMP 'ant-router-ci-tools'
$mingwArchive = Join-Path $toolsDir 'mingw64.7z'
New-Item -ItemType Directory -Force -Path $toolsDir | Out-Null
$downloaded = $false
for ($attempt = 1; $attempt -le 4 -and -not $downloaded; $attempt++) {
    try {
        Write-Host "+ download $mingwUrl (attempt $attempt)"
        Invoke-WebRequest -Uri $mingwUrl -OutFile $mingwArchive -UseBasicParsing -TimeoutSec 300
        $downloaded = $true
    } catch {
        if ($attempt -eq 4) { throw }
        Write-Host "download failed: $($_.Exception.Message); retrying"
        Start-Sleep -Seconds (15 * $attempt)
    }
}
$actualSha256 = (Get-FileHash -LiteralPath $mingwArchive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSha256 -ne $mingwSha256) { throw "MinGW archive SHA-256 mismatch: $actualSha256" }
$sevenZip = (Get-Command 7z -ErrorAction SilentlyContinue).Source
if (-not $sevenZip) { $sevenZip = 'C:\Program Files\7-Zip\7z.exe' }
if (-not (Test-Path -LiteralPath $sevenZip -PathType Leaf)) { throw '7-Zip not found' }
Invoke-Native $sevenZip @('x', '-y', '-bso0', "-o$toolsDir", $mingwArchive)
$mingwBin = Join-Path $toolsDir 'mingw64\bin'
$make = Join-Path $mingwBin 'mingw32-make.exe'
foreach ($tool in $make, (Join-Path $mingwBin 'gcc.exe')) {
    if (-not (Test-Path -LiteralPath $tool -PathType Leaf)) { throw "$tool missing after extraction" }
}
$makeVersion = (& $make --version | Select-Object -First 1)
if ($makeVersion -ne 'GNU Make 4.4.1') { throw "Unexpected make: $makeVersion" }

# GNU make runs the Makefile recipes through Git's sh.exe (POSIX quoting).
$gitUsrBin = 'C:\Program Files\Git\usr\bin'
if (-not (Test-Path -LiteralPath (Join-Path $gitUsrBin 'sh.exe'))) { throw 'Git sh.exe not found' }
$env:PATH = $gitUsrBin + ';' + $env:PATH

Invoke-Native go @('vet', './...')
Invoke-Native go @('test', './...')
Invoke-Native go @('test', './internal/controltransport', '-run', 'TestWindows', '-count=20')
Invoke-Native $make @('phase1c-e2e')
Invoke-Native go @('test', './internal/canonical', './internal/canonicaljson', '-count=20')
Invoke-Native go @('test', './internal/streaming', '-count=20')
Invoke-Native go @('test', './internal/dataplane', '-run', 'TestOfficialOpenAISDKModelsAndResponses', '-count=20')
Invoke-Native go @('test', '-tags=e2e', './test/e2e', '-run', 'TestMockResponsesSubprocessWorkflow', '-count=3', '-timeout=5m')
foreach ($target in 'phase3b-sdk', 'phase4a-contract', 'phase4a-live-compile', 'phase4b-contract', 'phase4b-live-compile', 'phase4c-contract', 'phase4c-e2e', 'phase4c-live-compile') {
    Invoke-Native $make @($target)
}

# Native install.ps1 tests (present once the installer lands). Windows has no
# POSIX installer target, so only PowerShell is required here.
if (Test-Path -LiteralPath 'test/install' -PathType Container) {
    $null = Get-Command pwsh -ErrorAction Stop
    $env:ANT_ROUTER_REQUIRE_INSTALL_TOOLS = 'pwsh'
    Invoke-Native go @('test', './test/install/...', '-run', 'PS1', '-count=1', '-v')
    Remove-Item Env:ANT_ROUTER_REQUIRE_INSTALL_TOOLS
}

# Race coverage for Windows-specific code needs cgo and MinGW-w64 >= 8.
$env:PATH = $mingwBin + ';' + $env:PATH
$env:CGO_ENABLED = '1'
$env:CC = 'gcc'
$racePackages = @('./internal/controltoken', './internal/inferencetoken', './internal/controltransport', './internal/atomicfile', './internal/filelock', './internal/configstore')
if (Test-Path -LiteralPath 'internal/service' -PathType Container) { $racePackages += './internal/service' }
Invoke-Native go (@('test', '-race', '-count=1', '-timeout=10m') + $racePackages)
