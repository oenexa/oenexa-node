#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# OENEXA (ONXC) — Local Development Network Launcher (Bash)
# ─────────────────────────────────────────────────────────────────────────────
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/../go.mod" ]; then
    REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
elif [ -f "$SCRIPT_DIR/go.mod" ]; then
    REPO_ROOT="$SCRIPT_DIR"
else
    REPO_ROOT="$(pwd)"
fi

cd "$REPO_ROOT"

BIN_EXT=""
case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) BIN_EXT=".exe" ;;
esac

BIN_DIR="$REPO_ROOT/bin"
DATA_DIR="$REPO_ROOT/data/local-devnet"
CONFIG_PATH="$REPO_ROOT/configs/local-dev.toml"
NODE_BIN="$BIN_DIR/oenexa-node${BIN_EXT}"
CLI_BIN="$BIN_DIR/oenexa-cli${BIN_EXT}"

RESET=false
REBUILD=false
MINER=true
PORT=8545
METRICS_PORT=9090
PASSWORD="devpassword"

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
        --no-miner)
            MINER=false
            shift
            ;;
        --port)
            PORT="$2"
            shift 2
            ;;
        --metrics-port)
            METRICS_PORT="$2"
            shift 2
            ;;
        --password)
            PASSWORD="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: ./run-local.sh [--reset] [--rebuild] [--no-miner] [--port 8545] [--metrics-port 9090] [--password <pwd>]"
            exit 1
            ;;
    esac
done

echo "================================================================="
echo "       OENEXA (ONXC) — Local Development Network Launcher       "
echo "================================================================="
echo "Repository : $REPO_ROOT"
echo "Config     : $CONFIG_PATH"
echo "Data Dir   : $DATA_DIR"

# 1. Reset state if requested
if [ "$RESET" = true ]; then
    echo -e "\n[1/3] Resetting local chain state ($DATA_DIR)..."
    rm -rf "$DATA_DIR"
    echo "      ✓ Previous chain data purged."
else
    echo -e "\n[1/3] Checking local state..."
    if [ -d "$DATA_DIR" ]; then
        echo "      ✓ Existing chain data found (use --reset to start fresh)."
    fi
fi

# 2. Check and build binaries (auto-rebuild when Go sources are newer than bin/)
NEED_BUILD=false
if [ "$REBUILD" = true ] || [ ! -f "$NODE_BIN" ] || [ ! -f "$CLI_BIN" ]; then
    NEED_BUILD=true
elif [ -n "$(find "$REPO_ROOT/cmd" "$REPO_ROOT/internal" "$REPO_ROOT/go.mod" -type f \( -newer "$NODE_BIN" -o -newer "$CLI_BIN" \) 2>/dev/null | head -n1)" ]; then
    echo "      ℹ Source changes detected; recompiling binaries..."
    NEED_BUILD=true
fi

if [ "$NEED_BUILD" = true ]; then
    echo -e "\n[2/3] Building node and CLI binaries..."
    if ! command -v go &>/dev/null; then
        echo "Error: Go compiler ('go') not found in PATH. Please install Go (https://go.dev/dl/)."
        exit 1
    fi

    mkdir -p "$BIN_DIR"
    echo "      Compiling oenexa-node..."
    CGO_ENABLED=0 go build -trimpath -o "$NODE_BIN" ./cmd/node
    echo "      Compiling oenexa-cli..."
    CGO_ENABLED=0 go build -trimpath -o "$CLI_BIN" ./cmd/oenexa-cli
    echo "      ✓ Binaries compiled successfully into bin/"
else
    echo -e "\n[2/3] Binaries verified in bin/ (use --rebuild to recompile)."
fi

# 3. Ensure local config exists
if [ ! -f "$CONFIG_PATH" ]; then
    echo -e "\nGenerating default configs/local-dev.toml..."
    mkdir -p "$(dirname "$CONFIG_PATH")"
    cat << 'EOF' > "$CONFIG_PATH"
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
EOF
fi

ARGS=("start" "--config" "$CONFIG_PATH")
if [ "$MINER" = true ]; then
    ARGS+=("--miner")
fi
if [ "$PORT" != "8545" ]; then
    ARGS+=("--rpc-listen" "127.0.0.1:$PORT")
fi
if [ "$METRICS_PORT" != "9090" ]; then
    ARGS+=("--metrics-addr" "127.0.0.1:$METRICS_PORT")
fi

export ONXC_PASSWORD="$PASSWORD"

echo -e "\n[3/3] Starting OENEXA Local Full Node..."
echo "-----------------------------------------------------------------"
echo "  Web3 JSON-RPC   : http://127.0.0.1:$PORT"
echo "  REST API Status : http://127.0.0.1:$PORT/api/status"
echo "  Health Endpoint : http://127.0.0.1:$PORT/healthz"
echo "  Prometheus      : http://127.0.0.1:$METRICS_PORT/metrics"
echo "  Validator Mode  : $( [ "$MINER" = true ] && echo 'Active (Producing Blocks)' || echo 'Standby' )"
echo "-----------------------------------------------------------------"
echo "Quick commands to run in a separate terminal:"
echo "  ./bin/oenexa-cli info"
echo "  ./bin/oenexa-cli keygen"
echo "  curl -s http://127.0.0.1:$PORT/api/status"
echo "-----------------------------------------------------------------"
echo -e "Press Ctrl+C to stop the node gracefully.\n"

exec "$NODE_BIN" "${ARGS[@]}"
