#!/bin/bash
# Wrapper invoked over SSH by the orchestrator as `doublezero-agent`.
# The orchestrator's SSH command is hardcoded today as:
#   doublezero-agent -verbose [-controller HOST:PORT]
# It does not pass -pubkey or enable metrics. This wrapper supplies both so
# the agent can fetch its config and the observer can scrape its counters.
#
# The agent must run as root: it shells out to `ip netns exec default
# /usr/bin/Cli` to inspect staged configure-session diffs, and that requires
# CAP_SYS_ADMIN. We invoke ourselves through sudo so the agent runs with
# the privilege it needs even when SSH lands the orchestrator as the `stress`
# user. (The Dockerfile grants `stress` passwordless sudo.)
set -eu

if [ "$(id -u)" -ne 0 ]; then
    exec sudo -E -- "$0" "$@"
fi

PUBKEY_FILE="/etc/doublezero/agent/pubkey"
PUBKEY=""
if [ -r "$PUBKEY_FILE" ]; then
    PUBKEY="$(tr -d '[:space:]' < "$PUBKEY_FILE")"
fi

EXTRA_ARGS=()
if [ -n "$PUBKEY" ]; then
    EXTRA_ARGS+=(-pubkey "$PUBKEY")
fi
# Pick a port the controller-pushed MAIN-CONTROL-PLANE-ACL already permits.
# That ACL binds `system control-plane in` and the controller fully redefines
# it on every apply (`no ip access-list MAIN-CONTROL-PLANE-ACL` + recreate),
# so anything we add gets wiped on the agent's next tick. The default ACL
# does permit TCP 50000-50100, so park the metrics endpoint there.
EXTRA_ARGS+=(-metrics-enable -metrics-addr ":50100")

exec /mnt/flash/doublezero-agent "${EXTRA_ARGS[@]}" "$@"
