#!/bin/bash

set -e

SOLANA_KEYPAIR=/root/.config/solana/id.json
VALIDATOR_URL=http://127.0.0.1:8899
VALIDATOR_WS=ws://127.0.0.1:8900
PROGRAM_ID=7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX

CONTROLLER_ADDR=0.0.0.0
CONTROLLER_PORT=7000
export CONTROLLER_ADDR
export CONTROLLER_PORT

# This is the pubkey for the agent correlating to ny5-dz01
# Device pubkeys are created deterministically based on the order
# in which they are created onchain.
# WARNING - IF YOU CHANGE THE DEVICE CREATION ORDERING, THIS WILL BE
# INCORRECT AND THE TEST WILL FAIL
AGENT_PUBKEY=8scDVeZ8aB1TRTkBqaZgzxuk7WwpARdF1a39wYA7nR3W
AGENT_DEVICE=ny5-dz01
export AGENT_PUBKEY

main() {
    # WARNING: docker networks are connected unordered to containers. This can break
    #          networking between the e2e container and the device container. The Arista
    #          cEOS container requires at least two networks attached (a management network
    #          and at least 1 front panel port network i.e. to the e2e container). Docker
    #          will attach these in a random order which can cause the network facing the e2e container
    #          to be incorrect from the perspective of the device container.
    print_banner "Check networking to DoubleZero device\n"
    ping -c 3 -q 64.86.249.80

    print_banner "Starting local validator w/ smartcontract program"
    solana-test-validator --reset --bpf-program ./bin/keypair.json ./bin/doublezero_sla_program.so >/tmp/solana.log 2>&1 &
    echo "Waiting 15 seconds to start the solana test cluster"
    sleep 15

    print_banner "Initialize doublezero client configuration"
    init_doublezero

    print_banner "Initialize smart contract"
    doublezero --keypair $SOLANA_KEYPAIR init

    print_banner "Initializing activator"
    doublezero-activator --program-id $PROGRAM_ID >/tmp/activator.log 2>&1 &

    print_banner "Initializing onchain data"
    populate_data_onchain

    print_banner "Initializing doublezero controller"
    doublezero-controller start -listen-addr 0.0.0.0 -listen-port 7000 -program-id $PROGRAM_ID -solana-rpc-endpoint $VALIDATOR_URL -no-hardware &

    print_banner "Initializing doublezero daemon"
    start_doublezerod
    sleep 5

    print_banner "Waiting for latency results (75 second timeout)"
    e2e_test -test.v -test.run "^TestWaitForLatencyResults"

    print_banner "Latency results"
    doublezero latency

    print_banner "Running IBRL tests"
    test_ibrl

    print_banner "Running IBRL w/ allocated address tests"
    test_ibrl_with_allocated_addr

    print_banner "Running multicast publisher tests"
    test_multicast_publisher

    print_banner "Running multicast subscriber tests"
    test_multicast_subscriber
}

print_banner() {
    echo "------------------------------------------------"
    echo $*
    echo "------------------------------------------------"
}

test_ibrl_with_allocated_addr() {
    print_banner "Connecting user tunnel"
    doublezero --keypair $SOLANA_KEYPAIR connect ibrl --client-ip 64.86.249.86 --allocate-addr

    print_banner "Creating multiple users to exhaust the /30 and allocate from the /29, ie use both blocks"
    create_multiple_ibrl_with_allocated_address_users

    print_banner "Waiting for client tunnel to be up before starting tests"
    e2e_test -test.v -test.run "^TestWaitForClientTunnelUp"

    print_banner "Running post-connect tests"
    e2e_test -test.v -test.run "^TestIBRLWithAllocatedAddress_Connect_Networking"

    print_banner "Disconnecting user tunnel"
    doublezero --keypair $SOLANA_KEYPAIR disconnect --client-ip 64.86.249.86

    print_banner "Running post-disconnect tests"
    e2e_test -test.v -test.run "^TestIBRLWithAllocatedAddress_Disconnect_Networking"
    e2e_test -test.v -test.run "^TestIBRLWithAllocatedAddress_Disconnect_Output"
}

test_ibrl() {
    print_banner "Connecting user tunnel"
    doublezero --keypair $SOLANA_KEYPAIR connect ibrl --client-ip 64.86.249.86

    print_banner "Waiting for client tunnel to be up before starting tests"
    e2e_test -test.v -test.run "^TestWaitForClientTunnelUp"

    print_banner "Running post-connect tests"
    e2e_test -test.v -test.run "^TestIBRL_Connect_Networking"

    print_banner "Disconnecting user tunnel"
    doublezero --keypair $SOLANA_KEYPAIR disconnect --client-ip 64.86.249.86

    print_banner "Running post-disconnect tests"
    e2e_test -test.v -test.run "TestIBRL_Disconnect_Networking"
    # e2e_test -test.v -test.run "TestIBRL_Disconnect_Output"
}

