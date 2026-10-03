#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# OENEXA (OEN) — Unified Docker Deployment & Runner Script
# Clones latest main branch of backend and frontend, builds, and runs via Docker.
# ─────────────────────────────────────────────────────────────────────────────
set -e
export GIT_TERMINAL_PROMPT=0

# Terminal colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Default parameters
NODE_REPO="https://github.com/oenexa/oenexa-node.git"
FRONTEND_REPO="https://github.com/oenexa/oenexa-frontend.git"
FRONTEND_FALLBACK_REPO="https://github.com/qbc-qubitscoin/oenexa-frontend.git"
BRANCH="main"
FRONTEND_PORT=3000
RPC_PORT=8545
METRICS_PORT=9090
P2P_PORT=8765
DEPLOY_DIR="./.oenexa-docker-deploy"
RESET=false
REBUILD=false
ACTION="run"

# Display usage
usage() {
    cat << EOF
Usage: ./oenexa-run.sh [OPTIONS]

Options:
  --reset          Remove previous containers, networks, and chain data volumes
  --rebuild        Force rebuild of Docker images without cache
  --stop           Stop running OENEXA Docker containers
  --logs           Tail live logs from all running containers
  --status         Check health and status of OENEXA containers
  --port <port>    Frontend Web UI port (default: 3000)
  --rpc-port <p>   Web3 JSON-RPC port (default: 8545)
  --branch <b>     Git branch to clone from GitHub (default: main)
  --help           Show this help message

Examples:
  ./oenexa-run.sh               # Clone main branch, build with Docker, and run all services
  ./oenexa-run.sh --rebuild     # Pull latest main updates and rebuild Docker images
  ./oenexa-run.sh --reset       # Reset all chain data and restart fresh
  ./oenexa-run.sh --stop        # Stop all running containers
  ./oenexa-run.sh --logs        # View container logs
EOF
    exit 0
}

# Parse CLI arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --reset)
            RESET=true
            shift
            ;;
        --rebuild)
            REBUILD=true
            shift
            ;;
        --stop)
            ACTION="stop"
            shift
            ;;
        --logs)
            ACTION="logs"
            shift
            ;;
        --status)
            ACTION="status"
            shift
            ;;
        --port)
            FRONTEND_PORT="$2"
            shift 2
            ;;
        --rpc-port)
            RPC_PORT="$2"
            shift 2
            ;;
        --branch)
            BRANCH="$2"
            shift 2
            ;;
        --help|-h)
            usage
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            usage
            ;;
    esac
done

# Action: STOP
if [ "$ACTION" = "stop" ]; then
    echo -e "${YELLOW}Stopping OENEXA Docker containers...${NC}"
    if [ -f "$DEPLOY_DIR/docker-compose.yml" ]; then
        docker compose -f "$DEPLOY_DIR/docker-compose.yml" down
    else
        docker stop oenexa-frontend oenexa-node 2>/dev/null || true
        docker rm oenexa-frontend oenexa-node 2>/dev/null || true
    fi
    echo -e "${GREEN}✓ All OENEXA services stopped.${NC}"
    exit 0
fi

# Action: LOGS
if [ "$ACTION" = "logs" ]; then
    if [ -f "$DEPLOY_DIR/docker-compose.yml" ]; then
        docker compose -f "$DEPLOY_DIR/docker-compose.yml" logs -f
    else
        docker logs -f oenexa-node
    fi
    exit 0
fi

# Action: STATUS
if [ "$ACTION" = "status" ]; then
    echo -e "${CYAN}=== OENEXA Container Status ===${NC}"
    if [ -f "$DEPLOY_DIR/docker-compose.yml" ]; then
        docker compose -f "$DEPLOY_DIR/docker-compose.yml" ps
    else
        docker ps --filter "name=oenexa"
    fi
    echo ""
    echo -e "${CYAN}=== Node Telemetry (/api/status) ===${NC}"
    curl -s --connect-timeout 2 "http://127.0.0.1:$RPC_PORT/api/status" || echo "Node unreachable at http://127.0.0.1:$RPC_PORT"
    echo ""
    exit 0
fi

