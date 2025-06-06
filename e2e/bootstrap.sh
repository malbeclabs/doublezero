#!/bin/bash

GIT_SHA=`git rev-parse --short HEAD`
DZD_NAME=dzd01_$GIT_SHA
NET_CYOA=$GIT_SHA
TIMEOUT=60  # Healthcheck timeout in seconds for the DZ device container
INTERVAL=2  # Check interval in seconds


start_time=$(date +%s)

function cleanup {
    docker rm -f $DZD_NAME
    docker network rm $NET_CYOA
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        iptables -D DOCKER-USER -j ACCEPT
    fi
}

trap cleanup EXIT

main() {
    if [ "$EUID" -ne 0 ]
        then echo "error: This script must be run as root"
        exit 1
    fi
    print_banner "Starting DoubleZero device container"
    start_doublezero_device

    print_banner "Wait for DoubleZero device healthcheck to pass"
    check_doublezero_device_health

    print_banner "Run end-to-end tests"
    start_e2e_tests
}

print_banner() {
    echo "------------------------------------------------"
    echo $*
    echo "------------------------------------------------"
}

start_doublezero_device() {
    docker network create --subnet 64.86.249.0/24 --internal $NET_CYOA

    # Interface ordering very much matters with containerized EOS. The first network
    # attached is the management interface, then subsequent networks correspond to
    # ethernet interfaces.d
    #
    # Docker attaches interfaces in seemingly random order if the container is not yet started.
    # If the networks end up attached in the wrong order, this test will fail as the CYOA network
    # will not be attached to Ethernet1. To avoid this, we start the container with the default bridge
    # network attached, then attach the CYOA network to the container.
    docker create --name=$DZD_NAME --privileged -t agent:$GIT_SHA
    docker start $DZD_NAME
    docker network connect --ip=64.86.249.80 $NET_CYOA $DZD_NAME

    # In github actions w/ the arista container, docker iptables rules
    # only allow traffic to/from the interconnect link which causes traffic
    # to other interfaces on the container (i.e. loopback0) to be dropped.
    # We allow all traffic as an override during the test
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        iptables -I DOCKER-USER -j ACCEPT
    fi
}

check_doublezero_device_health() {
    while true; do
        health_status=$(docker inspect --format='{{.State.Health.Status}}' "$DZD_NAME" 2>/dev/null)

        if [[ "$health_status" == "healthy" ]]; then
            echo "Container $DZD_NAME is healthy."
            break
        fi

        current_time=$(date +%s)
        elapsed_time=$((current_time - start_time))

        if (( elapsed_time >= TIMEOUT )); then
            echo "Timed out waiting for $DZD_NAME to become healthy."
            exit 1
        fi

        sleep $INTERVAL
    done
}

start_e2e_tests() {
    # The e2e test container is connected directly to the DZ device container.
    docker run --platform linux/amd64 --name e2e_$GIT_SHA --privileged --rm --net $NET_CYOA --ip=64.86.249.86 ghcr.io/malbeclabs/doublezero-e2e:$GIT_SHA
}

main "$@"; exit