init_doublezero() {
    solana config set --url $VALIDATOR_URL

    doublezero --keypair $SOLANA_KEYPAIR config set --url $VALIDATOR_URL
    doublezero --keypair $SOLANA_KEYPAIR config set --ws $VALIDATOR_WS
    doublezero --keypair $SOLANA_KEYPAIR config set --program-id $PROGRAM_ID
}

start_doublezerod() {
    # create path for socket file
    mkdir /var/run/doublezerod
    # create state file directory
    mkdir /var/lib/doublezerod
    doublezerod -program-id $PROGRAM_ID -solana-rpc-endpoint $VALIDATOR_URL -probe-interval 5 -cache-update-interval 3 &
}

populate_data_onchain() {
    print_banner "Populate global configuration onchain"
    echo doublezero global-config set --local-asn 65000 --remote-asn 65342 --tunnel-tunnel-block 172.16.0.0/16 --device-tunnel-block 169.254.0.0/16 --multicastgroup-block 224.5.6.0/24
    doublezero global-config set --local-asn 65000 --remote-asn 65342 --tunnel-tunnel-block 172.16.0.0/16 --device-tunnel-block 169.254.0.0/16 --multicastgroup-block 224.5.6.0/24
    print_banner "Global configuration onchain"
    doublezero global-config get

    print_banner "Populate location information onchain"
    doublezero --keypair $SOLANA_KEYPAIR location create --code lax --name "Los Angeles" --country US --lat 34.049641274076464 --lng -118.25939642499903
    doublezero location create --code ewr --name "New York" --country US --lat 40.780297071772125 --lng -74.07203003496925
    doublezero location create --code lhr --name "London" --country UK --lat 51.513999803939384 --lng -0.12014764843092213
    doublezero location create --code fra --name "Frankfurt" --country DE --lat 50.1215356432098 --lng 8.642047117175098
    doublezero location create --code sin --name "Singapore" --country SG --lat 1.2807150707390342 --lng 103.85507136144396
    doublezero location create --code tyo --name "Tokyo" --country JP --lat 35.66875144228767 --lng 139.76565267564501
    doublezero location create --code pit --name "Pittsburgh" --country US --lat 40.45119259881935 --lng -80.00498215509094
    doublezero location create --code ams --name "Amsterdam" --country US --lat 52.30085793004002 --lng 4.942241140085309
    print_banner "Location information onchain"
    doublezero location list

    print_banner "Populate exchange information onchain"
    doublezero exchange create --code xlax --name "Los Angeles" --lat 34.049641274076464 --lng -118.25939642499903
    doublezero exchange create --code xewr --name "New York" --lat 40.780297071772125 --lng -74.07203003496925
    doublezero exchange create --code xlhr --name "London" --lat 51.513999803939384 --lng -0.12014764843092213
    doublezero exchange create --code xfra --name "Frankfurt" --lat 50.1215356432098 --lng 8.642047117175098
    doublezero exchange create --code xsin --name "Singapore" --lat 1.2807150707390342 --lng 103.85507136144396
    doublezero exchange create --code xtyo --name "Tokyo" --lat 35.66875144228767 --lng 139.76565267564501
    doublezero exchange create --code xpit --name "Pittsburgh" --lat 40.45119259881935 --lng -80.00498215509094
    doublezero exchange create --code xams --name "Amsterdam" --lat 52.30085793004002 --lng 4.942241140085309
    print_banner "Exchange information onchain"
    doublezero exchange list

    print_banner "Populate device information onchain - DO NOT SHUFFLE THESE AS THE PUBKEYS WILL CHANGE"
    doublezero device create --code la2-dz01 --location lax --exchange xlax --public-ip "207.45.216.134" --dz-prefixes "207.45.216.136/30,200.12.12.12/29"
    doublezero device create --code ny5-dz01 --location ewr --exchange xewr --public-ip "64.86.249.80" --dz-prefixes "64.86.249.80/29"
    doublezero device create --code ld4-dz01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.72/29"
    doublezero device create --code frk-dz01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.88/29"
    doublezero device create --code sg1-dz01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.104/29"
    doublezero device create --code ty2-dz01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.112/29"
    doublezero device create --code pit-dzd01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32"
    doublezero device create --code ams-dz001 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29"
    print_banner "Device information onchain"
    doublezero device list

    print_banner "Adding null routes to test latency selection to ny5-dz01."
    ip rule add priority 1 from 64.86.249.86/32 to all table main
    ip route add 207.45.216.134/32 dev lo proto static scope host
    ip route add 195.219.120.72/32 dev lo proto static scope host
    ip route add 195.219.220.88/32 dev lo proto static scope host
    ip route add 180.87.102.104/32 dev lo proto static scope host
    ip route add 180.87.154.112/32 dev lo proto static scope host
    ip route add 204.16.241.243/32 dev lo proto static scope host
    ip route add 195.219.138.50/32 dev lo proto static scope host

    print_banner "Populate tunnel information onchain"
    doublezero tunnel create --code "la2-dz01:ny5-dz01" --side-a la2-dz01 --side-z ny5-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
    doublezero tunnel create --code "ny5-dz01:ld4-dz01" --side-a ny5-dz01 --side-z ld4-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
    doublezero tunnel create --code "ld4-dz01:frk-dz01" --side-a ld4-dz01 --side-z frk-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
    doublezero tunnel create --code "ld4-dz01:sg1-dz01" --side-a ld4-dz01 --side-z sg1-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9
    doublezero tunnel create --code "sg1-dz01:ty2-dz01" --side-a sg1-dz01 --side-z ty2-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7
    doublezero tunnel create --code "ty2-dz01:la2-dz01" --side-a ty2-dz01 --side-z la2-dz01 --tunnel-type MPLSoGRE --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10
    print_banner "Tunnel information onchain"
    doublezero tunnel list

    print_banner "Populate multicast group information onchain"
    doublezero multicast group create --code mg01 --max-bandwidth 10Gbps --owner me

    print_banner "Waiting for multicast group to be activated by activator"
    sleep 5

    print_banner "Multicast group information onchain"
    doublezero multicast group list

    print_banner "Add me to multicast group allowlist"
    doublezero multicast group allowlist publisher add --code mg01 --pubkey me
    doublezero multicast group allowlist subscriber add --code mg01 --pubkey me
}