echo -e "${CYAN}${BOLD}"
echo "=========================================================================="
echo "    OENEXA (OEN) — Full Stack Docker Runner (Clone & Run from Main)       "
echo "=========================================================================="
echo -e "${NC}"

# Step 1: Pre-flight tool verification
echo -e "${BLUE}[1/5] Verifying system prerequisites...${NC}"

if ! command -v git &>/dev/null; then
    echo -e "${RED}Error: 'git' is not installed or not in PATH.${NC}"
    exit 1
fi
echo "      ✓ Git found: $(git --version)"

if ! command -v docker &>/dev/null; then
    echo -e "${RED}Error: 'docker' is not installed or not in PATH.${NC}"
    exit 1
fi
echo "      ✓ Docker found: $(docker --version)"

if ! docker compose version &>/dev/null; then
    echo -e "${RED}Error: 'docker compose' plugin is not available.${NC}"
    exit 1
fi
echo "      ✓ Docker Compose found: $(docker compose version --short)"

# Verify Docker daemon is running
if ! docker info &>/dev/null; then
    echo -e "${RED}Error: Docker daemon is not running!${NC}"
    echo "Please launch Docker Desktop or start the Docker service and try again."
    exit 1
fi
echo "      ✓ Docker daemon is active and responsive."

# Step 2: Prepare clean staging workspace
mkdir -p "$DEPLOY_DIR"
cd "$DEPLOY_DIR"
DEPLOY_ROOT="$(pwd)"

if [ "$RESET" = true ]; then
    echo -e "\n${YELLOW}[2/5] Reset requested: Purging containers, networks, and data...${NC}"
    if [ -f "docker-compose.yml" ]; then
        docker compose down -v --remove-orphans 2>/dev/null || true
    fi
    rm -rf oenexa-node oenexa-frontend
    echo "      ✓ Clean state prepared."
else
    echo -e "\n${BLUE}[2/5] Preparing deployment workspace in $DEPLOY_DIR...${NC}"
fi

# Step 3: Clone or update main branch from GitHub
echo -e "\n${BLUE}[3/5] Cloning latest '$BRANCH' branch from GitHub...${NC}"

# Backend clone / pull
if [ -d "oenexa-node/.git" ]; then
    echo "      Updating existing oenexa-node repository..."
    cd oenexa-node
    git fetch origin "$BRANCH" 2>/dev/null || true
    git checkout "$BRANCH" 2>/dev/null || true
    git pull origin "$BRANCH" 2>/dev/null || true
    cd "$DEPLOY_ROOT"
else
    echo "      Cloning oenexa-node ($NODE_REPO) branch $BRANCH..."
    if ! git clone -b "$BRANCH" --depth 1 "$NODE_REPO" oenexa-node 2>/dev/null; then
        echo -e "      ${YELLOW}oenexa/oenexa-node not yet public; cloning from local workspace as fallback...${NC}"
        git clone -b "$BRANCH" --depth 1 ../ oenexa-node
    fi
fi
echo "      ✓ Backend oenexa-node ready."

# Frontend clone / pull with fallback
if [ -d "oenexa-frontend/.git" ]; then
    echo "      Updating existing oenexa-frontend repository..."
    cd oenexa-frontend
    git fetch origin "$BRANCH" 2>/dev/null || true
    git checkout "$BRANCH" 2>/dev/null || true
    git pull origin "$BRANCH" 2>/dev/null || true
    cd "$DEPLOY_ROOT"
else
    echo "      Cloning oenexa-frontend ($FRONTEND_REPO) branch $BRANCH..."
    if ! git clone -b "$BRANCH" --depth 1 "$FRONTEND_REPO" oenexa-frontend 2>/dev/null; then
        echo -e "      ${YELLOW}oenexa/oenexa-frontend not yet public; attempting fallback ($FRONTEND_FALLBACK_REPO)...${NC}"
        if ! git clone -b "$BRANCH" --depth 1 "$FRONTEND_FALLBACK_REPO" oenexa-frontend 2>/dev/null; then
            echo -e "      ${YELLOW}Cloning from local repository workspace as fallback...${NC}"
            cp -r ../../oenexa-frontend ./oenexa-frontend 2>/dev/null || cp -r ../oenexa-frontend ./oenexa-frontend
        fi
    fi
