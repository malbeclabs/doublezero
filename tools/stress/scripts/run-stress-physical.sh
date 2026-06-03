#!/usr/bin/env bash
# Bring up a host-local controller against the devnet ledger and run the
# stress-test orchestrator + observer against a physical Arista EOS device.
#
# Sister to run-stress-local.sh; same orchestrator/observer binaries, different
# environment:
#   - ledger:     devnet RPC (DZ_RPC_URL)
#   - serviceability: pre-deployed program at DZ_PROGRAM_ID, initialized here
#                     on first run via doublezero CLI (idempotent)
#   - controller: launched directly via `go run controlplane/controller/...`
#   - device:     physical DUT reachable over SSH (DUT_HOST), agent invoked
#                 inside the device's `ns-management` network namespace
#
# Usage:
#   tools/stress/scripts/run-stress-physical.sh \
#       --target-users 4 --users-per-batch 2 --hold 0
#
# Required env:
#   DZ_PROGRAM_ID       serviceability program id (no default — operator
#                       passes the stress-test program id, e.g. from the
#                       private infra repo)
# Env (defaults shown — override per operator):
#   DZ_RPC_URL          devnet RPC (default: doublezerolocalnet pool)
#   DUT_HOST            device IP or hostname
#   DUT_SSH_USER        SSH user on the DUT
#   DUT_SSH_KEY         SSH private key path
#   CONTROLLER_BIND_ADDR  controller listen address (default: 0.0.0.0)
#   AGENT_BINARY        path to doublezero-agent on the DUT
#   SOLANA_KEYPAIR      operator's keypair (default: $HOME/.config/doublezero/id.json
#                       — the same default the doublezero CLI uses, so `dz` calls
#                       and orchestrator share the operator's authority)
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
WORKSPACE_DIR="$(cd -- "${SCRIPT_DIR}/../../.." &> /dev/null && pwd)"

# ---------------------------------------------------------------------------
# Config (env-overridable)
# ---------------------------------------------------------------------------
DZ_RPC_URL="${DZ_RPC_URL:-https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16}"
# DZ_PROGRAM_ID has no default: the stress-test serviceability program ID is
# kept in the private infra repo, not here. Operators export it before
# running the script.
DZ_PROGRAM_ID="${DZ_PROGRAM_ID:?DZ_PROGRAM_ID is required (stress-test serviceability program id; see infra repo)}"

DUT_HOST="${DUT_HOST:-10.0.0.15}"
DUT_SSH_USER="${DUT_SSH_USER:-nik}"
DUT_SSH_KEY="${DUT_SSH_KEY:-$HOME/.ssh/nik@malbeclabs.com}"
# Default to /mnt/flash on the EOS device: it persists across reboots and is
# writable by sudo. Override via AGENT_BINARY if installed somewhere else.
AGENT_BINARY="${AGENT_BINARY:-/mnt/flash/doublezero-agent}"
# `bash` escapes EOS Cli (RunCli) into the underlying shell; `sudo` gets us
# CAP_SYS_ADMIN for `ip netns exec`. Both are needed for the per-default
# SSH login as `nik` on a real EOS device.
AGENT_COMMAND_PREFIX="${AGENT_COMMAND_PREFIX:-bash sudo /sbin/ip netns exec ns-management}"
AGENT_METRICS_PORT="${AGENT_METRICS_PORT:-50100}"
# eAPI HTTP basic auth for the device-observer's `show ...` polls. The
# containerized cEOS device accepts an empty password (dev default), but a
# real EOS device requires real credentials. Default user to the SSH user;
# password must be supplied via env (empty default keeps the containerized
# path unchanged).
EAPI_USER="${EAPI_USER:-$DUT_SSH_USER}"
EAPI_PASS="${EAPI_PASS:-}"