create_multiple_ibrl_with_allocated_address_users() {
    print_banner "Creating multiple users on a single device"
    doublezero user create --device la2-dz01 --client-ip 1.2.3.4
    doublezero user create --device la2-dz01 --client-ip 2.3.4.5
    doublezero user create --device la2-dz01 --client-ip 3.4.5.6
    doublezero user create --device la2-dz01 --client-ip 4.5.6.7
    doublezero user create --device la2-dz01 --client-ip 5.6.7.8
    print_banner "Multiple users created"
}

test_multicast_publisher() {
    print_banner "Connecting multicast publisher"
    doublezero --keypair $SOLANA_KEYPAIR connect multicast publisher mg01 --client-ip 64.86.249.86

    print_banner "Waiting for client tunnel to be up before starting tests"
    e2e_test -test.v -test.run "^TestWaitForClientTunnelUp"

    print_banner "Running multicast publisher connect tests"
    e2e_test -test.v -test.run "^TestMulticastPublisher_Connect"

    print_banner "Disconnecting multicast publisher"
    doublezero --keypair $SOLANA_KEYPAIR disconnect multicast

    print_banner "Running multicast publisher disconnect tests"
    e2e_test -test.v -test.run "^TestMulticastPublisher_Disconnect_Networking"
    e2e_test -test.v -test.run "^TestMulticastPublisher_Disconnect_Output"
}

test_multicast_subscriber() {
    print_banner "Connecting multicast subscriber"
    doublezero --keypair $SOLANA_KEYPAIR connect multicast subscriber mg01 --client-ip 64.86.249.87

    print_banner "Waiting for client tunnel to be up before starting tests"
    e2e_test -test.v -test.run "^TestWaitForClientTunnelUp"

    print_banner "Running multicast subscriber connect tests"
    e2e_test -test.v -test.run "^TestMulticastSubscriber_Connect"

    print_banner "Disconnecting multicast subscriber"
    doublezero --keypair $SOLANA_KEYPAIR disconnect multicast

    print_banner "Running multicast subscriber disconnect tests"
    e2e_test -test.v -test.run "^TestMulticastSubscriber_Disconnect_Networking"
    e2e_test -test.v -test.run "^TestMulticastSubscriber_Disconnect_Output"
}

err() {
    echo "[$(date +'%Y-%m-%dT%H:%M:%S%z')]: $*" >&2
}

main "$@"
exit
