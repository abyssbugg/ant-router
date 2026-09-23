param([switch]$Worker, [string]$VerifyResults)
$ErrorActionPreference = 'Stop'

function Assert-Results([string]$Path) {
    $package = 'github.com/abyssbugg/ant-router/test/e2e'
    $passed = @{
        TestConfigurationWorkflow = 0
        TestMockResponsesSubprocessWorkflow = 0
        TestAnthropicProviderSubprocessAvailability = 0
    }
    $packagePasses = 0
    foreach ($line in [System.IO.File]::ReadLines($Path)) {
        $event = ConvertFrom-Json -InputObject $line -ErrorAction Stop
        if ($event.Package -ne $package) { continue }
        if ($event.Action -eq 'fail' -or $event.Action -eq 'skip') {
            throw 'Required E2E evidence contains failure or skip'
        }
        if ($event.Action -eq 'pass') {
            if ($event.Test -and $passed.ContainsKey($event.Test)) { $passed[$event.Test]++ }
            elseif (-not $event.Test) { $packagePasses++ }
        }
    }
    if ($packagePasses -ne 1 -or @($passed.Values | Where-Object { $_ -ne 1 }).Count -ne 0) {
        throw 'Missing or duplicate required offline E2E passes'
    }
    Write-Host 'Verified all three required offline E2E tests passed'
}

# Parser-only mode is exercised by the Windows safety-test fixtures.
if ($VerifyResults) {
    Assert-Results $VerifyResults
    exit 0
}
if (-not $Worker) {
    # This parent stays alive while the child runs all setup and tests. Kill the
    # full descendant tree on elapsed-time expiry, not just the current command.
    $start = New-Object System.Diagnostics.ProcessStartInfo
    $start.FileName = (Join-Path $PSHOME 'powershell.exe')
    $start.Arguments = '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + $PSCommandPath + '" -Worker'
    $start.UseShellExecute = $false
    $start.WorkingDirectory = (Get-Location).Path
    $child = [System.Diagnostics.Process]::Start($start)
    try {
        if (-not $child.WaitForExit(720000)) {
            & taskkill.exe /PID $child.Id /T /F
            if ($LASTEXITCODE -ne 0) { throw 'Failed to terminate expired coverage process tree' }
            throw 'Coverage exceeded its 12-minute elapsed deadline'
        }
        exit $child.ExitCode
    } finally {
        $child.Dispose()
    }
}
if ($env:OS -ne 'Windows_NT') { throw 'Native Windows required' }
$revision = git rev-parse HEAD
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if (-not $env:CIRCLE_SHA1 -or $revision -ne $env:CIRCLE_SHA1) { throw 'Checkout revision mismatch' }
Write-Host "Coverage checkout: $revision"

Remove-Item Env:GOROOT -ErrorAction SilentlyContinue
$pin = @(Get-Content go.mod | Where-Object { $_ -match '^toolchain\s+go\d+\.\d+\.\d+\s*$' })
if ($pin.Count -ne 1) { throw 'Expected one exact Go toolchain pin' }
$toolchain = ($pin[0] -split '\s+')[1]
$env:GOTOOLCHAIN = $toolchain
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '1'
$env:CC = 'gcc'
$env:GOFLAGS = '-mod=readonly'
$env:GOSUMDB = 'sum.golang.org'
$goroot = go env GOROOT
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$env:PATH = (Join-Path $goroot 'bin') + ';' + $env:PATH
$env:GOTOOLCHAIN = 'local'
$version = go version
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if ($version -ne "go version $toolchain windows/amd64") { throw 'Pinned compiler mismatch' }
$hostOS = go env GOHOSTOS
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$hostArch = go env GOHOSTARCH
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if ($hostOS -ne 'windows' -or $hostArch -ne 'amd64') { throw 'Native Windows amd64 required' }
$null = Get-Command gcc -ErrorAction Stop
Write-Host $version
go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./... -count=1 -timeout=5m
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test -race ./internal/controltoken ./internal/inferencetoken -count=1 -timeout=3m
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./internal/controltransport -run '^TestWindows' -count=20 -timeout=3m
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go build ./cmd/routerd ./cmd/routerctl
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$results = Join-Path ([System.IO.Path]::GetTempPath()) ('tmp_rovodev_pr_e2e_' + [guid]::NewGuid().ToString('N') + '.json')
try {
    # UTF-8 redirection avoids Windows PowerShell Out-File's default UTF-16 encoding.
    go test -json -tags=e2e ./test/e2e -run '^(TestConfigurationWorkflow|TestMockResponsesSubprocessWorkflow|TestAnthropicProviderSubprocessAvailability)$' -count=1 -timeout=5m | Out-File -FilePath $results -Encoding utf8
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Get-Content $results
    Assert-Results $results
} finally {
    Remove-Item $results -ErrorAction SilentlyContinue
}
