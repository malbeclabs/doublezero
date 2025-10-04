#!/usr/bin/env bash
set -euo pipefail

# Check for required environment variables.
if [ -z "${DZ_LEDGER_URL}" ]; then
  echo "DZ_LEDGER_URL is not set"
  exit 1
fi
if [ -z "${DZ_SERVICEABILITY_PROGRAM_ID}" ]; then
  echo "DZ_SERVICEABILITY_PROGRAM_ID is not set"
  exit 1
fi

# Wait for the solana validator to be healthy.
while ! curl -sf -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' \
  ${DZ_LEDGER_URL} | grep -q '"result":"ok"'; do
    echo "Waiting for solana validator to be ready..."
    sleep 1
done

# Generate a TLS cert and key, non-interactively.
echo "Generating TLS cert and key..."
openssl ecparam -genkey -name prime256v1 -out server.key
openssl req -new -key server.key -out server.csr -subj "/CN=localhost"
openssl x509 -req -in server.csr -signkey server.key -out server.crt
echo "TLS cert and key generated in server.crt and server.key"

# Start the controller.
doublezero-controller start -listen-addr 0.0.0.0 -listen-port 8443 -program-id ${DZ_SERVICEABILITY_PROGRAM_ID} -solana-rpc-endpoint ${DZ_LEDGER_URL} -device-local-asn 65342 -no-hardware -tls-cert server.crt -tls-key server.key $CONTROLLER_FLAGS
