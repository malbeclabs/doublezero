#!/usr/bin/env bash
# Bring up a containerized devnet plus a stress-test device, then run the
# orchestrator and observer against it. Reuses the e2e ledger / manager /
# controller stack (via `dev/dzctl start`) and adds one custom stress device
# whose EOS config does NOT run a doublezero-agent daemon — the orchestrator
# starts the agent over SSH instead.
#
# Usage:
#   tools/stress/scripts/run-stress-local.sh                # default run
#   tools/stress/scripts/run-stress-local.sh --clean        # destroy first
#   tools/stress/scripts/run-stress-local.sh --no-build     # skip docker build
#   tools/stress/scripts/run-stress-local.sh --target-users 4 --hold 60
#
# After it returns, the orchestrator and observer keep running in the
# background. Their PIDs, log files, and working directory are printed.
# Stop them with: kill $(cat <working-dir>/orchestrator.pid <working-dir>/observer.pid)
set -euo pipefail

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
WORKSPACE_DIR="$(cd -- "${SCRIPT_DIR}/../../.." &> /dev/null && pwd)"

DEPLOY_ID="${DZ_DEPLOY_ID:-dz-local}"
STRESS_IMAGE="${DZ_STRESS_DEVICE_IMAGE:-dz-local/device-stress:dev}"
BASE_DEVICE_IMAGE="${DZ_DEVICE_IMAGE:-dz-local/device:dev}"

DEVICE_CODE="${DZ_STRESS_DEVICE_CODE:-dzstress}"
DEVICE_LOCATION="ewr"
DEVICE_EXCHANGE="xewr"
DEVICE_HOST_ID="${DZ_STRESS_DEVICE_HOST_ID:-50}"   # offset inside the CYOA /24

CONTAINER_NAME="${DEPLOY_ID}-device-${DEVICE_CODE}"
DEFAULT_NETWORK="${DEPLOY_ID}-default"
CYOA_NETWORK="${DEPLOY_ID}-cyoa"

DEPLOY_DIR="${WORKSPACE_DIR}/dev/.deploy/${DEPLOY_ID}/stress"
WORKING_DIR="${DZ_STRESS_WORKING_DIR:-${DEPLOY_DIR}/run}"
SSH_KEY_PATH="${DEPLOY_DIR}/orchestrator_ed25519"

TARGET_USERS="${DZ_STRESS_TARGET_USERS:-4}"
USERS_PER_BATCH="${DZ_STRESS_USERS_PER_BATCH:-2}"
HOLD_SECONDS="${DZ_STRESS_HOLD_SECONDS:-30}"
SAMPLE_INTERVAL="${DZ_STRESS_SAMPLE_INTERVAL:-10s}"
# The serviceability program rejects user creates whose client_ip isn't
# globally routable (rejects CGNAT 100.64.0.0/10 and friends). Pin the
# orchestrator's IP allocator to a global-unicast /16 instead of its CGNAT
# default.
CLIENT_IP_BASE="${DZ_STRESS_CLIENT_IP_BASE:-9.200.0.0}"

CLEAN=false
NO_BUILD=false
NO_AGENT=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --clean) CLEAN=true; shift ;;
        --no-build) NO_BUILD=true; shift ;;
        --no-agent) NO_AGENT=true; shift ;;
        --target-users) TARGET_USERS="$2"; shift 2 ;;
        --users-per-batch) USERS_PER_BATCH="$2"; shift 2 ;;
        --hold) HOLD_SECONDS="$2"; shift 2 ;;
        --sample-interval) SAMPLE_INTERVAL="$2"; shift 2 ;;
        -h|--help) sed -n '1,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) echo "unknown flag: $1" >&2; exit 2 ;;
    esac
done

log() { printf '\033[1;36m[stress]\033[0m %s\n' "$*" >&2; }

require() {
    command -v "$1" >/dev/null 2>&1 || { echo "missing required tool: $1" >&2; exit 1; }
}
require docker
require jq
require go
require ssh-keygen
require python3   # for IP-in-subnet math

mkdir -p "$DEPLOY_DIR" "$WORKING_DIR"

# ---------------------------------------------------------------------------
# Phase 1: bring up the core devnet
# ---------------------------------------------------------------------------
if [ "$CLEAN" = true ]; then
    log "destroying any prior devnet ($DEPLOY_ID)"
    "${WORKSPACE_DIR}/dev/dzctl" destroy -y || true
    docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
fi

