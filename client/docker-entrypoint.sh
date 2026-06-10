#!/usr/bin/env bash
#
# Entrypoint for the thin DoubleZero client image.
#
# Starts the doublezerod daemon (the deb's systemd unit does not run inside a
# container) and then hands off to the `doublezero` CLI:
#
#   * with arguments  -> exec `doublezero <args>` (e.g. connect / status)
#   * without args    -> print status and stay alive on the daemon so the
#                        container can be `docker exec`'d into for manual commands
#
set -euo pipefail

# mainnet-beta matches the compiled default of the doublezero package published to
# the public Cloudsmith repo; the daemon and CLI must use the same environment.
DZ_ENV="${DZ_ENV:-mainnet-beta}"
DZ_SOCK="${DZ_SOCK:-/run/doublezerod/doublezerod.sock}"

# doublezerod creates its Unix socket in this directory.
mkdir -p "$(dirname "$DZ_SOCK")"

# Persist the environment into the CLI config (~/.config/doublezero/cli/config.yml)
# so every `doublezero` invocation in this container -- the entrypoint, a
# `docker exec`, or an interactive shell -- uses the same env as the daemon and
# avoids the "client and daemon are using different environments" mismatch.
echo "[entrypoint] setting CLI config env=$DZ_ENV" >&2
doublezero config set --env "$DZ_ENV" >/dev/null

echo "[entrypoint] starting doublezerod (env=$DZ_ENV sock=$DZ_SOCK)" >&2
doublezerod -sock-file "$DZ_SOCK" -env "$DZ_ENV" &
DZD_PID=$!

# Forward termination so `docker stop` shuts the daemon down cleanly.
trap 'kill -TERM "$DZD_PID" 2>/dev/null || true' TERM INT

# Wait (up to ~10s) for the daemon socket before issuing any CLI command.
for _ in $(seq 1 50); do
    [ -S "$DZ_SOCK" ] && break
    if ! kill -0 "$DZD_PID" 2>/dev/null; then
        echo "[entrypoint] doublezerod exited before its socket was ready" >&2
        wait "$DZD_PID"
        exit 1
    fi
    sleep 0.2
done

if [ ! -S "$DZ_SOCK" ]; then
    echo "[entrypoint] timed out waiting for $DZ_SOCK" >&2
    kill -TERM "$DZD_PID" 2>/dev/null || true
    exit 1
fi
echo "[entrypoint] doublezerod ready" >&2

# (Optional startup hooks -- e.g. auto-running a `doublezero connect ...` on
# boot -- would go here.)

if [ "$#" -gt 0 ]; then
    exec doublezero --sock-file "$DZ_SOCK" --env "$DZ_ENV" "$@"
fi

# No command given: show status, then stay attached to the daemon.
doublezero --sock-file "$DZ_SOCK" --env "$DZ_ENV" status || true
wait "$DZD_PID"
