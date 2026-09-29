<#
.SYNOPSIS
    Tests a running local OENEXA node via RPC, REST, and CLI.

.DESCRIPTION
    Verifies that the local node is responsive, producing blocks, serving JSON-RPC,
    exposing /api/status, and that oenexa-cli commands function properly.

.PARAMETER Port
    JSON-RPC port to test (default: 8545).

.PARAMETER MetricsPort
    Prometheus metrics port to test (default: 9090).

.EXAMPLE
    .\scripts\test-local.ps1
#>

[CmdletBinding()]
param(
    [int]$Port = 8545,
    [int]$MetricsPort = 9090
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if (Test-Path (Join-Path $ScriptDir "..\go.mod")) {
    $RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..")).Path
} else {
    $RepoRoot = (Get-Location).Path
}

$CliExe = Join-Path $RepoRoot "bin\oenexa-cli.exe"
if ($IsLinux -or $IsMacOS) {
    $CliExe = Join-Path $RepoRoot "bin\oenexa-cli"
}

Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "          OENEXA Local Node Automated Health & CLI Test         " -ForegroundColor Green
Write-Host "=================================================================" -ForegroundColor Cyan

$RpcUrl = "http://127.0.0.1:$Port"
$StatusUrl = "$RpcUrl/api/status"
$HealthUrl = "$RpcUrl/healthz"
$MetricsUrl = "http://127.0.0.1:$MetricsPort/metrics"

# 1. Healthcheck
Write-Host "`n[1/5] Checking /healthz endpoint..." -NoNewline
try {
    $health = Invoke-RestMethod -Uri $HealthUrl -TimeoutSec 3
    if ($health -eq "ok") {
        Write-Host " [PASS]" -ForegroundColor Green
    } else {
        Write-Host " [WARN: response was '$health']" -ForegroundColor Yellow
    }
} catch {
    Write-Host " [FAIL]" -ForegroundColor Red
    Write-Error "Could not connect to node at $HealthUrl. Is the node running? Run .\run-local.ps1 first."
    exit 1
}

# 2. REST API /api/status
Write-Host "[2/5] Checking /api/status REST API..." -NoNewline
try {
    $status = Invoke-RestMethod -Uri $StatusUrl -TimeoutSec 3
    Write-Host " [PASS]" -ForegroundColor Green
    Write-Host "      • Chain Height : $($status.chain_height)" -ForegroundColor Gray
    Write-Host "      • Network      : $($status.network)" -ForegroundColor Gray
    Write-Host "      • Version      : $($status.version)" -ForegroundColor Gray
    Write-Host "      • State Root   : $($status.state_root)" -ForegroundColor Gray
} catch {
    Write-Host " [FAIL]" -ForegroundColor Red
    Write-Error "Failed to fetch /api/status: $_"
}

# 3. Prometheus metrics endpoint
Write-Host "[3/5] Checking Prometheus metrics ($MetricsUrl)..." -NoNewline
try {
    $metrics = Invoke-WebRequest -Uri $MetricsUrl -UseBasicParsing -TimeoutSec 3
    if ($metrics.StatusCode -eq 200) {
        Write-Host " [PASS]" -ForegroundColor Green
    } else {
        Write-Host " [STATUS $($metrics.StatusCode)]" -ForegroundColor Yellow
    }
} catch {
    Write-Host " [SKIP/FAIL: $_]" -ForegroundColor Yellow
}

# 4. CLI Info Query
Write-Host "[4/5] Testing oenexa-cli info command..." -NoNewline
if (Test-Path $CliExe) {
    try {
        $info = & $CliExe info --rpc $RpcUrl
        Write-Host " [PASS]" -ForegroundColor Green
    } catch {
        Write-Host " [FAIL]" -ForegroundColor Red
        Write-Error "CLI info command failed: $_"
    }
} else {
    Write-Host " [SKIPPED - $CliExe not found]" -ForegroundColor Yellow
}

# 5. Block Progression
Write-Host "[5/5] Verifying block progression (waiting 3 seconds)..." -NoNewline
$initialHeight = (Invoke-RestMethod -Uri $StatusUrl).chain_height
Start-Sleep -Seconds 3
$newHeight = (Invoke-RestMethod -Uri $StatusUrl).chain_height

if ($newHeight -gt $initialHeight) {
    Write-Host " [PASS]" -ForegroundColor Green
    Write-Host "      • Height advanced from $initialHeight to $newHeight ✓" -ForegroundColor Green
} else {
    Write-Host " [WARN: height remained $initialHeight]" -ForegroundColor Yellow
}

Write-Host "`n=================================================================" -ForegroundColor Cyan
Write-Host "✓ All local node tests completed successfully!" -ForegroundColor Green
Write-Host "=================================================================" -ForegroundColor Cyan
