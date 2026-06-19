param (
    [switch]$Elevated,
    # Pin a specific git ref (release tag) to fetch the upgrade script from.
    # Defaults to the latest published release tag. NEVER falls back to a moving
    # branch like 'main'.
    [string]$Ref = "",
    # Optional: expected SHA-256 of upgrade-agent.ps1. When provided, the wrapper
    # verifies the downloaded script before executing it and aborts on mismatch.
    [string]$ExpectedSha256 = ""
)

# Vigil Agent Upgrade Wrapper
# Downloads and executes the upgrade orchestrator (upgrade-agent.ps1) from an
# immutable release tag. The orchestrator self-elevates and updates the NSSM
# service, so fetching it from a mutable ref would be a remote-code-execution
# risk; we therefore pin to a tag and support optional checksum verification.

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

# Force TLS 1.2 (older Windows PowerShell defaults to TLS 1.0/1.1).
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {
    # Ignore on platforms where SecurityProtocol is not configurable (PS Core).
}

$repo = "Gu1llaum-3/vigil"

function Resolve-LatestTag {
    $apiUrl = "https://api.github.com/repos/$repo/releases/latest"
    try {
        $release = Invoke-RestMethod -Uri $apiUrl -UseBasicParsing -Headers @{ "User-Agent" = "vigil-upgrade-wrapper" }
        return $release.tag_name
    } catch {
        return $null
    }
}

$tempScriptPath = $null

try {
    Write-Host "Vigil Agent Upgrade Wrapper" -ForegroundColor Cyan
    Write-Host "============================" -ForegroundColor Cyan
    Write-Host ""

    # Resolve the ref to pin to.
    if (-not $Ref) {
        Write-Host "Resolving the latest release tag..." -ForegroundColor Yellow
        $Ref = Resolve-LatestTag
        if (-not $Ref) {
            Write-Host "ERROR: Could not resolve the latest release tag from GitHub." -ForegroundColor Red
            Write-Host "Pass an explicit release tag with -Ref <tag> (e.g. -Ref v0.2.0)." -ForegroundColor Red
            exit 1
        }
    }
    Write-Host "Using ref: $Ref" -ForegroundColor Green

    # raw.githubusercontent.com content for a tag is immutable for that tag.
    $scriptUrl = "https://raw.githubusercontent.com/$repo/$Ref/supplemental/scripts/upgrade-agent.ps1"
    $tempScriptPath = Join-Path $env:TEMP ("vigil-upgrade-agent-" + [guid]::NewGuid().ToString() + ".ps1")

    Write-Host "Downloading upgrade script..." -ForegroundColor Yellow
    Write-Host "From: $scriptUrl"
    Write-Host "To: $tempScriptPath"

    try {
        Invoke-WebRequest -Uri $scriptUrl -OutFile $tempScriptPath -UseBasicParsing
        Write-Host "Download completed successfully." -ForegroundColor Green
    }
    catch {
        Write-Host "Failed to download upgrade script: $($_.Exception.Message)" -ForegroundColor Red
        Write-Host "Please check your internet connection and the -Ref value, then try again." -ForegroundColor Red
        exit 1
    }

    if (-not (Test-Path $tempScriptPath)) {
        Write-Host "ERROR: Downloaded script not found at $tempScriptPath" -ForegroundColor Red
        exit 1
    }

    # Always show the hash so a careful operator can compare it; verify when an
    # expected value was supplied.
    $actualHash = (Get-FileHash -Path $tempScriptPath -Algorithm SHA256).Hash.ToUpperInvariant()
    Write-Host "Downloaded script SHA-256: $actualHash" -ForegroundColor Cyan

    if ($ExpectedSha256) {
        if ($actualHash -ne $ExpectedSha256.ToUpperInvariant()) {
            Write-Host "ERROR: Checksum mismatch. Refusing to execute the downloaded script." -ForegroundColor Red
            Write-Host "  Expected: $($ExpectedSha256.ToUpperInvariant())" -ForegroundColor Red
            Write-Host "  Actual:   $actualHash" -ForegroundColor Red
            Remove-Item $tempScriptPath -Force -ErrorAction SilentlyContinue
            exit 1
        }
        Write-Host "Checksum verified." -ForegroundColor Green
    }

    Write-Host ""
    Write-Host "Executing upgrade script..." -ForegroundColor Yellow

    if ($Elevated) {
        & $tempScriptPath -Elevated
    } else {
        & $tempScriptPath
    }

    $scriptExitCode = $LASTEXITCODE

    Write-Host ""
    Write-Host "Cleaning up temporary files..." -ForegroundColor Yellow

    try {
        Remove-Item $tempScriptPath -Force -ErrorAction SilentlyContinue
        Write-Host "Cleanup completed." -ForegroundColor Green
    }
    catch {
        Write-Host "Warning: Could not remove temporary script: $tempScriptPath" -ForegroundColor Yellow
    }

    exit $scriptExitCode
}
catch {
    Write-Host "ERROR: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host "Upgrade wrapper failed. Please check the error message above." -ForegroundColor Red

    if ($tempScriptPath -and (Test-Path $tempScriptPath)) {
        try {
            Remove-Item $tempScriptPath -Force -ErrorAction SilentlyContinue
        }
        catch {
            # Ignore cleanup errors
        }
    }

    exit 1
}
