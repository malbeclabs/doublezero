#!/bin/bash
set -eou pipefail

: "${DIND_LOCALHOST:?DIND_LOCALHOST must be set}"

# Add ${DIND_LOCALHOST} to /etc/hosts if not already present.
# In Docker-in-Docker setups, ports exposed with `-p` are bound to the container's external interface,
# not its loopback (localhost). However, accessing those ports from within the same container via
# its own IP often bypasses Docker's NAT and fails. This workaround creates a hostname (e.g., dind.localhost)
# that resolves to the container's own external IP, enabling internal processes to reliably reach
# published ports as if they were external clients.
if ! getent hosts "${DIND_LOCALHOST}" >/dev/null 2>&1; then
  localhost_ip=$(ip route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}')
  echo "$localhost_ip ${DIND_LOCALHOST}" >> /etc/hosts
fi

# Execute the passed command
exec "$@"
