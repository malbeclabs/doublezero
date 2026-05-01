#!/bin/bash
#
# start-test-permissions.sh
#
# Two-phase validation of the Permission account model:
#
# PHASE 1 — Legacy mode (require-permission-accounts OFF)
#   Runs the full initial network setup (locations, exchanges, contributors,
#   devices, interfaces, links, access passes, users) using only the
#   foundation allowlist for authorization, identical to start-test.sh.
#
# PHASE 2 — Permission account mode (require-permission-accounts ON)
#   Disables the legacy path, creates Permission accounts for the payer,
#   then adds more devices, links, access passes and users — all authorized
#   exclusively via Permission accounts.

clear
killall solana-test-validator > /dev/null 2>&1
killall solana > /dev/null 2>&1

set -e
set -x

mkdir -p ./logs ./target

export OPENSSL_NO_VENDOR=1

# ── Build ────────────────────────────────────────────────────────────────────

echo "Build the program"
cargo build-sbf --manifest-path ../programs/doublezero-serviceability/Cargo.toml -- -Znext-lockfile-bump --target-dir ../../target/
cp ../../target/deploy/doublezero_serviceability.so ./target/doublezero_serviceability.so

echo "Build the client"
cargo build --manifest-path ../../client/doublezero/Cargo.toml --target-dir ../../target/
cp ../../target/debug/doublezero ./target/

# ── Start validator ───────────────────────────────────────────────────────────

solana config set --url http://127.0.0.1:8899
./target/doublezero config set --url http://127.0.0.1:8899
./target/doublezero config set --program-id 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX

echo "Start solana local test cluster"
solana-test-validator --reset --bpf-program ./keypair.json ./target/doublezero_serviceability.so >./logs/validator.log 2>&1 &

echo "Waiting 15 seconds for the validator to start"
sleep 15

echo "Start instruction logger"
solana logs >./logs/instruction.log 2>&1 &

# ── Init ─────────────────────────────────────────────────────────────────────

./target/doublezero init

./target/doublezero global-config set \
    --local-asn 65100 --remote-asn 65001 \
    --device-tunnel-block 172.16.0.0/16 \
    --user-tunnel-block 169.254.0.0/16 \
    --multicastgroup-block 233.84.178.0/24

./target/doublezero global-config authority set \
    --activator-authority me --sentinel-authority me

# Enable onchain-allocation only — legacy path still active at this point.
./target/doublezero global-config feature-flags set --enable onchain-allocation

./target/doublezero global-config feature-flags get

PAYER=$(./target/doublezero address)
echo "Payer pubkey: $PAYER"


# ════════════════════════════════════════════════════════════════════════════
# PHASE 1 — Legacy authorization (foundation allowlist)
# ════════════════════════════════════════════════════════════════════════════

echo "###################################################################"
echo "# PHASE 1: Legacy mode — foundation allowlist authorization"
echo "###################################################################"

# ── Locations ─────────────────────────────────────────────────────────────────

echo "Creating locations"
./target/doublezero location create --code lax --name "Los Angeles" --country US --lat 34.049641274076464 --lng -118.25939642499903
./target/doublezero location create --code ewr --name "New York"    --country US --lat 40.780297071772125 --lng -74.07203003496925
./target/doublezero location create --code lhr --name "London"      --country UK --lat 51.513999803939384 --lng -0.12014764843092213
./target/doublezero location create --code fra --name "Frankfurt"   --country DE --lat 50.1215356432098   --lng 8.642047117175098
./target/doublezero location create --code ams --name "Amsterdam"   --country NL --lat 52.30085793004002  --lng 4.942241140085309
./target/doublezero location create --code sin --name "Singapore"   --country SG --lat 1.2807150707390342 --lng 103.85507136144396
./target/doublezero location create --code tyo --name "Tokyo"       --country JP --lat 35.66875144228767  --lng 139.76565267564501

# ── Exchanges ─────────────────────────────────────────────────────────────────