DEVICE_CODE="${DZ_STRESS_DEVICE_CODE:-chi-dn-dzd5}"
DEVICE_LOCATION="${DZ_STRESS_DEVICE_LOCATION:-ewr}"
DEVICE_EXCHANGE="${DZ_STRESS_DEVICE_EXCHANGE:-xewr}"
# Smart-contract device validation requires public_ip to be globally
# routable (is_global() — rejects RFC1918) AND not overlap any of the
# device's own dz_prefixes (processors/device/create.rs ~line 152). The
# default below is a sentinel publicly-routable IP inside the 9.210.180.0/24
# block but outside the /29 used for dz_prefixes; override only if you need
# the device's real public address recorded onchain.
DEVICE_PUBLIC_IP="${DZ_STRESS_DEVICE_PUBLIC_IP:-9.210.180.5}"
# /29 carved out of a globally-routable block. dz-prefixes is the route range
# the device advertises for its tunnels — separate from device-tunnel-block
# (which is the private pool the program auto-allocates loopback IPs from).
DEVICE_DZ_PREFIX="${DZ_STRESS_DEVICE_DZ_PREFIX:-9.210.180.176/29}"

# Where the device reaches the controller. Default to the same address the
# user mentioned the device can reach (chi-dn-bm1's mgmt IP from the DUT).
# Override if the host has a different routable address.
CONTROLLER_BIND_ADDR="${CONTROLLER_BIND_ADDR:-0.0.0.0}"
CONTROLLER_ADVERTISE_ADDR="${CONTROLLER_ADVERTISE_ADDR:-}"
CONTROLLER_LISTEN_PORT="${CONTROLLER_LISTEN_PORT:-7000}"

# Default to the doublezero CLI's keypair location so `dz` (which reads this
# location implicitly) and the orchestrator's --keypair both use the same
# operator authority. The standard solana CLI default at ~/.config/solana/id.json
# may be a different key and would need an explicit override.
SOLANA_KEYPAIR="${SOLANA_KEYPAIR:-$HOME/.config/doublezero/id.json}"

DEPLOY_DIR="${WORKSPACE_DIR}/dev/.deploy/stress-physical"
WORKING_DIR="${DZ_STRESS_WORKING_DIR:-${DEPLOY_DIR}/run}"

TARGET_USERS="${DZ_STRESS_TARGET_USERS:-4}"
USERS_PER_BATCH="${DZ_STRESS_USERS_PER_BATCH:-2}"
HOLD_SECONDS="${DZ_STRESS_HOLD_SECONDS:-0}"
SAMPLE_INTERVAL="${DZ_STRESS_SAMPLE_INTERVAL:-10s}"
ACCESS_PASS_PARALLEL="${DZ_STRESS_ACCESS_PASS_PARALLEL:-16}"
# Same routability constraint as run-stress-local.sh: client_ip must be
# globally routable (the program rejects CGNAT).
CLIENT_IP_BASE="${DZ_STRESS_CLIENT_IP_BASE:-9.200.0.0}"

NO_AGENT=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --no-agent) NO_AGENT=true; shift ;;
        --target-users) TARGET_USERS="$2"; shift 2 ;;
        --users-per-batch) USERS_PER_BATCH="$2"; shift 2 ;;
        --hold) HOLD_SECONDS="$2"; shift 2 ;;
        --sample-interval) SAMPLE_INTERVAL="$2"; shift 2 ;;
        -h|--help) sed -n '1,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) echo "unknown flag: $1" >&2; exit 2 ;;
    esac
done

