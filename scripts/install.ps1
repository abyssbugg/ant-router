#Requires -Version 5.1
<#
.SYNOPSIS
Ant Router installer for Windows.

Downloads a release archive, verifies it against the release checksums.txt
(and, when cosign is available, verifies the keyless signature on
checksums.txt), then atomically installs routerd.exe and routerctl.exe.

.PARAMETER Version
Release tag to install, e.g. v1.2.3 (default: latest)

.PARAMETER InstallDir
Installation directory (default: $env:ANT_ROUTER_INSTALL_DIR or $env:LOCALAPPDATA\Programs\ant-router\bin)

.PARAMETER BaseUrl
Release host base URL (default: https://github.com/abyssbugg/ant-router)

.PARAMETER NoVerifySignature
Skip cosign signature verification (checksums are always verified)

.PARAMETER DryRun
Print what would be installed and change nothing
#>
[CmdletBinding()]
param(
    [string]$Version,
    [string]$InstallDir,
    [string]$BaseUrl = 'https://github.com/abyssbugg/ant-router',
    [switch]$NoVerifySignature,
    [switch]$DryRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$RepoUrl = 'https://github.com/abyssbugg/ant-router'
$CertIdentityPrefix = "$RepoUrl/.github/workflows/release.yml@refs/tags/"
$OidcIssuer = 'https://token.actions.githubusercontent.com'
$Binaries = @('routerd.exe', 'routerctl.exe')

function Fail {
    param([string]$Message)
    [Console]::Error.WriteLine("Error: $Message")
    exit 1
}

function Invoke-Download {
    param([string]$Uri, [string]$OutFile)
    Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $OutFile
}

# Windows PowerShell 5.1 may default to TLS 1.0/1.1; GitHub requires 1.2+.
if ($PSVersionTable.PSVersion.Major -lt 6) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
}

if (-not $InstallDir) {
    if ($env:ANT_ROUTER_INSTALL_DIR) {
        $InstallDir = $env:ANT_ROUTER_INSTALL_DIR
    } elseif ($env:LOCALAPPDATA) {
        $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\ant-router\bin'
    } else {
        Fail 'install directory is empty; pass -InstallDir or set ANT_ROUTER_INSTALL_DIR'
    }
}

# Only HTTPS release hosts are accepted. Plain HTTP is allowed solely for
# loopback hosts so local mirrors and tests do not need certificates.
$BaseUrl = $BaseUrl.TrimEnd('/')
$parsedBase = $null
if (-not [Uri]::TryCreate($BaseUrl, [UriKind]::Absolute, [ref]$parsedBase)) {
    Fail "base URL is not an absolute URL: $BaseUrl"
}
$allowed = $parsedBase.Scheme -eq 'https'
if (-not $allowed -and $parsedBase.Scheme -eq 'http') {
    $allowed = @('127.0.0.1', 'localhost', '[::1]') -contains $parsedBase.Host
}
if (-not $allowed) {
    Fail "base URL must use https (http is allowed only for loopback): $BaseUrl"
}

$archRaw = $env:PROCESSOR_ARCHITEW6432
if (-not $archRaw) {
    $archRaw = $env:PROCESSOR_ARCHITECTURE
}
switch ($archRaw) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { Fail "unsupported architecture: $archRaw" }
}

if (-not $Version) {
    Write-Host 'Resolving latest release...'
    try {
        $resp = Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/releases/latest" -MaximumRedirection 10
    } catch {
        Fail "could not resolve the latest release: $($_.Exception.Message)"
    }
    # The tag comes from the final redirect target, never from the body.
    # Windows PowerShell exposes HttpWebResponse.ResponseUri; PowerShell 7
    # exposes HttpResponseMessage.RequestMessage.RequestUri.
    $base = $resp.BaseResponse
    if ($base.PSObject.Properties['ResponseUri']) {
        $finalUri = $base.ResponseUri.AbsoluteUri
    } else {
        $finalUri = $base.RequestMessage.RequestUri.AbsoluteUri
    }
    $marker = '/releases/tag/'
    $index = $finalUri.IndexOf($marker)
    if ($index -lt 0) {
        Fail "could not resolve the latest release from: $finalUri"
    }
    $Version = $finalUri.Substring($index + $marker.Length)
}

if ($Version -cnotmatch '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$') {
    Fail "invalid version: $Version (expected a tag like v1.2.3)"
}

$archive = "ant-router_$($Version.Substring(1))_windows_$arch.zip"
$releaseUrl = "$BaseUrl/releases/download/$Version"

Write-Host "Installing ant-router $Version for windows/$arch into $InstallDir"
if ($DryRun) {
    Write-Host "[dry run] would download $releaseUrl/$archive"
    Write-Host "[dry run] would verify $releaseUrl/checksums.txt"
    Write-Host "[dry run] would install $($Binaries -join ' and ') into $InstallDir"
    exit 0
}

