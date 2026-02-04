#!/bin/bash

clear
killall solana-test-validator > /dev/null 2>&1
killall doublezero-activator > /dev/null 2>&1
killall solana > /dev/null 2>&1

set -e
set -x

mkdir -p ./logs ./target

export OPENSSL_NO_VENDOR=1

# Build the program
echo "Build the program"
cargo build-sbf --manifest-path ../programs/doublezero-serviceability/Cargo.toml -- -Znext-lockfile-bump
cp ../../target/deploy/doublezero_serviceability.so ./target/doublezero_serviceability.so

#Build the activator
echo "Build the activator"
cargo build --manifest-path ../../activator/Cargo.toml ; cp ../../target/debug/doublezero-activator ./target/

#Build the activator
echo "Build the client"
cargo build --manifest-path ../../client/doublezero/Cargo.toml; cp ../../target/debug/doublezero ./target/

# Configure to connect to localnet
solana config set --url http://127.0.0.1:8899

# configure doublezero to connect to local test cluster
./target/doublezero config set --url http://127.0.0.1:8899
./target/doublezero config set --program-id 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX

# start the solana test cluster
echo "Start solana local test cluster"
solana-test-validator --reset --bpf-program ./keypair.json ./target/doublezero_serviceability.so >./logs/validator.log 2>&1 &

# Wait for the solana test cluster to start
echo "Waiting 15 seconds to start the solana test cluster"
sleep 15

# start isntruction logger
echo "Start instruction logger"
solana logs >./logs/instruction.log 2>&1 &

# Initialize doublezero smart contract
./target/doublezero init

### Configure global setting
./target/doublezero global-config set --local-asn 65100 --remote-asn 65001 --device-tunnel-block 172.16.0.0/16 --user-tunnel-block 169.254.0.0/16 \
    --multicastgroup-block 233.84.178.0/24 

./target/doublezero global-config authority set --activator-authority me --sentinel-authority me

# Build the activator
echo "Start the activator"
RUST_LOG=debug ./target/doublezero-activator --program-id 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX --rpc http://127.0.0.1:8899 --ws ws://127.0.0.1:8900 --keypair ~/.config/doublezero/id.json --onchain-allocation >./logs/activator.log 2>&1 &

echo "Add allowlist"
./target/doublezero global-config allowlist add --pubkey 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX

### Initialize locations
echo "Creating locations"
./target/doublezero location create --code ewr --name "New York" --country US --lat 40.780297071772125 --lng -74.07203003496925

### Initialize exchanges
echo "Creating exchanges"
./target/doublezero exchange create --code xewr --name "New York" --lat 40.780297071772125 --lng -74.07203003496925

### Initialize contributor
echo "Creating contributor"
./target/doublezero contributor create --code co01 --owner me

### Initialize devices
echo "Creating devices"
./target/doublezero device create --code ny5-dz01 --contributor co01 --location ewr --exchange xewr --public-ip "64.86.249.80" --dz-prefixes "101.0.0.0/31" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt --desired-status activated -w

### Initialize device interfaces
echo "Creating device interfaces"
./target/doublezero device interface create ny5-dz01 "Switch1/1/1" -w
./target/doublezero device interface create ny5-dz01 "Switch1/1/2" -w

### Update devices to set max users
echo "Update devices to set max users"
./target/doublezero device update --pubkey ny5-dz01 --max-users 128

# create access pass
echo "Create AccessPass for all IPs"
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 100.0.0.5
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 100.0.0.6

./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 177.54.159.95
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 147.28.171.51
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 100.100.100.100
./target/doublezero access-pass set --accesspass-type prepaid --user-payer me --client-ip 200.200.200.200

echo "Creating multicast groups"
./target/doublezero multicast group create --code mg01 --max-bandwidth 1Gbps --owner me -w


echo "Add me to multicast group allowlist"
./target/doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip 100.0.0.5
./target/doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip 100.0.0.5
./target/doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip 100.0.0.6
./target/doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip 100.0.0.6

echo "Creating multicast user & subscribe"
./target/doublezero user create-subscribe --device ny5-dz01 --client-ip 100.0.0.5 --publisher mg01 -w
./target/doublezero user list
./target/doublezero user delete --pubkey 7URnG9XJasEUqkvCSE9C8qgvAxzsbU8a9e1QebMkqxeb
sleep 1

./target/doublezero user create-subscribe --device ny5-dz01 --client-ip 100.0.0.5 --publisher mg01
sleep 1
./target/doublezero user list
tail ./logs/activator.log

echo "########################################################################"
echo "Setup complete"