if [ "$NO_BUILD" = false ]; then
    log "building e2e docker images via dzctl"
    "${WORKSPACE_DIR}/dev/dzctl" build
fi

log "starting core devnet (ledger, manager, controller, funder)"
# dzctl start currently fails at "doublezero geolocation init" because that
# subcommand was removed from the CLI. The earlier steps (ledger up,
# serviceability deploy, smart-contract init) succeed and that's all we
# need. Re-check post-failure that the smart contract is initialized and
# continue.
if ! "${WORKSPACE_DIR}/dev/dzctl" start --no-build; then
    log "dzctl start exited non-zero; verifying chain state"
    if ! docker exec "${DEPLOY_ID}-manager" \
            doublezero global-config get >/dev/null 2>&1; then
        echo "dzctl start failed AND smart-contract is not initialized" >&2
        exit 1
    fi
    log "smart contract is initialized; continuing despite dzctl failure"
fi

# ---------------------------------------------------------------------------
# Phase 2: build the stress device image
# ---------------------------------------------------------------------------
log "building stress device image: $STRESS_IMAGE"
docker build \
    --build-arg "DZ_DEVICE_IMAGE=${BASE_DEVICE_IMAGE}" \
    -t "$STRESS_IMAGE" \
    "${WORKSPACE_DIR}/tools/stress/docker/device"

# ---------------------------------------------------------------------------
# Phase 3: discover network / address state
# ---------------------------------------------------------------------------
log "inspecting networks"
CYOA_SUBNET="$(docker network inspect "$CYOA_NETWORK" \
    --format '{{(index .IPAM.Config 0).Subnet}}' 2>/dev/null || true)"
if [ -z "$CYOA_SUBNET" ]; then
    # dzctl bailed before creating the CYOA network. Make it ourselves.
    log "creating $CYOA_NETWORK (dzctl skipped it)"
    docker network create \
        --driver bridge \
        --subnet 9.128.0.0/24 \
        --label "dz.malbeclabs.com/type=devnet" \
        --label "dz.malbeclabs.com/deploy-id=${DEPLOY_ID}" \
        "$CYOA_NETWORK" >/dev/null
    CYOA_SUBNET="9.128.0.0/24"
