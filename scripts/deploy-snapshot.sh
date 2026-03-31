#!/usr/bin/env bash
# Build a goreleaser snapshot, scp the deb to remote nodes, optionally install
# it, and optionally open a tmux session tailing service logs on all nodes.
#
# Usage: ./scripts/deploy-snapshot.sh <env> <config-name> <user> <node1> [node2 ...] [flags]
#
# Examples:
#   ./scripts/deploy-snapshot.sh devnet controller ubuntu 10.0.1.1 10.0.1.2
#   ./scripts/deploy-snapshot.sh devnet controller ubuntu 10.0.1.1 10.0.1.2 --install
#   ./scripts/deploy-snapshot.sh devnet controller ubuntu 10.0.1.1 10.0.1.2 --install --tail doublezero-controller
#   ./scripts/deploy-snapshot.sh devnet controller ubuntu 10.0.1.1 10.0.1.2 --tail doublezero-controller --skip-build
#
# Flags:
#   --install       Install the deb on each node via dpkg -i after scp
#   --tail SERVICE  After install, open a tmux session with synchronized panes
#                   tailing the systemd journal for SERVICE on each node
#   --skip-build    Skip the build step; use existing artifacts in dist/
#   --quiet, -q     Suppress verbose goreleaser output (passed to build-snapshot.sh)
#   --help, -h      Show this help
#
# Requirements: ssh, scp, tmux (if --tail)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Formatting ---

BOLD='\033[1m' DIM='\033[2m' GREEN='\033[0;32m'
RED='\033[0;31m' CYAN='\033[0;36m' RESET='\033[0m'
if [[ ! -t 1 ]]; then BOLD="" DIM="" GREEN="" RED="" CYAN="" RESET=""; fi

die() { echo -e "${RED}ERROR:${RESET} $*" >&2; exit 1; }

# --- Args ---

INSTALL=false
TAIL_SERVICE=""
SKIP_BUILD=false
QUIET=false
ENV_NAME=""
CONFIG_NAME=""
SSH_USER=""
NODES=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --install)    INSTALL=true; shift ;;
        --tail)       TAIL_SERVICE="$2"; shift 2 ;;
        --skip-build) SKIP_BUILD=true; shift ;;
        --quiet|-q)   QUIET=true; shift ;;
        --help|-h)
            echo "Usage: $0 <env> <config-name> <user> <node1> [node2 ...] [flags]"
            echo ""
            echo "Build a snapshot, deploy the deb to remote nodes, and optionally"
            echo "install it and tail service logs in a synchronized tmux session."
            echo ""
            echo "Arguments:"
            echo "  <env>           Environment: devnet or testnet"
            echo "  <config-name>   Name of the goreleaser config (e.g. controller, client)"
            echo "  <user>          SSH user for remote nodes"
            echo "  <node>...       One or more node hostnames or IPs"
            echo ""
            echo "Flags:"
            echo "  --install       Install the deb on each node via dpkg -i"
            echo "  --tail SERVICE  Open tmux session tailing service logs (implies --install)"
            echo "  --skip-build    Skip building; use existing artifacts in dist/"
            echo "  --quiet, -q     Suppress verbose goreleaser output"
            echo "  --help, -h      Show this help"
            exit 0
            ;;
        -*)  die "Unknown flag: $1" ;;
        *)
            if [[ -z "$ENV_NAME" ]]; then
                ENV_NAME="$1"; shift
            elif [[ -z "$CONFIG_NAME" ]]; then
                CONFIG_NAME="$1"; shift
            elif [[ -z "$SSH_USER" ]]; then
                SSH_USER="$1"; shift
            else
                NODES+=("$1"); shift
            fi
            ;;
    esac
done

