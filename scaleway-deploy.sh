#!/usr/bin/env bash
# ==============================================================================
# OENEXA (OEN) — Scaleway Interactive / Automated Deployment Script
# Target: Scaleway Elastic Metal Dedicated Servers & Scaleway Instances
# OS: Ubuntu 22.04 LTS / 24.04 LTS / Debian 12
# ==============================================================================
set -e

# Terminal Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

ROOT_DIR="/opt/oenexa"
LOG_FILE="/var/log/oenexa-deploy.log"

echo -e "${CYAN}${BOLD}"
echo "=========================================================================="
echo "    🏛️ OENEXA (OEN) — Scaleway Dedicated Cloud Node Deployment Runner      "
echo "=========================================================================="
echo -e "${NC}"

# Check for root
if [ "$(id -u)" -ne 0 ]; then
  echo -e "${RED}Error: This script must be run as root or with sudo.${NC}"
  exit 1
fi

echo -e "${BLUE}[1/7] Applying System Hardening & Kernel Tuning...${NC}"
cat << 'EOF' > /etc/sysctl.d/99-oenexa.conf
fs.file-max = 2097152
net.core.somaxconn = 65535
net.core.netdev_max_backlog = 10000
net.ipv4.tcp_max_syn_backlog = 8192
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216
vm.max_map_count = 262144
vm.swappiness = 10
EOF
sysctl --system > /dev/null 2>&1

cat << 'EOF' > /etc/security/limits.d/99-oenexa.conf
* hard core 0
* soft core 0
* soft nofile 65536
* hard nofile 65536
root soft nofile 65536
root hard nofile 65536
EOF

echo -e "${BLUE}[2/7] Installing System Packages & Official Docker Engine...${NC}"
apt-get update -y > /dev/null
apt-get install -y apt-transport-https ca-certificates curl gnupg lsb-release git ufw fail2ban jq htop tar wget net-tools > /dev/null

if ! command -v docker &> /dev/null; then
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
  apt-get update -y > /dev/null
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin > /dev/null
  systemctl enable docker
  systemctl start docker
fi

echo -e "${BLUE}[3/7] Setting up dedicated user and directory hierarchy...${NC}"
if ! id "oenexa" &>/dev/null; then
  useradd -m -s /bin/bash -g docker -G sudo oenexa || true
fi
mkdir -p "$ROOT_DIR/src" "$ROOT_DIR/configs" "$ROOT_DIR/backups"

echo -e "${BLUE}[4/7] Configuring UFW Firewall & Fail2ban...${NC}"
ufw default deny incoming > /dev/null 2>&1
ufw default allow outgoing > /dev/null 2>&1
ufw allow 22/tcp comment "SSH Remote Administration" > /dev/null 2>&1
ufw allow 8765/tcp comment "OENEXA P2P Consensus Network" > /dev/null 2>&1
ufw allow 80/tcp comment "HTTP Web Explorer & JSON-RPC Gateway" > /dev/null 2>&1
ufw allow 443/tcp comment "HTTPS Web Explorer & JSON-RPC Gateway" > /dev/null 2>&1
ufw --force enable > /dev/null 2>&1
systemctl enable fail2ban > /dev/null 2>&1
systemctl restart fail2ban > /dev/null 2>&1

echo -e "${BLUE}[5/7] Deploying OENEXA Configuration & Orchestration Files...${NC}"
if [ ! -f "$ROOT_DIR/.env" ]; then
  AUTO_PASS=$(openssl rand -hex 24)
  cat << EOF > "$ROOT_DIR/.env"
OEN_PASSWORD=${AUTO_PASS}
NETWORK_MODE=standalone-miner
LOG_LEVEL=info
P2P_PORT=8765
RPC_PORT=8545
METRICS_PORT=9090
EXPLORER_PORT=80
DOMAIN_NAME=
GITHUB_TOKEN=
GRAFANA_PASSWORD=admin
EOF
  chmod 600 "$ROOT_DIR/.env"
fi

cat << 'EOF' > "$ROOT_DIR/configs/node.toml"
[chain]
network_id = 1
name       = "OENEXA Production Node"

[node]
data_dir      = "/data"
keystore_file = "keystore.json"
miner_enabled = true
log_level     = "info"

[p2p]
listen_addr     = "0.0.0.0:8765"
external_addr   = ""
max_peers       = 50
bootstrap_peers = [
  "oenexa-seed1.oenexa.io:8765",
  "oenexa-seed2.oenexa.io:8765"
]

[rpc]
enabled       = true
listen_addr   = "0.0.0.0:8545"
cors_origins  = ["*"]
read_timeout  = "30s"
write_timeout = "30s"

[storage]
blocks_dir = "blocks"
state_dir  = "state"

[metrics]
enabled     = true
listen_addr = "0.0.0.0:9090"

[upgrade]
release_url    = "https://api.github.com/repos/oenexa/oenexa-node/releases/latest"
check_interval = "1h"
auto_apply     = false
EOF

