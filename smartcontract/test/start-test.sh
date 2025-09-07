#!/bin/bash

clear
pkill -9 -f validator
pkill -9 -f activator
pkill -9 -f solana

kill -9 $(ps -ef | grep activator | grep -v grep | awk '{print $2}')

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
RUST_LOG=debug ./target/doublezero-activator --program-id 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX --rpc http://127.0.0.1:8899 --ws ws://127.0.0.1:8900 --keypair ~/.config/doublezero/id.json >./logs/activator.log 2>&1 &

echo "Add allowlist"
./target/doublezero global-config allowlist add --pubkey 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX
./target/doublezero device allowlist add --pubkey 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX

### Initialize locations
echo "Creating locations"
./target/doublezero location create --code lax --name "XXXXXXX" --country US --lat 34.049641274076464 --lng -118.25939642499903
./target/doublezero location create --code ewr --name "New York" --country US --lat 40.780297071772125 --lng -74.07203003496925
./target/doublezero location create --code lhr --name "London" --country UK --lat 51.513999803939384 --lng -0.12014764843092213
./target/doublezero location create --code fra --name "Frankfurt" --country DE --lat 50.1215356432098 --lng 8.642047117175098
./target/doublezero location create --code sin --name "Singapore" --country SG --lat 1.2807150707390342 --lng 103.85507136144396
./target/doublezero location create --code tyo --name "Tokyo" --country JP --lat 35.66875144228767 --lng 139.76565267564501
./target/doublezero location create --code pit --name "Pittsburgh" --country US --lat 40.45119259881935 --lng -80.00498215509094
./target/doublezero location create --code ams --name "Amsterdam" --country US --lat 52.30085793004002 --lng 4.942241140085309

echo "Update locations"
./target/doublezero location update --pubkey XEY7fFCJ8r1FM9xwyvMqZ3GEgEbKNBTw65N2ynGJXRD --name "Los Angeles"

### Initialize exchanges
echo "Creating exchanges"
./target/doublezero exchange create --code xlax --name "XXXXXXXX" --lat 34.049641274076464 --lng -118.25939642499903
./target/doublezero exchange create --code xewr --name "New York" --lat 40.780297071772125 --lng -74.07203003496925
./target/doublezero exchange create --code xlhr --name "London" --lat 51.513999803939384 --lng -0.12014764843092213
./target/doublezero exchange create --code xfra --name "Frankfurt" --lat 50.1215356432098 --lng 8.642047117175098
./target/doublezero exchange create --code xsin --name "Singapore" --lat 1.2807150707390342 --lng 103.85507136144396
./target/doublezero exchange create --code xtyo --name "Tokyo" --lat 35.66875144228767 --lng 139.76565267564501
./target/doublezero exchange create --code xpit --name "Pittsburgh" --lat 40.45119259881935 --lng -80.00498215509094
./target/doublezero exchange create --code xams --name "Amsterdam" --lat 52.30085793004002 --lng 4.942241140085309

echo "Update exchanges"
./target/doublezero exchange update --pubkey EpE1QxRzUXFLSAPKcsGrHrdareBZ7hNsyJtTPw1iL7q8 --name "Los Angeles"

### Initialize contributor
echo "Creating contributor"
./target/doublezero contributor create --code co01 --owner me

### Initialize devices
echo "Creating devices"
./target/doublezero device create --code la2-dz01 --contributor co01 --location lax --exchange xlax --public-ip "207.45.216.134" --dz-prefixes "100.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w
./target/doublezero device create --code ny5-dz01 --contributor co01 --location ewr --exchange xewr --public-ip "64.86.249.80" --dz-prefixes "101.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w
./target/doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "102.0.0.0/29,103.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w
./target/doublezero device create --code frk-dz01 --contributor co01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "104.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w
./target/doublezero device create --code sg1-dz01 --contributor co01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "105.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w
./target/doublezero device create --code ty2-dz01 --contributor co01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "106.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w
./target/doublezero device create --code pit-dzd01 --contributor co01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "107.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w
./target/doublezero device create --code ams-dz001 --contributor co01 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "108.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w

### Initialize device interfaces
echo "Creating device interfaces"
./target/doublezero device interface create la2-dz01 "Switch1/1/1" -w
./target/doublezero device interface create la2-dz01 "Switch1/1/2" -w
./target/doublezero device interface create la2-dz01 "Switch1/1/3" -w
./target/doublezero device interface create ny5-dz01 "Switch1/1/1" -w
./target/doublezero device interface create ny5-dz01 "Switch1/1/2" -w
./target/doublezero device interface create ld4-dz01 "Switch1/1/1" -w
./target/doublezero device interface create ld4-dz01 "Switch1/1/2" -w
./target/doublezero device interface create ld4-dz01 "Switch1/1/3" -w
./target/doublezero device interface create frk-dz01 "Switch1/1/1" -w
./target/doublezero device interface create frk-dz01 "Switch1/1/2" -w
./target/doublezero device interface create sg1-dz01 "Switch1/1/1" -w
./target/doublezero device interface create sg1-dz01 "Switch1/1/2" -w
./target/doublezero device interface create ty2-dz01 "Switch1/1/1" -w
./target/doublezero device interface create ty2-dz01 "Switch1/1/2" -w
./target/doublezero device interface create pit-dzd01 "Switch1/1/1" -w
./target/doublezero device interface create pit-dzd01 "Switch1/1/2" -w
./target/doublezero device interface create ams-dz001 "Switch1/1/1" -w
./target/doublezero device interface create ams-dz001 "Switch1/1/2" -w