echo "Creating exchanges"
./target/doublezero exchange create --code xlax --name "Los Angeles" --lat 34.049641274076464 --lng -118.25939642499903
./target/doublezero exchange create --code xewr --name "New York"    --lat 40.780297071772125 --lng -74.07203003496925
./target/doublezero exchange create --code xlhr --name "London"      --lat 51.513999803939384 --lng -0.12014764843092213
./target/doublezero exchange create --code xfra --name "Frankfurt"   --lat 50.1215356432098   --lng 8.642047117175098
./target/doublezero exchange create --code xams --name "Amsterdam"   --lat 52.30085793004002  --lng 4.942241140085309
./target/doublezero exchange create --code xsin --name "Singapore"   --lat 1.2807150707390342 --lng 103.85507136144396
./target/doublezero exchange create --code xtyo --name "Tokyo"       --lat 35.66875144228767  --lng 139.76565267564501

# ── Contributors ──────────────────────────────────────────────────────────────

echo "Creating contributors"
./target/doublezero contributor create --code co01 --owner me
./target/doublezero contributor create --code co02 --owner me

# ── Tenants ───────────────────────────────────────────────────────────────────

echo "Creating tenants"
./target/doublezero tenant create --code acme --administrator me
./target/doublezero tenant create --code corp --administrator me

./target/doublezero tenant update --pubkey acme --vrf-id 100
./target/doublezero tenant update --pubkey corp --vrf-id 200

# ── Phase 1 devices ───────────────────────────────────────────────────────────

echo "Creating devices (phase 1)"
./target/doublezero device create --code la2-dz01 --contributor co01 --location lax --exchange xlax \
    --public-ip "207.45.216.134" --dz-prefixes "100.0.0.0/16" \
    --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt \
    --desired-status activated -w
./target/doublezero device create --code ny5-dz01 --contributor co01 --location ewr --exchange xewr \
    --public-ip "64.86.249.80"  --dz-prefixes "101.0.0.0/16" \
    --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt \
    --desired-status activated -w
./target/doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr \
    --public-ip "195.219.120.72" --dz-prefixes "102.0.0.0/29,103.0.0.0/16" \
    --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt \
    --desired-status activated -w
./target/doublezero device create --code ams-dz01 --contributor co01 --location ams --exchange xams \
    --public-ip "195.219.138.50" --dz-prefixes "108.0.0.0/16" \
    --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt \
    --desired-status activated -w

./target/doublezero device update --pubkey la2-dz01 --max-users 128
./target/doublezero device update --pubkey ny5-dz01 --max-users 128
./target/doublezero device update --pubkey ld4-dz01 --max-users 128
./target/doublezero device update --pubkey ams-dz01 --max-users 128

# ── Phase 1 interfaces ────────────────────────────────────────────────────────

echo "Creating device interfaces (phase 1)"
./target/doublezero device interface create la2-dz01 "Switch1/1/1" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create la2-dz01 "Switch1/1/2" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create ny5-dz01 "Switch1/1/1" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create ny5-dz01 "Switch1/1/2" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create ld4-dz01 "Switch1/1/1" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create ld4-dz01 "Switch1/1/2" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create ams-dz01 "Switch1/1/1" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create ams-dz01 "Switch1/1/2" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w

# ── Phase 1 links ─────────────────────────────────────────────────────────────

echo "Creating links (phase 1)"
./target/doublezero link create wan --code "la2-dz01:ny5-dz01" --contributor co01 \
    --side-a la2-dz01 --side-a-interface Switch1/1/1 \
    --side-z ny5-dz01 --side-z-interface Switch1/1/2 \
    --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3 \
    --desired-status activated -w
./target/doublezero link create wan --code "ny5-dz01:ld4-dz01" --contributor co01 \
    --side-a ny5-dz01 --side-a-interface Switch1/1/1 \
    --side-z ld4-dz01 --side-z-interface Switch1/1/2 \
    --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 \
    --desired-status activated -w
./target/doublezero link create wan --code "ld4-dz01:ams-dz01" --contributor co01 \
    --side-a ld4-dz01 --side-a-interface Switch1/1/1 \
    --side-z ams-dz01 --side-z-interface Switch1/1/2 \
    --bandwidth "10 Gbps" --mtu 9000 --delay-ms 10 --jitter-ms 2 \
    --desired-status activated -w

# ── Multicast groups ──────────────────────────────────────────────────────────

echo "Creating multicast groups"
./target/doublezero multicast group create --code mg01 --max-bandwidth 1Gbps --owner me -w
./target/doublezero multicast group create --code mg02 --max-bandwidth 1Gbps --owner me -w

