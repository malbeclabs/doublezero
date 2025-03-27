#!/bin/bash

# Build the program
echo "Build the program"
cargo build-sbf --manifest-path ../programs/dz-sla-program/Cargo.toml
cp ../programs/dz-sla-program/target/deploy/double_zero_sla_program.so ./target/double_zero_sla_program.so

#Build the activator
echo "Build the activator"
cargo build --manifest-path ../../activator/Cargo.toml
cp ../../activator/target/debug/doublezero-activator ./target/

#Build the activator
echo "Build the client"
cargo build --manifest-path ../../client/doublezero/Cargo.toml 
cp ../../client/doublezero/target/debug/doublezero ./target/


# start the solana test cluster
echo "Start solana local test cluster"
solana-test-validator --reset --bpf-program ./keypair.json ./target/double_zero_sla_program.so > ./logs/validator.log 2>&1 &

# Wait for the solana test cluster to start
echo "Waiting 15 seconds to start the solana test cluster"
sleep 15


# Configure to connect to localnet
solana config set --url http://127.0.0.1:8899

# configure doublezero to connect to local test cluster
./target/doublezero config set --url localnet
./target/doublezero config set --program 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX

# initialice doublezero smart contract
./target/doublezero init 

### Configure global setting
./target/doublezero global-config set --local-asn 65100 --remote-asn 65001 --tunnel-tunnel-block 172.16.0.0/16 --device-tunnel-block 169.254.0.0/16


# Build the activator
echo "Start the activator"
./target/doublezero-activator --program-id 7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX  > ./logs/activator.log 2>&1 &

### Initialice locations
echo "Location"
./target/doublezero location create --code lax --name "Los Angeles" --country US --lat 34.049641274076464 --lng -118.25939642499903
./target/doublezero location create --code ewr --name "New York" --country US --lat 40.780297071772125 --lng -74.07203003496925
./target/doublezero location create --code lhr --name "London" --country UK --lat 51.513999803939384 --lng -0.12014764843092213
./target/doublezero location create --code fra --name "Frankfurt" --country DE --lat 50.1215356432098 --lng 8.642047117175098
./target/doublezero location create --code sin --name "Singapore" --country SG --lat 1.2807150707390342 --lng 103.85507136144396
./target/doublezero location create --code tyo --name "Tokyo" --country JP --lat 35.66875144228767 --lng 139.76565267564501
./target/doublezero location create --code pit --name "Pittsburgh" --country US --lat 40.45119259881935 --lng -80.00498215509094
./target/doublezero location create --code ams --name "Amsterdam" --country US --lat 52.30085793004002 --lng 4.942241140085309

### Initialice exchanges
echo "Exchange"
./target/doublezero exchange create --code xlax --name "Los Angeles" --lat 34.049641274076464 --lng -118.25939642499903
./target/doublezero exchange create --code xewr --name "New York" --lat 40.780297071772125 --lng -74.07203003496925
./target/doublezero exchange create --code xlhr --name "London" --lat 51.513999803939384 --lng -0.12014764843092213
./target/doublezero exchange create --code xfra --name "Frankfurt" --lat 50.1215356432098 --lng 8.642047117175098
./target/doublezero exchange create --code xsin --name "Singapore" --lat 1.2807150707390342 --lng 103.85507136144396
./target/doublezero exchange create --code xtyo --name "Tokyo" --lat 35.66875144228767 --lng 139.76565267564501
./target/doublezero exchange create --code xpit --name "Pittsburgh" --lat 40.45119259881935 --lng -80.00498215509094
./target/doublezero exchange create --code xams --name "Amsterdam" --lat 52.30085793004002 --lng 4.942241140085309

### Initialice devices
echo "Device"
./target/doublezero device create --code la2-dz01 --location lax --exchange xlax --public-ip "207.45.216.134" --dz-prefixes "207.45.216.136/29"
./target/doublezero device create --code ny5-dz01 --location ewr --exchange xewr --public-ip "64.86.249.80" --dz-prefixes "64.86.249.80/29"
./target/doublezero device create --code ld4-dz01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.72/30,195.219.120.76/30"
./target/doublezero device create --code frk-dz01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.88/29"
./target/doublezero device create --code sg1-dz01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.104/29"
./target/doublezero device create --code ty2-dz01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.112/29"
./target/doublezero device create --code pit-dzd01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32"
./target/doublezero device create --code ams-dz001 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29"


### Initialice tunnels
echo "Tunnel"
./target/doublezero tunnel create --code "la2-dz01:ny5-dz01" --side-a la2-dz01 --side-z ny5-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
./target/doublezero tunnel create --code "ny5-dz01:ld4-dz01" --side-a ny5-dz01 --side-z ld4-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
./target/doublezero tunnel create --code "ld4-dz01:frk-dz01" --side-a ld4-dz01 --side-z frk-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
./target/doublezero tunnel create --code "ld4-dz01:sg1-dz01" --side-a ld4-dz01 --side-z sg1-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9
./target/doublezero tunnel create --code "sg1-dz01:ty2-dz01" --side-a sg1-dz01 --side-z ty2-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7
./target/doublezero tunnel create --code "ty2-dz01:la2-dz01" --side-a ty2-dz01 --side-z la2-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10

echo "Doublezero smart contract initialized"

# create a user
./target/doublezero user create --device ld4-dz01 --client-ip 10.0.0.1
./target/doublezero user create --device ld4-dz01 --client-ip 10.0.0.2
./target/doublezero user create --device ld4-dz01 --client-ip 10.0.0.3
./target/doublezero user create --device ld4-dz01 --client-ip 10.0.0.4