log() { printf '\033[1;36m[stress-physical]\033[0m %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

require() {
    command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}
require doublezero
require solana
require go
require jq
require ssh
require nc
require python3   # IP-in-subnet math
[ -r "$SOLANA_KEYPAIR" ] || die "solana keypair not readable: $SOLANA_KEYPAIR"
[ -r "$DUT_SSH_KEY" ]    || die "DUT SSH key not readable: $DUT_SSH_KEY"

# All doublezero CLI invocations go through this wrapper so RPC + program id
# overrides are consistent regardless of $DOUBLEZERO_ENV in the operator's
# environment.
dz() {
    doublezero --url "$DZ_RPC_URL" --program-id "$DZ_PROGRAM_ID" "$@"
}

solana_cli() {
    solana --url "$DZ_RPC_URL" --keypair "$SOLANA_KEYPAIR" "$@"
}

mkdir -p "$DEPLOY_DIR" "$WORKING_DIR"

# ---------------------------------------------------------------------------
# Phase 1: connectivity sanity checks
# ---------------------------------------------------------------------------
log "verifying SSH reachability of $DUT_HOST"
# EOS pins the SSH login shell to RunCli, so the remote command must start
# with `bash` to escape into the underlying shell.
ssh -i "$DUT_SSH_KEY" -o BatchMode=yes -o ConnectTimeout=5 \
    -o StrictHostKeyChecking=accept-new \
    "$DUT_SSH_USER@$DUT_HOST" "bash echo ssh-ok" >/dev/null \
    || die "cannot SSH into $DUT_SSH_USER@$DUT_HOST with $DUT_SSH_KEY"

log "verifying $AGENT_BINARY exists on the DUT"
ssh -i "$DUT_SSH_KEY" -o BatchMode=yes "$DUT_SSH_USER@$DUT_HOST" \
    "bash test -x $AGENT_BINARY" \
    || die "agent binary not found / not executable at $AGENT_BINARY on $DUT_HOST — set AGENT_BINARY or scp it first"

log "verifying solana RPC reachability"
solana_cli cluster-version >/dev/null \
    || die "cannot reach Solana RPC at $DZ_RPC_URL"

# ---------------------------------------------------------------------------
# Phase 2: initialize the serviceability program (idempotent)
#
# Mirrors the steps in e2e/internal/devnet/smartcontract_init.go:64-148,
# scoped down to the single location/exchange we use. Each step checks
# whether the resource already exists so reruns are safe.
# ---------------------------------------------------------------------------
PAYER_PUBKEY="$(solana-keygen pubkey "$SOLANA_KEYPAIR" | tr -d '[:space:]')"
log "operator pubkey (signer + access-pass payer): $PAYER_PUBKEY"

balance="$(solana_cli balance --output json 2>/dev/null | jq -r '.lamports // 0')"
if [ "${balance:-0}" -eq 0 ]; then
    log "operator balance is 0; requesting airdrop"
    solana_cli airdrop 10 "$PAYER_PUBKEY" >/dev/null \
        || die "airdrop failed — fund $PAYER_PUBKEY manually before retrying"
fi

if dz global-config get >/dev/null 2>&1; then
    log "global-config already initialized; skipping init"
else
    log "running doublezero init"
    dz init
    # device-tunnel-block must be in a private (RFC1918) range — the program
    # allocates loopback IPs from it and validates `is_private()` on the
    # result (smartcontract/programs/doublezero-serviceability/src/state/interface.rs:494).
    # 172.16.0.0/16 mirrors the standard from smartcontract/test/start-test.sh.
    dz global-config set \
        --local-asn 65000 --remote-asn 65342 \
        --device-tunnel-block 172.16.0.0/16 \
        --user-tunnel-block 169.254.0.0/16 \
        --multicastgroup-block 233.84.178.0/24 \
        --multicast-publisher-block 148.51.120.0/21
    dz global-config authority set \
        --activator-authority me --sentinel-authority me
fi

if ! dz location get --code "$DEVICE_LOCATION" >/dev/null 2>&1; then
    log "creating location $DEVICE_LOCATION"
    dz location create --code "$DEVICE_LOCATION" --name "New York" \
        --country US --lat 40.780297071772125 --lng -74.07203003496925
fi

if ! dz exchange get --code "$DEVICE_EXCHANGE" >/dev/null 2>&1; then
    log "creating exchange $DEVICE_EXCHANGE"
    dz exchange create --code "$DEVICE_EXCHANGE" --name "New York" \
        --lat 40.780297071772125 --lng -74.07203003496925
fi

if ! dz contributor get --code co01 >/dev/null 2>&1; then
    log "creating contributor co01"
    dz contributor create --code co01 --owner me
fi

# ---------------------------------------------------------------------------
# Phase 3: create the device onchain
# ---------------------------------------------------------------------------
if ! dz device get --code "$DEVICE_CODE" >/dev/null 2>&1; then
    log "creating device onchain (code=$DEVICE_CODE)"
    dz device create \
        --contributor co01 \
        --code "$DEVICE_CODE" \
        --location "$DEVICE_LOCATION" \
        --exchange "$DEVICE_EXCHANGE" \
        --public-ip "$DEVICE_PUBLIC_IP" \
        --dz-prefixes "$DEVICE_DZ_PREFIX" \
        --mgmt-vrf mgmt
fi

DEVICE_PUBKEY="$(dz device get --code "$DEVICE_CODE" --json | jq -r .account)"
log "device onchain pubkey: $DEVICE_PUBKEY"

# Keep the onchain cap in sync with TARGET_USERS on every run. The create
# branch above sets it implicitly on first run, but reruns with a larger
# --target-users would otherwise leave the cap at its original value and
# silently truncate the sweep — the controller's --max-user-tunnel-slots
# would advertise the new number while the device account still capped at
# the old one. (Reported by @elitegreg in #3829 review.)
dz device update --pubkey "$DEVICE_PUBKEY" --max-users "$TARGET_USERS" \
    --desired-status activated

for entry in "Loopback255:vpnv4" "Loopback256:ipv4"; do
    iface="${entry%:*}"
    iftype="${entry#*:}"
    # No --ip-net: the program rejects ip_net on plain loopbacks
    # (interface/create.rs:155-162) and auto-allocates from the
    # device-tunnel-block instead (interface/create.rs:213-218).
    out=$(dz device interface create "$DEVICE_CODE" "$iface" \
        --loopback-type "$iftype" --bandwidth 10G 2>&1) || true
    if echo "$out" | grep -q "already exists"; then
        log "loopback ${iface} (${iftype}) already exists onchain"
    elif echo "$out" | grep -qiE "error|failed"; then
        log "WARNING: loopback ${iface} (${iftype}) create may have failed:"
        echo "$out" | tail -3 >&2
    else
        log "registered loopback ${iface} (${iftype})"
    fi
done

# ---------------------------------------------------------------------------
# Phase 4: launch the controller
#
# `go run` is intentional per the user spec — convenient for iteration; the
# operator can later swap to a built binary if startup time matters.
# ---------------------------------------------------------------------------
CONTROLLER_LOG="${WORKING_DIR}/controller.log"
CONTROLLER_PID_FILE="${WORKING_DIR}/controller.pid"

# Fail fast if something is already listening on the controller port. Without
# this check, our `go run` silently fails to bind and the readiness wait
# (`nc -z 127.0.0.1 $PORT`) succeeds against the stale process, so the script
# proceeds against a controller pointing at the wrong program / RPC. Common
# culprits: a leftover controller from a previous run (recoverable via
# `kill $(cat $CONTROLLER_PID_FILE)`), or the dz-local-controller docker
# container with a host-port mapping.
if ss -ltn "sport = :$CONTROLLER_LISTEN_PORT" 2>/dev/null | tail -n +2 | grep -q .; then
    log "ERROR: port ${CONTROLLER_LISTEN_PORT} already has a listener:"
    ss -ltnp "sport = :$CONTROLLER_LISTEN_PORT" >&2 || true
    if [ -f "$CONTROLLER_PID_FILE" ]; then
        log "hint: prior run's controller pid is in $CONTROLLER_PID_FILE — kill it first"
    fi
    die "refusing to start controller; release port ${CONTROLLER_LISTEN_PORT} and rerun"
fi

log "starting controller (listen=${CONTROLLER_BIND_ADDR}:${CONTROLLER_LISTEN_PORT}, max-slots=${TARGET_USERS})"
(
    cd "$WORKSPACE_DIR"
    nohup go run ./controlplane/controller/cmd/controller start \
        --program-id "$DZ_PROGRAM_ID" \
        --solana-rpc-endpoint "$DZ_RPC_URL" \
        --device-local-asn 65000 \
        --listen-addr "$CONTROLLER_BIND_ADDR" \
        --listen-port "$CONTROLLER_LISTEN_PORT" \
        --max-user-tunnel-slots "$TARGET_USERS" \
        --no-hardware \
        > "$CONTROLLER_LOG" 2>&1 &
    echo $! > "$CONTROLLER_PID_FILE"
)
CONTROLLER_PID="$(cat "$CONTROLLER_PID_FILE")"
log "controller pid: $CONTROLLER_PID (log: $CONTROLLER_LOG)"

# Teardown trap: kill the controller on script exit so a Ctrl-C doesn't leave
# it lingering. The orchestrator + observer are intentionally NOT killed by
# this script — they run in the background past script exit.
cleanup_controller() {
    if kill -0 "$CONTROLLER_PID" 2>/dev/null; then
        log "stopping controller (pid=$CONTROLLER_PID)"
        kill "$CONTROLLER_PID" 2>/dev/null || true
    fi
}
trap cleanup_controller EXIT

# Wait for the controller's listen port to accept connections (gRPC handshake).
# The cleanup trap stays armed through every following phase (access-pass
# setup, build, orchestrator + observer launch) so a `set -e` failure in any
# of them tears the controller down instead of orphaning it. The trap is
# disarmed only once the orchestrator has actually launched. (Reported by
# @elitegreg in #3829 review.)
log "waiting for controller to accept connections"
controller_ready=false
for _ in $(seq 1 30); do
    if nc -z -w 1 127.0.0.1 "$CONTROLLER_LISTEN_PORT" 2>/dev/null; then
        log "controller listening on :${CONTROLLER_LISTEN_PORT}"
        controller_ready=true
        break
    fi
    sleep 1
done
if ! $controller_ready; then
    log "controller log tail:"
    tail -50 "$CONTROLLER_LOG" >&2 || true
    die "controller did not start listening within 30s"
fi

# Determine the address the device will use to reach the controller. If the
# operator didn't override it, default to the host's primary IP that's
# routable from the device's subnet (best-effort).
if [ -z "$CONTROLLER_ADVERTISE_ADDR" ]; then
    CONTROLLER_ADVERTISE_ADDR="$(python3 -c '
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.connect(("'"$DUT_HOST"'", 80))
print(s.getsockname()[0])
s.close()
' 2>/dev/null || echo "")"
fi
[ -n "$CONTROLLER_ADVERTISE_ADDR" ] || die "could not determine controller advertise addr; set CONTROLLER_ADVERTISE_ADDR"
log "controller reachable to device at: ${CONTROLLER_ADVERTISE_ADDR}:${CONTROLLER_LISTEN_PORT}"

# ---------------------------------------------------------------------------
# Phase 5: access-pass setup (parallel)
# ---------------------------------------------------------------------------
IFS=. read -r b1 b2 b3 b4 <<<"$CLIENT_IP_BASE"
log "creating access passes for ${CLIENT_IP_BASE}+0..$((TARGET_USERS-1)) (payer=$PAYER_PUBKEY, parallel=$ACCESS_PASS_PARALLEL)"
export DZ_RPC_URL DZ_PROGRAM_ID PAYER_PUBKEY b1 b2 b3 b4
seq 0 $((TARGET_USERS - 1)) | xargs -P "$ACCESS_PASS_PARALLEL" -I{} bash -c '
    i=$1
    host=$(( (b3 << 8) + b4 + i ))
    octet3=$(( (host >> 8) & 0xff ))
    octet4=$(( host & 0xff ))
    client_ip="${b1}.${b2}.${octet3}.${octet4}"
    doublezero --url "$DZ_RPC_URL" --program-id "$DZ_PROGRAM_ID" \
        access-pass set \
            --accesspass-type prepaid \
            --client-ip "$client_ip" \
            --user-payer "$PAYER_PUBKEY" >/dev/null
' _ {}

# ---------------------------------------------------------------------------
# Phase 6: build orchestrator + observer
# ---------------------------------------------------------------------------
log "building orchestrator + observer binaries"
ORCH_BIN="${DEPLOY_DIR}/device-orchestrator"
OBS_BIN="${DEPLOY_DIR}/device-observer"
( cd "$WORKSPACE_DIR" && \
  go build -o "$ORCH_BIN" ./tools/stress/device-orchestrator/cmd/device-orchestrator && \
  go build -o "$OBS_BIN"  ./tools/stress/device-observer/cmd/device-observer )

# ---------------------------------------------------------------------------
# Phase 7: launch orchestrator + observer
# ---------------------------------------------------------------------------
RUN_DIR="${WORKING_DIR}/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$RUN_DIR"
log "run working-dir: $RUN_DIR"

ORCH_ARGS=(
    --dut-pubkey "$DEVICE_PUBKEY"
    --rpc-url "$DZ_RPC_URL"
    --program-id "$DZ_PROGRAM_ID"
    --keypair "$SOLANA_KEYPAIR"
    --working-dir "$RUN_DIR"
    --abort-file "${RUN_DIR}/abort"
    --target-user-count "$TARGET_USERS"
    --users-per-batch "$USERS_PER_BATCH"
    --hold-seconds "$HOLD_SECONDS"
    --client-ip-base "$CLIENT_IP_BASE"
    --dut-ssh-host "${DUT_HOST}:22"
    --dut-ssh-user "$DUT_SSH_USER"
    --dut-ssh-key "$DUT_SSH_KEY"
    --controller "${CONTROLLER_ADVERTISE_ADDR}:${CONTROLLER_LISTEN_PORT}"
    --agent-binary "$AGENT_BINARY"
    --agent-command-prefix "$AGENT_COMMAND_PREFIX"
    --agent-pubkey "$DEVICE_PUBKEY"
    --agent-metrics-addr ":${AGENT_METRICS_PORT}"
)
if [ "$NO_AGENT" = true ]; then
    ORCH_ARGS+=(--no-agent)
fi

log "launching orchestrator (background)"
nohup "$ORCH_BIN" "${ORCH_ARGS[@]}" \
    > "${RUN_DIR}/orchestrator.stdout" \
    2> "${RUN_DIR}/orchestrator.stderr" &
ORCH_PID=$!
echo "$ORCH_PID" > "${RUN_DIR}/orchestrator.pid"

# Orchestrator is up — the controller has a long-running consumer now. Disarm
# the cleanup trap so script exit doesn't terminate the controller out from
# under it.
trap - EXIT

log "launching observer (background)"
nohup "$OBS_BIN" \
    --dut-host "$DUT_HOST" \
    --eapi-user "$EAPI_USER" --eapi-pass "$EAPI_PASS" \
    --agent-metrics-url "http://${DUT_HOST}:${AGENT_METRICS_PORT}/metrics" \
    --working-dir "$RUN_DIR" \
    --abort-file "${RUN_DIR}/abort" \
    --sample-interval "$SAMPLE_INTERVAL" \
    --force \
    > "${RUN_DIR}/observer.stdout" \
    2> "${RUN_DIR}/observer.stderr" &
OBS_PID=$!
echo "$OBS_PID" > "${RUN_DIR}/observer.pid"

cat <<EOF

==> stress test launched against $DUT_HOST
    controller   pid : $CONTROLLER_PID  (log: $CONTROLLER_LOG)
    orchestrator pid : $ORCH_PID  (logs: ${RUN_DIR}/orchestrator.std{out,err})
    observer     pid : $OBS_PID  (logs: ${RUN_DIR}/observer.std{out,err})
    working dir      : ${RUN_DIR}
    abort sentinel   : ${RUN_DIR}/abort

To stop everything:  kill \$(cat ${CONTROLLER_PID_FILE} ${RUN_DIR}/orchestrator.pid ${RUN_DIR}/observer.pid)
To follow:           tail -F ${RUN_DIR}/orchestrator.stderr ${RUN_DIR}/observer.stderr ${CONTROLLER_LOG}
EOF