# ── Phase 1 access passes (legacy — no Permission account needed) ─────────────

echo "Phase 1 access passes (legacy path via foundation allowlist)"
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 100.0.0.5 --tenant acme
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 100.0.0.6 --tenant acme
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 177.54.159.95 --tenant corp
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 147.28.171.51 --tenant corp

# ── Phase 1 users ─────────────────────────────────────────────────────────────

echo "Phase 1 users (legacy path)"
./target/doublezero user create --device ld4-dz01 --client-ip 177.54.159.95 --tenant corp -w
./target/doublezero user create --device ld4-dz01 --client-ip 147.28.171.51 --tenant corp -w

./target/doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip 100.0.0.5
./target/doublezero multicast group allowlist publisher  add --code mg01 --user-payer me --client-ip 100.0.0.6
./target/doublezero user create-subscribe --device la2-dz01 --client-ip 100.0.0.5 --subscriber mg01 -w
./target/doublezero user create-subscribe --device la2-dz01 --client-ip 100.0.0.6 --publisher mg01 -w

echo "Phase 1 complete — listing accounts"
./target/doublezero device list
./target/doublezero link list
./target/doublezero access-pass list
./target/doublezero user list

# ════════════════════════════════════════════════════════════════════════════
# PHASE 2 — Switch to Permission account model
# ════════════════════════════════════════════════════════════════════════════

echo "###################################################################"
echo "# PHASE 2: Enabling require-permission-accounts"
echo "###################################################################"

# Enable the flag.  From this point, the legacy path is blocked for all
# instructions that use authorize() (access-pass, user delete, permissions).
# Processors that still check foundation_allowlist directly (device, link,
# contributor, etc.) continue to work — the flag only enforces the new model
# for instructions already migrated to authorize().
./target/doublezero global-config feature-flags set --enable require-permission-accounts
./target/doublezero global-config feature-flags get

# Bootstrap the payer's Permission account.
# Foundation members retain access to permission management even in strict
# mode (PERMISSION_ADMIN bootstrap exception in authorize.rs), so this works
# without a pre-existing Permission account.
echo "Creating Permission account for payer"
./target/doublezero permission set --user-payer "$PAYER" \
    --add permission-admin \
    --add access-pass-admin \
    --add user-admin \
    --add network-admin \
    --add infra-admin \
    --add contributor-admin \
    --add tenant-admin \
    --add multicast-admin

./target/doublezero permission get --user-payer "$PAYER"
./target/doublezero permission list

# ── Phase 2 locations & exchanges ────────────────────────────────────────────

echo "Phase 2: adding more locations and exchanges"
./target/doublezero location create --code pit --name "Pittsburgh" --country US --lat 40.45119259881935 --lng -80.00498215509094

./target/doublezero exchange create --code xpit --name "Pittsburgh" --lat 40.45119259881935 --lng -80.00498215509094

# ── Phase 2 contributor ───────────────────────────────────────────────────────

echo "Phase 2: creating contributor co03"
./target/doublezero contributor create --code co03 --owner me

# ── Phase 2 devices ───────────────────────────────────────────────────────────

echo "Phase 2: creating devices (permission-account mode)"
./target/doublezero device create --code fra-dz01 --contributor co02 --location fra --exchange xfra \
    --public-ip "195.219.220.88" --dz-prefixes "104.0.0.0/16" \
    --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt \
    --desired-status activated -w
./target/doublezero device create --code sin-dz01 --contributor co02 --location sin --exchange xsin \
    --public-ip "180.87.102.104" --dz-prefixes "105.0.0.0/16" \
    --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt \
    --desired-status activated -w
./target/doublezero device create --code tyo-dz01 --contributor co02 --location tyo --exchange xtyo \
    --public-ip "180.87.154.112" --dz-prefixes "106.0.0.0/16" \
    --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt \
    --desired-status activated -w
./target/doublezero device create --code pit-dz01 --contributor co03 --location pit --exchange xpit \
    --public-ip "204.16.241.243" --dz-prefixes "107.0.0.0/16" \
    --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt \
    --desired-status activated -w

./target/doublezero device update --pubkey fra-dz01 --max-users 128
./target/doublezero device update --pubkey sin-dz01 --max-users 128
./target/doublezero device update --pubkey tyo-dz01 --max-users 128
./target/doublezero device update --pubkey pit-dz01 --max-users 128

