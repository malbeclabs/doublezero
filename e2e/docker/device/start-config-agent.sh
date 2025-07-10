#!/bin/bash
set -e

# Wait up to 10 seconds for ns-management
for i in {1..20}; do
  if ip netns list | grep -q ns-management; then
    exec ip netns exec ns-management /mnt/flash/doublezero-agent "$@"
  fi
  echo "Waiting for ns-management to appear..."
  sleep 0.5
done

echo "ERROR: ns-management did not appear in time" >&2
exit 1
