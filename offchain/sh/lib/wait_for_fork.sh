#!/bin/bash
#
# Shared readiness wait for a mainnet-beta fork started by doublezero-solana-fork.
#
# The caller starts the loader in the background, redirects its output to a log
# and writes its pid to a pid file; this waits for the fork to serve RPC. The
# loader clones accounts from mainnet-beta, so it is slower to serve than a bare
# validator boot, and a plain timeout cannot tell a crash apart from a slow
# start -- hence the pid check and the log dump on both failure paths.
#
# Usage:
#
#   source "$(dirname "${BASH_SOURCE[0]}")/lib/wait_for_fork.sh"
#   wait_for_fork [log_path] [pid_path] [wait_seconds]
#
# Exits 1 if the loader dies or the window expires. Installs an EXIT trap that
# removes the pid file: nothing reads it after the run, and a stale one left
# behind would make the next local run read a dead pid and report a crash.

wait_for_fork() {
    local log="${1:-solana-fork.log}"
    local wait_seconds="${3:-180}"
    # Global so the EXIT trap can read it after this function returns.
    _WAIT_FOR_FORK_PID_FILE="${2:-solana-fork.pid}"

    trap 'rm -f "$_WAIT_FOR_FORK_PID_FILE"' EXIT

    for _ in $(seq 1 $((wait_seconds / 2))); do
        if solana cluster-version -u l > /dev/null 2>&1; then
            echo "Solana fork is ready."
            return 0
        fi
        # A loader that died is reported now rather than after the whole window.
        if [ -s "$_WAIT_FOR_FORK_PID_FILE" ] &&
            ! kill -0 "$(cat "$_WAIT_FOR_FORK_PID_FILE")" 2>/dev/null; then
            echo "Solana fork loader exited before becoming ready." >&2
            if [ -f "$log" ]; then
                tail -50 "$log" >&2
            fi
            exit 1
        fi
        sleep 2
    done

    # One last probe: the loop's final `sleep 2` bought two more seconds.
    if solana cluster-version -u l > /dev/null 2>&1; then
        echo "Solana fork is ready."
        return 0
    fi

    echo "Solana fork did not start within ${wait_seconds} seconds." >&2
    if [ -f "$log" ]; then
        tail -50 "$log" >&2
    fi
    exit 1
}