# ── Phase 2 interfaces ────────────────────────────────────────────────────────

echo "Phase 2: creating device interfaces"
./target/doublezero device interface create fra-dz01 "Switch1/1/1" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create fra-dz01 "Switch1/1/2" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create sin-dz01 "Switch1/1/1" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create sin-dz01 "Switch1/1/2" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create tyo-dz01 "Switch1/1/1" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create tyo-dz01 "Switch1/1/2" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create pit-dz01 "Switch1/1/1" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w
./target/doublezero device interface create pit-dz01 "Switch1/1/2" --bandwidth "10 Gbps" --cir "10 Gbps" --mtu 1500 --routing-mode static -w

# ── Phase 2 links ─────────────────────────────────────────────────────────────

echo "Phase 2: creating links"
./target/doublezero link create wan --code "ams-dz01:fra-dz01" --contributor co01 \
    --side-a ams-dz01 --side-a-interface Switch1/1/2 \
    --side-z fra-dz01 --side-z-interface Switch1/1/1 \
    --bandwidth "10 Gbps" --mtu 9000 --delay-ms 8 --jitter-ms 1 \
    --desired-status activated -w
./target/doublezero link create wan --code "fra-dz01:sin-dz01" --contributor co02 \
    --side-a fra-dz01 --side-a-interface Switch1/1/2 \
    --side-z sin-dz01 --side-z-interface Switch1/1/1 \
    --bandwidth "10 Gbps" --mtu 9000 --delay-ms 150 --jitter-ms 10 \
    --desired-status activated -w
./target/doublezero link create wan --code "sin-dz01:tyo-dz01" --contributor co02 \
    --side-a sin-dz01 --side-a-interface Switch1/1/2 \
    --side-z tyo-dz01 --side-z-interface Switch1/1/1 \
    --bandwidth "10 Gbps" --mtu 9000 --delay-ms 70 --jitter-ms 5 \
    --desired-status activated -w
./target/doublezero link create wan --code "la2-dz01:pit-dz01" --contributor co03 \
    --side-a la2-dz01 --side-a-interface Switch1/1/2 \
    --side-z pit-dz01 --side-z-interface Switch1/1/1 \
    --bandwidth "10 Gbps" --mtu 9000 --delay-ms 50 --jitter-ms 4 \
    --desired-status activated -w

# ── Phase 2 access passes (new path — Permission account used) ────────────────

echo "Phase 2 access passes (permission-account path)"
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 200.0.0.1
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 200.0.0.2
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 200.0.0.3 --tenant acme

# ── Phase 2 users ─────────────────────────────────────────────────────────────

echo "Phase 2 users (permission-account path)"
./target/doublezero user create --device fra-dz01 --client-ip 200.0.0.1 -w
./target/doublezero user create --device sin-dz01 --client-ip 200.0.0.2 -w
./target/doublezero user create --device tyo-dz01 --client-ip 200.0.0.3 --tenant acme -w

# ── Permission management validation ─────────────────────────────────────────

echo "Phase 2: permission management"
SECOND_PUBKEY=testGjWJiksK7wdGmH7ZZsaqRGU695LHgvjRd6jfHYF

./target/doublezero permission set --user-payer "$SECOND_PUBKEY" \
    --add network-admin \
    --add infra-admin

./target/doublezero permission set --user-payer "$SECOND_PUBKEY" --add user-admin
./target/doublezero permission set --user-payer "$SECOND_PUBKEY" --remove infra-admin
./target/doublezero permission get --user-payer "$SECOND_PUBKEY"

./target/doublezero permission suspend --user-payer "$SECOND_PUBKEY"
./target/doublezero permission get --user-payer "$SECOND_PUBKEY"

./target/doublezero permission resume --user-payer "$SECOND_PUBKEY"
./target/doublezero permission get --user-payer "$SECOND_PUBKEY"

# ── Final state ───────────────────────────────────────────────────────────────

echo "Final state"
./target/doublezero device list
./target/doublezero link list
./target/doublezero access-pass list
./target/doublezero user list
./target/doublezero permission list

echo "########################################################################"
echo "Phase 1 (legacy) + Phase 2 (permission accounts) — validation complete"
