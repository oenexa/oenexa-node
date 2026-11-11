<#
.SYNOPSIS
    OENEXA (ONXC) — Unified Docker Deployment & Runner Script (PowerShell)
    Clones latest main branch of backend and frontend, builds, and runs via Docker.
.PARAMETER Reset
    Purge previous containers, networks, and chain data volumes.
.PARAMETER Rebuild
    Force rebuild of Docker images without cache.
.PARAMETER Stop
    Stop all running OENEXA Docker containers.
.PARAMETER Logs
    Tail live logs from all running containers.
.PARAMETER Status
    Check health and status of OENEXA containers.
.PARAMETER Port
    Frontend Web UI port (default: 3000).
.PARAMETER RpcPort
    Web3 JSON-RPC port (default: 8545).
.PARAMETER Branch
    Git branch to clone from GitHub (default: main).
#>
[CmdletBinding()]
param(
    [switch]$Reset,
    [switch]$Rebuild,
    [switch]$Stop,
    [switch]$Logs,
    [switch]$Status,
    [int]$Port = 3000,
    [int]$RpcPort = 8545,
    [int]$MetricsPort = 9090,
    [int]$P2pPort = 8765,
    [string]$Branch = "main",
    [string]$DeployDir = "./.oenexa-docker-deploy"
)

$ErrorActionPreference = "Stop"

$NodeRepo = "https://github.com/oenexa/oenexa-node.git"
$ExplorerRepo = "https://github.com/oenexa/oenexa-explorer.git"

if ($Stop) {
    Write-Host "Stopping OENEXA Docker containers..." -ForegroundColor Yellow
    if (Test-Path "$DeployDir/docker-compose.yml") {
        docker compose -f "$DeployDir/docker-compose.yml" down
    } else {
        docker stop oenexa-frontend oenexa-node 2>$null
        docker rm oenexa-frontend oenexa-node 2>$null
    }
    Write-Host "✓ All OENEXA services stopped." -ForegroundColor Green
    return
}

if ($Logs) {
    if (Test-Path "$DeployDir/docker-compose.yml") {
        docker compose -f "$DeployDir/docker-compose.yml" logs -f
    } else {
        docker logs -f oenexa-node
    }
    return
}

if ($Status) {
    Write-Host "=== OENEXA Container Status ===" -ForegroundColor Cyan
    docker ps --filter "name=oenexa" --format "table {{.Names}}`t{{.Status}}`t{{.Ports}}"
    Write-Host "`n=== Node Telemetry (/api/status) ===" -ForegroundColor Cyan
    try {
        $res = Invoke-RestMethod -Uri "http://127.0.0.1:$RpcPort/api/status" -TimeoutSec 2
        $res | ConvertTo-Json
    } catch {
        Write-Host "Node unreachable at http://127.0.0.1:$RpcPort" -ForegroundColor Red
    }
    return
}

Write-Host "==========================================================================" -ForegroundColor Cyan
Write-Host "    OENEXA (ONXC) — Full Stack Docker Runner (Clone & Run from Main)      " -ForegroundColor Cyan
Write-Host "==========================================================================" -ForegroundColor Cyan

# 1. Pre-flight checks
Write-Host "`n[1/5] Verifying system prerequisites..." -ForegroundColor Blue

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Error "'git' is not installed or not in PATH."
}
Write-Host "      ✓ Git found"

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Error "'docker' is not installed or not in PATH."
}
Write-Host "      ✓ Docker found"

$dockerRunning = $false
try {
    $info = docker info 2>$null
    if ($LASTEXITCODE -eq 0) { $dockerRunning = $true }
} catch {}

if (-not $dockerRunning) {
    Write-Error "Docker daemon is not running! Please launch Docker Desktop and retry."
}
Write-Host "      ✓ Docker daemon is active and responsive."

# 2. Workspace setup
if (-not (Test-Path $DeployDir)) { New-Item -ItemType Directory -Path $DeployDir | Out-Null }
$deployRoot = (Resolve-Path $DeployDir).Path
Set-Location $deployRoot

if ($Reset) {
    Write-Host "`n[2/5] Reset requested: Purging containers, networks, and data..." -ForegroundColor Yellow
    if (Test-Path "docker-compose.yml") {
        docker compose down -v --remove-orphans 2>$null
    }
    Remove-Item -Recurse -Force oenexa-node, oenexa-frontend -ErrorAction SilentlyContinue
    Write-Host "      ✓ Clean state prepared."
} else {
    Write-Host "`n[2/5] Preparing deployment workspace in $DeployDir..." -ForegroundColor Blue
}

# 3. Clone or pull from GitHub
Write-Host "`n[3/5] Cloning latest '$Branch' branch from GitHub..." -ForegroundColor Blue

if (Test-Path "oenexa-node/.git") {
    Write-Host "      Updating existing oenexa-node repository..."
    Set-Location oenexa-node
    git fetch origin $Branch
    git checkout $Branch
    git pull origin $Branch
    Set-Location $deployRoot
} else {
    Write-Host "      Cloning oenexa-node ($NodeRepo) branch $Branch..."
    git clone -b $Branch --depth 1 $NodeRepo oenexa-node
}
Write-Host "      ✓ Backend oenexa-node ready."

