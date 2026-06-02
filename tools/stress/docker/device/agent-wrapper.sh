#!/bin/bash
# Wrapper invoked over SSH by the orchestrator as `doublezero-agent`.
# The orchestrator's SSH command is hardcoded today as:
#   doublezero-agent -verbose [-controller HOST:PORT]
# It does not pass -pubkey or enable metrics. This wrapper supplies both so
# the agent can fetch its config and the observer can scrape its counters.
set -eu

PUBKEY_FILE="/etc/doublezero/agent/pubkey"
PUBKEY=""
if [ -r "$PUBKEY_FILE" ]; then
    PUBKEY="$(tr -d '[:space:]' < "$PUBKEY_FILE")"
fi

EXTRA_ARGS=()
if [ -n "$PUBKEY" ]; then
    EXTRA_ARGS+=(-pubkey "$PUBKEY")
fi
EXTRA_ARGS+=(-metrics-enable -metrics-addr ":9100")

exec /mnt/flash/doublezero-agent "${EXTRA_ARGS[@]}" "$@"
