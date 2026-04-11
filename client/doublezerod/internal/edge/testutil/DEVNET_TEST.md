# Testing Edge Feed Parser in Local Devnet

This guide walks through testing the edge feed parser end-to-end in the
local devnet environment with real containers and multicast infrastructure.

## Prerequisites

- Docker running with sufficient resources (8GB+ RAM for cEOS devices)
- Local devnet not already running (or destroy first with `dev/dzctl destroy -y`)

## Quick Start

From the repo root:

```bash
# 1. Build all container images (includes your code changes)
dev/dzctl build

# 2. Start core devnet (ledger, manager, activator, controller)
dev/dzctl start

# 3. Add two devices
dev/dzctl add-device --code dz1 --exchange xams --location ams \
  --cyoa-network-host-id 8 --additional-networks dz1:dz2
dev/dzctl add-device --code dz2 --exchange xams --location ams \
  --cyoa-network-host-id 9 --additional-networks dz1:dz2

# 4. Add a publisher client and a subscriber client
dev/dzctl add-client --cyoa-network-host-id 100
dev/dzctl add-client --cyoa-network-host-id 110
```

## Identify Your Containers

```bash
docker ps --filter "label=dz.malbeclabs.com/type=devnet" --format "table {{.Names}}\t{{.Status}}"
```

Note the client container names — they include the Solana pubkey, e.g.:
- `dz-local-client-FposHWrkvPP3VErBAWCd4ELWGuh2mgx2Wx6cuNEA4X2S` (publisher)
- `dz-local-client-7bK9xpQwR2tN...` (subscriber)

## Create Multicast Group and Connect

```bash
# Get the client pubkeys
PUB_KEY=$(docker exec dz-local-client-<publisher> solana address)
SUB_KEY=$(docker exec dz-local-client-<subscriber> solana address)

# Create multicast group
docker exec dz-local-manager doublezero multicast group create \
  --code mg01 --max-bandwidth 10Gbps

# Add publisher to allowlist
docker exec dz-local-manager doublezero multicast group allowlist publisher add \
  --code mg01 --user-payer $PUB_KEY --client-ip 10.0.100.100

# Add subscriber to allowlist
docker exec dz-local-manager doublezero multicast group allowlist subscriber add \
  --code mg01 --user-payer $SUB_KEY --client-ip 10.0.100.110

# Connect publisher
docker exec dz-local-client-<publisher> doublezero connect multicast publisher mg01

# Connect subscriber
docker exec dz-local-client-<subscriber> doublezero connect multicast subscriber mg01

# Verify connections
docker exec dz-local-client-<subscriber> doublezero status
```

## Enable Edge Feed Parser

```bash
# Enable the feed parser on the subscriber
docker exec dz-local-client-<subscriber> curl -s -X POST \
  --unix-socket /var/run/doublezerod/doublezerod.sock \
  -H 'Content-Type: application/json' \
  -d '{"code":"mg01","parser":"topofbook","format":"json","output":"/tmp/feed.jsonl","marketdata_port":7000,"refdata_port":7001}' \
  http://doublezero/edge/enable

# Check status
docker exec dz-local-client-<subscriber> curl -s \
  --unix-socket /var/run/doublezerod/doublezerod.sock \
  http://doublezero/edge/status | jq .
```

## Send Synthetic Top-of-Book Data

Build and copy the publisher tool into the publisher container:

```bash
# Build the publisher binary (from repo root)
CGO_ENABLED=0 go build -o /tmp/topofbook-publisher \
  ./client/doublezerod/internal/edge/testutil/cmd/topofbook-publisher/

# Copy into publisher container
docker cp /tmp/topofbook-publisher dz-local-client-<publisher>:/usr/local/bin/

# Get the multicast IP for mg01
MCAST_IP=$(docker exec dz-local-manager doublezero multicast group list --json | jq -r '.[] | select(.code=="mg01") | .multicast_ip')

# Run the publisher (sends 3 instruments, 10 quotes/sec for 30s)
docker exec dz-local-client-<publisher> topofbook-publisher \
  -group $MCAST_IP -port 7000 -instruments 3 -rate 10 -duration 30s
```

## Verify Output

```bash
# Check how many records were decoded
docker exec dz-local-client-<subscriber> wc -l /tmp/feed.jsonl

# View the first few decoded records
docker exec dz-local-client-<subscriber> head -20 /tmp/feed.jsonl | jq .

# Check parser status (records_written count)
docker exec dz-local-client-<subscriber> curl -s \
  --unix-socket /var/run/doublezerod/doublezerod.sock \
  http://doublezero/edge/status | jq .

# Disable the feed parser
docker exec dz-local-client-<subscriber> curl -s -X POST \
  --unix-socket /var/run/doublezerod/doublezerod.sock \
  -H 'Content-Type: application/json' \
  -d '{"code":"mg01"}' \
  http://doublezero/edge/disable
```

## Using the CLI Instead of curl

Once the Rust CLI is deployed in the container image, you can use:

```bash
docker exec dz-local-client-<subscriber> doublezero edge enable \
  --code mg01 --parser topofbook --format json --output /tmp/feed.jsonl \
  --marketdata-port 7000 --refdata-port 7001

docker exec dz-local-client-<subscriber> doublezero edge status --json

docker exec dz-local-client-<subscriber> doublezero edge disable --code mg01
```

## Testing CSV Output

```bash
docker exec dz-local-client-<subscriber> curl -s -X POST \
  --unix-socket /var/run/doublezerod/doublezerod.sock \
  -H 'Content-Type: application/json' \
  -d '{"code":"mg01","parser":"topofbook","format":"csv","output":"/tmp/feed.csv","marketdata_port":7000,"refdata_port":7001}' \
  http://doublezero/edge/enable

# After sending data:
docker exec dz-local-client-<subscriber> head -20 /tmp/feed.csv
```

## Testing Unix Socket Output

```bash
# Enable with socket output
docker exec dz-local-client-<subscriber> curl -s -X POST \
  --unix-socket /var/run/doublezerod/doublezerod.sock \
  -H 'Content-Type: application/json' \
  -d '{"code":"mg01","parser":"topofbook","format":"json","output":"unix:///tmp/feed.sock","marketdata_port":7000,"refdata_port":7001}' \
  http://doublezero/edge/enable

# Connect a reader to the socket (in another terminal)
docker exec dz-local-client-<subscriber> socat UNIX-CONNECT:/tmp/feed.sock -
```

## Cleanup

```bash
dev/dzctl destroy -y
```