[[ -n "$ENV_NAME" ]]    || die "Missing required argument: <env>\nRun '$0 --help' for usage."
[[ "$ENV_NAME" == "devnet" || "$ENV_NAME" == "testnet" ]] || die "Invalid env: $ENV_NAME (must be devnet or testnet)"
[[ -n "$CONFIG_NAME" ]] || die "Missing required argument: <config-name>\nRun '$0 --help' for usage."
[[ -n "$SSH_USER" ]]    || die "Missing required argument: <user>\nRun '$0 --help' for usage."
[[ ${#NODES[@]} -gt 0 ]] || die "Missing required argument: at least one <node>\nRun '$0 --help' for usage."

# --tail implies --install
if [[ -n "$TAIL_SERVICE" ]]; then
    INSTALL=true
fi

# --- Preflight ---

command -v ssh >/dev/null 2>&1 || die "ssh is required"
command -v scp >/dev/null 2>&1 || die "scp is required"
if [[ -n "$TAIL_SERVICE" ]]; then
    command -v tmux >/dev/null 2>&1 || die "tmux is required (for --tail)"
fi

cd "$REPO_ROOT"

# Extract project name from the base config.
BASE_CONFIG="release/.goreleaser.base.${CONFIG_NAME}.yaml"
PROJECT_NAME=""
if [[ -f "$BASE_CONFIG" ]]; then
    PROJECT_NAME=$(grep 'project_name:' "$BASE_CONFIG" | head -1 | sed 's/.*project_name: *//' | tr -d '[:space:]')
fi
[[ -n "$PROJECT_NAME" ]] || PROJECT_NAME="$CONFIG_NAME"

# --- Step 1: Build ---

STEP=1
TOTAL_STEPS=2
if [[ "$INSTALL" == true ]]; then TOTAL_STEPS=3; fi

if [[ "$SKIP_BUILD" == true ]]; then
    echo ""
    echo -e "${DIM}Skipping build (--skip-build)${RESET}"
else
    echo ""
    echo -e "${BOLD}${CYAN}[${STEP}/${TOTAL_STEPS}]${RESET} ${BOLD}Building snapshot${RESET}"
    echo ""
    QUIET_FLAG=""
    if [[ "$QUIET" == true ]]; then QUIET_FLAG="--quiet"; fi
    "$SCRIPT_DIR/build-snapshot.sh" "$ENV_NAME" "$CONFIG_NAME" $QUIET_FLAG
fi

# Find the deb in dist/.
DEB_FILES=("$REPO_ROOT"/dist/*.deb)
if [[ ! -f "${DEB_FILES[0]}" ]]; then
    die "No .deb files found in dist/. Run without --skip-build or build first."
fi

# If multiple debs, pick the amd64 one.
DEB_FILE=""
for f in "${DEB_FILES[@]}"; do
    if [[ "$(basename "$f")" == *amd64* ]]; then
        DEB_FILE="$f"
        break
    fi
done
[[ -n "$DEB_FILE" ]] || DEB_FILE="${DEB_FILES[0]}"
DEB_BASENAME="$(basename "$DEB_FILE")"

echo ""
echo -e "  Package: ${DIM}${DEB_BASENAME}${RESET}"
echo -e "  Nodes:   ${DIM}${NODES[*]}${RESET}"
echo ""

# --- Step 2: SCP ---

STEP=$((STEP + 1))
echo -e "${BOLD}${CYAN}[${STEP}/${TOTAL_STEPS}]${RESET} ${BOLD}Copying deb to ${#NODES[@]} node(s)${RESET}"
echo ""

REMOTE_PATH="/tmp/${DEB_BASENAME}"
for node in "${NODES[@]}"; do
    echo -e "  ${DIM}${node}${RESET} ..."
    scp -o StrictHostKeyChecking=no -q "$DEB_FILE" "${SSH_USER}@${node}:${REMOTE_PATH}"
    echo -e "  ${GREEN}✓${RESET} ${node}"
done
echo ""

# --- Step 3: Install ---

if [[ "$INSTALL" == true ]]; then
    STEP=$((STEP + 1))
    echo -e "${BOLD}${CYAN}[${STEP}/${TOTAL_STEPS}]${RESET} ${BOLD}Installing on ${#NODES[@]} node(s)${RESET}"
    echo ""

    for node in "${NODES[@]}"; do
        echo -e "  ${DIM}${node}${RESET} ..."
        ssh -o StrictHostKeyChecking=no "${SSH_USER}@${node}" "sudo dpkg -i ${REMOTE_PATH}"
        echo -e "  ${GREEN}✓${RESET} ${node}"
    done
    echo ""
fi

# --- Step 4: Tail logs in tmux ---

if [[ -n "$TAIL_SERVICE" ]]; then
    SESSION_NAME="deploy-${PROJECT_NAME}"

    # Kill existing session with the same name if it exists.
    tmux kill-session -t "$SESSION_NAME" 2>/dev/null || true

    echo -e "${BOLD}${CYAN}[*]${RESET} ${BOLD}Opening tmux session: ${SESSION_NAME}${RESET}"
    echo ""

    # Create a new detached session with the first node.
    FIRST_NODE="${NODES[0]}"
    tmux new-session -d -s "$SESSION_NAME" \
        "ssh -o StrictHostKeyChecking=no ${SSH_USER}@${FIRST_NODE} 'sudo journalctl -fu ${TAIL_SERVICE}'"

    # Split panes for remaining nodes.
    for node in "${NODES[@]:1}"; do
        tmux split-window -t "$SESSION_NAME" \
            "ssh -o StrictHostKeyChecking=no ${SSH_USER}@${node} 'sudo journalctl -fu ${TAIL_SERVICE}'"
        tmux select-layout -t "$SESSION_NAME" tiled
    done

    # Enable synchronized input across all panes.
    tmux set-window-option -t "$SESSION_NAME" synchronize-panes on

    # Attach to the session.
    echo -e "  Nodes: ${DIM}${NODES[*]}${RESET}"
    echo -e "  Panes are ${BOLD}synchronized${RESET} — input goes to all nodes"
    echo -e "  Detach: ${DIM}Ctrl-b d${RESET}    Kill: ${DIM}Ctrl-b : kill-session${RESET}"
    echo ""

    exec tmux attach-session -t "$SESSION_NAME"
fi

echo -e "  ${GREEN}✓${RESET} Deploy complete"
echo ""