$tempDir = Join-Path ([IO.Path]::GetTempPath()) ('ant-router-install-' + [Guid]::NewGuid().ToString('N'))
$staged = @()
try {
    $null = New-Item -ItemType Directory -Path $tempDir

    $archivePath = Join-Path $tempDir $archive
    $checksumsPath = Join-Path $tempDir 'checksums.txt'
    $bundlePath = Join-Path $tempDir 'checksums.txt.sigstore.json'

    Write-Host "Downloading $archive..."
    try { Invoke-Download "$releaseUrl/$archive" $archivePath } catch { Fail "failed to download $archive" }
    try { Invoke-Download "$releaseUrl/checksums.txt" $checksumsPath } catch { Fail 'failed to download checksums.txt' }

    if (-not $NoVerifySignature) {
        $cosign = Get-Command cosign -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($cosign) {
            Write-Host 'Verifying checksums.txt signature...'
            try { Invoke-Download "$releaseUrl/checksums.txt.sigstore.json" $bundlePath } catch { Fail 'failed to download checksums.txt.sigstore.json' }
            & $cosign.Source verify-blob `
                --bundle $bundlePath `
                --certificate-identity "$CertIdentityPrefix$Version" `
                --certificate-oidc-issuer $OidcIssuer `
                $checksumsPath
            if ($LASTEXITCODE -ne 0) {
                Fail 'signature verification failed'
            }
        } else {
            Write-Host 'Note: cosign not found; skipping signature verification (checksum is still verified).'
        }
    }

    Write-Host 'Verifying checksum...'
    # Exact filename match on the second field; exactly one well-formed line.
    $matched = @()
    foreach ($line in [IO.File]::ReadAllLines($checksumsPath)) {
        $fields = @($line.Trim() -split '\s+')
        if ($fields.Count -lt 2) { continue }
        $name = $fields[1]
        if ($name.StartsWith('*')) { $name = $name.Substring(1) }
        if ($name -ceq $archive) { $matched += $fields[0] }
    }
    if ($matched.Count -eq 0) {
        Fail "no checksum for $archive in checksums.txt"
    }
    if ($matched.Count -ne 1 -or $matched[0] -cnotmatch '^[0-9a-f]{64}$') {
        Fail "malformed or duplicate checksum entry for $archive"
    }
    $expected = $matched[0]
    $actual = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -cne $expected) {
        Fail "checksum mismatch for $archive (expected $expected, got $actual)"
    }

    # Read only the two expected entries; nothing else in the archive is written.
    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $extractDir = Join-Path $tempDir 'extract'
    $null = New-Item -ItemType Directory -Path $extractDir
    $zip = $null
    try {
        $zip = [IO.Compression.ZipFile]::OpenRead($archivePath)
        foreach ($bin in $Binaries) {
            $entries = @($zip.Entries | Where-Object { $_.FullName -ceq $bin })
            if ($entries.Count -ne 1) {
                Fail "failed to extract $bin from $archive"
            }
            [IO.Compression.ZipFileExtensions]::ExtractToFile($entries[0], (Join-Path $extractDir $bin), $false)
        }
    } catch {
        Fail "failed to extract binaries from ${archive}: $($_.Exception.Message)"
    } finally {
        if ($zip) { $zip.Dispose() }
    }

    if (-not (Test-Path -LiteralPath $InstallDir -PathType Container)) {
        $null = New-Item -ItemType Directory -Path $InstallDir
    }

    # Stage both binaries next to their destination, then move each into
    # place so a failure never leaves a partially written binary.
    try {
        foreach ($bin in $Binaries) {
            $target = Join-Path $InstallDir ".$bin.tmp.$PID"
            Copy-Item -LiteralPath (Join-Path $extractDir $bin) -Destination $target
            $staged += $target
        }
        foreach ($bin in $Binaries) {
            Move-Item -LiteralPath (Join-Path $InstallDir ".$bin.tmp.$PID") -Destination (Join-Path $InstallDir $bin) -Force
            Write-Host "Installed $(Join-Path $InstallDir $bin)"
        }
    } catch {
        Fail "failed to install binaries: $($_.Exception.Message)"
    }

    $pathEntries = @(([string]$env:PATH) -split [IO.Path]::PathSeparator | ForEach-Object { $_.TrimEnd('\', '/') })
    if ($pathEntries -notcontains $InstallDir.TrimEnd('\', '/')) {
        Write-Host ''
        Write-Host "Note: $InstallDir is not on your PATH. Add it for this session with:"
        Write-Host "  `$env:PATH = '$InstallDir' + [IO.Path]::PathSeparator + `$env:PATH"
    }

    Write-Host 'Installation complete.'
} finally {
    foreach ($file in $staged) {
        Remove-Item -LiteralPath $file -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