fi
CONTROLLER_IP="$(docker inspect "${DEPLOY_ID}-controller" \
    --format "{{(index .NetworkSettings.Networks \"${DEFAULT_NETWORK}\").IPAddress}}" 2>/dev/null || true)"
LEDGER_IP="$(docker inspect "${DEPLOY_ID}-ledger" \
    --format "{{(index .NetworkSettings.Networks \"${DEFAULT_NETWORK}\").IPAddress}}")"

# The controller defaults `-max-user-tunnel-slots` to 128 — beyond that the
# controller renders only the first 128 tunnels in the device config and
# the agent never knows about higher-index users (the orchestrator's
# onchain provisions succeed regardless, so the bug is silent). Always
# restart the controller with our slot count derived from TARGET_USERS so
# stress sweeps past 128 users actually exercise the device. This also
# replaces any controller dzctl may have started before failing on its
# broken geolocation init step (we override the entrypoint to keep the
# ledger-readiness wait and inject the flag).
CONTROLLER_NAME="${DEPLOY_ID}-controller"
CONTROLLER_MAX_SLOTS="${DZ_STRESS_CONTROLLER_MAX_SLOTS:-$TARGET_USERS}"
if [ "$CONTROLLER_MAX_SLOTS" -lt 128 ]; then
    CONTROLLER_MAX_SLOTS=128
fi
log "starting $CONTROLLER_NAME (max-user-tunnel-slots=$CONTROLLER_MAX_SLOTS)"
docker rm -f "$CONTROLLER_NAME" >/dev/null 2>&1 || true
SERVICEABILITY_PROGRAM_ID="$(docker exec "${DEPLOY_ID}-manager" \
    solana address -k /etc/doublezero/manager/dz-program-keypair.json \
    | tr -d '[:space:]')"
docker run -d \
    --name "$CONTROLLER_NAME" \
    --hostname controller \
    --network "$DEFAULT_NETWORK" \
    --label "dz.malbeclabs.com/type=devnet" \
    --label "dz.malbeclabs.com/deploy-id=${DEPLOY_ID}" \
    -e "DZ_LEDGER_URL=http://ledger:8899" \
    -e "DZ_SERVICEABILITY_PROGRAM_ID=${SERVICEABILITY_PROGRAM_ID}" \
    --entrypoint bash \
    "${DZ_CONTROLLER_IMAGE:-dz-local/controller:dev}" \
    -c "
        while ! curl -sf -X POST -H 'Content-Type: application/json' \\
            --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"getHealth\"}' \\
            \"\${DZ_LEDGER_URL}\" | grep -q '\"result\":\"ok\"'; do
            echo 'Waiting for solana validator to be ready...'
            sleep 1
        done
        exec doublezero-controller start \\
            -listen-addr 0.0.0.0 -listen-port 7000 \\
            -program-id \"\${DZ_SERVICEABILITY_PROGRAM_ID}\" \\
            -solana-rpc-endpoint \"\${DZ_LEDGER_URL}\" \\
            -device-local-asn 65342 \\
            -max-user-tunnel-slots ${CONTROLLER_MAX_SLOTS} \\
            -no-hardware
    " >/dev/null
# Re-inspect for the IP.
CONTROLLER_IP=""
for _ in $(seq 1 10); do
    CONTROLLER_IP="$(docker inspect "$CONTROLLER_NAME" \
        --format "{{(index .NetworkSettings.Networks \"${DEFAULT_NETWORK}\").IPAddress}}" 2>/dev/null || true)"
    [ -n "$CONTROLLER_IP" ] && break
    sleep 1
done
[ -n "$CONTROLLER_IP" ] || { echo "controller did not get a default-network IP" >&2; exit 1; }

# Derive an IP inside the CYOA subnet (host octet = DEVICE_HOST_ID) and a
# globally-routable /29 dz_prefix at a non-overlapping host offset. Mirrors
# the rules in e2e/internal/devnet/device.go. Snap to a /29 boundary so the
# prefix is network-aligned in case `--dz-prefixes` ever validates alignment.
read -r CYOA_IP DZ_PREFIX < <(python3 - <<PY
import ipaddress
net = ipaddress.ip_network("$CYOA_SUBNET")
host_id = $DEVICE_HOST_ID
ip = net.network_address + host_id
last = (host_id + 128) if (host_id + 128) < 256 else (host_id - 128)
last &= ~0x7
prefix = ipaddress.ip_address(int(net.network_address) + last)
print(ip, f"{prefix}/29")
PY
)

log "device CYOA IP=$CYOA_IP  dz_prefix=$DZ_PREFIX  controller=${CONTROLLER_IP:-<none>}"

# ---------------------------------------------------------------------------
# Phase 4: SSH keypair for the orchestrator
# ---------------------------------------------------------------------------
if [ ! -f "$SSH_KEY_PATH" ]; then
    log "generating orchestrator SSH keypair: $SSH_KEY_PATH"
    ssh-keygen -t ed25519 -f "$SSH_KEY_PATH" -N '' -C 'doublezero-stress-orchestrator' >/dev/null
fi
SSH_PUBKEY="$(cat "${SSH_KEY_PATH}.pub")"

# ---------------------------------------------------------------------------
# Phase 5: start the stress device container (it will block on its
# entrypoint's wait-for-startup-config loop until phase 6 copies the config)
# ---------------------------------------------------------------------------
if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    log "removing existing stress device container"
    docker rm -f "$CONTAINER_NAME" >/dev/null
fi

log "starting stress device container: $CONTAINER_NAME"
docker run -d \
    --name "$CONTAINER_NAME" \
    --hostname "device-${DEVICE_CODE}" \
    --privileged \
    --network "$DEFAULT_NETWORK" \
    --label "dz.malbeclabs.com/type=devnet" \
    --label "dz.malbeclabs.com/deploy-id=${DEPLOY_ID}" \
    "$STRESS_IMAGE" >/dev/null

# Network ordering matters with containerized EOS: the first network
# attached is the management interface (Management0/eth0), then subsequent
# networks correspond to Ethernet1+ in order. So default → eth0, cyoa → eth1.
docker network connect --ip "$CYOA_IP" "$CYOA_NETWORK" "$CONTAINER_NAME"

# Inspect for the default-network IP / gateway / prefix Docker assigned —
# the agent's path to the controller goes through Management0, so the EOS
# config needs all three.
DEFAULT_IP="$(docker inspect "$CONTAINER_NAME" \
    --format "{{(index .NetworkSettings.Networks \"${DEFAULT_NETWORK}\").IPAddress}}")"
DEFAULT_PREFIX="$(docker inspect "$CONTAINER_NAME" \
    --format "{{(index .NetworkSettings.Networks \"${DEFAULT_NETWORK}\").IPPrefixLen}}")"
DEFAULT_GATEWAY="$(docker inspect "$CONTAINER_NAME" \
    --format "{{(index .NetworkSettings.Networks \"${DEFAULT_NETWORK}\").Gateway}}")"
log "default network: ip=${DEFAULT_IP}/${DEFAULT_PREFIX} gw=${DEFAULT_GATEWAY}"

# ---------------------------------------------------------------------------
# Phase 6: render EOS startup-config and unblock the container's init
# ---------------------------------------------------------------------------
STARTUP_CONFIG_PATH="${WORKING_DIR}/startup-config"
CYOA_CIDR_PREFIX="${CYOA_SUBNET##*/}"

# Note: the orchestrator's SSH runner exec's `doublezero-agent`. cEOS pins
# `admin`'s NSS shell to /usr/bin/RunCli, so SSH-exec'd commands hit the EOS
# Cli parser. We connect as a separate /bin/bash system user (`stress`,
# added in the stress device image) and plant the orchestrator's pubkey
# into its authorized_keys post-boot. The `protocol http` / eos-sdk-rpc /
# Loopback0 blocks mirror the e2e device so the agent's hardcoded
# 127.0.0.1:9543 endpoint works.
#
# The agent's prometheus metrics listen on :50100 (set by agent-wrapper.sh)
# because the controller-pushed MAIN-CONTROL-PLANE-ACL — which is the one
# actually bound to `system control-plane in` — permits TCP 50000-50100 by
# default. The controller fully redefines that ACL on every apply, so we
# can't add a port-9100 permit ourselves and have it survive.
cat > "$STARTUP_CONFIG_PATH" <<EOF
! stress-test device startup-config (no doublezero-agent daemon)
!
no aaa root
!
username admin privilege 15 role network-admin secret sha512 \$6\$hb.8VFI7A9D/0zi2\$sZady959HlXHgFdWU9r01VDwmbM2CrhDYIXBJzHb3scDP8/t/4ozwxpZbwEgDxbWL.mHYtie0rSO8fRSZ5D0T1
username admin sshkey ${SSH_PUBKEY}
!
service configuration session commit merge
!
vrf instance vrf1
ip routing vrf vrf1
!
ip access-list standard allow-all
   permit any
!
management api http-commands
   protocol http
   ip access-group allow-all
   no shutdown
!
management api eos-sdk-rpc
   transport grpc foo
      localhost loopback
      service all
      no disabled
!
management api gnmi
   transport grpc gnmi
!
management ssh
   no shutdown
!
hostname ${DEVICE_CODE}
!
no service interface inactive port-id allocation disabled
!
transceiver qsfp default-mode 4x10G
!
service routing protocols model multi-agent
!
agent PowerManager shutdown
agent LedPolicy shutdown
agent Thermostat shutdown
agent PowerFuse shutdown
agent StandbyCpld shutdown
agent LicenseManager shutdown
!
spanning-tree mode mstp
!
system l1
   unsupported speed action error
   unsupported error-correction action error
!
interface Loopback0
  vrf vrf1
  ip address 8.8.8.8/32
!
interface Ethernet1
   no switchport
   ip address ${CYOA_IP}/${CYOA_CIDR_PREFIX}
!
interface Management0
   no shutdown
   ip address ${DEFAULT_IP}/${DEFAULT_PREFIX}
!
ip routing
!
ip route 0.0.0.0/0 ${DEFAULT_GATEWAY}
!
router bgp 65342
   router-id 10.10.10.10
   vrf vrf1
     network 8.8.8.8/32 route-map e2e
!
route-map e2e permit 10
   set community 21682:1200
!
end
EOF
log "rendered startup-config: $STARTUP_CONFIG_PATH"

docker exec "$CONTAINER_NAME" mkdir -p /etc/doublezero/agent
docker cp "$STARTUP_CONFIG_PATH" "${CONTAINER_NAME}:/etc/doublezero/agent/startup-config"

log "waiting for stress device to become healthy"
for _ in $(seq 1 60); do
    status="$(docker inspect "$CONTAINER_NAME" --format '{{.State.Health.Status}}' 2>/dev/null || echo starting)"
    if [ "$status" = "healthy" ]; then
        break
    fi
    sleep 5
done
if [ "$status" != "healthy" ]; then
    echo "stress device did not become healthy (last status: $status)" >&2
    docker logs --tail 50 "$CONTAINER_NAME" >&2 || true
    exit 1
fi

# Plant the orchestrator's SSH pubkey into the `stress` system user's
# authorized_keys. The user is created in the Dockerfile with /bin/bash so
# SSH-exec'd commands run through bash rather than EOS Cli. The pubkey can
# only be installed at runtime because the keypair is generated per devnet.
docker exec "$CONTAINER_NAME" bash -c '
    mkdir -p /home/stress/.ssh &&
    chown stress:stress /home/stress/.ssh &&
    chmod 700 /home/stress/.ssh
'
docker exec -i "$CONTAINER_NAME" bash -c '
    cat > /home/stress/.ssh/authorized_keys &&
    chown stress:stress /home/stress/.ssh/authorized_keys &&
    chmod 600 /home/stress/.ssh/authorized_keys
' < "${SSH_KEY_PATH}.pub"

# ---------------------------------------------------------------------------
# Phase 7: create the device onchain
# ---------------------------------------------------------------------------
log "creating device onchain (code=${DEVICE_CODE})"
docker exec "${DEPLOY_ID}-manager" bash -c "
    set -e
    if ! doublezero device get --code ${DEVICE_CODE} >/dev/null 2>&1; then
        doublezero device create \
            --contributor co01 \
            --code ${DEVICE_CODE} \
            --location ${DEVICE_LOCATION} \
            --exchange ${DEVICE_EXCHANGE} \
            --public-ip ${CYOA_IP} \
            --dz-prefixes ${DZ_PREFIX} \
            --mgmt-vrf mgmt
        DEVICE_PK=\$(doublezero device get --code ${DEVICE_CODE} --json | jq -r .account)
        doublezero device update --pubkey \"\$DEVICE_PK\" --max-users 128 --desired-status activated
    fi
"

DEVICE_PUBKEY="$(docker exec "${DEPLOY_ID}-manager" \
    bash -c "doublezero device get --code ${DEVICE_CODE} --json" | jq -r .account)"
log "device onchain pubkey: $DEVICE_PUBKEY"

# Plant the pubkey on the device so the agent wrapper can supply --pubkey.
echo -n "$DEVICE_PUBKEY" | docker exec -i "$CONTAINER_NAME" \
    bash -c 'cat > /etc/doublezero/agent/pubkey'

# Register VPNv4/IPv4 loopback interfaces onchain. Without these, the
# controller reports "device has pathology" every poll and returns an
# empty config — the agent runs but has nothing to apply. The interface
# names + types mirror the e2e harness.
for entry in "Loopback255:vpnv4" "Loopback256:ipv4"; do
    iface="${entry%:*}"
    iftype="${entry#*:}"
    out=$(docker exec "${DEPLOY_ID}-manager" \
        doublezero device interface create "$DEVICE_CODE" "$iface" \
            --loopback-type "$iftype" --bandwidth 10G 2>&1) || true
    if echo "$out" | grep -q "already exists"; then
        log "loopback ${iface} (${iftype}) already exists onchain"
    else
        log "registered loopback ${iface} (${iftype})"
    fi
done

PROGRAM_ID="$(docker exec "${DEPLOY_ID}-manager" \
    solana address -k /etc/doublezero/manager/dz-program-keypair.json | tr -d '[:space:]')"
log "serviceability program id: $PROGRAM_ID"

KEYPAIR_LOCAL="${DEPLOY_DIR}/manager-keypair.json"
docker cp "${DEPLOY_ID}-manager:/root/.config/doublezero/id.json" "$KEYPAIR_LOCAL"
# docker cp preserves the container's file mode (000 for keypairs); make it
# readable by the orchestrator running as the host user.
chmod 600 "$KEYPAIR_LOCAL"

# Each user the orchestrator provisions onchain needs a prepaid access pass
# keyed on (client_ip, user_payer). The orchestrator signs as the manager, so
# user_payer is the manager's pubkey. Sweep CLIENT_IP_BASE + 0..N to cover
# every IP the orchestrator might use.
#
# The set-access-pass calls fan out via xargs -P so high user counts don't
# bottleneck on serial CLI roundtrips — at 1024 users a serial loop is 12+
# minutes of sustained txn submission per second and can knock the local
# validator over, while batches of ACCESS_PASS_PARALLEL concurrent calls
# complete in well under a minute.
PAYER_PUBKEY="$(docker exec "${DEPLOY_ID}-manager" \
    solana-keygen pubkey /root/.config/doublezero/id.json | tr -d '[:space:]')"
IFS=. read -r b1 b2 b3 b4 <<<"$CLIENT_IP_BASE"
ACCESS_PASS_PARALLEL="${DZ_STRESS_ACCESS_PASS_PARALLEL:-16}"
log "creating access passes for ${CLIENT_IP_BASE}+0..$((TARGET_USERS-1)) (payer=$PAYER_PUBKEY, parallel=$ACCESS_PASS_PARALLEL)"
export DEPLOY_ID PAYER_PUBKEY b1 b2 b3 b4
seq 0 $((TARGET_USERS - 1)) | xargs -P "$ACCESS_PASS_PARALLEL" -I{} bash -c '
    i=$1
    host=$(( (b3 << 8) + b4 + i ))
    octet3=$(( (host >> 8) & 0xff ))
    octet4=$(( host & 0xff ))
    client_ip="${b1}.${b2}.${octet3}.${octet4}"
    docker exec "${DEPLOY_ID}-manager" \
        doublezero access-pass set \
            --accesspass-type prepaid \
            --client-ip "$client_ip" \
            --user-payer "$PAYER_PUBKEY" >/dev/null
' _ {}

# ---------------------------------------------------------------------------
# Phase 8: build orchestrator + observer
# ---------------------------------------------------------------------------
log "building orchestrator + observer binaries"
ORCH_BIN="${DEPLOY_DIR}/device-orchestrator"
OBS_BIN="${DEPLOY_DIR}/device-observer"
( cd "$WORKSPACE_DIR" && \
  go build -o "$ORCH_BIN" ./tools/stress/device-orchestrator/cmd/device-orchestrator && \
  go build -o "$OBS_BIN"  ./tools/stress/device-observer/cmd/device-observer )

# ---------------------------------------------------------------------------
# Phase 9: launch orchestrator + observer
# ---------------------------------------------------------------------------
RUN_DIR="${WORKING_DIR}/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$RUN_DIR"
log "run working-dir: $RUN_DIR"

ORCH_ARGS=(
    --dut-pubkey "$DEVICE_PUBKEY"
    --rpc-url "http://${LEDGER_IP}:8899"
    --program-id "$PROGRAM_ID"
    --keypair "$KEYPAIR_LOCAL"
    --working-dir "$RUN_DIR"
    --abort-file "${RUN_DIR}/abort"
    --target-user-count "$TARGET_USERS"
    --users-per-batch "$USERS_PER_BATCH"
    --hold-seconds "$HOLD_SECONDS"
    --client-ip-base "$CLIENT_IP_BASE"
    --log-level info
)
if [ -n "$CONTROLLER_IP" ]; then
    ORCH_ARGS+=(--controller "${CONTROLLER_IP}:7000")
fi
if [ "$NO_AGENT" = true ]; then
    ORCH_ARGS+=(--no-agent)
else
    ORCH_ARGS+=(
        --dut-ssh-host "${CYOA_IP}:22"
        --dut-ssh-key  "$SSH_KEY_PATH"
        --dut-ssh-user stress
    )
fi

log "launching orchestrator (background)"
nohup "$ORCH_BIN" "${ORCH_ARGS[@]}" \
    > "${RUN_DIR}/orchestrator.stdout" \
    2> "${RUN_DIR}/orchestrator.stderr" &
ORCH_PID=$!
echo "$ORCH_PID" > "${RUN_DIR}/orchestrator.pid"

log "launching observer (background)"
nohup "$OBS_BIN" \
    --dut-host "$CYOA_IP" \
    --eapi-user admin --eapi-pass admin \
    --agent-metrics-url "http://${CYOA_IP}:50100/metrics" \
    --working-dir "$RUN_DIR" \
    --abort-file "${RUN_DIR}/abort" \
    --sample-interval "$SAMPLE_INTERVAL" \
    --force \
    > "${RUN_DIR}/observer.stdout" \
    2> "${RUN_DIR}/observer.stderr" &
OBS_PID=$!
echo "$OBS_PID" > "${RUN_DIR}/observer.pid"

cat <<EOF

==> stress test launched
    orchestrator pid : $ORCH_PID  (logs: ${RUN_DIR}/orchestrator.std{out,err})
    observer     pid : $OBS_PID  (logs: ${RUN_DIR}/observer.std{out,err})
    working dir      : ${RUN_DIR}
    abort sentinel   : ${RUN_DIR}/abort

To stop both: kill \$(cat ${RUN_DIR}/orchestrator.pid ${RUN_DIR}/observer.pid)
To follow:    tail -F ${RUN_DIR}/orchestrator.stderr ${RUN_DIR}/observer.stderr
To tear down: ${WORKSPACE_DIR}/dev/dzctl destroy -y && docker rm -f ${CONTAINER_NAME}
EOF