fi
echo "      ✓ Frontend oenexa-frontend ready."

# Step 4: Generate unified docker-compose configuration
echo -e "\n${BLUE}[4/5] Generating Docker orchestration manifests...${NC}"

cat << EOF > docker-compose.yml
services:
  oenexa-node:
    build:
      context: ./oenexa-node
      dockerfile: Dockerfile
    image: oenexa-node:latest
    container_name: oenexa-node
    restart: unless-stopped
    environment:
      - OEN_PASSWORD=devpassword
    ports:
      - "${P2P_PORT}:8765"
      - "${RPC_PORT}:8545"
      - "${METRICS_PORT}:9090"
    volumes:
      - oen-chain-data:/data
      - ./oenexa-node/configs/docker-devnet.toml:/data/config.toml:ro
    command: >
      start
      --config /data/config.toml
      --datadir /data
      --miner
      --metrics
    networks:
      - oenexa-net

  oenexa-frontend:
    build:
      context: ./oenexa-frontend
      dockerfile: Dockerfile
    image: oenexa-frontend:latest
    container_name: oenexa-frontend
    restart: unless-stopped
    ports:
      - "${FRONTEND_PORT}:80"
    depends_on:
      - oenexa-node
    networks:
      - oenexa-net

volumes:
  oen-chain-data:

networks:
  oenexa-net:
    driver: bridge
EOF

echo "      ✓ docker-compose.yml generated."

# Step 5: Build and run containers
echo -e "\n${BLUE}[5/5] Building Docker images and launching stack...${NC}"

BUILD_FLAGS=""
if [ "$REBUILD" = true ]; then
    BUILD_FLAGS="--no-cache"
fi

docker compose build $BUILD_FLAGS
docker compose up -d

echo -e "\n${YELLOW}Waiting for OENEXA Layer-1 Node to initialize and produce blocks...${NC}"

MAX_RETRIES=30
RETRY_COUNT=0
ONLINE=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if curl -s "http://127.0.0.1:$RPC_PORT/api/status" 2>/dev/null | grep -q '"online":true'; then
        ONLINE=true
        break
    fi
    sleep 2
    RETRY_COUNT=$((RETRY_COUNT + 1))
    echo -n "."
done
echo ""

if [ "$ONLINE" = true ]; then
    echo -e "${GREEN}${BOLD}✓ OENEXA Node is ONLINE and actively producing blocks!${NC}"
else
    echo -e "${YELLOW}Node is taking longer to start. Checking container status:${NC}"
    docker ps --filter "name=oenexa"
fi

echo -e "\n${GREEN}${BOLD}==========================================================================${NC}"
echo -e "${GREEN}${BOLD}       OENEXA Full Stack is RUNNING in Docker!                            ${NC}"
echo -e "${GREEN}${BOLD}==========================================================================${NC}"
echo -e "  ${BOLD}Web Dashboard UI${NC} : ${CYAN}http://localhost:${FRONTEND_PORT}${NC}"
echo -e "  ${BOLD}Web3 JSON-RPC 2.0${NC}: ${CYAN}http://127.0.0.1:${RPC_PORT}${NC}"
echo -e "  ${BOLD}REST Status API${NC}  : ${CYAN}http://127.0.0.1:${RPC_PORT}/api/status${NC}"
echo -e "  ${BOLD}Prometheus Metrics${NC}: ${CYAN}http://127.0.0.1:${METRICS_PORT}/metrics${NC}"
echo -e "--------------------------------------------------------------------------"
echo -e "Useful Commands:"
echo -e "  View live logs    : ${YELLOW}./oenexa-run.sh --logs${NC}"
echo -e "  Check node health : ${YELLOW}./oenexa-run.sh --status${NC}"
echo -e "  Stop all services : ${YELLOW}./oenexa-run.sh --stop${NC}"
echo -e "  Reset chain data  : ${YELLOW}./oenexa-run.sh --reset${NC}"
echo -e "=========================================================================="