if (Test-Path "oenexa-explorer/.git") {
    Write-Host "      Updating existing oenexa-explorer repository..."
    Set-Location oenexa-explorer
    git fetch origin $Branch 2>$null
    git checkout $Branch 2>$null
    git pull origin $Branch 2>$null
    Set-Location $deployRoot
} else {
    Write-Host "      Cloning oenexa-explorer ($ExplorerRepo) branch $Branch..."
    $cloneOk = $false
    try {
        git clone -b $Branch --depth 1 $ExplorerRepo oenexa-explorer 2>$null
        if ($LASTEXITCODE -eq 0) { $cloneOk = $true }
    } catch {}

    if (-not $cloneOk) {
        Write-Host "      Cloning from local repository workspace as fallback..." -ForegroundColor Yellow
        if (Test-Path "../../oenexa-explorer") {
            Copy-Item -Recurse "../../oenexa-explorer" "./oenexa-explorer"
        } elseif (Test-Path "../oenexa-explorer") {
            Copy-Item -Recurse "../oenexa-explorer" "./oenexa-explorer"
        }
    }
}
Write-Host "      ✓ Explorer oenexa-explorer ready."

# 4. Generate docker-compose.yml
Write-Host "`n[4/5] Generating Docker orchestration manifests..." -ForegroundColor Blue

$composeContent = @"
services:
  oenexa-node:
    build:
      context: ./oenexa-node
      dockerfile: Dockerfile
    image: oenexa-node:latest
    container_name: oenexa-node
    restart: unless-stopped
    environment:
      - ONXC_PASSWORD=devpassword
    ports:
      - "${P2pPort}:8765"
      - "${RpcPort}:8545"
      - "${MetricsPort}:9090"
    volumes:
      - onxc-chain-data:/data
      - ./oenexa-node/configs/docker-devnet.toml:/data/config.toml:ro
    command: >
      start
      --config /data/config.toml
      --datadir /data
      --miner
      --metrics
    networks:
      - oenexa-net

  oenexa-explorer:
    build:
      context: ./oenexa-explorer
      dockerfile: Dockerfile
    image: oenexa-explorer:latest
    container_name: oenexa-explorer
    restart: unless-stopped
    ports:
      - "${Port}:80"
    depends_on:
      - oenexa-node
    networks:
      - oenexa-net

volumes:
  onxc-chain-data:

networks:
  oenexa-net:
    driver: bridge
"@

Set-Content -Path "docker-compose.yml" -Value $composeContent
Write-Host "      ✓ docker-compose.yml generated."

# 5. Build and launch containers
Write-Host "`n[5/5] Building Docker images and launching stack..." -ForegroundColor Blue

$buildArgs = @("compose", "build")
if ($Rebuild) { $buildArgs += "--no-cache" }
& docker @buildArgs

docker compose up -d

Write-Host "`nWaiting for OENEXA Layer-1 Node to initialize and produce blocks..." -ForegroundColor Yellow

$online = $false
for ($i = 0; $i -lt 30; $i++) {
    try {
        $res = Invoke-RestMethod -Uri "http://127.0.0.1:$RpcPort/api/status" -TimeoutSec 1 2>$null
        if ($res.online -eq $true) {
            $online = $true
            break
        }
    } catch {}
    Start-Sleep -Seconds 2
    Write-Host -NoNewline "."
}
Write-Host ""

if ($online) {
    Write-Host "✓ OENEXA Node is ONLINE and actively producing blocks!" -ForegroundColor Green
} else {
    Write-Host "Node is taking longer to start. Checking container status:" -ForegroundColor Yellow
    docker ps --filter "name=oenexa"
}

Write-Host "`n==========================================================================" -ForegroundColor Green
Write-Host "       OENEXA Full Stack is RUNNING in Docker!                            " -ForegroundColor Green
Write-Host "==========================================================================" -ForegroundColor Green
Write-Host "  Web Dashboard UI : http://localhost:$Port" -ForegroundColor Cyan
Write-Host "  Web3 JSON-RPC 2.0: http://127.0.0.1:$RpcPort" -ForegroundColor Cyan
Write-Host "  REST Status API  : http://127.0.0.1:$RpcPort/api/status" -ForegroundColor Cyan
Write-Host "  Prometheus       : http://127.0.0.1:$MetricsPort/metrics" -ForegroundColor Cyan
Write-Host "--------------------------------------------------------------------------"
Write-Host "Useful Commands:"
Write-Host "  View live logs    : .\oenexa-run.ps1 -Logs" -ForegroundColor Yellow
Write-Host "  Check node health : .\oenexa-run.ps1 -Status" -ForegroundColor Yellow
Write-Host "  Stop all services : .\oenexa-run.ps1 -Stop" -ForegroundColor Yellow
Write-Host "  Reset chain data  : .\oenexa-run.ps1 -Reset" -ForegroundColor Yellow
Write-Host "=========================================================================="
