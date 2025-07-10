#!/bin/bash
set -e

# If DZ_MANAGEMENT_NAMESPACE is set, wait for it to appear.
if [ -n "$DZ_MANAGEMENT_NAMESPACE" ]; then
  for i in {1..20}; do
    if ip netns list | grep -q "$DZ_MANAGEMENT_NAMESPACE"; then
      exec ip netns exec "$DZ_MANAGEMENT_NAMESPACE" /mnt/flash/doublezero-agent "$@"
    fi
    echo "Waiting for $DZ_MANAGEMENT_NAMESPACE namespace to appear..."
    sleep 0.5
  done

  echo "ERROR: $DZ_MANAGEMENT_NAMESPACE namespace did not appear in time" >&2
  exit 1
fi

# Otherwise, just run the agent.
exec /mnt/flash/doublezero-agent "$@"
