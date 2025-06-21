#!/bin/bash
set -eou pipefail

# Add host.docker.internal to /etc/hosts if it's not already present
# This is normally for Linux only, since Docker Desktop for Mac and Windows already have it.
if ! getent hosts host.docker.internal >/dev/null 2>&1; then
  host_ip=$(ip route | awk '/default/ { print $3 }')
  echo "$host_ip host.docker.internal" >> /etc/hosts
fi

# Execute the passed command
exec "$@"