cat << 'EOF' > "$ROOT_DIR/docker-compose.yml"
services:
  oenexa-node:
    build:
      context: ./src/oenexa-node
      dockerfile: Dockerfile
    image: oenexa-node:latest
    container_name: oenexa-node
    restart: unless-stopped
    environment:
      OEN_PASSWORD: "${OEN_PASSWORD}"
    ports:
      - "${P2P_PORT:-8765}:8765/tcp"
      - "127.0.0.1:${RPC_PORT:-8545}:8545/tcp"
      - "127.0.0.1:${METRICS_PORT:-9090}:9090/tcp"
    volumes:
      - oen-chain-data:/data
      - ./configs/node.toml:/data/config.toml:ro
    command: >
      start
      --config /data/config.toml
      --datadir /data
      --miner
      --metrics
    healthcheck:
      test: ["CMD", "/healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 3
      start_period: 10s
    networks:
      - oenexa-net

  oenexa-explorer:
    build:
      context: ./src/oenexa-explorer
      dockerfile: Dockerfile
    image: oenexa-explorer:latest
    container_name: oenexa-explorer
    restart: unless-stopped
    ports:
      - "${EXPLORER_PORT:-80}:80/tcp"
    depends_on:
      oenexa-node:
        condition: service_healthy
    networks:
      - oenexa-net

volumes:
  oen-chain-data:
    name: oen-chain-data

networks:
  oenexa-net:
    name: oenexa-net
    driver: bridge
EOF

# Install oenexa-ctl helper
cat << 'EOF' > /usr/local/bin/oenexa-ctl
#!/usr/bin/env bash
set -e
ROOT_DIR="/opt/oenexa"
ENV_FILE="$ROOT_DIR/.env"
if [ -f "$ENV_FILE" ]; then export $(grep -v '^#' "$ENV_FILE" | xargs -d '\n'); fi

case "$1" in
  status)
    docker compose -f "$ROOT_DIR/docker-compose.yml" ps
    echo ""
    curl -s --connect-timeout 2 http://127.0.0.1:8545/api/status | jq . 2>/dev/null || curl -s http://127.0.0.1:8545/api/status || echo "Node unreachable"
    ;;
  logs)
    docker compose -f "$ROOT_DIR/docker-compose.yml" logs -f "${2:-}"
    ;;
  restart)
    docker compose -f "$ROOT_DIR/docker-compose.yml" restart
    ;;
  stop)
    docker compose -f "$ROOT_DIR/docker-compose.yml" stop
    ;;
  start)
    docker compose -f "$ROOT_DIR/docker-compose.yml" up -d
    ;;
  backup-keystore)
    mkdir -p /opt/oenexa/backups
    TS=$(date +%Y%m%d_%H%M%S)
    docker cp oenexa-node:/data/keystore.json "/opt/oenexa/backups/keystore_$TS.json"
    echo "✓ Keystore backed up to /opt/oenexa/backups/keystore_$TS.json"
    ;;
  rpc)
    curl -s -X POST http://127.0.0.1:8545/ -H "Content-Type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"${2:-oen_chainInfo}\",\"params\":${3:-null}}" | jq .
    ;;
  height)
    curl -s -X POST http://127.0.0.1:8545/ -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","id":1,"method":"oen_blockHeight","params":null}' | jq -r .result
    ;;
  *)
    echo "Usage: oenexa-ctl {status|logs|restart|start|stop|backup-keystore|rpc|height}"
    ;;
esac
EOF
chmod 755 /usr/local/bin/oenexa-ctl

echo -e "${BLUE}[6/7] Cloning and Building OENEXA Services...${NC}"
cd "$ROOT_DIR/src"
ENV_FILE="$ROOT_DIR/.env"
GH_TOKEN=$(grep '^GITHUB_TOKEN=' "$ENV_FILE" | cut -d '=' -f2-)

if [ ! -d "$ROOT_DIR/src/oenexa-node" ]; then
  if [ -n "$GH_TOKEN" ]; then
    git clone "https://${GH_TOKEN}@github.com/oenexa/oenexa-node.git" "$ROOT_DIR/src/oenexa-node" || true
  else
    git clone "https://github.com/oenexa/oenexa-node.git" "$ROOT_DIR/src/oenexa-node" || true
  fi
fi

if [ ! -d "$ROOT_DIR/src/oenexa-explorer" ]; then
  if [ -n "$GH_TOKEN" ]; then
    git clone "https://${GH_TOKEN}@github.com/oenexa/oenexa-explorer.git" "$ROOT_DIR/src/oenexa-explorer" || true
  else
    git clone "https://github.com/oenexa/oenexa-explorer.git" "$ROOT_DIR/src/oenexa-explorer" || true
  fi
fi

chown -R oenexa:oenexa "$ROOT_DIR"

echo -e "${BLUE}[7/7] Launching Stack via Docker Compose...${NC}"
docker compose -f "$ROOT_DIR/docker-compose.yml" up -d --build

PUBLIC_IP=$(curl -s https://api.ipify.org || hostname -I | awk '{print $1}')
echo -e "\n${GREEN}${BOLD}==========================================================================${NC}"
echo -e "${GREEN}${BOLD} ✓ OENEXA Deployment Complete on Scaleway!${NC}"
echo -e "${GREEN}${BOLD}==========================================================================${NC}"
echo -e " • Web Explorer UI   : ${CYAN}http://${PUBLIC_IP}${NC}"
echo -e " • Web3 JSON-RPC URL : ${CYAN}http://${PUBLIC_IP}/rpc${NC} (Internal: 127.0.0.1:8545)"
echo -e " • P2P Consensus Port: ${CYAN}8765/tcp${NC} (Public)"
echo -e " • Stack Management  : ${YELLOW}oenexa-ctl status${NC} | ${YELLOW}oenexa-ctl logs${NC}"
echo -e "=========================================================================="
