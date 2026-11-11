<#
.SYNOPSIS
    Starts a local OENEXA devnet node and builds required binaries if needed.

.DESCRIPTION
    Builds oenexa-node and oenexa-cli (if missing or if -Rebuild is passed),
    configures a local single-node blockchain network with block production,
    serves JSON-RPC (port 8545), REST API (/api/status), and Prometheus metrics (port 9090).

.PARAMETER Reset
    Wipes previous local blockchain state (data/local-devnet) to start fresh.

.PARAMETER Rebuild
    Forces recompilation of oenexa-node and oenexa-cli binaries.

.PARAMETER NoMiner
    Disables local block production (node runs as pure observer).

.PARAMETER Port
    Overrides the JSON-RPC listen port (default: 8545).

.PARAMETER MetricsPort
    Overrides the Prometheus metrics port (default: 9090).

.PARAMETER Password
    Keystore password for the local validator wallet (default: devpassword).

.EXAMPLE
    .\run-local.ps1
    .\run-local.ps1 -Reset
    .\run-local.ps1 -Rebuild
#>

[CmdletBinding()]
param(
    [switch]$Reset,
    [switch]$Rebuild,
    [switch]$NoMiner,
    [int]$Port = 8545,
    [int]$MetricsPort = 9090,
    [string]$Password = "devpassword"
)

$ErrorActionPreference = "Stop"

# Determine repository root
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if (Test-Path (Join-Path $ScriptDir "..\go.mod")) {
    $RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..")).Path
} elseif (Test-Path (Join-Path $ScriptDir "go.mod")) {
    $RepoRoot = (Resolve-Path $ScriptDir).Path
} else {
    $RepoRoot = (Get-Location).Path
}

Set-Location $RepoRoot

$BinDir = Join-Path $RepoRoot "bin"
$DataDir = Join-Path $RepoRoot "data\local-devnet"
$ConfigPath = Join-Path $RepoRoot "configs\local-dev.toml"
$NodeExe = Join-Path $BinDir "oenexa-node.exe"
$CliExe = Join-Path $BinDir "oenexa-cli.exe"

# If on non-Windows environment with PowerShell Core
if ($IsLinux -or $IsMacOS) {
    $NodeExe = Join-Path $BinDir "oenexa-node"
    $CliExe = Join-Path $BinDir "oenexa-cli"
}

Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "       OENEXA (ONXC) — Local Development Network Launcher       " -ForegroundColor Green
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host "Repository : $RepoRoot" -ForegroundColor Gray
Write-Host "Config     : $ConfigPath" -ForegroundColor Gray
Write-Host "Data Dir   : $DataDir" -ForegroundColor Gray

# 1. Reset state if requested
if ($Reset) {
    Write-Host "`n[1/3] Resetting local chain state ($DataDir)..." -ForegroundColor Yellow
    if (Test-Path $DataDir) {
        Remove-Item -Recurse -Force $DataDir
        Write-Host "      ✓ Previous chain data purged." -ForegroundColor Green
    } else {
        Write-Host "      ✓ No existing chain data found." -ForegroundColor Gray
    }
} else {
    Write-Host "`n[1/3] Checking local state..." -ForegroundColor Gray
    if (Test-Path $DataDir) {
        Write-Host "      ✓ Existing chain data found (use -Reset to start fresh)." -ForegroundColor Gray
    }
}

# 2. Check and build binaries
$NeedBuild = $Rebuild -or (-not (Test-Path $NodeExe)) -or (-not (Test-Path $CliExe))

if ($NeedBuild) {
    Write-Host "`n[2/3] Building node and CLI binaries..." -ForegroundColor Yellow
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Error "Go compiler ('go') not found in PATH. Please install Go (https://go.dev/dl/)."
        exit 1
    }

    if (-not (Test-Path $BinDir)) {
        New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    }

    Write-Host "      Compiling oenexa-node..." -ForegroundColor Gray
    go build -trimpath -o $NodeExe ./cmd/node
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Failed to build oenexa-node"
        exit 1
    }

    Write-Host "      Compiling oenexa-cli..." -ForegroundColor Gray
    go build -trimpath -o $CliExe ./cmd/oenexa-cli
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Failed to build oenexa-cli"
        exit 1
    }

    Write-Host "      ✓ Binaries compiled successfully into bin/" -ForegroundColor Green
} else {
    Write-Host "`n[2/3] Binaries verified in bin/ (use -Rebuild to recompile)." -ForegroundColor Gray
}

# 3. Ensure local config exists
if (-not (Test-Path $ConfigPath)) {
    Write-Host "`nGenerating default configs/local-dev.toml..." -ForegroundColor Yellow
    $DefaultConfig = @"
[chain]
network_id = 1337
name       = "OENEXA Local Devnet"

[node]
data_dir      = "data/local-devnet"
keystore_file = "keystore.json"
miner_enabled = true
log_level     = "info"

[p2p]
listen_addr     = "127.0.0.1:8765"
external_addr   = "127.0.0.1:8765"
max_peers       = 10
bootstrap_peers = []

[rpc]
enabled       = true
listen_addr   = "127.0.0.1:8545"
cors_origins  = ["*"]
read_timeout  = "30s"
write_timeout = "30s"

[storage]
blocks_dir = "blocks"
state_dir  = "state"

[metrics]
enabled     = true
listen_addr = "127.0.0.1:9090"

[upgrade]
release_url    = ""
check_interval = "24h"
auto_apply     = false
"@
    Set-Content -Path $ConfigPath -Value $DefaultConfig -Encoding utf8
}

# Prepare runtime arguments
$ArgsList = @("start", "--config", $ConfigPath)
if (-not $NoMiner) {
    $ArgsList += "--miner"
}
if ($Port -ne 8545) {
    $ArgsList += @("--rpc-listen", "127.0.0.1:$Port")
}
if ($MetricsPort -ne 9090) {
    $ArgsList += @("--metrics-addr", "127.0.0.1:$MetricsPort")
}

$env:ONXC_PASSWORD = $Password

Write-Host "`n[3/3] Starting OENEXA Local Full Node..." -ForegroundColor Green
Write-Host "-----------------------------------------------------------------" -ForegroundColor Cyan
Write-Host "  Web3 JSON-RPC   : http://127.0.0.1:$Port" -ForegroundColor White
Write-Host "  REST API Status : http://127.0.0.1:$Port/api/status" -ForegroundColor White
Write-Host "  Health Endpoint : http://127.0.0.1:$Port/healthz" -ForegroundColor White
Write-Host "  Prometheus      : http://127.0.0.1:$MetricsPort/metrics" -ForegroundColor White
Write-Host "  Validator Mode  : $(if (-not $NoMiner) {'Active (Producing Blocks)'} else {'Standby'})" -ForegroundColor White
Write-Host "-----------------------------------------------------------------" -ForegroundColor Cyan
Write-Host "Quick commands to run in a separate terminal:" -ForegroundColor Yellow
Write-Host "  .\bin\oenexa-cli.exe info" -ForegroundColor Gray
Write-Host "  .\bin\oenexa-cli.exe keygen" -ForegroundColor Gray
Write-Host "  Invoke-RestMethod http://127.0.0.1:$Port/api/status" -ForegroundColor Gray
Write-Host "-----------------------------------------------------------------" -ForegroundColor Cyan
Write-Host "Press Ctrl+C to stop the node gracefully.`n" -ForegroundColor DarkGray

& $NodeExe @ArgsList
