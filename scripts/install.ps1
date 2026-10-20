#Requires -Version 5.1
<#
.SYNOPSIS
Ant Router installer for Windows.

Downloads a release archive, verifies the keyless Sigstore signature on the
release checksums.txt with cosign, verifies the archive against that file,
then installs routerd.exe and routerctl.exe together: either both binaries
are replaced or, on any failure, the previous ones are restored.

.PARAMETER Version
Release tag to install, e.g. v1.2.3 (default: latest)

.PARAMETER InstallDir
Installation directory (default: $env:ANT_ROUTER_INSTALL_DIR or $env:LOCALAPPDATA\Programs\ant-router\bin)

.PARAMETER BaseUrl
Release host base URL (default: https://github.com/abyssbugg/ant-router)

.PARAMETER NoVerifySignature
INSECURE: skip cosign signature verification (also
ANT_ROUTER_INSECURE_SKIP_SIGNATURE=1). Checksums are still verified, but they
come from the same host as the archive.

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
# Releases are signed keylessly by CircleCI's tag-only "release" pipeline
# definition (.circleci/release.yml); its URL is the certificate identity.
# The identity does not name the tag, so the signed checksums.txt is bound to
# this version by requiring the exact versioned archive name below.
$CertIdentity = 'https://circleci.com/api/v2/projects/cc5f04e8-66e9-4f18-91b0-71c671674d14/pipeline-definitions/b37a0ec3-4a25-4686-b480-6b4eda16099f'
$OidcIssuer = 'https://oidc.circleci.com'
$Binaries = @('routerd.exe', 'routerctl.exe')

function Fail {
    param([string]$Message)
    [Console]::Error.WriteLine("Error: $Message")
    exit 1
}

# Test-AllowedUri accepts https to any host, or plain http to an exact
# loopback host so local mirrors and tests do not need certificates.
# Credentials (userinfo) are always rejected.
function Test-AllowedUri {
    param([string]$Uri)
    $parsed = $null
    if (-not [Uri]::TryCreate($Uri, [UriKind]::Absolute, [ref]$parsed)) { return $false }
    if ($parsed.UserInfo) { return $false }
    if ($parsed.Scheme -ceq 'https') { return $true }
    if ($parsed.Scheme -ceq 'http') {
        return @('127.0.0.1', 'localhost', '[::1]') -contains $parsed.Host
    }
    return $false
}

$script:HttpClient = $null

# Invoke-Download follows at most 10 redirects manually, validating every hop
# with Test-AllowedUri before any request is made to it, and writes the body
# to OutFile when one is given. Returns the final URI. HttpClient is used
# instead of Invoke-WebRequest so redirects can be inspected and so Windows
# PowerShell's progress rendering does not slow large downloads.
function Invoke-Download {
    param([string]$Uri, [string]$OutFile)
    if (-not $script:HttpClient) {
        Add-Type -AssemblyName System.Net.Http
        $handler = New-Object System.Net.Http.HttpClientHandler
        $handler.AllowAutoRedirect = $false
        $script:HttpClient = New-Object System.Net.Http.HttpClient($handler)
        # Bounds the whole request, body included, so a stalled host cannot
        # hang the installer.
        $script:HttpClient.Timeout = [TimeSpan]::FromMinutes(10)
    }
    $current = $Uri
    for ($hop = 0; $hop -le 10; $hop++) {
        if (-not (Test-AllowedUri $current)) {
            Fail "refusing to download from $current (only https, or http to loopback, without credentials, is allowed)"
        }
        try {
            $response = $script:HttpClient.GetAsync($current).GetAwaiter().GetResult()
        } catch {
            Fail "failed to download ${current}: $($_.Exception.GetBaseException().Message)"
        }
        try {
            $code = [int]$response.StatusCode
            if ($code -ge 200 -and $code -lt 300) {
                if ($OutFile) {
                    $bytes = $response.Content.ReadAsByteArrayAsync().GetAwaiter().GetResult()
                    [IO.File]::WriteAllBytes($OutFile, $bytes)
                }
                return $current
            }
            if (@(301, 302, 303, 307, 308) -contains $code) {
                $location = $response.Headers.Location
                if (-not $location) { Fail "redirect without a Location header from $current" }
                # Decide from the raw header text: on Unix, .NET can parse a
                # relative '/path' as an absolute file:// URI.
                $target = $location.OriginalString
                if ($target -notmatch '^[A-Za-z][A-Za-z0-9+.-]*:') {
                    $target = (New-Object Uri((New-Object Uri($current)), $target)).AbsoluteUri
                }
                $current = $target
                continue
            }
            Fail "HTTP $code from $current"
        } finally {
            $response.Dispose()
        }
    }
    Fail "too many redirects downloading $Uri"
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
if (-not (Test-AllowedUri $BaseUrl)) {
    Fail "base URL must use https (http is allowed only for loopback) and must not contain credentials: $BaseUrl"
}
$VerifySignature = -not ($NoVerifySignature -or $env:ANT_ROUTER_INSECURE_SKIP_SIGNATURE -ceq '1')

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
    # The tag comes from the final redirect target, never from the body.
    $finalUri = Invoke-Download "$BaseUrl/releases/latest" $null
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

if ($VerifySignature) {
    $cosign = Get-Command cosign -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $cosign) {
        Fail 'cosign is required to verify the release signature; install it (https://docs.sigstore.dev/cosign/system_config/installation/) or rerun with -NoVerifySignature to accept the risk'
    }
} else {
    [Console]::Error.WriteLine('WARNING: signature verification is disabled; checksums.txt is not authenticated and only guards against corrupted downloads.')
}

$tempDir = Join-Path ([IO.Path]::GetTempPath()) ('ant-router-install-' + [Guid]::NewGuid().ToString('N'))
$staged = @()
$backups = @{}
$replaced = @()
try {
    $null = New-Item -ItemType Directory -Path $tempDir

    $archivePath = Join-Path $tempDir $archive
    $checksumsPath = Join-Path $tempDir 'checksums.txt'
    $bundlePath = Join-Path $tempDir 'checksums.txt.sigstore.json'

    Write-Host "Downloading $archive..."
    $null = Invoke-Download "$releaseUrl/$archive" $archivePath
    $null = Invoke-Download "$releaseUrl/checksums.txt" $checksumsPath

    if ($VerifySignature) {
        Write-Host 'Verifying checksums.txt signature...'
        $null = Invoke-Download "$releaseUrl/checksums.txt.sigstore.json" $bundlePath
        & $cosign.Source verify-blob `
            --bundle $bundlePath `
            --certificate-identity $CertIdentity `
            --certificate-oidc-issuer $OidcIssuer `
            $checksumsPath
        if ($LASTEXITCODE -ne 0) {
            Fail 'signature verification failed'
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
            [IO.Compression.ZipFileExtensions]::ExtractToFile($entries[0], (Join-Path $extractDir $bin))
        }
    } catch {
        Fail "failed to extract binaries from ${archive}: $($_.Exception.Message)"
    } finally {
        if ($zip) { $zip.Dispose() }
    }

    if (-not (Test-Path -LiteralPath $InstallDir -PathType Container)) {
        $null = New-Item -ItemType Directory -Path $InstallDir
    }

    # A directory at the destination would make Move-Item nest the binary
    # inside it and report success without installing anything.
    foreach ($bin in $Binaries) {
        if (Test-Path -LiteralPath (Join-Path $InstallDir $bin) -PathType Container) {
            Fail "$(Join-Path $InstallDir $bin) is a directory"
        }
    }

    # Backups left by an earlier upgrade whose old executable was still
    # running can be removed now; ignore any that are still locked.
    foreach ($bin in $Binaries) {
        Get-ChildItem -LiteralPath $InstallDir -Filter ".$bin.old.*" -Force -ErrorAction SilentlyContinue |
            Remove-Item -Force -ErrorAction SilentlyContinue
    }

    # Stage both binaries next to their destination before touching either.
    foreach ($bin in $Binaries) {
        $target = Join-Path $InstallDir ".$bin.tmp.$PID"
        try {
            Copy-Item -LiteralPath (Join-Path $extractDir $bin) -Destination $target
        } catch {
            Fail "failed to stage ${bin}: $($_.Exception.Message)"
        }
        $staged += $target
    }

    # Windows refuses to overwrite or delete a running executable but does
    # allow renaming it, so each existing binary is first renamed aside; a
    # running routerd.exe keeps executing from the renamed file until it is
    # restarted. Any failure restores the previous pair.
    try {
        foreach ($bin in $Binaries) {
            $dest = Join-Path $InstallDir $bin
            if (Test-Path -LiteralPath $dest) {
                $backup = Join-Path $InstallDir ".$bin.old.$PID"
                Move-Item -LiteralPath $dest -Destination $backup -Force
                $backups[$bin] = $backup
            }
            Move-Item -LiteralPath (Join-Path $InstallDir ".$bin.tmp.$PID") -Destination $dest
            $replaced += $bin
        }
    } catch {
        $reason = $_.Exception.Message
        foreach ($bin in $Binaries) {
            $dest = Join-Path $InstallDir $bin
            if ($replaced -contains $bin) {
                Remove-Item -LiteralPath $dest -Force -ErrorAction SilentlyContinue
            }
            if ($backups.ContainsKey($bin)) {
                try {
                    Move-Item -LiteralPath $backups[$bin] -Destination $dest -Force
                } catch {
                    [Console]::Error.WriteLine("Error: could not restore $dest from $($backups[$bin]): $($_.Exception.Message)")
                }
            }
        }
        Fail "failed to install binaries: $reason"
    }

    $restartNeeded = $false
    foreach ($bin in $Binaries) {
        Write-Host "Installed $(Join-Path $InstallDir $bin)"
        if ($backups.ContainsKey($bin)) {
            try {
                Remove-Item -LiteralPath $backups[$bin] -Force
            } catch {
                # Still executing; removed by the next install.
                $restartNeeded = $true
            }
        }
    }
    if ($restartNeeded) {
        Write-Host ''
        Write-Host 'Note: a previous ant-router binary is still running. Restart routerd to use the new version.'
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
    if ($script:HttpClient) {
        $script:HttpClient.Dispose()
    }
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