### Initialize links
echo "Creating internal links"
./target/doublezero link create wan --code "la2-dz01:ny5-dz01" --contributor co01 --side-a la2-dz01 --side-a-interface Switch1/1/1 --side-z ny5-dz01 --side-z-interface Switch1/1/2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3 -w
./target/doublezero link create wan --code "ny5-dz01:ld4-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Switch1/1/1 --side-z ld4-dz01 --side-z-interface Switch1/1/2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 -w
./target/doublezero link create wan --code "ld4-dz01:frk-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Switch1/1/1 --side-z frk-dz01 --side-z-interface Switch1/1/2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10 -w
./target/doublezero link create wan --code "ld4-dz01:sg1-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Switch1/1/3 --side-z sg1-dz01 --side-z-interface Switch1/1/2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9 -w
./target/doublezero link create wan --code "sg1-dz01:ty2-dz01" --contributor co01 --side-a sg1-dz01 --side-a-interface Switch1/1/1 --side-z ty2-dz01 --side-z-interface Switch1/1/2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7 -w
./target/doublezero link create wan --code "ty2-dz01:la2-dz01" --contributor co01 --side-a ty2-dz01 --side-a-interface Switch1/1/1 --side-z la2-dz01 --side-z-interface Switch1/1/2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10 -w

### Initialize contributor
echo "Creating contributor two"
./target/doublezero contributor create --code co02 --owner me

### Initialize devices
echo "Creating devices"
./target/doublezero device create --code la2-dz02 --contributor co02 --location lax --exchange xlax --public-ip "207.45.216.135" --dz-prefixes "130.0.0.0/16" --metrics-publisher 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB --mgmt-vrf mgmt -w

### Initialize device interfaces
echo "Creating device interfaces"
./target/doublezero device interface create la2-dz02 "Switch1/1/1" -w

### Initialize links
echo "Creating external links"
./target/doublezero link create dzx --code "la2-dz02-la2-dz01" --contributor co02 --side-a la2-dz02 --side-a-interface Switch1/1/1 --side-z la2-dz01 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3

### Initialize links
echo "Accepting external link"
./target/doublezero link accept --code "la2-dz02-la2-dz01" --side-z-interface Switch1/1/3 -w

### Update devices to set max users
echo "Update devices to set max users"
./target/doublezero device update --pubkey la2-dz01 --max-users 128
./target/doublezero device update --pubkey ny5-dz01 --max-users 128
./target/doublezero device update --pubkey ld4-dz01 --max-users 128
./target/doublezero device update --pubkey frk-dz01 --max-users 128
./target/doublezero device update --pubkey sg1-dz01 --max-users 128
./target/doublezero device update --pubkey ty2-dz01 --max-users 128
./target/doublezero device update --pubkey pit-dzd01 --max-users 128
./target/doublezero device update --pubkey ams-dz001 --max-users 128

# create access pass
echo "Create AccessPass for users"
./target/doublezero access-pass set --accesspass-type prepaid --client-ip 177.54.159.95 --user-payer me
./target/doublezero access-pass set --accesspass-type prepaid --client-ip 147.28.171.51 --user-payer me
./target/doublezero access-pass set --accesspass-type prepaid --client-ip 100.100.100.100 --user-payer me
./target/doublezero access-pass set --accesspass-type prepaid --client-ip 200.200.200.200 --user-payer me
./target/doublezero access-pass set --accesspass-type prepaid --client-ip 100.0.0.5 --user-payer me
./target/doublezero access-pass set --accesspass-type prepaid --client-ip 100.0.0.6 --user-payer me

./target/doublezero access-pass set --accesspass-type solana-validator --solana-validator 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX --client-ip 177.54.159.95 --user-payer me

# create a user
echo "Creating users"
./target/doublezero user create --device ld4-dz01 --client-ip 177.54.159.95 -w
./target/doublezero user create --device ld4-dz01 --client-ip 147.28.171.51 -w
./target/doublezero user create --device ld4-dz01 --client-ip 100.100.100.100 -w
./target/doublezero user create --device ld4-dz01 --client-ip 200.200.200.200 -w

echo "Creating multicast groups"
./target/doublezero multicast group create --code mg01 --max-bandwidth 1Gbps --owner me -w
./target/doublezero multicast group create --code mg02 --max-bandwidth 1Gbps --owner me -w
./target/doublezero multicast group create --code mg03 --max-bandwidth 1Gbps --owner me -w


echo "Add me to multicast group allowlist"
./target/doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip 100.0.0.5
./target/doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip 100.0.0.6

./target/doublezero multicast group allowlist subscriber add --code mg02 --user-payer me --client-ip 100.0.0.5
./target/doublezero multicast group allowlist subscriber add --code mg03 --user-payer me --client-ip 100.0.0.5

./target/doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip 100.0.0.6
./target/doublezero multicast group allowlist publisher add --code mg02 --user-payer me --client-ip 100.0.0.6
./target/doublezero multicast group allowlist publisher add --code mg03 --user-payer me --client-ip 100.0.0.6

echo "Creating multicast user & subscribe"
./target/doublezero user create-subscribe --device ty2-dz01 --client-ip 100.0.0.5 --subscriber mg01 -w
./target/doublezero user create-subscribe --device ty2-dz01 --client-ip 100.0.0.6 --subscriber mg01 -w


./target/doublezero user subscribe --user vwHPjLfH7aU4G2vDBAqV3on5WQgXLEKq67kNw7Q5Mos --group mg01 --publisher -w
./target/doublezero user subscribe --user vwHPjLfH7aU4G2vDBAqV3on5WQgXLEKq67kNw7Q5Mos --group mg02 --publisher -w


echo "########################################################################"
echo "Setup complete"